package winlaunch

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	tpEarlySystemWriteRVA   = uintptr(0x7843F9)
	tpEarlySystemScanRVA    = uintptr(0x785C73)
	tpContextReadRVA        = uintptr(0x7647FF)
	tpContextReadPatchSize  = 7
	tpContextRecordOffset   = uintptr(0x800)
	tpContextStubOffset     = uintptr(0x600)
	tpContextStubReadOffset = uintptr(0x20)
	tpEarlyStubOffset       = uintptr(0x700)
)

type tpContextReadCompatibility struct {
	process             syscall.Handle
	removeHandler       uintptr
	handleAddress       uintptr
	recordAddress       uintptr
	workerThreadAddress uintptr
}

// installTPContextReadCompatibility replaces the old fixed 0x21020000 page
// assumption at one verified ClientBase boundary. The main thread registers a
// narrowly scoped VEH itself immediately before the old protected read, which
// avoids loader-lock deadlock. Read AVs are accepted only on that thread, first
// at the fixed boundary and then inside the same ClientBase image while the
// access and at least one general register still name the same invalid page.
// Matching registers are redirected to a private, initially zeroed read/write
// shadow page with their page offsets preserved. This narrowly scoped handler
// and the shadow page remain for the lifetime of the owned process.
func installTPContextReadCompatibility(
	process syscall.Handle,
	pid, mainThreadID uint32,
	earlySystemScan bool,
	timeout time.Duration,
) ([]ImportStub, *tpContextReadCompatibility, error) {
	base, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return nil, nil, err
	}
	clientBaseRange, ok := tpETWNamedModuleRange(snapshotRemoteModules(process), "ClientBase.dll")
	if !ok {
		return nil, nil, fmt.Errorf("ClientBase module range unavailable")
	}
	target := base + tpContextReadRVA
	expected := []byte{0x8B, 0x0A, 0xE8, 0x2B, 0x48, 0x02, 0x00, 0x72, 0x8B, 0x1C, 0x24}
	original, ok := readRemote(process, target, len(expected))
	if !ok {
		return nil, nil, fmt.Errorf("read ClientBase+0x%X at 0x%X", tpContextReadRVA, target)
	}
	if !bytes.Equal(original, expected) {
		return nil, nil, fmt.Errorf("ClientBase+0x%X signature mismatch: %X", tpContextReadRVA, original)
	}
	var earlyTarget uintptr
	var earlyExpected, earlyOriginal []byte
	if earlySystemScan {
		earlyTarget = base + tpEarlySystemWriteRVA
		earlyExpected = []byte{0x89, 0x02, 0x81, 0xEC, 0xFC, 0xFF, 0xFF, 0xFF}
		earlyOriginal, ok = readRemote(process, earlyTarget, len(earlyExpected))
		if !ok || !bytes.Equal(earlyOriginal, earlyExpected) {
			return nil, nil, fmt.Errorf("ClientBase+0x%X signature mismatch: %X", tpEarlySystemWriteRVA, earlyOriginal)
		}
	}
	addHandler, err := findRemoteExport(process, pid, "NTDLL.dll", "RtlAddVectoredExceptionHandler", timeout, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve RtlAddVectoredExceptionHandler: %w", err)
	}
	removeHandler, err := findRemoteExport(process, pid, "NTDLL.dll", "RtlRemoveVectoredExceptionHandler", timeout, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve RtlRemoveVectoredExceptionHandler: %w", err)
	}
	callDisplacement := int32(binary.LittleEndian.Uint32(original[3:7]))
	callTarget := uintptr(int64(target+tpContextReadPatchSize) + int64(callDisplacement))
	if callTarget < base || callTarget > 0xFFFFFFFF {
		return nil, nil, fmt.Errorf("ClientBase context-read call target is invalid: 0x%X", callTarget)
	}
	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, nil, fmt.Errorf("VirtualAllocEx TP context-read compatibility: 0x%X (%v)", page, allocErr)
	}
	zeroPage, _, zeroErr := procVirtualAllocEx.Call(
		uintptr(process), 0, 0x1000, memReserve|memCommit, pageReadWrite,
	)
	if zeroPage == 0 || zeroPage > 0xFFFFFFFF {
		return nil, nil, fmt.Errorf("VirtualAllocEx TP shadow-context page: 0x%X (%v)", zeroPage, zeroErr)
	}
	recordAddress := uint32(page + tpContextRecordOffset)
	handleAddress := recordAddress + 36
	stubAddress := page + tpContextStubOffset
	earlyStubAddress := page + tpEarlyStubOffset
	earlyScanEIP := uint32(0)
	if earlySystemScan {
		earlyScanEIP = uint32(base + tpEarlySystemScanRVA)
	}
	handler, err := buildTPContextReadVEH(
		mainThreadID, uint32(stubAddress+tpContextStubReadOffset), earlyScanEIP,
		clientBaseRange, recordAddress, uint32(zeroPage),
	)
	if err != nil {
		return nil, nil, err
	}
	if len(handler) >= int(tpContextStubOffset) {
		return nil, nil, fmt.Errorf("TP context-read handler is too large: %d", len(handler))
	}
	bootstrap := buildTPContextReadBootstrap(
		uint32(addHandler), uint32(page), handleAddress,
		uint32(target+tpContextReadPatchSize), uint32(callTarget),
	)
	var earlyBootstrap []byte
	if earlySystemScan {
		earlyBootstrap = buildTPEarlyScanBootstrap(
			uint32(addHandler), uint32(page), handleAddress, uint32(earlyTarget+uintptr(len(earlyExpected))),
		)
	}
	if err := writeRemote(process, page, handler); err != nil {
		return nil, nil, fmt.Errorf("write TP context-read handler: %w", err)
	}
	if err := writeRemote(process, stubAddress, bootstrap); err != nil {
		return nil, nil, fmt.Errorf("write TP context-read bootstrap: %w", err)
	}
	if earlySystemScan {
		if err := writeRemote(process, earlyStubAddress, earlyBootstrap); err != nil {
			return nil, nil, fmt.Errorf("write TP early-scan bootstrap: %w", err)
		}
	}
	patch := []byte{0x68, 0, 0, 0, 0, 0xC3, 0x90} // push stub; ret; nop
	binary.LittleEndian.PutUint32(patch[1:5], uint32(stubAddress))
	if !ensureRemoteBytes(process, target, patch, pageExecuteReadWrite) {
		return nil, nil, fmt.Errorf("patch ClientBase+0x%X at 0x%X", tpContextReadRVA, target)
	}
	var earlyPatch []byte
	if earlySystemScan {
		earlyPatch = []byte{0x68, 0, 0, 0, 0, 0xC3, 0x90, 0x90}
		binary.LittleEndian.PutUint32(earlyPatch[1:5], uint32(earlyStubAddress))
		if !ensureRemoteBytes(process, earlyTarget, earlyPatch, pageExecuteReadWrite) {
			return nil, nil, fmt.Errorf("patch ClientBase+0x%X at 0x%X", tpEarlySystemWriteRVA, earlyTarget)
		}
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(handler)))
	procFlushInstruction.Call(uintptr(process), stubAddress, uintptr(len(bootstrap)))
	if earlySystemScan {
		procFlushInstruction.Call(uintptr(process), earlyStubAddress, uintptr(len(earlyBootstrap)))
	}
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(patch)))
	if earlySystemScan {
		procFlushInstruction.Call(uintptr(process), earlyTarget, uintptr(len(earlyPatch)))
	}
	compatibility := &tpContextReadCompatibility{
		process: process, removeHandler: removeHandler, handleAddress: uintptr(handleAddress),
		recordAddress: uintptr(recordAddress), workerThreadAddress: uintptr(recordAddress + 40),
	}
	stubs := []ImportStub{{
		Module:              "ClientBase.dll",
		Library:             "ClientBase.dll",
		Symbol:              "startup shadow-context VEH ClientBase+0x7647FF",
		StubAddress:         fmt.Sprintf("0x%X", stubAddress),
		TargetAddress:       fmt.Sprintf("0x%X", target),
		TargetRVA:           "0x7647FF",
		TargetOriginal:      fmt.Sprintf("%X", original[:tpContextReadPatchSize]),
		TargetContext:       fmt.Sprintf("%X", original),
		HelperAddress:       fmt.Sprintf("0x%X", addHandler),
		Behavior:            fmt.Sprintf("same-thread bootstrap installs a main-thread plus captured-TP-worker/ClientBase/one-invalid-page scoped read/write AV handler; matching registers are redirected to initially zeroed shadow page 0x%X with page offsets preserved; the handler is removed only after the gated ETW repair completes", zeroPage),
		targetValue:         target,
		targetBytes:         patch,
		originalBytes:       append([]byte(nil), original[:tpContextReadPatchSize]...),
		counterValue:        uintptr(recordAddress),
		bypassedValue:       uintptr(recordAddress + 4),
		strictTargetRestore: true,
	}}
	if earlySystemScan {
		stubs = append(stubs, ImportStub{
			Module:              "ClientBase.dll",
			Library:             "ClientBase.dll",
			Symbol:              "early system-header/scan compatibility ClientBase+0x7843F9",
			StubAddress:         fmt.Sprintf("0x%X", earlyStubAddress),
			TargetAddress:       fmt.Sprintf("0x%X", earlyTarget),
			TargetRVA:           "0x7843F9",
			TargetOriginal:      fmt.Sprintf("%X", earlyOriginal),
			HelperAddress:       fmt.Sprintf("0x%X", addHandler),
			Behavior:            "same-thread bootstrap installs the bounded startup VEH, skips the obsolete system PE-header write, and handles only unreadable probes at ClientBase+0x785C73",
			targetValue:         earlyTarget,
			targetBytes:         earlyPatch,
			originalBytes:       append([]byte(nil), earlyOriginal...),
			counterValue:        uintptr(recordAddress),
			bypassedValue:       uintptr(recordAddress + 4),
			strictTargetRestore: true,
		})
	}
	return stubs, compatibility, nil
}

func (compatibility *tpContextReadCompatibility) allowWorkerThread(threadID uint32) error {
	value := make([]byte, 4)
	binary.LittleEndian.PutUint32(value, threadID)
	if err := writeRemote(compatibility.process, compatibility.workerThreadAddress, value); err != nil {
		return fmt.Errorf("write allowed TP worker TID %d: %w", threadID, err)
	}
	return nil
}

func (compatibility *tpContextReadCompatibility) remove(timeout time.Duration) error {
	value, ok := readRemote(compatibility.process, compatibility.handleAddress, 4)
	if !ok {
		return fmt.Errorf("read startup VEH handle at 0x%X", compatibility.handleAddress)
	}
	handle := binary.LittleEndian.Uint32(value)
	if handle == 0 || handle == 0xFFFFFFFF {
		return fmt.Errorf("startup VEH handle is 0x%08X", handle)
	}
	removed, err := remoteCallOne(compatibility.process, compatibility.removeHandler, uintptr(handle), timeout)
	if err != nil {
		return err
	}
	if removed == 0 {
		return fmt.Errorf("RtlRemoveVectoredExceptionHandler returned zero")
	}
	disabled := make([]byte, 4)
	binary.LittleEndian.PutUint32(disabled, 0xFFFFFFFF)
	if err := writeRemote(compatibility.process, compatibility.handleAddress, disabled); err != nil {
		return fmt.Errorf("disable startup VEH bootstrap: %w", err)
	}
	return nil
}

func buildTPContextReadBootstrap(addHandler, handler, handleAddress, continuation, callTarget uint32) []byte {
	stub := []byte{
		0x9C, 0x60, // pushfd; pushad
		0x83, 0x3D, 0, 0, 0, 0, 0x00, // cmp dword ptr [handle],0
		0x75, 0x13, // jne restore
		0x68, 0, 0, 0, 0, // push handler
		0x6A, 0x01, // push 1
		0xB8, 0, 0, 0, 0, // mov eax,RtlAddVectoredExceptionHandler
		0xFF, 0xD0,
		0xA3, 0, 0, 0, 0, // mov [handle],eax
		0x61, 0x9D, // restore: popad; popfd
		0x8B, 0x0A, // original mov ecx,[edx]
		0x68, 0, 0, 0, 0,
		0x68, 0, 0, 0, 0,
		0xC3,
	}
	binary.LittleEndian.PutUint32(stub[4:8], handleAddress)
	binary.LittleEndian.PutUint32(stub[12:16], handler)
	binary.LittleEndian.PutUint32(stub[19:23], addHandler)
	binary.LittleEndian.PutUint32(stub[26:30], handleAddress)
	binary.LittleEndian.PutUint32(stub[len(stub)-10:len(stub)-6], continuation)
	binary.LittleEndian.PutUint32(stub[len(stub)-5:len(stub)-1], callTarget)
	return stub
}

func buildTPEarlyScanBootstrap(addHandler, handler, handleAddress, continuation uint32) []byte {
	stub := []byte{
		0x9C, 0x60, // pushfd; pushad
		0x83, 0x3D, 0, 0, 0, 0, 0x00, // cmp dword ptr [handle],0
		0x75, 0x13, // jne restore
		0x68, 0, 0, 0, 0, // push handler
		0x6A, 0x01, // push 1
		0xB8, 0, 0, 0, 0, // mov eax,RtlAddVectoredExceptionHandler
		0xFF, 0xD0,
		0xA3, 0, 0, 0, 0, // mov [handle],eax
		0x61, 0x9D, // restore: popad; popfd
		// The replaced bytes were MOV [EDX],EAX followed by ADD ESP,4. The
		// write target is a system PE header on current Windows and is TP-only;
		// preserve the verified stack transition and continue after both.
		0x81, 0xEC, 0xFC, 0xFF, 0xFF, 0xFF,
		0x68, 0, 0, 0, 0,
		0xC3,
	}
	binary.LittleEndian.PutUint32(stub[4:8], handleAddress)
	binary.LittleEndian.PutUint32(stub[12:16], handler)
	binary.LittleEndian.PutUint32(stub[19:23], addHandler)
	binary.LittleEndian.PutUint32(stub[26:30], handleAddress)
	binary.LittleEndian.PutUint32(stub[len(stub)-5:len(stub)-1], continuation)
	return stub
}

func buildTPContextReadVEH(
	threadID, firstEIP, earlyScanEIP uint32,
	clientBaseRange tpETWRepairModule,
	recordAddress, zeroPage uint32,
) ([]byte, error) {
	program := newX86Program()
	program.emit(
		0x55, 0x8B, 0xEC, 0x53, 0x56, 0x57, // prologue; save ebx,esi,edi
		0x8B, 0x45, 0x08, 0x8B, 0x10, 0x8B, 0x48, 0x04, // exception record/context
		0xFF, 0x05,
	)
	program.imm32(recordAddress) // total exceptions
	program.emit(0x8B, 0x02, 0xA3)
	program.imm32(recordAddress + 12)
	program.emit(0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0xA3)
	program.imm32(recordAddress + 16)
	program.emit(0x8B, 0x81)
	program.imm32(wow64ContextEIP)
	program.emit(0xA3)
	program.imm32(recordAddress + 20)
	program.emit(0x8B, 0x42, 0x18, 0xA3)
	program.imm32(recordAddress + 24)
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 28)
	program.imm32(0)
	program.emit(0x81, 0x3A, 0x05, 0x00, 0x00, 0xC0)
	program.jcc(0x85, "forward")
	program.emit(0x83, 0x7A, 0x10, 0x02)
	program.jcc(0x85, "forward")
	program.emit(0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0x3D)
	program.imm32(threadID)
	program.jcc(0x84, "thread_ok")
	program.emit(0x3B, 0x05)
	program.imm32(recordAddress + 40)
	program.jcc(0x85, "forward")
	program.label("thread_ok")
	program.emit(0x81, 0x3D)
	program.imm32(recordAddress + 4)
	program.imm32(4095) // diagnostic bound; raised after the live loop boundary is captured
	program.jcc(0x83, "forward")

	// New Windows builds expose no implicit exception swallowing for this old
	// protected memory probe. Handle only the exact 16-bit read instruction,
	// only on the startup thread, and only when ExceptionInformation[1] equals
	// ECX. Readable probes still execute normally and retain their real value.
	program.emit(0x8B, 0x81)
	program.imm32(wow64ContextEIP)
	program.emit(0x3D)
	program.imm32(earlyScanEIP)
	program.jcc(0x85, "normal_context")
	program.emit(0x83, 0x7A, 0x14, 0x00)
	program.jcc(0x85, "forward")
	program.emit(0x8B, 0x42, 0x18, 0x3B, 0x81)
	program.imm32(wow64ContextECX)
	program.jcc(0x85, "forward")
	program.emit(0x8B, 0x42, 0x18)             // fault address
	program.emit(0x25, 0x00, 0xF0, 0xFF, 0xFF) // page base
	program.emit(0x05, 0xFE, 0x0F, 0x00, 0x00) // last word in the unreadable page
	program.emit(0x89, 0x81)
	program.imm32(wow64ContextECX)
	program.emit(0x81, 0xA1)
	program.imm32(wow64ContextEDX)
	program.imm32(0xFFFF0000)
	program.emit(0x83, 0x81)
	program.imm32(wow64ContextEIP)
	program.emit(0x03)
	program.emit(0xFF, 0x05)
	program.imm32(recordAddress + 4)
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 28)
	program.imm32(6)
	program.emit(0x5F, 0x5E, 0x5B, 0x83, 0xC8, 0xFF, 0x5D, 0xC2, 0x04, 0x00)

	program.label("normal_context")
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 28)
	program.imm32(1)
	program.emit(0x8B, 0x81)
	program.imm32(wow64ContextEIP)
	program.emit(0x8B, 0x1D)
	program.imm32(recordAddress + 8) // original invalid page
	program.emit(0x85, 0xDB)
	program.jcc(0x85, "subsequent")
	program.emit(0x3D)
	program.imm32(firstEIP)
	program.jcc(0x85, "forward")
	program.emit(0x83, 0x7A, 0x14, 0x00) // first boundary must be a read
	program.jcc(0x85, "forward")
	program.emit(0x8B, 0x7A, 0x18, 0x3B, 0xB9)
	program.imm32(wow64ContextEDX)
	program.jcc(0x85, "forward")
	program.emit(0x8B, 0xDF, 0x81, 0xE3, 0x00, 0xF0, 0xFF, 0xFF)
	program.emit(0x89, 0x1D)
	program.imm32(recordAddress + 8)
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 28)
	program.imm32(2)
	program.jmp("replace")

	program.label("subsequent")
	program.emit(0x3D)
	program.imm32(firstEIP)
	program.jcc(0x84, "subsequent_eip_ok")
	program.emit(0x3D)
	program.imm32(clientBaseRange.base)
	program.jcc(0x82, "forward")
	program.emit(0x3D)
	program.imm32(clientBaseRange.end)
	program.jcc(0x83, "forward")
	program.label("subsequent_eip_ok")
	program.emit(0x83, 0x7A, 0x14, 0x01) // read(0) or write(1), never execute(8)
	program.jcc(0x87, "forward")         // ja
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 28)
	program.imm32(2)
	program.emit(0x8B, 0x7A, 0x18, 0x8B, 0xF7, 0x81, 0xE6, 0x00, 0xF0, 0xFF, 0xFF)
	program.emit(0x3B, 0xF3)
	program.jcc(0x85, "forward")
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 28)
	program.imm32(3)

	program.label("replace")
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 28)
	program.imm32(4)
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 32)
	program.imm32(0)
	registers := []struct {
		name   string
		offset uint32
		bit    uint32
	}{
		{name: "edi", offset: wow64ContextEDI, bit: 1 << 0},
		{name: "esi", offset: wow64ContextESI, bit: 1 << 1},
		{name: "ebx", offset: wow64ContextEBX, bit: 1 << 2},
		{name: "edx", offset: wow64ContextEDX, bit: 1 << 3},
		{name: "ecx", offset: wow64ContextECX, bit: 1 << 4},
		{name: "eax", offset: wow64ContextEAX, bit: 1 << 5},
	}
	for _, register := range registers {
		skip := "skip_" + register.name
		program.emit(0x8B, 0x81)
		program.imm32(register.offset)
		program.emit(0x8B, 0xF0, 0x81, 0xE6, 0x00, 0xF0, 0xFF, 0xFF)
		program.emit(0x3B, 0xF3)
		program.jcc(0x85, skip)
		program.emit(0x25, 0xFF, 0x0F, 0x00, 0x00, 0x05)
		program.imm32(zeroPage)
		program.emit(0x89, 0x81)
		program.imm32(register.offset)
		program.emit(0x81, 0x0D)
		program.imm32(recordAddress + 32)
		program.imm32(register.bit)
		program.label(skip)
	}
	program.emit(0x83, 0x3D)
	program.imm32(recordAddress + 32)
	program.emit(0x00)
	program.jcc(0x84, "forward")
	program.emit(0xFF, 0x05)
	program.imm32(recordAddress + 4)
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 28)
	program.imm32(5)
	program.emit(0x5F, 0x5E, 0x5B, 0x83, 0xC8, 0xFF, 0x5D, 0xC2, 0x04, 0x00)

	program.label("forward")
	program.emit(0x5F, 0x5E, 0x5B, 0x33, 0xC0, 0x5D, 0xC2, 0x04, 0x00)
	return program.resolve()
}

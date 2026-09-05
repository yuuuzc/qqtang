package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

// UnpackedEntryGateReport describes the point where the protected executable
// has finished restoring Client.exe but has not executed the original CRT
// entry point yet. The main thread is redirected to a private Sleep(INFINITE)
// stub so a clean pre-initialisation image can be captured without attaching a
// debugger or changing any Client.exe instruction.
type UnpackedEntryGateReport struct {
	Address  string `json:"address"`
	ThreadID uint32 `json:"thread_id"`
	EIP      string `json:"eip"`
	ESP      string `json:"esp"`
	EBP      string `json:"ebp"`
	CodeHex  string `json:"code_hex,omitempty"`
}

func installUnpackedEntryGate(
	process syscall.Handle,
	mainThread syscall.Handle,
	pid uint32,
	target uintptr,
	timeout time.Duration,
) (uintptr, error) {
	addHandler, err := findRemoteExport(process, pid, "NTDLL.dll", "RtlAddVectoredExceptionHandler", timeout, 0)
	if err != nil {
		return 0, err
	}
	sleepAddress, err := findRemoteExport(process, pid, "KERNEL32.dll", "Sleep", timeout, 0)
	if err != nil {
		return 0, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return 0, fmt.Errorf("VirtualAllocEx unpacked-entry gate: 0x%X (%v)", page, allocErr)
	}
	recordAddress := page + 0x200
	waitAddress := page + 0x300
	installerAddress := page + 0x380
	handler := buildUnpackedEntryGateVEH(target, recordAddress, waitAddress)
	waitStub := []byte{0x68, 0xFF, 0xFF, 0xFF, 0xFF, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0xEB, 0xFE}
	binary.LittleEndian.PutUint32(waitStub[6:10], uint32(sleepAddress))
	installer := []byte{0x68, 0, 0, 0, 0, 0x6A, 0x01, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0xC2, 0x04, 0x00}
	binary.LittleEndian.PutUint32(installer[1:5], uint32(page))
	binary.LittleEndian.PutUint32(installer[8:12], uint32(addHandler))
	if err := writeRemote(process, page, handler); err != nil {
		return 0, fmt.Errorf("write unpacked-entry gate VEH: %w", err)
	}
	if err := writeRemote(process, waitAddress, waitStub); err != nil {
		return 0, fmt.Errorf("write unpacked-entry wait stub: %w", err)
	}
	if err := writeRemote(process, installerAddress, installer); err != nil {
		return 0, fmt.Errorf("write unpacked-entry installer: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(handler)))
	procFlushInstruction.Call(uintptr(process), waitAddress, uintptr(len(waitStub)))
	procFlushInstruction.Call(uintptr(process), installerAddress, uintptr(len(installer)))
	registered, err := remoteCallOne(process, installerAddress, 0, timeout)
	if err != nil {
		return 0, err
	}
	if registered == 0 {
		return 0, fmt.Errorf("RtlAddVectoredExceptionHandler for unpacked-entry gate returned NULL")
	}
	if err := armUnpackedEntryBreakpoint(mainThread, target); err != nil {
		return 0, err
	}
	return recordAddress, nil
}

func buildUnpackedEntryGateVEH(target, record, waitAddress uintptr) []byte {
	// EAX=EXCEPTION_POINTERS, EDX=EXCEPTION_RECORD, ECX=WOW64_CONTEXT.
	stub := []byte{0x55, 0x8B, 0xEC, 0x8B, 0x45, 0x08, 0x8B, 0x10, 0x8B, 0x48, 0x04}
	stub = append(stub, 0x81, 0x3A, 0x04, 0x00, 0x00, 0x80, 0x74, 0x0C)
	stub = append(stub, 0x81, 0x3A, 0x1E, 0x00, 0x00, 0x40, 0x0F, 0x85, 0, 0, 0, 0)
	forwardCodeJump := len(stub) - 4
	stub = append(stub, 0x81, 0x7A, 0x0C)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(target))
	stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)
	forwardAddressJump := len(stub) - 4
	stub = append(stub, 0xF7, 0x41, 0x14, 0x01, 0, 0, 0, 0x0F, 0x84, 0, 0, 0, 0)
	forwardDR6Jump := len(stub) - 4
	stub = append(stub, 0x81, 0xB9, 0xB8, 0, 0, 0)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(target))
	stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)
	forwardEIPJump := len(stub) - 4
	stub = append(stub, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	// Record thread ID, EIP, ESP and EBP before redirecting execution.
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	for _, field := range []struct {
		offset uint32
		dest   uintptr
	}{{0xB8, record + 8}, {0xC4, record + 12}, {0xB4, record + 16}} {
		stub = append(stub, 0x8B, 0x81)
		stub = binary.LittleEndian.AppendUint32(stub, field.offset)
		stub = append(stub, 0xA3)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(field.dest))
	}
	stub = append(stub, 0xC7, 0x41, 0x04, 0, 0, 0, 0)
	stub = append(stub, 0xC7, 0x41, 0x14, 0, 0, 0, 0)
	stub = append(stub, 0x81, 0x61, 0x18, 0xFC, 0xFF, 0xF0, 0xFF)
	stub = append(stub, 0xC7, 0x81, 0xB8, 0, 0, 0)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(waitAddress))
	stub = append(stub, 0x83, 0xC8, 0xFF, 0x5D, 0xC2, 0x04, 0x00)
	forwardOffset := len(stub)
	patchNearJump(stub, forwardCodeJump, forwardOffset)
	patchNearJump(stub, forwardAddressJump, forwardOffset)
	patchNearJump(stub, forwardDR6Jump, forwardOffset)
	patchNearJump(stub, forwardEIPJump, forwardOffset)
	stub = append(stub, 0x33, 0xC0, 0x5D, 0xC2, 0x04, 0x00)
	return stub
}

func armUnpackedEntryBreakpoint(mainThread syscall.Handle, target uintptr) error {
	context := make([]byte, 716)
	binary.LittleEndian.PutUint32(context[0:4], wow64ContextDebugRegisters)
	read, _, contextErr := procWow64GetThreadContext.Call(uintptr(mainThread), uintptr(unsafe.Pointer(&context[0])))
	if read == 0 {
		return fmt.Errorf("Wow64GetThreadContext for unpacked-entry gate: %v", contextErr)
	}
	dr7 := binary.LittleEndian.Uint32(context[24:28])
	if dr7&0x3 != 0 {
		return fmt.Errorf("cannot arm unpacked-entry gate: DR0 is already active (DR7=0x%08X)", dr7)
	}
	binary.LittleEndian.PutUint32(context[4:8], uint32(target))
	binary.LittleEndian.PutUint32(context[20:24], 0)
	dr7 &^= uint32(0xF0003)
	dr7 |= 1
	binary.LittleEndian.PutUint32(context[24:28], dr7)
	written, _, contextErr := procWow64SetThreadContext.Call(uintptr(mainThread), uintptr(unsafe.Pointer(&context[0])))
	if written == 0 {
		return fmt.Errorf("Wow64SetThreadContext for unpacked-entry gate: %v", contextErr)
	}
	return nil
}

func readUnpackedEntryGateReport(process syscall.Handle, record, target uintptr) *UnpackedEntryGateReport {
	data, ok := readRemote(process, record, 20)
	if !ok || binary.LittleEndian.Uint32(data[0:4]) == 0 {
		return nil
	}
	report := &UnpackedEntryGateReport{
		Address:  fmt.Sprintf("0x%08X", target),
		ThreadID: binary.LittleEndian.Uint32(data[4:8]),
		EIP:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(data[8:12])),
		ESP:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(data[12:16])),
		EBP:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(data[16:20])),
	}
	if code, codeOK := readRemote(process, target, 64); codeOK {
		report.CodeHex = fmt.Sprintf("%X", code)
	}
	return report
}

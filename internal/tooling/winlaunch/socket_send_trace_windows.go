package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"
)

const (
	socketSendTraceCapacity    = 512
	socketSendTracePayloadSize = 2048
	socketSendTraceHeaderSize  = 40
	socketSendTraceRecordSize  = socketSendTraceHeaderSize + socketSendTracePayloadSize
	// CMessageLayer::AddLocalPlayerData tests NotifyGameStart.ReportFlag bit 0
	// immediately before appending the same local event to the 0x10EB upload
	// queue. This RVA points at the two-byte JZ following that test. The
	// diagnostic override below is deliberately temporary and is always
	// restored by SocketSendTracer.Close.
	clientGameplayUploadGateRVA = 0x001A30D5
)

// SocketSendTraceCall is one pre-call snapshot of send/sendto. Payload is
// copied inside the target process before the original Winsock function runs,
// so caller-owned buffers cannot be reused before the tracer observes them.
type SocketSendTraceCall struct {
	Index          uint32 `json:"index"`
	Module         string `json:"module"`
	API            string `json:"api"`
	ThreadID       uint32 `json:"thread_id"`
	Caller         string `json:"caller"`
	Socket         uint32 `json:"socket"`
	RequestedBytes uint32 `json:"requested_bytes"`
	Flags          uint32 `json:"flags"`
	Family         uint16 `json:"family,omitempty"`
	Port           uint16 `json:"port,omitempty"`
	IPv4           string `json:"ipv4,omitempty"`
	CapturedBytes  uint32 `json:"captured_bytes"`
	PayloadHex     string `json:"payload_hex"`
}

type SocketSendTraceCapture struct {
	PID        uint32                `json:"pid"`
	ElapsedMS  int64                 `json:"elapsed_ms"`
	TotalCalls uint32                `json:"total_calls"`
	Truncated  bool                  `json:"truncated"`
	Hooks      []string              `json:"hooks"`
	Patches    []string              `json:"patches,omitempty"`
	Calls      []SocketSendTraceCall `json:"calls"`
}

type SocketSendExportInfo struct {
	Library string `json:"library"`
	API     string `json:"api"`
	Address string `json:"address"`
	Bytes   string `json:"bytes"`
}

// InspectSocketSendExports resolves the exact 32-bit Winsock targets used in
// the client process. WSOCK32 exports may forward to WS2_32, so inspection is
// performed in the target process rather than by assuming local RVAs.
func InspectSocketSendExports(pid uint32, timeout time.Duration) ([]SocketSendExportInfo, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	var result []SocketSendExportInfo
	seen := make(map[uintptr]struct{})
	for _, library := range []string{"WS2_32.dll", "WSOCK32.dll"} {
		for _, api := range []string{"send", "sendto"} {
			address, resolveErr := findRemoteExport(process, pid, library, api, timeout, 0)
			if resolveErr != nil {
				continue
			}
			if _, duplicate := seen[address]; duplicate {
				continue
			}
			seen[address] = struct{}{}
			data, ok := readRemote(process, address, 32)
			if !ok {
				return nil, fmt.Errorf("read %s!%s at 0x%08X", library, api, address)
			}
			result = append(result, SocketSendExportInfo{
				Library: library, API: api, Address: fmt.Sprintf("0x%08X", address), Bytes: hex.EncodeToString(data),
			})
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no Winsock send exports resolved in pid %d", pid)
	}
	return result, nil
}

type socketSendTraceHook struct {
	id          uint32
	module      string
	library     string
	api         string
	iatAddress  uintptr
	originalIAT []byte
	target      uintptr
	original    []byte
	page        uintptr
}

type socketSendTracePatch struct {
	name     string
	address  uintptr
	original []byte
}

type SocketSendTracer struct {
	pid            uint32
	process        syscall.Handle
	started        time.Time
	storage        uintptr
	counterAddress uintptr
	recordsAddress uintptr
	hooks          []socketSendTraceHook
	patches        []socketSendTracePatch
}

// InstallSocketSendTracer transparently wraps the game components that import
// Winsock send/sendto. It does not patch recv, alter destinations, or retain
// any hook after Close.
func InstallSocketSendTracer(pid uint32, timeout time.Duration) (*SocketSendTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &SocketSendTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	storageSize := uintptr(4 + socketSendTraceCapacity*socketSendTraceRecordSize)
	storage, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, storageSize, memReserve|memCommit, pageReadWrite)
	if storage == 0 || storage > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx socket trace storage: 0x%X (%v)", storage, allocErr)
	}
	tracer.storage = storage
	tracer.counterAddress = storage
	tracer.recordsAddress = storage + 4

	var installErrors []string
	// NetCenter resolves its actual socket calls behind its old CTcpEngine and
	// CUdpSocket wrappers, so no stable game-module IAT owns every send. The
	// process-private WS2_32 entry points are the narrowest complete boundary.
	for _, api := range []string{"send", "sendto"} {
		hook, hookErr := installSocketSendExportTraceHook(tracer, api, timeout)
		if hookErr != nil {
			installErrors = append(installErrors, fmt.Sprintf("WS2_32.dll!%s: %v", api, hookErr))
			continue
		}
		tracer.hooks = append(tracer.hooks, hook)
	}
	if len(tracer.hooks) == 0 {
		return nil, fmt.Errorf("no game Winsock send imports could be traced: %v", installErrors)
	}
	failed = false
	return tracer, nil
}

// EnableGameplayUploadGate temporarily bypasses the ReportFlag bit test that
// suppresses 0x10EB uploads in the client's locally-created room scene. It is
// used only to prove the original outer transport before the server-owned room
// transport is wired; Close restores the exact original instruction bytes.
func (tracer *SocketSendTracer) EnableGameplayUploadGate(timeout time.Duration) error {
	if tracer == nil || tracer.process == 0 {
		return fmt.Errorf("socket send tracer is not installed")
	}
	moduleBase, err := waitForModule(tracer.process, tracer.pid, "Client.exe", timeout)
	if err != nil {
		return err
	}
	address := moduleBase + clientGameplayUploadGateRVA
	original, ok := readRemote(tracer.process, address, 2)
	if !ok {
		return fmt.Errorf("read gameplay upload gate at 0x%08X", address)
	}
	if original[0] != 0x74 || original[1] != 0x12 {
		return fmt.Errorf("unexpected gameplay upload gate %x at 0x%08X", original, address)
	}
	if !ensureRemoteBytes(tracer.process, address, []byte{0x90, 0x90}, pageExecuteReadWrite) {
		return fmt.Errorf("patch gameplay upload gate at 0x%08X", address)
	}
	procFlushInstruction.Call(uintptr(tracer.process), address, 2)
	tracer.patches = append(tracer.patches, socketSendTracePatch{
		name: "Client.exe/CMessageLayer.ReportFlag-upload-gate", address: address, original: original,
	})
	return nil
}

func installSocketSendExportTraceHook(tracer *SocketSendTracer, api string, timeout time.Duration) (socketSendTraceHook, error) {
	targetAddress, err := findRemoteExport(tracer.process, tracer.pid, "WS2_32.dll", api, timeout, 0)
	if err != nil {
		return socketSendTraceHook{}, err
	}
	original, ok := readRemote(tracer.process, targetAddress, 5)
	if !ok {
		return socketSendTraceHook{}, fmt.Errorf("read target 0x%08X", targetAddress)
	}
	// Both x86 functions in the shipped Win11 environment use the standard
	// five-byte hot-patch prologue. Refuse unknown instruction boundaries
	// rather than constructing an unsafe trampoline by guesswork.
	want := []byte{0x8B, 0xFF, 0x55, 0x8B, 0xEC}
	for index := range want {
		if original[index] != want[index] {
			return socketSendTraceHook{}, fmt.Errorf("unsupported prologue %x at 0x%08X", original, targetAddress)
		}
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(tracer.process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return socketSendTraceHook{}, fmt.Errorf("VirtualAllocEx export stub: 0x%X (%v)", page, allocErr)
	}
	hookID := uint32(len(tracer.hooks))
	stub := buildSocketSendTraceStub(
		page, targetAddress+uintptr(len(original)), tracer.counterAddress, tracer.recordsAddress,
		hookID, api == "sendto", original,
	)
	if err := writeRemote(tracer.process, page, stub); err != nil {
		procVirtualFreeEx.Call(uintptr(tracer.process), page, 0, memRelease)
		return socketSendTraceHook{}, err
	}
	patch := []byte{0xE9, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(targetAddress+5)))
	if !ensureRemoteBytes(tracer.process, targetAddress, patch, pageExecuteReadWrite) {
		procVirtualFreeEx.Call(uintptr(tracer.process), page, 0, memRelease)
		return socketSendTraceHook{}, fmt.Errorf("patch target 0x%08X", targetAddress)
	}
	procFlushInstruction.Call(uintptr(tracer.process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(tracer.process), targetAddress, uintptr(len(patch)))
	return socketSendTraceHook{
		id: hookID, module: "WS2_32.dll", library: "WS2_32.dll", api: api,
		target: targetAddress, original: original, page: page,
	}, nil
}

func buildSocketSendTraceStub(stubAddress, targetAddress, counterAddress, recordsAddress uintptr, hookID uint32, sendTo bool, trampolinePrefix []byte) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xB8, 0x01, 0x00, 0x00, 0x00)
	stub = append(stub, 0xF0, 0x0F, 0xC1, 0x05) // lock xadd [counter],eax
	stub = binary.LittleEndian.AppendUint32(stub, uint32(counterAddress))
	stub = append(stub, 0x3D)
	stub = binary.LittleEndian.AppendUint32(stub, socketSendTraceCapacity)
	stub = append(stub, 0x0F, 0x83, 0, 0, 0, 0) // jae done
	doneJump := len(stub) - 4
	stub = append(stub, 0x69, 0xC0)
	stub = binary.LittleEndian.AppendUint32(stub, socketSendTraceRecordSize)
	stub = append(stub, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordsAddress))

	storeImmediate := func(offset byte, value uint32) {
		stub = append(stub, 0xC7, 0x40, offset)
		stub = binary.LittleEndian.AppendUint32(stub, value)
	}
	storeStack := func(recordOffset, stackOffset byte) {
		stub = append(stub, 0x8B, 0x54, 0x24, stackOffset, 0x89, 0x50, recordOffset)
	}
	storeImmediate(0, hookID)
	stub = append(stub, 0x64, 0x8B, 0x15, 0x24, 0, 0, 0, 0x89, 0x50, 0x04) // thread ID
	storeStack(8, 0x24)                                                    // caller
	storeStack(12, 0x28)                                                   // socket
	storeStack(16, 0x30)                                                   // length
	storeStack(20, 0x34)                                                   // flags
	if sendTo {
		stub = append(stub, 0x8B, 0x4C, 0x24, 0x38, 0x85, 0xC9, 0x0F, 0x84, 0, 0, 0, 0)
		noAddressJump := len(stub) - 4
		stub = append(stub, 0x0F, 0xB7, 0x11, 0x89, 0x50, 0x18)       // family
		stub = append(stub, 0x0F, 0xB7, 0x51, 0x02, 0x89, 0x50, 0x1C) // network-order port
		stub = append(stub, 0x8B, 0x51, 0x04, 0x89, 0x50, 0x20)       // IPv4 bytes
		patchRelative32(stub, noAddressJump, len(stub))
	}

	stub = append(stub, 0x8B, 0x4C, 0x24, 0x30, 0x85, 0xC9, 0x0F, 0x8E, 0, 0, 0, 0)
	noCopyJump := len(stub) - 4
	stub = append(stub, 0x81, 0xF9)
	stub = binary.LittleEndian.AppendUint32(stub, socketSendTracePayloadSize)
	stub = append(stub, 0x0F, 0x86, 0, 0, 0, 0) // jbe lengthOK
	lengthOKJump := len(stub) - 4
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, socketSendTracePayloadSize)
	patchRelative32(stub, lengthOKJump, len(stub))
	stub = append(stub, 0x89, 0x48, 0x24)                      // captured length
	stub = append(stub, 0x8B, 0x74, 0x24, 0x2C)                // source buffer
	stub = append(stub, 0x8D, 0x78, socketSendTraceHeaderSize) // record payload
	stub = append(stub, 0xF3, 0xA4)                            // rep movsb
	patchRelative32(stub, noCopyJump, len(stub))
	patchRelative32(stub, doneJump, len(stub))
	stub = append(stub, 0x61, 0x9D) // popad; popfd
	stub = append(stub, trampolinePrefix...)
	stub = append(stub, 0xE9) // jmp original/continuation
	jumpImmediate := len(stub)
	stub = binary.LittleEndian.AppendUint32(stub, 0)
	jumpNext := stubAddress + uintptr(len(stub))
	binary.LittleEndian.PutUint32(stub[jumpImmediate:], uint32(targetAddress-jumpNext))
	return stub
}

func patchRelative32(code []byte, immediateOffset, targetOffset int) {
	binary.LittleEndian.PutUint32(code[immediateOffset:immediateOffset+4], uint32(int32(targetOffset-(immediateOffset+4))))
}

func (tracer *SocketSendTracer) Capture(duration time.Duration) SocketSendTraceCapture {
	if duration <= 0 {
		duration = 30 * time.Second
	}
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	return tracer.capture()
}

func (tracer *SocketSendTracer) capture() SocketSendTraceCapture {
	result := SocketSendTraceCapture{PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds()}
	for _, hook := range tracer.hooks {
		result.Hooks = append(result.Hooks, hook.module+"/"+hook.library+"!"+hook.api)
	}
	for _, patch := range tracer.patches {
		result.Patches = append(result.Patches, patch.name)
	}
	counter, ok := readRemote(tracer.process, tracer.counterAddress, 4)
	if !ok {
		return result
	}
	result.TotalCalls = binary.LittleEndian.Uint32(counter)
	count := result.TotalCalls
	if count > socketSendTraceCapacity {
		count = socketSendTraceCapacity
		result.Truncated = true
	}
	if count == 0 {
		return result
	}
	records, ok := readRemote(tracer.process, tracer.recordsAddress, int(count)*socketSendTraceRecordSize)
	if !ok {
		return result
	}
	for index := uint32(0); index < count; index++ {
		record := records[int(index)*socketSendTraceRecordSize : int(index+1)*socketSendTraceRecordSize]
		hookID := binary.LittleEndian.Uint32(record[0:4])
		call := SocketSendTraceCall{
			Index: index, ThreadID: binary.LittleEndian.Uint32(record[4:8]),
			Caller: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12])),
			Socket: binary.LittleEndian.Uint32(record[12:16]), RequestedBytes: binary.LittleEndian.Uint32(record[16:20]),
			Flags: binary.LittleEndian.Uint32(record[20:24]), Family: uint16(binary.LittleEndian.Uint32(record[24:28])),
		}
		if int(hookID) < len(tracer.hooks) {
			call.Module, call.API = tracer.hooks[hookID].module, tracer.hooks[hookID].api
		}
		rawPort := uint16(binary.LittleEndian.Uint32(record[28:32]))
		call.Port = rawPort<<8 | rawPort>>8
		rawAddress := binary.LittleEndian.Uint32(record[32:36])
		if rawAddress != 0 {
			call.IPv4 = fmt.Sprintf("%d.%d.%d.%d", byte(rawAddress), byte(rawAddress>>8), byte(rawAddress>>16), byte(rawAddress>>24))
		}
		call.CapturedBytes = binary.LittleEndian.Uint32(record[36:40])
		if call.CapturedBytes > socketSendTracePayloadSize {
			call.CapturedBytes = socketSendTracePayloadSize
		}
		call.PayloadHex = hex.EncodeToString(record[40 : 40+call.CapturedBytes])
		result.Calls = append(result.Calls, call)
	}
	return result
}

func (tracer *SocketSendTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	for index := len(tracer.patches) - 1; index >= 0; index-- {
		patch := tracer.patches[index]
		if patch.address != 0 && len(patch.original) != 0 {
			ensureRemoteBytes(tracer.process, patch.address, patch.original, pageExecuteReadWrite)
			procFlushInstruction.Call(uintptr(tracer.process), patch.address, uintptr(len(patch.original)))
		}
	}
	for index := len(tracer.hooks) - 1; index >= 0; index-- {
		hook := tracer.hooks[index]
		if hook.target != 0 && len(hook.original) != 0 {
			ensureRemoteBytes(tracer.process, hook.target, hook.original, pageExecuteReadWrite)
			procFlushInstruction.Call(uintptr(tracer.process), hook.target, uintptr(len(hook.original)))
		}
		if hook.iatAddress != 0 && len(hook.originalIAT) == 4 {
			ensureRemoteBytes(tracer.process, hook.iatAddress, hook.originalIAT, pageReadWrite)
		}
		if hook.page != 0 {
			procVirtualFreeEx.Call(uintptr(tracer.process), hook.page, 0, memRelease)
		}
	}
	if tracer.storage != 0 {
		procVirtualFreeEx.Call(uintptr(tracer.process), tracer.storage, 0, memRelease)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

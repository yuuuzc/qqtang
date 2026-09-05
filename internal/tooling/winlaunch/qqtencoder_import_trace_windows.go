package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"
)

const (
	qqtEncoderImportTraceCapacity = 24
	qqtEncoderImportTraceRecord   = 124
)

type QQTEncoderImportTraceCall struct {
	ThreadID    uint32            `json:"thread_id"`
	Caller      string            `json:"caller"`
	Arguments   []string          `json:"arguments"`
	Stack       []string          `json:"stack_after_arguments"`
	Registers   map[string]string `json:"registers"`
	InputBytes  string            `json:"input_bytes,omitempty"`
	SourceBytes string            `json:"source_bytes,omitempty"`
}

type QQTEncoderImportTraceCapture struct {
	PID        uint32                      `json:"pid"`
	ElapsedMS  int64                       `json:"elapsed_ms"`
	Interface  string                      `json:"interface"`
	VTable     string                      `json:"vtable"`
	Slot       string                      `json:"slot"`
	Method     string                      `json:"method"`
	TotalCalls uint32                      `json:"total_calls"`
	Calls      []QQTEncoderImportTraceCall `json:"calls"`
	Behavior   string                      `json:"behavior"`
}

type QQTEncoderImportTracer struct {
	pid              uint32
	process          syscall.Handle
	started          time.Time
	interfaceAddress uintptr
	vtableAddress    uintptr
	slotAddress      uintptr
	originalMethod   uintptr
	originalSlot     []byte
	page             uintptr
	counterAddress   uintptr
	recordsAddress   uintptr
}

// InstallQQTEncoderImportTracer replaces only the selected live interface's
// shared decode vtable slot. The trampoline records entry arguments and then
// tail-jumps to the original method. Close restores the exact original slot.
func InstallQQTEncoderImportTracer(pid uint32, interfaceAddress uintptr) (*QQTEncoderImportTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	if interfaceAddress == 0 || interfaceAddress > 0xffffffff {
		return nil, fmt.Errorf("interface address 0x%X is not a 32-bit address", interfaceAddress)
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &QQTEncoderImportTracer{
		pid: pid, process: process, started: time.Now(), interfaceAddress: interfaceAddress,
	}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	interfaceBytes, ok := readRemote(process, interfaceAddress, 4)
	if !ok {
		return nil, fmt.Errorf("read QQTEncoder interface 0x%08X", interfaceAddress)
	}
	tracer.vtableAddress = uintptr(binary.LittleEndian.Uint32(interfaceBytes))
	if tracer.vtableAddress == 0 {
		return nil, fmt.Errorf("QQTEncoder interface vtable is NULL")
	}
	tracer.slotAddress = tracer.vtableAddress + 0x14
	tracer.originalSlot, ok = readRemote(process, tracer.slotAddress, 4)
	if !ok {
		return nil, fmt.Errorf("read QQTEncoder import slot 0x%08X", tracer.slotAddress)
	}
	tracer.originalMethod = uintptr(binary.LittleEndian.Uint32(tracer.originalSlot))
	if tracer.originalMethod == 0 {
		return nil, fmt.Errorf("QQTEncoder import method is NULL")
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx QQTEncoder import trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.counterAddress = page + 0x300
	tracer.recordsAddress = page + 0x400
	stub := buildQQTEncoderImportTraceStub(tracer.counterAddress, tracer.recordsAddress)
	finalizeQQTEncoderImportTraceStub(stub, page, tracer.originalMethod)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write QQTEncoder import trace stub: %w", err)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(page))
	if !ensureRemoteBytes(process, tracer.slotAddress, pointer, pageReadWrite) {
		return nil, fmt.Errorf("patch QQTEncoder import slot 0x%08X", tracer.slotAddress)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))

	failed = false
	return tracer, nil
}

func buildQQTEncoderImportTraceStub(counterAddress, recordsAddress uintptr) []byte {
	stub := []byte{0x9C, 0x60}                        // pushfd; pushad
	stub = append(stub, 0xB8, 0x01, 0x00, 0x00, 0x00) // mov eax,1
	stub = append(stub, 0xF0, 0x0F, 0xC1, 0x05)       // lock xadd [counter],eax
	stub = binary.LittleEndian.AppendUint32(stub, uint32(counterAddress))
	stub = append(stub, 0x83, 0xF8, qqtEncoderImportTraceCapacity) // cmp eax,capacity
	stub = append(stub, 0x73, 0x00)                                // jae skip (finalized below)
	skipDisplacement := len(stub) - 1
	stub = append(stub, 0x6B, 0xC0, qqtEncoderImportTraceRecord) // imul eax,eax,record-size
	stub = append(stub, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordsAddress)) // add eax,records
	stub = append(stub, 0x64, 0x8B, 0x15, 0x24, 0, 0, 0)                  // mov edx,fs:[24]
	stub = append(stub, 0x89, 0x10)                                       // mov [eax],edx
	for index := 0; index < 7; index++ {
		// Original return address is at saved-ESP+36; six explicit arguments follow it.
		stackOffset := byte(0x24 + index*4)
		recordOffset := byte(0x04 + index*4)
		stub = append(stub, 0x8B, 0x54, 0x24, stackOffset) // mov edx,[esp+offset]
		stub = append(stub, 0x89, 0x50, recordOffset)      // mov [eax+offset],edx
	}
	for index := 0; index < 16; index++ {
		stackOffset := byte(0x40 + index*4)
		recordOffset := byte(0x20 + index*4)
		stub = append(stub, 0x8B, 0x54, 0x24, stackOffset) // mov edx,[esp+offset]
		stub = append(stub, 0x89, 0x50, recordOffset)      // mov [eax+offset],edx
	}
	registerStackOffsets := []byte{0x00, 0x04, 0x08, 0x10, 0x14, 0x18, 0x1c}
	for index, stackOffset := range registerStackOffsets {
		recordOffset := byte(0x60 + index*4)
		stub = append(stub, 0x8B, 0x54, 0x24, stackOffset) // mov edx,[esp+saved-register]
		stub = append(stub, 0x89, 0x50, recordOffset)      // mov [eax+offset],edx
	}
	stub[skipDisplacement] = byte(len(stub) - (skipDisplacement + 1))
	stub = append(stub, 0x61, 0x9D, 0xE9) // popad; popfd; jmp original
	stub = binary.LittleEndian.AppendUint32(stub, 0)
	return stub
}

func finalizeQQTEncoderImportTraceStub(stub []byte, stubAddress, originalMethod uintptr) {
	jumpImmediate := len(stub) - 4
	jumpNext := stubAddress + uintptr(len(stub))
	binary.LittleEndian.PutUint32(stub[jumpImmediate:], uint32(originalMethod-jumpNext))
}

// CaptureCalls waits until at least minCalls have passed through the transparent
// decoder hook, the client exits, or the duration elapses.  Waiting for more
// than one call is useful when periodic lobby traffic would otherwise consume
// the first record immediately before a diagnostic packet is sent.
func (tracer *QQTEncoderImportTracer) CaptureCalls(duration time.Duration, minCalls uint32) QQTEncoderImportTraceCapture {
	if duration <= 0 {
		duration = 10 * time.Second
	}
	if minCalls == 0 {
		minCalls = 1
	}
	if minCalls > qqtEncoderImportTraceCapacity {
		minCalls = qqtEncoderImportTraceCapacity
	}
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		counter, ok := readRemote(tracer.process, tracer.counterAddress, 4)
		if ok && binary.LittleEndian.Uint32(counter) >= minCalls {
			time.Sleep(100 * time.Millisecond)
			break
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return tracer.capture()
}

func (tracer *QQTEncoderImportTracer) capture() QQTEncoderImportTraceCapture {
	result := QQTEncoderImportTraceCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		Interface: fmt.Sprintf("0x%08X", tracer.interfaceAddress),
		VTable:    fmt.Sprintf("0x%08X", tracer.vtableAddress),
		Slot:      fmt.Sprintf("0x%08X", tracer.slotAddress), Method: fmt.Sprintf("0x%08X", tracer.originalMethod),
		Behavior: "transparent QQTEncoder decode-entry trace; exact vtable slot restored on close",
	}
	counter, ok := readRemote(tracer.process, tracer.counterAddress, 4)
	if !ok {
		return result
	}
	result.TotalCalls = binary.LittleEndian.Uint32(counter)
	count := result.TotalCalls
	if count > qqtEncoderImportTraceCapacity {
		count = qqtEncoderImportTraceCapacity
	}
	records, ok := readRemote(tracer.process, tracer.recordsAddress, int(count)*qqtEncoderImportTraceRecord)
	if !ok {
		return result
	}
	for index := uint32(0); index < count; index++ {
		record := records[index*qqtEncoderImportTraceRecord : (index+1)*qqtEncoderImportTraceRecord]
		call := QQTEncoderImportTraceCall{
			ThreadID:  binary.LittleEndian.Uint32(record[0:4]),
			Caller:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[4:8])),
			Registers: make(map[string]string),
		}
		for arg := 0; arg < 6; arg++ {
			value := binary.LittleEndian.Uint32(record[8+arg*4 : 12+arg*4])
			call.Arguments = append(call.Arguments, fmt.Sprintf("0x%08X", value))
			if arg != 2 && arg != 3 || value == 0 {
				continue
			}
			preview, previewOK := readRemote(tracer.process, uintptr(value), 0x100)
			if !previewOK {
				continue
			}
			if arg == 2 {
				call.InputBytes = hex.EncodeToString(preview)
			} else {
				call.SourceBytes = hex.EncodeToString(preview)
			}
		}
		for stackIndex := 0; stackIndex < 16; stackIndex++ {
			value := binary.LittleEndian.Uint32(record[32+stackIndex*4 : 36+stackIndex*4])
			call.Stack = append(call.Stack, fmt.Sprintf("0x%08X", value))
		}
		registerNames := []string{"edi", "esi", "ebp", "ebx", "edx", "ecx", "eax"}
		for registerIndex, name := range registerNames {
			value := binary.LittleEndian.Uint32(record[96+registerIndex*4 : 100+registerIndex*4])
			call.Registers[name] = fmt.Sprintf("0x%08X", value)
		}
		result.Calls = append(result.Calls, call)
	}
	return result
}

func (tracer *QQTEncoderImportTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.slotAddress != 0 && len(tracer.originalSlot) == 4 {
		ensureRemoteBytes(tracer.process, tracer.slotAddress, tracer.originalSlot, pageReadWrite)
	}
	// Do not release the trampoline page while the client is alive. Restoring
	// the shared vtable prevents new entries, but a network thread may already
	// be executing inside the stub. VirtualFreeEx here creates a narrow
	// use-after-free race and has produced an access violation after an
	// otherwise successful trace. The private 4 KiB page is reclaimed by
	// Windows when Client.exe exits.
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

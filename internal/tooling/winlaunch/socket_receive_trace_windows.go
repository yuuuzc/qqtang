package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"
)

const (
	socketReceiveTraceCapacity    = 256
	socketReceiveTraceRecordSize  = 128
	socketReceiveTracePayloadSize = 64
)

type SocketReceiveTraceCall struct {
	Index          uint32 `json:"index"`
	ThreadID       uint32 `json:"thread_id"`
	Caller         string `json:"caller"`
	CallerModule   string `json:"caller_module,omitempty"`
	CallerRVA      string `json:"caller_rva,omitempty"`
	Caller2        string `json:"caller_2"`
	Caller2Module  string `json:"caller_2_module,omitempty"`
	Caller2RVA     string `json:"caller_2_rva,omitempty"`
	Caller3        string `json:"caller_3"`
	Caller3Module  string `json:"caller_3_module,omitempty"`
	Caller3RVA     string `json:"caller_3_rva,omitempty"`
	Socket         uint32 `json:"socket"`
	Buffer         string `json:"buffer"`
	RequestedBytes uint32 `json:"requested_bytes"`
	Flags          uint32 `json:"flags"`
	From           string `json:"from"`
	FromLength     string `json:"from_length"`
	ReceivedBytes  int32  `json:"received_bytes"`
	Family         uint16 `json:"family,omitempty"`
	IPv4           string `json:"ipv4,omitempty"`
	Port           uint16 `json:"port,omitempty"`
	CapturedBytes  uint32 `json:"captured_bytes"`
	PayloadHex     string `json:"payload_hex,omitempty"`
}

type SocketReceiveTraceCapture struct {
	PID        uint32                   `json:"pid"`
	ElapsedMS  int64                    `json:"elapsed_ms"`
	Target     string                   `json:"target"`
	TotalCalls uint32                   `json:"total_calls"`
	Truncated  bool                     `json:"truncated,omitempty"`
	Calls      []SocketReceiveTraceCall `json:"calls"`
	Behavior   string                   `json:"behavior"`
}

// SocketReceiveTracer wraps process-private WSOCK32!recvfrom. Unlike a pre-call
// probe it records only after the real API returns, while the payload and peer
// sockaddr are valid. It is diagnostic-only and restores the export prologue.
type SocketReceiveTracer struct {
	pid      uint32
	process  syscall.Handle
	started  time.Time
	library  string
	target   uintptr
	original []byte
	page     uintptr
	counter  uintptr
	records  uintptr
}

func InstallSocketReceiveTracer(pid uint32, timeout time.Duration) (*SocketReceiveTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &SocketReceiveTracer{pid: pid, process: process, started: time.Now(), library: "WSOCK32.dll"}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	tracer.target, err = findRemoteExport(process, pid, tracer.library, "recvfrom", timeout, 0)
	if err != nil {
		return nil, err
	}
	tracer.original, _ = readRemote(process, tracer.target, 5)
	want := []byte{0x8B, 0xFF, 0x55, 0x8B, 0xEC}
	if len(tracer.original) != len(want) || !equalBytes(tracer.original, want) {
		return nil, fmt.Errorf("unsupported %s recvfrom prologue %X at 0x%08X", tracer.library, tracer.original, tracer.target)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x9000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx recvfrom trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	trampoline := page + 0x300
	tracer.counter = page + 0x800
	tracer.records = page + 0x1000
	trampolineCode := append([]byte{}, tracer.original...)
	trampolineCode = append(trampolineCode, 0xE9)
	trampolineCode = binary.LittleEndian.AppendUint32(trampolineCode, uint32((tracer.target+5)-(trampoline+uintptr(len(trampolineCode))+4)))
	stub := buildSocketReceiveTraceStub(page, trampoline, tracer.counter, tracer.records)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write recvfrom trace stub: %w", err)
	}
	if err := writeRemote(process, trampoline, trampolineCode); err != nil {
		return nil, fmt.Errorf("write recvfrom trampoline: %w", err)
	}
	patch := relativeJumpPatch(tracer.target, page, len(tracer.original))
	if !ensureRemoteBytes(process, tracer.target, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch %s recvfrom at 0x%08X", tracer.library, tracer.target)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), trampoline, uintptr(len(trampolineCode)))
	procFlushInstruction.Call(uintptr(process), tracer.target, uintptr(len(patch)))
	failed = false
	return tracer, nil
}

func buildSocketReceiveTraceStub(stubAddress, trampolineAddress, counterAddress, recordsAddress uintptr) []byte {
	// Establish a private frame, copy the six original arguments and call the
	// trampoline. recvfrom is stdcall, so the copied arguments are removed by
	// the real API while the caller's untouched frame remains for our final RET.
	stub := []byte{0x55, 0x8B, 0xEC}
	for _, offset := range []byte{0x1C, 0x18, 0x14, 0x10, 0x0C, 0x08} {
		stub = append(stub, 0xFF, 0x75, offset)
	}
	stub = append(stub, 0xE8)
	callImmediate := len(stub)
	stub = binary.LittleEndian.AppendUint32(stub, 0)
	callNext := stubAddress + uintptr(len(stub))
	binary.LittleEndian.PutUint32(stub[callImmediate:], uint32(trampolineAddress-callNext))

	stub = append(stub, 0x9C, 0x60) // preserve return value and volatile state
	stub = append(stub, 0xB8, 0x01, 0, 0, 0)
	stub = append(stub, 0xF0, 0x0F, 0xC1, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(counterAddress))
	stub = append(stub, 0x3D)
	stub = binary.LittleEndian.AppendUint32(stub, socketReceiveTraceCapacity)
	stub = append(stub, 0x0F, 0x83, 0, 0, 0, 0)
	doneJump := len(stub) - 4
	stub = append(stub, 0x69, 0xC0)
	stub = binary.LittleEndian.AppendUint32(stub, socketReceiveTraceRecordSize)
	stub = append(stub, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordsAddress))

	stub = append(stub, 0x64, 0x8B, 0x15, 0x24, 0, 0, 0, 0x89, 0x10) // thread
	for _, field := range []struct{ source, target byte }{
		{0x04, 0x04}, {0x08, 0x08}, {0x0C, 0x0C}, {0x10, 0x10},
		{0x14, 0x14}, {0x18, 0x18}, {0x1C, 0x1C},
	} {
		stub = append(stub, 0x8B, 0x4D, field.source, 0x89, 0x48, field.target)
	}
	stub = append(stub, 0x8B, 0x54, 0x24, 0x1C, 0x89, 0x50, 0x20) // saved EAX / result
	// WSOCK32 recvfrom was called by MFC's framed ReceiveFromHelper. Preserve
	// two additional return addresses so the owning client module and exact
	// receive callsite can be identified without repeating module-specific
	// probes.
	stub = append(stub, 0x8B, 0x4D, 0x00, 0x89, 0x48, 0x34)             // helper EBP
	stub = append(stub, 0x8B, 0x51, 0x04, 0x89, 0x50, 0x38)             // helper caller
	stub = append(stub, 0x8B, 0x09, 0x8B, 0x51, 0x04, 0x89, 0x50, 0x3C) // caller's caller
	stub = append(stub, 0x8B, 0x54, 0x24, 0x1C)                         // restore saved recvfrom result
	stub = append(stub, 0x85, 0xD2, 0x0F, 0x8E, 0, 0, 0, 0)
	noCopyJump := len(stub) - 4

	// Peer address is an output parameter and is now populated.
	stub = append(stub, 0x8B, 0x4D, 0x18, 0x85, 0xC9, 0x0F, 0x84, 0, 0, 0, 0)
	noAddressJump := len(stub) - 4
	stub = append(stub, 0x0F, 0xB7, 0x11, 0x89, 0x50, 0x24)
	stub = append(stub, 0x0F, 0xB7, 0x51, 0x02, 0x89, 0x50, 0x28)
	stub = append(stub, 0x8B, 0x51, 0x04, 0x89, 0x50, 0x2C)
	patchRelative32(stub, noAddressJump, len(stub))

	stub = append(stub, 0x8B, 0x4C, 0x24, 0x1C) // ecx = saved recvfrom return value
	stub = append(stub, 0x83, 0xF9, socketReceiveTracePayloadSize, 0x0F, 0x86, 0, 0, 0, 0)
	lengthOKJump := len(stub) - 4
	stub = append(stub, 0xB9, socketReceiveTracePayloadSize, 0, 0, 0)
	patchRelative32(stub, lengthOKJump, len(stub))
	stub = append(stub, 0x89, 0x48, 0x30, 0x8B, 0x75, 0x0C, 0x85, 0xF6, 0x0F, 0x84, 0, 0, 0, 0)
	missingBufferJump := len(stub) - 4
	stub = append(stub, 0x8D, 0x78, 0x40, 0xFC, 0xF3, 0xA4)
	patchRelative32(stub, missingBufferJump, len(stub))
	patchRelative32(stub, noCopyJump, len(stub))
	patchRelative32(stub, doneJump, len(stub))
	stub = append(stub, 0x61, 0x9D, 0x8B, 0xE5, 0x5D, 0xC2, 0x18, 0)
	return stub
}

func (tracer *SocketReceiveTracer) Capture(duration time.Duration) SocketReceiveTraceCapture {
	if duration <= 0 {
		duration = 10 * time.Second
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

func (tracer *SocketReceiveTracer) capture() SocketReceiveTraceCapture {
	result := SocketReceiveTraceCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(), Target: fmt.Sprintf("%s!recvfrom@0x%08X", tracer.library, tracer.target),
		Behavior: "transparent post-call WSOCK32!recvfrom trace; captures payload and peer address; original prologue restored on close",
	}
	counter, ok := readRemote(tracer.process, tracer.counter, 4)
	if !ok {
		return result
	}
	result.TotalCalls = binary.LittleEndian.Uint32(counter)
	count := result.TotalCalls
	if count > socketReceiveTraceCapacity {
		count = socketReceiveTraceCapacity
		result.Truncated = true
	}
	if count == 0 {
		return result
	}
	records, ok := readRemote(tracer.process, tracer.records, int(count)*socketReceiveTraceRecordSize)
	if !ok {
		return result
	}
	modules := snapshotRemoteModules(tracer.process)
	for index := uint32(0); index < count; index++ {
		record := records[int(index)*socketReceiveTraceRecordSize : int(index+1)*socketReceiveTraceRecordSize]
		caller := binary.LittleEndian.Uint32(record[4:8])
		caller2 := binary.LittleEndian.Uint32(record[56:60])
		caller3 := binary.LittleEndian.Uint32(record[60:64])
		call := SocketReceiveTraceCall{
			Index: index, ThreadID: binary.LittleEndian.Uint32(record[0:4]), Caller: fmt.Sprintf("0x%08X", caller),
			Caller2: fmt.Sprintf("0x%08X", caller2), Caller3: fmt.Sprintf("0x%08X", caller3),
			Socket: binary.LittleEndian.Uint32(record[8:12]), Buffer: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[12:16])),
			RequestedBytes: binary.LittleEndian.Uint32(record[16:20]), Flags: binary.LittleEndian.Uint32(record[20:24]),
			From:          fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[24:28])),
			FromLength:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[28:32])),
			ReceivedBytes: int32(binary.LittleEndian.Uint32(record[32:36])), Family: uint16(binary.LittleEndian.Uint32(record[36:40])),
		}
		rawPort := uint16(binary.LittleEndian.Uint32(record[40:44]))
		call.Port = rawPort<<8 | rawPort>>8
		rawAddress := binary.LittleEndian.Uint32(record[44:48])
		if rawAddress != 0 {
			call.IPv4 = fmt.Sprintf("%d.%d.%d.%d", byte(rawAddress), byte(rawAddress>>8), byte(rawAddress>>16), byte(rawAddress>>24))
		}
		call.CapturedBytes = binary.LittleEndian.Uint32(record[48:52])
		if call.CapturedBytes > socketReceiveTracePayloadSize {
			call.CapturedBytes = socketReceiveTracePayloadSize
		}
		call.PayloadHex = hex.EncodeToString(record[64 : 64+call.CapturedBytes])
		for _, target := range []struct {
			address uint32
			module  *string
			rva     *string
		}{
			{caller, &call.CallerModule, &call.CallerRVA},
			{caller2, &call.Caller2Module, &call.Caller2RVA},
			{caller3, &call.Caller3Module, &call.Caller3RVA},
		} {
			for _, module := range modules {
				if target.address >= module.base && target.address-module.base < module.size {
					*target.module = module.name
					*target.rva = fmt.Sprintf("0x%X", target.address-module.base)
					break
				}
			}
		}
		result.Calls = append(result.Calls, call)
	}
	return result
}

func (tracer *SocketReceiveTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.target != 0 && len(tracer.original) != 0 {
		ensureRemoteBytes(tracer.process, tracer.target, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.target, uintptr(len(tracer.original)))
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

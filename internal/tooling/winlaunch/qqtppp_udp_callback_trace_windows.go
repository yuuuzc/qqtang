package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"syscall"
	"time"
)

const (
	qqtPPPUDPCallbackPatchRVA   = uintptr(0x7871)
	qqtPPPUDPCallbackResumeRVA  = uintptr(0x7881)
	qqtPPPUDPCallbackCapacity   = 128
	qqtPPPUDPCallbackRecordSize = 128
	qqtPPPUDPCallbackPayload    = 64
)

var qqtPPPUDPCallbackSignature = []byte{
	0xFF, 0x75, 0xFC, // push [ebp-04] (length)
	0x8D, 0xB5, 0xE8, 0xEF, 0xFF, 0xFF, // lea esi,[ebp-1018] (payload)
	0x8B, 0x11, // mov edx,[ecx]
	0x56,       // push esi
	0x50,       // push eax (source port)
	0x53,       // push ebx (source IPv4)
	0xFF, 0x12, // call [edx]
}

type QQTPPPUDPCallbackCall struct {
	Index         uint32 `json:"index"`
	ThreadID      uint32 `json:"thread_id"`
	Sink          string `json:"sink"`
	SinkVTable    string `json:"sink_vtable"`
	Method        string `json:"method"`
	MethodModule  string `json:"method_module,omitempty"`
	MethodRVA     string `json:"method_rva,omitempty"`
	SourceIPv4    string `json:"source_ipv4"`
	SourcePort    uint32 `json:"source_port"`
	ReceivedBytes uint32 `json:"received_bytes"`
	Buffer        string `json:"buffer"`
	CapturedBytes uint32 `json:"captured_bytes"`
	PayloadHex    string `json:"payload_hex,omitempty"`
}

type QQTPPPUDPCallbackCapture struct {
	PID        uint32                  `json:"pid"`
	ElapsedMS  int64                   `json:"elapsed_ms"`
	ModuleBase string                  `json:"module_base"`
	Patch      string                  `json:"patch"`
	TotalCalls uint32                  `json:"total_calls"`
	Truncated  bool                    `json:"truncated,omitempty"`
	Calls      []QQTPPPUDPCallbackCall `json:"calls"`
	Behavior   string                  `json:"behavior"`
}

type QQTPPPUDPCallbackTracer struct {
	pid      uint32
	process  syscall.Handle
	started  time.Time
	base     uintptr
	patch    uintptr
	original []byte
	page     uintptr
	counter  uintptr
	records  uintptr
}

func InstallQQTPPPUDPCallbackTracer(pid uint32, timeout time.Duration) (*QQTPPPUDPCallbackTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &QQTPPPUDPCallbackTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	tracer.base, err = waitForModule(process, pid, "QQTPPP.dll", timeout)
	if err != nil {
		return nil, err
	}
	tracer.patch = tracer.base + qqtPPPUDPCallbackPatchRVA
	tracer.original, _ = readRemote(process, tracer.patch, len(qqtPPPUDPCallbackSignature))
	if len(tracer.original) != len(qqtPPPUDPCallbackSignature) || !equalBytes(tracer.original, qqtPPPUDPCallbackSignature) {
		return nil, fmt.Errorf("QQTPPP UDP callback signature mismatch at 0x%08X: got %X", tracer.patch, tracer.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x6000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx QQTPPP UDP callback trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.counter = page + 0x800
	tracer.records = page + 0x1000
	stub := buildQQTPPPUDPCallbackTraceStub(page, tracer.counter, tracer.records, tracer.base+qqtPPPUDPCallbackResumeRVA)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write QQTPPP UDP callback trace stub: %w", err)
	}
	patch := relativeJumpPatch(tracer.patch, page, len(tracer.original))
	if !ensureRemoteBytes(process, tracer.patch, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch QQTPPP UDP callback at 0x%08X", tracer.patch)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), tracer.patch, uintptr(len(patch)))
	failed = false
	return tracer, nil
}

func buildQQTPPPUDPCallbackTraceStub(stubAddress, counterAddress, recordsAddress, resumeAddress uintptr) []byte {
	stub := []byte{0x9C, 0x60}
	stub = append(stub, 0xB8, 0x01, 0, 0, 0)
	stub = append(stub, 0xF0, 0x0F, 0xC1, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(counterAddress))
	stub = append(stub, 0x3D)
	stub = binary.LittleEndian.AppendUint32(stub, qqtPPPUDPCallbackCapacity)
	stub = append(stub, 0x0F, 0x83, 0, 0, 0, 0)
	doneJump := len(stub) - 4
	stub = append(stub, 0x69, 0xC0)
	stub = binary.LittleEndian.AppendUint32(stub, qqtPPPUDPCallbackRecordSize)
	stub = append(stub, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordsAddress))

	stub = append(stub, 0x64, 0x8B, 0x15, 0x24, 0, 0, 0, 0x89, 0x10)          // thread ID
	stub = append(stub, 0x8B, 0x54, 0x24, 0x18, 0x89, 0x50, 0x04)             // sink / original ECX
	stub = append(stub, 0x8B, 0x0A, 0x89, 0x48, 0x08)                         // vtable
	stub = append(stub, 0x8B, 0x09, 0x89, 0x48, 0x0C)                         // callback method
	stub = append(stub, 0x8B, 0x4C, 0x24, 0x10, 0x89, 0x48, 0x10)             // source IPv4 / original EBX
	stub = append(stub, 0x8B, 0x4C, 0x24, 0x1C, 0x89, 0x48, 0x14)             // source port / original EAX
	stub = append(stub, 0x8B, 0x54, 0x24, 0x08)                               // receiver EBP
	stub = append(stub, 0x8B, 0x4A, 0xFC, 0x89, 0x48, 0x18)                   // received length
	stub = append(stub, 0x8D, 0xB2, 0xE8, 0xEF, 0xFF, 0xFF, 0x89, 0x70, 0x1C) // payload pointer

	stub = append(stub, 0x85, 0xC9, 0x0F, 0x8E, 0, 0, 0, 0)
	noCopyJump := len(stub) - 4
	stub = append(stub, 0x83, 0xF9, qqtPPPUDPCallbackPayload, 0x0F, 0x86, 0, 0, 0, 0)
	lengthOKJump := len(stub) - 4
	stub = append(stub, 0xB9, qqtPPPUDPCallbackPayload, 0, 0, 0)
	patchRelative32(stub, lengthOKJump, len(stub))
	stub = append(stub, 0x89, 0x48, 0x20, 0x8D, 0x78, 0x40, 0xFC, 0xF3, 0xA4)
	patchRelative32(stub, noCopyJump, len(stub))
	patchRelative32(stub, doneJump, len(stub))

	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, qqtPPPUDPCallbackSignature...)
	stub = append(stub, 0xE9)
	jumpImmediate := len(stub)
	stub = binary.LittleEndian.AppendUint32(stub, 0)
	jumpNext := stubAddress + uintptr(len(stub))
	binary.LittleEndian.PutUint32(stub[jumpImmediate:], uint32(resumeAddress-jumpNext))
	return stub
}

func (tracer *QQTPPPUDPCallbackTracer) Capture(duration time.Duration) QQTPPPUDPCallbackCapture {
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

func (tracer *QQTPPPUDPCallbackTracer) capture() QQTPPPUDPCallbackCapture {
	result := QQTPPPUDPCallbackCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		ModuleBase: fmt.Sprintf("0x%08X", tracer.base), Patch: fmt.Sprintf("0x%08X", tracer.patch),
		Behavior: "transparent QQTPPP raw UDP sink-call trace; no packet mutation; original dispatch restored on close",
	}
	counter, ok := readRemote(tracer.process, tracer.counter, 4)
	if !ok {
		return result
	}
	result.TotalCalls = binary.LittleEndian.Uint32(counter)
	count := result.TotalCalls
	if count > qqtPPPUDPCallbackCapacity {
		count = qqtPPPUDPCallbackCapacity
		result.Truncated = true
	}
	if count == 0 {
		return result
	}
	records, ok := readRemote(tracer.process, tracer.records, int(count)*qqtPPPUDPCallbackRecordSize)
	if !ok {
		return result
	}
	modules := snapshotRemoteModules(tracer.process)
	for index := uint32(0); index < count; index++ {
		record := records[int(index)*qqtPPPUDPCallbackRecordSize : int(index+1)*qqtPPPUDPCallbackRecordSize]
		method := binary.LittleEndian.Uint32(record[12:16])
		ipv4Host := binary.LittleEndian.Uint32(record[16:20])
		ipv4Bytes := make(net.IP, 4)
		binary.BigEndian.PutUint32(ipv4Bytes, ipv4Host)
		call := QQTPPPUDPCallbackCall{
			Index: index, ThreadID: binary.LittleEndian.Uint32(record[0:4]),
			Sink: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[4:8])), SinkVTable: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12])),
			Method: fmt.Sprintf("0x%08X", method), SourceIPv4: ipv4Bytes.String(), SourcePort: binary.LittleEndian.Uint32(record[20:24]) & 0xffff,
			ReceivedBytes: binary.LittleEndian.Uint32(record[24:28]), Buffer: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[28:32])),
			CapturedBytes: binary.LittleEndian.Uint32(record[32:36]),
		}
		if call.CapturedBytes > qqtPPPUDPCallbackPayload {
			call.CapturedBytes = qqtPPPUDPCallbackPayload
		}
		call.PayloadHex = hex.EncodeToString(record[64 : 64+call.CapturedBytes])
		for _, module := range modules {
			if method >= module.base && method-module.base < module.size {
				call.MethodModule = module.name
				call.MethodRVA = fmt.Sprintf("0x%X", method-module.base)
				break
			}
		}
		result.Calls = append(result.Calls, call)
	}
	return result
}

func (tracer *QQTPPPUDPCallbackTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.patch != 0 && len(tracer.original) != 0 {
		ensureRemoteBytes(tracer.process, tracer.patch, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.patch, uintptr(len(tracer.original)))
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

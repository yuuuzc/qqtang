package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"
)

const (
	netCenterUDPCallbackPatchRVA   = uintptr(0x26D6D)
	netCenterUDPCallbackResumeRVA  = uintptr(0x26D79)
	netCenterUDPCallbackCapacity   = 128
	netCenterUDPCallbackRecordSize = 128
	netCenterUDPCallbackPayload    = 64
)

var netCenterUDPCallbackSignature = []byte{
	0x8B, 0x08, // mov ecx,[eax]
	0xFF, 0x75, 0x0C, // push [ebp+0c]
	0xFF, 0x75, 0x08, // push [ebp+08]
	0x50,             // push eax
	0xFF, 0x51, 0x10, // call [ecx+10]
}

// NetCenterUDPCallbackCall is one delivery from NetCenter's UDP socket to a
// CConnectionPoint subscriber. The five parameters are the exact native
// callback arguments; PayloadHex is a bounded pre-call copy of argument 5.
type NetCenterUDPCallbackCall struct {
	Index         uint32 `json:"index"`
	ThreadID      uint32 `json:"thread_id"`
	Sink          string `json:"sink"`
	SinkVTable    string `json:"sink_vtable"`
	Method        string `json:"method"`
	MethodModule  string `json:"method_module,omitempty"`
	MethodRVA     string `json:"method_rva,omitempty"`
	Status        uint32 `json:"status"`
	RemoteText    string `json:"remote_text_pointer"`
	RemotePort    uint32 `json:"remote_port"`
	ReceivedBytes uint32 `json:"received_bytes"`
	Buffer        string `json:"buffer"`
	Connections   string `json:"connections"`
	DispatcherEBP string `json:"dispatcher_ebp"`
	CapturedBytes uint32 `json:"captured_bytes"`
	PayloadHex    string `json:"payload_hex,omitempty"`
}

type NetCenterUDPCallbackCapture struct {
	PID        uint32                     `json:"pid"`
	ElapsedMS  int64                      `json:"elapsed_ms"`
	ModuleBase string                     `json:"module_base"`
	Patch      string                     `json:"patch"`
	TotalCalls uint32                     `json:"total_calls"`
	Truncated  bool                       `json:"truncated,omitempty"`
	Calls      []NetCenterUDPCallbackCall `json:"calls"`
	Behavior   string                     `json:"behavior"`
}

// NetCenterUDPCallbackTracer transparently wraps the one native indirect call
// that fans a received datagram out to each CConnectionPoint subscriber. This
// identifies the real consumer method without interpreting or injecting any
// packet and restores the original instruction block on Close.
type NetCenterUDPCallbackTracer struct {
	pid      uint32
	process  syscall.Handle
	started  time.Time
	base     uintptr
	patch    uintptr
	original []byte
	page     uintptr
	counter  uintptr
	records  uintptr
	stubSize uintptr
}

func InstallNetCenterUDPCallbackTracer(pid uint32, timeout time.Duration) (*NetCenterUDPCallbackTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &NetCenterUDPCallbackTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	tracer.base, err = waitForModule(process, pid, "NetCenter.dll", timeout)
	if err != nil {
		return nil, err
	}
	tracer.patch = tracer.base + netCenterUDPCallbackPatchRVA
	tracer.original, _ = readRemote(process, tracer.patch, len(netCenterUDPCallbackSignature))
	if len(tracer.original) != len(netCenterUDPCallbackSignature) || !equalBytes(tracer.original, netCenterUDPCallbackSignature) {
		return nil, fmt.Errorf("NetCenter UDP callback signature mismatch at 0x%08X: got %X", tracer.patch, tracer.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x6000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx NetCenter UDP callback trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.counter = page + 0x800
	tracer.records = page + 0x1000
	stub := buildNetCenterUDPCallbackTraceStub(page, tracer.counter, tracer.records, tracer.base+netCenterUDPCallbackResumeRVA)
	tracer.stubSize = uintptr(len(stub))
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write NetCenter UDP callback trace stub: %w", err)
	}
	patch := relativeJumpPatch(tracer.patch, page, len(tracer.original))
	if !ensureRemoteBytes(process, tracer.patch, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch NetCenter UDP callback dispatch at 0x%08X", tracer.patch)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), tracer.patch, uintptr(len(patch)))
	failed = false
	return tracer, nil
}

func buildNetCenterUDPCallbackTraceStub(stubAddress, counterAddress, recordsAddress, resumeAddress uintptr) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xB8, 0x01, 0, 0, 0)
	stub = append(stub, 0xF0, 0x0F, 0xC1, 0x05) // lock xadd [counter],eax
	stub = binary.LittleEndian.AppendUint32(stub, uint32(counterAddress))
	stub = append(stub, 0x3D)
	stub = binary.LittleEndian.AppendUint32(stub, netCenterUDPCallbackCapacity)
	stub = append(stub, 0x0F, 0x83, 0, 0, 0, 0) // jae done
	doneJump := len(stub) - 4
	stub = append(stub, 0x69, 0xC0)
	stub = binary.LittleEndian.AppendUint32(stub, netCenterUDPCallbackRecordSize)
	stub = append(stub, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordsAddress))

	// Record the subscriber and exact five native parameters. At this patch
	// point EAX is the sink object and EBP is FUN_10026d3a's frame.
	stub = append(stub, 0x64, 0x8B, 0x15, 0x24, 0, 0, 0, 0x89, 0x10) // thread ID
	stub = append(stub, 0x8B, 0x54, 0x24, 0x1C, 0x89, 0x50, 0x04)    // sink
	stub = append(stub, 0x8B, 0x0A, 0x89, 0x48, 0x08)                // vtable
	stub = append(stub, 0x8B, 0x49, 0x10, 0x89, 0x48, 0x0C)          // method slot +0x10
	stub = append(stub, 0x8B, 0x54, 0x24, 0x08, 0x89, 0x50, 0x2C)    // dispatcher EBP
	for _, field := range []struct{ source, target byte }{
		{0x08, 0x10}, {0x0C, 0x14}, {0x10, 0x18}, {0x14, 0x1C}, {0x18, 0x20},
	} {
		stub = append(stub, 0x8B, 0x4A, field.source, 0x89, 0x48, field.target)
	}
	stub = append(stub, 0x8B, 0x4C, 0x24, 0x00, 0x89, 0x48, 0x24) // saved EDI / connections

	// Copy only the first 64 bytes while the callback-owned buffer is valid.
	stub = append(stub, 0x8B, 0x4A, 0x14, 0x85, 0xC9, 0x0F, 0x8E, 0, 0, 0, 0)
	noCopyJump := len(stub) - 4
	stub = append(stub, 0x83, 0xF9, netCenterUDPCallbackPayload, 0x0F, 0x86, 0, 0, 0, 0)
	lengthOKJump := len(stub) - 4
	stub = append(stub, 0xB9, netCenterUDPCallbackPayload, 0, 0, 0)
	patchRelative32(stub, lengthOKJump, len(stub))
	stub = append(stub, 0x89, 0x48, 0x28, 0x8B, 0x72, 0x18, 0x85, 0xF6, 0x0F, 0x84, 0, 0, 0, 0)
	missingBufferJump := len(stub) - 4
	stub = append(stub, 0x8D, 0x78, 0x40, 0xFC, 0xF3, 0xA4) // payload at record+64
	patchRelative32(stub, noCopyJump, len(stub))
	patchRelative32(stub, missingBufferJump, len(stub))
	patchRelative32(stub, doneJump, len(stub))

	stub = append(stub, 0x61, 0x9D) // popad; popfd
	stub = append(stub, netCenterUDPCallbackSignature...)
	stub = append(stub, 0xE9)
	jumpImmediate := len(stub)
	stub = binary.LittleEndian.AppendUint32(stub, 0)
	jumpNext := stubAddress + uintptr(len(stub))
	binary.LittleEndian.PutUint32(stub[jumpImmediate:], uint32(resumeAddress-jumpNext))
	return stub
}

func (tracer *NetCenterUDPCallbackTracer) Capture(duration time.Duration) NetCenterUDPCallbackCapture {
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

func (tracer *NetCenterUDPCallbackTracer) capture() NetCenterUDPCallbackCapture {
	result := NetCenterUDPCallbackCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		ModuleBase: fmt.Sprintf("0x%08X", tracer.base), Patch: fmt.Sprintf("0x%08X", tracer.patch),
		Behavior: "transparent NetCenter UDP CConnectionPoint sink-call trace; no packet mutation; original dispatch restored on close",
	}
	counter, ok := readRemote(tracer.process, tracer.counter, 4)
	if !ok {
		return result
	}
	result.TotalCalls = binary.LittleEndian.Uint32(counter)
	count := result.TotalCalls
	if count > netCenterUDPCallbackCapacity {
		count = netCenterUDPCallbackCapacity
		result.Truncated = true
	}
	if count == 0 {
		return result
	}
	records, ok := readRemote(tracer.process, tracer.records, int(count)*netCenterUDPCallbackRecordSize)
	if !ok {
		return result
	}
	modules := snapshotRemoteModules(tracer.process)
	for index := uint32(0); index < count; index++ {
		record := records[int(index)*netCenterUDPCallbackRecordSize : int(index+1)*netCenterUDPCallbackRecordSize]
		method := binary.LittleEndian.Uint32(record[12:16])
		call := NetCenterUDPCallbackCall{
			Index: index, ThreadID: binary.LittleEndian.Uint32(record[0:4]),
			Sink:       fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[4:8])),
			SinkVTable: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12])),
			Method:     fmt.Sprintf("0x%08X", method), Status: binary.LittleEndian.Uint32(record[16:20]),
			RemoteText: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[20:24])),
			RemotePort: binary.LittleEndian.Uint32(record[24:28]), ReceivedBytes: binary.LittleEndian.Uint32(record[28:32]),
			Buffer:        fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[32:36])),
			Connections:   fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[36:40])),
			CapturedBytes: binary.LittleEndian.Uint32(record[40:44]),
			DispatcherEBP: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[44:48])),
		}
		if call.CapturedBytes > netCenterUDPCallbackPayload {
			call.CapturedBytes = netCenterUDPCallbackPayload
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

func (tracer *NetCenterUDPCallbackTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.patch != 0 && len(tracer.original) != 0 {
		ensureRemoteBytes(tracer.process, tracer.patch, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.patch, uintptr(len(tracer.original)))
	}
	// The small diagnostic allocation is intentionally left mapped after the
	// branch is restored. This avoids freeing code while an already-dispatched
	// callback could still be returning through the transparent stub.
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

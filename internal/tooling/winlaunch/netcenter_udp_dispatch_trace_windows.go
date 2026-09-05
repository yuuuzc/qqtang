package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"
)

const (
	netCenterUDPDispatchPatchRVA   = uintptr(0x26D3A)
	netCenterUDPDispatchResumeRVA  = uintptr(0x26D41)
	netCenterUDPDispatchCapacity   = 128
	netCenterUDPDispatchRecordSize = 128
	netCenterUDPDispatchPayload    = 64
)

var netCenterUDPDispatchSignature = []byte{
	0x55,       // push ebp
	0x8B, 0xEC, // mov ebp,esp
	0x57,             // push edi
	0x83, 0xC1, 0x4C, // add ecx,4c
}

// NetCenterUDPDispatchCall records one invocation of the datagram dispatcher.
// It is intentionally captured before CConnectionPoint::GetConnections so an
// accepted relay datagram remains observable even when no gameplay subscriber
// is registered in the client's current UI state.
type NetCenterUDPDispatchCall struct {
	Index         uint32 `json:"index"`
	ThreadID      uint32 `json:"thread_id"`
	Caller        string `json:"caller"`
	CallerModule  string `json:"caller_module,omitempty"`
	CallerRVA     string `json:"caller_rva,omitempty"`
	Manager       string `json:"manager"`
	Status        uint32 `json:"status"`
	RemoteText    string `json:"remote_text_pointer"`
	RemotePort    uint32 `json:"remote_port"`
	ReceivedBytes uint32 `json:"received_bytes"`
	Buffer        string `json:"buffer"`
	CapturedBytes uint32 `json:"captured_bytes"`
	PayloadHex    string `json:"payload_hex,omitempty"`
}

type NetCenterUDPDispatchCapture struct {
	PID        uint32                     `json:"pid"`
	ElapsedMS  int64                      `json:"elapsed_ms"`
	ModuleBase string                     `json:"module_base"`
	Patch      string                     `json:"patch"`
	TotalCalls uint32                     `json:"total_calls"`
	Truncated  bool                       `json:"truncated,omitempty"`
	Calls      []NetCenterUDPDispatchCall `json:"calls"`
	Behavior   string                     `json:"behavior"`
}

type NetCenterUDPDispatchTracer struct {
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

func InstallNetCenterUDPDispatchTracer(pid uint32, timeout time.Duration) (*NetCenterUDPDispatchTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &NetCenterUDPDispatchTracer{pid: pid, process: process, started: time.Now()}
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
	tracer.patch = tracer.base + netCenterUDPDispatchPatchRVA
	tracer.original, _ = readRemote(process, tracer.patch, len(netCenterUDPDispatchSignature))
	if len(tracer.original) != len(netCenterUDPDispatchSignature) || !equalBytes(tracer.original, netCenterUDPDispatchSignature) {
		return nil, fmt.Errorf("NetCenter UDP dispatch signature mismatch at 0x%08X: got %X", tracer.patch, tracer.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x6000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx NetCenter UDP dispatch trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.counter = page + 0x800
	tracer.records = page + 0x1000
	stub := buildNetCenterUDPDispatchTraceStub(page, tracer.counter, tracer.records, tracer.base+netCenterUDPDispatchResumeRVA)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write NetCenter UDP dispatch trace stub: %w", err)
	}
	patch := relativeJumpPatch(tracer.patch, page, len(tracer.original))
	if !ensureRemoteBytes(process, tracer.patch, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch NetCenter UDP dispatcher at 0x%08X", tracer.patch)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), tracer.patch, uintptr(len(patch)))
	failed = false
	return tracer, nil
}

func buildNetCenterUDPDispatchTraceStub(stubAddress, counterAddress, recordsAddress, resumeAddress uintptr) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xB8, 0x01, 0, 0, 0)
	stub = append(stub, 0xF0, 0x0F, 0xC1, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(counterAddress))
	stub = append(stub, 0x3D)
	stub = binary.LittleEndian.AppendUint32(stub, netCenterUDPDispatchCapacity)
	stub = append(stub, 0x0F, 0x83, 0, 0, 0, 0)
	doneJump := len(stub) - 4
	stub = append(stub, 0x69, 0xC0)
	stub = binary.LittleEndian.AppendUint32(stub, netCenterUDPDispatchRecordSize)
	stub = append(stub, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordsAddress))

	stub = append(stub, 0x64, 0x8B, 0x15, 0x24, 0, 0, 0, 0x89, 0x10) // thread ID
	stub = append(stub, 0x8B, 0x54, 0x24, 0x0C, 0x83, 0xC2, 0x04)    // EDX = entry ESP
	stub = append(stub, 0x8B, 0x0A, 0x89, 0x48, 0x04)                // caller
	stub = append(stub, 0x8B, 0x4C, 0x24, 0x18, 0x89, 0x48, 0x08)    // manager / original ECX
	for _, field := range []struct{ source, target byte }{
		{0x04, 0x0C}, {0x08, 0x10}, {0x0C, 0x14}, {0x10, 0x18}, {0x14, 0x1C},
	} {
		stub = append(stub, 0x8B, 0x4A, field.source, 0x89, 0x48, field.target)
	}

	stub = append(stub, 0x8B, 0x4A, 0x10, 0x85, 0xC9, 0x0F, 0x8E, 0, 0, 0, 0)
	noCopyJump := len(stub) - 4
	stub = append(stub, 0x83, 0xF9, netCenterUDPDispatchPayload, 0x0F, 0x86, 0, 0, 0, 0)
	lengthOKJump := len(stub) - 4
	stub = append(stub, 0xB9, netCenterUDPDispatchPayload, 0, 0, 0)
	patchRelative32(stub, lengthOKJump, len(stub))
	stub = append(stub, 0x89, 0x48, 0x20, 0x8B, 0x72, 0x14, 0x85, 0xF6, 0x0F, 0x84, 0, 0, 0, 0)
	missingBufferJump := len(stub) - 4
	stub = append(stub, 0x8D, 0x78, 0x40, 0xFC, 0xF3, 0xA4)
	patchRelative32(stub, noCopyJump, len(stub))
	patchRelative32(stub, missingBufferJump, len(stub))
	patchRelative32(stub, doneJump, len(stub))

	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, netCenterUDPDispatchSignature...)
	stub = append(stub, 0xE9)
	jumpImmediate := len(stub)
	stub = binary.LittleEndian.AppendUint32(stub, 0)
	jumpNext := stubAddress + uintptr(len(stub))
	binary.LittleEndian.PutUint32(stub[jumpImmediate:], uint32(resumeAddress-jumpNext))
	return stub
}

func (tracer *NetCenterUDPDispatchTracer) Capture(duration time.Duration) NetCenterUDPDispatchCapture {
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

func (tracer *NetCenterUDPDispatchTracer) capture() NetCenterUDPDispatchCapture {
	result := NetCenterUDPDispatchCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		ModuleBase: fmt.Sprintf("0x%08X", tracer.base), Patch: fmt.Sprintf("0x%08X", tracer.patch),
		Behavior: "transparent NetCenter accepted-datagram dispatcher trace; no packet mutation; original prologue restored on close",
	}
	counter, ok := readRemote(tracer.process, tracer.counter, 4)
	if !ok {
		return result
	}
	result.TotalCalls = binary.LittleEndian.Uint32(counter)
	count := result.TotalCalls
	if count > netCenterUDPDispatchCapacity {
		count = netCenterUDPDispatchCapacity
		result.Truncated = true
	}
	if count == 0 {
		return result
	}
	records, ok := readRemote(tracer.process, tracer.records, int(count)*netCenterUDPDispatchRecordSize)
	if !ok {
		return result
	}
	modules := snapshotRemoteModules(tracer.process)
	for index := uint32(0); index < count; index++ {
		record := records[int(index)*netCenterUDPDispatchRecordSize : int(index+1)*netCenterUDPDispatchRecordSize]
		caller := binary.LittleEndian.Uint32(record[4:8])
		call := NetCenterUDPDispatchCall{
			Index: index, ThreadID: binary.LittleEndian.Uint32(record[0:4]), Caller: fmt.Sprintf("0x%08X", caller),
			Manager: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12])), Status: binary.LittleEndian.Uint32(record[12:16]),
			RemoteText: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[16:20])), RemotePort: binary.LittleEndian.Uint32(record[20:24]),
			ReceivedBytes: binary.LittleEndian.Uint32(record[24:28]), Buffer: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[28:32])),
			CapturedBytes: binary.LittleEndian.Uint32(record[32:36]),
		}
		if call.CapturedBytes > netCenterUDPDispatchPayload {
			call.CapturedBytes = netCenterUDPDispatchPayload
		}
		call.PayloadHex = hex.EncodeToString(record[64 : 64+call.CapturedBytes])
		for _, module := range modules {
			if caller >= module.base && caller-module.base < module.size {
				call.CallerModule = module.name
				call.CallerRVA = fmt.Sprintf("0x%X", caller-module.base)
				break
			}
		}
		result.Calls = append(result.Calls, call)
	}
	return result
}

func (tracer *NetCenterUDPDispatchTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.patch != 0 && len(tracer.original) != 0 {
		ensureRemoteBytes(tracer.process, tracer.patch, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.patch, uintptr(len(tracer.original)))
	}
	// Keep the diagnostic page mapped after restoring the branch to avoid a
	// return race with a dispatch already executing in the transparent stub.
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

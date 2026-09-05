package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"
)

const (
	netCenterUDPDecodePatchRVA   = uintptr(0x204A4)
	netCenterUDPDecodeResumeRVA  = uintptr(0x204AB)
	netCenterUDPDecodeCapacity   = 128
	netCenterUDPDecodeRecordSize = 128
	netCenterUDPDecodePayload    = 64
)

var netCenterUDPDecodeSignature = []byte{
	0x83, 0x4D, 0xFC, 0xFF, // or [ebp-04],-1
	0x8D, 0x4D, 0xF0, // lea ecx,[ebp-10]
}

type NetCenterUDPDecodeCall struct {
	Index         uint32 `json:"index"`
	ThreadID      uint32 `json:"thread_id"`
	Caller        string `json:"caller"`
	CallerModule  string `json:"caller_module,omitempty"`
	CallerRVA     string `json:"caller_rva,omitempty"`
	DecodedBytes  uint32 `json:"decoded_bytes"`
	Destination   string `json:"destination"`
	RemoteText    string `json:"remote_text_pointer"`
	RemotePort    uint32 `json:"remote_port"`
	ReceiveFlags  uint32 `json:"receive_flags"`
	CapturedBytes uint32 `json:"captured_bytes"`
	PayloadHex    string `json:"payload_hex,omitempty"`
}

type NetCenterUDPDecodeCapture struct {
	PID        uint32                   `json:"pid"`
	ElapsedMS  int64                    `json:"elapsed_ms"`
	ModuleBase string                   `json:"module_base"`
	Patch      string                   `json:"patch"`
	TotalCalls uint32                   `json:"total_calls"`
	Truncated  bool                     `json:"truncated,omitempty"`
	Calls      []NetCenterUDPDecodeCall `json:"calls"`
	Behavior   string                   `json:"behavior"`
}

type NetCenterUDPDecodeTracer struct {
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

func InstallNetCenterUDPDecodeTracer(pid uint32, timeout time.Duration) (*NetCenterUDPDecodeTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &NetCenterUDPDecodeTracer{pid: pid, process: process, started: time.Now()}
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
	tracer.patch = tracer.base + netCenterUDPDecodePatchRVA
	tracer.original, _ = readRemote(process, tracer.patch, len(netCenterUDPDecodeSignature))
	if len(tracer.original) != len(netCenterUDPDecodeSignature) || !equalBytes(tracer.original, netCenterUDPDecodeSignature) {
		return nil, fmt.Errorf("NetCenter UDP decode signature mismatch at 0x%08X: got %X", tracer.patch, tracer.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x6000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx NetCenter UDP decode trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.counter = page + 0x800
	tracer.records = page + 0x1000
	stub := buildNetCenterUDPDecodeTraceStub(page, tracer.counter, tracer.records, tracer.base+netCenterUDPDecodeResumeRVA)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write NetCenter UDP decode trace stub: %w", err)
	}
	patch := relativeJumpPatch(tracer.patch, page, len(tracer.original))
	if !ensureRemoteBytes(process, tracer.patch, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch NetCenter UDP decoder at 0x%08X", tracer.patch)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), tracer.patch, uintptr(len(patch)))
	failed = false
	return tracer, nil
}

func buildNetCenterUDPDecodeTraceStub(stubAddress, counterAddress, recordsAddress, resumeAddress uintptr) []byte {
	stub := []byte{0x9C, 0x60}
	stub = append(stub, 0xB8, 0x01, 0, 0, 0)
	stub = append(stub, 0xF0, 0x0F, 0xC1, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(counterAddress))
	stub = append(stub, 0x3D)
	stub = binary.LittleEndian.AppendUint32(stub, netCenterUDPDecodeCapacity)
	stub = append(stub, 0x0F, 0x83, 0, 0, 0, 0)
	doneJump := len(stub) - 4
	stub = append(stub, 0x69, 0xC0)
	stub = binary.LittleEndian.AppendUint32(stub, netCenterUDPDecodeRecordSize)
	stub = append(stub, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordsAddress))

	stub = append(stub, 0x64, 0x8B, 0x15, 0x24, 0, 0, 0, 0x89, 0x10)    // thread ID
	stub = append(stub, 0x8B, 0x54, 0x24, 0x08)                         // decoder EBP
	stub = append(stub, 0x8B, 0x4A, 0x04, 0x89, 0x48, 0x04)             // caller
	stub = append(stub, 0x8B, 0x4C, 0x24, 0x04, 0x89, 0x48, 0x08)       // decoded ESI
	stub = append(stub, 0x8B, 0x4A, 0x08, 0x89, 0x48, 0x0C)             // destination
	stub = append(stub, 0x8B, 0x4A, 0x10, 0x8B, 0x09, 0x89, 0x48, 0x10) // CString data pointer
	stub = append(stub, 0x8B, 0x4A, 0x14, 0x8B, 0x09, 0x89, 0x48, 0x14) // decoded remote port
	stub = append(stub, 0x8B, 0x4A, 0x18, 0x89, 0x48, 0x18)             // receive flags

	stub = append(stub, 0x8B, 0x4C, 0x24, 0x04, 0x85, 0xC9, 0x0F, 0x8E, 0, 0, 0, 0)
	noCopyJump := len(stub) - 4
	stub = append(stub, 0x83, 0xF9, netCenterUDPDecodePayload, 0x0F, 0x86, 0, 0, 0, 0)
	lengthOKJump := len(stub) - 4
	stub = append(stub, 0xB9, netCenterUDPDecodePayload, 0, 0, 0)
	patchRelative32(stub, lengthOKJump, len(stub))
	stub = append(stub, 0x89, 0x48, 0x1C, 0x8B, 0x72, 0x08, 0x85, 0xF6, 0x0F, 0x84, 0, 0, 0, 0)
	missingDestinationJump := len(stub) - 4
	stub = append(stub, 0x8D, 0x78, 0x40, 0xFC, 0xF3, 0xA4)
	patchRelative32(stub, noCopyJump, len(stub))
	patchRelative32(stub, missingDestinationJump, len(stub))
	patchRelative32(stub, doneJump, len(stub))

	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, netCenterUDPDecodeSignature...)
	stub = append(stub, 0xE9)
	jumpImmediate := len(stub)
	stub = binary.LittleEndian.AppendUint32(stub, 0)
	jumpNext := stubAddress + uintptr(len(stub))
	binary.LittleEndian.PutUint32(stub[jumpImmediate:], uint32(resumeAddress-jumpNext))
	return stub
}

func (tracer *NetCenterUDPDecodeTracer) Capture(duration time.Duration) NetCenterUDPDecodeCapture {
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

func (tracer *NetCenterUDPDecodeTracer) capture() NetCenterUDPDecodeCapture {
	result := NetCenterUDPDecodeCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		ModuleBase: fmt.Sprintf("0x%08X", tracer.base), Patch: fmt.Sprintf("0x%08X", tracer.patch),
		Behavior: "transparent NetCenter UDP relay-decoder return trace; no packet mutation; original epilogue restored on close",
	}
	counter, ok := readRemote(tracer.process, tracer.counter, 4)
	if !ok {
		return result
	}
	result.TotalCalls = binary.LittleEndian.Uint32(counter)
	count := result.TotalCalls
	if count > netCenterUDPDecodeCapacity {
		count = netCenterUDPDecodeCapacity
		result.Truncated = true
	}
	if count == 0 {
		return result
	}
	records, ok := readRemote(tracer.process, tracer.records, int(count)*netCenterUDPDecodeRecordSize)
	if !ok {
		return result
	}
	modules := snapshotRemoteModules(tracer.process)
	for index := uint32(0); index < count; index++ {
		record := records[int(index)*netCenterUDPDecodeRecordSize : int(index+1)*netCenterUDPDecodeRecordSize]
		caller := binary.LittleEndian.Uint32(record[4:8])
		call := NetCenterUDPDecodeCall{
			Index: index, ThreadID: binary.LittleEndian.Uint32(record[0:4]), Caller: fmt.Sprintf("0x%08X", caller),
			DecodedBytes: binary.LittleEndian.Uint32(record[8:12]), Destination: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[12:16])),
			RemoteText: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[16:20])), RemotePort: binary.LittleEndian.Uint32(record[20:24]),
			ReceiveFlags: binary.LittleEndian.Uint32(record[24:28]), CapturedBytes: binary.LittleEndian.Uint32(record[28:32]),
		}
		if call.CapturedBytes > netCenterUDPDecodePayload {
			call.CapturedBytes = netCenterUDPDecodePayload
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

func (tracer *NetCenterUDPDecodeTracer) Close() {
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

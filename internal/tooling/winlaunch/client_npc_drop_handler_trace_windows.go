package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	clientNPCDropTraceAllocation = 0x4000
	clientNPCDropTraceStubStride = 0x100
	clientNPCDropTraceActive     = 0x1000
	clientNPCDropTraceRecords    = 0x2000
	clientNPCDropTraceRecordSize = 40
)

type clientNPCDropTraceSiteDefinition struct {
	name      string
	callRVA   uintptr
	targetRVA uintptr
}

var clientNPCDropTraceSiteDefinitions = []clientNPCDropTraceSiteDefinition{
	{name: "lookup_npc", callRVA: 0x1FD86F, targetRVA: 0x1A287F},
	{name: "membership_check", callRVA: 0x1FD8C5, targetRVA: 0x00A66E},
	{name: "player_count", callRVA: 0x1FD8D8, targetRVA: 0x1AA8E8},
	{name: "drop_header_log", callRVA: 0x1FD924, targetRVA: 0x1C81D7},
	{name: "factory_context", callRVA: 0x1FD94C, targetRVA: 0x1CB087},
	{name: "create_scene_item", callRVA: 0x1FD953, targetRVA: 0x1CB79A},
	{name: "drop_item_log", callRVA: 0x1FD974, targetRVA: 0x1C81D7},
	{name: "set_drop_origin", callRVA: 0x1FD985, targetRVA: 0x1CCE0A},
	{name: "register_scene_item", callRVA: 0x1FD996, targetRVA: 0x1D9A79},
	{name: "move_scene_item", callRVA: 0x1FD9B9, targetRVA: 0x1D9BBF},
	{name: "increment_item_stat", callRVA: 0x1FD9CC, targetRVA: 0x1A8C1B},
}

type ClientNPCDropHandlerStage struct {
	Name      string `json:"name"`
	CallSite  string `json:"call_site"`
	Target    string `json:"target"`
	Calls     uint32 `json:"calls"`
	ThreadID  uint32 `json:"thread_id,omitempty"`
	Caller    string `json:"caller,omitempty"`
	InputECX  string `json:"input_ecx,omitempty"`
	InputEAX  string `json:"input_eax,omitempty"`
	Argument1 string `json:"argument_1,omitempty"`
	Argument2 string `json:"argument_2,omitempty"`
	ResultEAX string `json:"result_eax,omitempty"`
	ResultEDX string `json:"result_edx,omitempty"`
}

type ClientNPCDropHandlerCapture struct {
	PID        uint32                      `json:"pid"`
	ElapsedMS  int64                       `json:"elapsed_ms"`
	Module     string                      `json:"module"`
	ModuleBase string                      `json:"module_base"`
	Active     uint32                      `json:"active_calls"`
	Stages     []ClientNPCDropHandlerStage `json:"stages"`
	Behavior   string                      `json:"behavior"`
}

type clientNPCDropTraceSite struct {
	definition clientNPCDropTraceSiteDefinition
	call       uintptr
	target     uintptr
	stub       uintptr
	record     uintptr
	original   []byte
	patch      []byte
}

type ClientNPCDropHandlerTracer struct {
	pid        uint32
	process    syscall.Handle
	started    time.Time
	moduleBase uintptr
	page       uintptr
	active     uintptr
	sites      []clientNPCDropTraceSite
}

// InstallClientNPCDropHandlerTracer wraps only the direct calls made by the
// native NOTIFY_NPC_DROPITEM process method. Each wrapper preserves registers,
// flags, arguments, return values, and the callee's original stack-cleanup
// convention. Close restores every exact five-byte CALL instruction.
func InstallClientNPCDropHandlerTracer(pid uint32, timeout time.Duration) (*ClientNPCDropHandlerTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ClientNPCDropHandlerTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	tracer.moduleBase, err = waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return nil, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, clientNPCDropTraceAllocation,
		memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx Client NPC-drop handler trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.active = page + clientNPCDropTraceActive

	for index, definition := range clientNPCDropTraceSiteDefinitions {
		site := clientNPCDropTraceSite{
			definition: definition,
			call:       tracer.moduleBase + definition.callRVA,
			target:     tracer.moduleBase + definition.targetRVA,
			stub:       page + uintptr(index*clientNPCDropTraceStubStride),
			record:     page + clientNPCDropTraceRecords + uintptr(index*clientNPCDropTraceRecordSize),
		}
		site.original, _ = readRemote(process, site.call, 5)
		if len(site.original) != 5 || site.original[0] != 0xE8 {
			return nil, fmt.Errorf("Client NPC-drop %s call signature mismatch at 0x%08X: got %X", definition.name, site.call, site.original)
		}
		actualTarget := uintptr(int64(site.call+5) + int64(int32(binary.LittleEndian.Uint32(site.original[1:5]))))
		if actualTarget != site.target {
			return nil, fmt.Errorf("Client NPC-drop %s target 0x%08X, want 0x%08X", definition.name, actualTarget, site.target)
		}
		stub := buildClientNPCDropCallTraceStub(site.stub, tracer.active, site.record, site.target)
		if len(stub) >= clientNPCDropTraceStubStride {
			return nil, fmt.Errorf("Client NPC-drop %s trace stub is unexpectedly large: %d", definition.name, len(stub))
		}
		if err := writeRemote(process, site.stub, stub); err != nil {
			return nil, fmt.Errorf("write Client NPC-drop %s trace stub: %w", definition.name, err)
		}
		site.patch = relativeCallPatch(site.call, site.stub)
		tracer.sites = append(tracer.sites, site)
	}
	procFlushInstruction.Call(uintptr(process), page, clientNPCDropTraceRecords)
	for index := range tracer.sites {
		site := &tracer.sites[index]
		if !ensureRemoteBytes(process, site.call, site.patch, pageExecuteReadWrite) {
			return nil, fmt.Errorf("patch Client NPC-drop %s call at 0x%08X", site.definition.name, site.call)
		}
		procFlushInstruction.Call(uintptr(process), site.call, uintptr(len(site.patch)))
	}
	failed = false
	return tracer, nil
}

func relativeCallPatch(source, target uintptr) []byte {
	patch := []byte{0xE8, 0, 0, 0, 0}
	displacement := int64(target) - int64(source+uintptr(len(patch)))
	binary.LittleEndian.PutUint32(patch[1:], uint32(int32(displacement)))
	return patch
}

func buildClientNPCDropCallTraceStub(stubAddress, active, record, target uintptr) []byte {
	stub := []byte{0x9C} // pushfd
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(active)) // active++
	stub = append(stub, 0x60)                                     // pushad
	stub = append(stub, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record)) // calls++
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4)) // thread ID
	fields := []struct {
		stackOffset  byte
		recordOffset uintptr
	}{
		{0x24, 8},  // original call-site return address
		{0x18, 12}, // input ECX
		{0x1C, 16}, // input EAX
		{0x28, 20}, // argument 1
		{0x2C, 24}, // argument 2
	}
	for _, field := range fields {
		stub = append(stub, 0x8B, 0x44, 0x24, field.stackOffset, 0xA3)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(record+field.recordOffset))
	}
	stub = append(stub, 0x61, 0x9D) // popad; popfd
	stub = append(stub, 0x8F, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+36)) // remove and save caller return
	stub = append(stub, 0xE8, 0, 0, 0, 0)                            // call original target
	targetDisplacement := len(stub) - 4
	stub = append(stub, 0x9C, 0x60) // preserve returned flags/registers
	stub = append(stub, 0x8B, 0x44, 0x24, 0x1C, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+28)) // result EAX
	stub = append(stub, 0x8B, 0x44, 0x24, 0x14, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+32)) // result EDX
	stub = append(stub, 0x61)                                        // popad
	stub = append(stub, 0xF0, 0xFF, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(active)) // active--
	stub = append(stub, 0x9D, 0xFF, 0x35)                         // popfd; push saved caller return
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+36))
	stub = append(stub, 0xC3) // ret to the original call site

	callNext := stubAddress + uintptr(targetDisplacement+4)
	binary.LittleEndian.PutUint32(stub[targetDisplacement:targetDisplacement+4], uint32(int32(int64(target)-int64(callNext))))
	return stub
}

// CaptureOne waits for the next NPC object lookup and then snapshots every
// synchronous downstream call made by that same drop handler invocation.
func (tracer *ClientNPCDropHandlerTracer) CaptureOne(duration time.Duration) ClientNPCDropHandlerCapture {
	if duration <= 0 {
		duration = 10 * time.Minute
	}
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.sites[0].record, 4)
		if ok && binary.LittleEndian.Uint32(record) != 0 {
			time.Sleep(100 * time.Millisecond)
			break
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return tracer.Snapshot()
}

func (tracer *ClientNPCDropHandlerTracer) Snapshot() ClientNPCDropHandlerCapture {
	result := ClientNPCDropHandlerCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(), Module: "Client.exe",
		ModuleBase: fmt.Sprintf("0x%08X", tracer.moduleBase),
		Behavior:   "one-shot native NOTIFY_NPC_DROPITEM call-chain trace; preserves registers, flags, arguments, return values, and each original calling convention; restores every exact five-byte CALL on close",
	}
	if active, ok := readRemote(tracer.process, tracer.active, 4); ok {
		result.Active = binary.LittleEndian.Uint32(active)
	}
	for _, site := range tracer.sites {
		stage := ClientNPCDropHandlerStage{
			Name: site.definition.name, CallSite: fmt.Sprintf("0x%08X", site.call), Target: fmt.Sprintf("0x%08X", site.target),
		}
		record, ok := readRemote(tracer.process, site.record, clientNPCDropTraceRecordSize)
		if ok {
			stage.Calls = binary.LittleEndian.Uint32(record[0:4])
			if stage.Calls != 0 {
				stage.ThreadID = binary.LittleEndian.Uint32(record[4:8])
				stage.Caller = formatTracePointer(record[8:12])
				stage.InputECX = formatTracePointer(record[12:16])
				stage.InputEAX = formatTracePointer(record[16:20])
				stage.Argument1 = formatTracePointer(record[20:24])
				stage.Argument2 = formatTracePointer(record[24:28])
				stage.ResultEAX = formatTracePointer(record[28:32])
				stage.ResultEDX = formatTracePointer(record[32:36])
			}
		}
		result.Stages = append(result.Stages, stage)
	}
	return result
}

func (tracer *ClientNPCDropHandlerTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	for index := len(tracer.sites) - 1; index >= 0; index-- {
		site := &tracer.sites[index]
		current, ok := readRemote(tracer.process, site.call, len(site.patch))
		if ok && equalBytes(current, site.patch) {
			ensureRemoteBytes(tracer.process, site.call, site.original, pageExecuteReadWrite)
			procFlushInstruction.Call(uintptr(tracer.process), site.call, uintptr(len(site.original)))
		}
	}
	if tracer.page != 0 {
		idle := false
		for attempt := 0; attempt < 100; attempt++ {
			active, ok := readRemote(tracer.process, tracer.active, 4)
			if !ok || binary.LittleEndian.Uint32(active) == 0 {
				idle = true
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if idle {
			procVirtualFreeEx.Call(uintptr(tracer.process), tracer.page, 0, memRelease)
		}
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

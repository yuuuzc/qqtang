package winlaunch

import (
	"encoding/binary"
	"fmt"
	"sort"
	"syscall"
	"time"
)

const (
	clientMessageDispatchRVA          = uintptr(0x1DD974)
	clientMessageHandlerLookupRVA     = uintptr(0x1DD98F)
	clientMessageDispatchCapacity     = 8192
	clientMessageDispatchRecordSize   = 44
	clientMessageDispatchRecordsStart = 0x1000
	clientMessageDispatchAllocation   = 0x5A000
)

var clientMessageDispatchSignature = []byte{
	0xFF, 0x74, 0x24, 0x04, // push dword ptr [esp+4]
	0xE8, 0x12, 0x00, 0x00, 0x00, // call Client+0x1DD98F
}

// ClientMessageDispatchCall is one observed invocation of Client's common
// decoded-message dispatcher. DispatchArgument deliberately remains opaque:
// live evidence shows it can be a small token/offset rather than a pointer.
type ClientMessageDispatchCall struct {
	Sequence                   uint32 `json:"sequence"`
	ThreadID                   uint32 `json:"thread_id"`
	MessageID                  uint32 `json:"message_id"`
	MessageHex                 string `json:"message_hex"`
	MessageName                string `json:"message_name,omitempty"`
	Direction                  string `json:"direction,omitempty"`
	Caller                     string `json:"caller"`
	Registry                   string `json:"registry"`
	DispatchArgument           string `json:"dispatch_argument"`
	Handler                    string `json:"handler,omitempty"`
	HandlerVTable              string `json:"handler_vtable,omitempty"`
	HandlerProcessMethod       string `json:"handler_process_method,omitempty"`
	HandlerProcessMethodRVA    string `json:"handler_process_method_rva,omitempty"`
	HandlerAcceptanceMethod    string `json:"handler_acceptance_method,omitempty"`
	HandlerAcceptanceMethodRVA string `json:"handler_acceptance_method_rva,omitempty"`
	Accepted                   bool   `json:"accepted"`
}

// ClientMessageDispatchRoute aggregates repeated calls that resolve to the
// same message ID and concrete handler method.
type ClientMessageDispatchRoute struct {
	MessageID                  uint32 `json:"message_id"`
	MessageHex                 string `json:"message_hex"`
	MessageName                string `json:"message_name,omitempty"`
	Direction                  string `json:"direction,omitempty"`
	HandlerVTable              string `json:"handler_vtable,omitempty"`
	HandlerProcessMethod       string `json:"handler_process_method,omitempty"`
	HandlerProcessMethodRVA    string `json:"handler_process_method_rva,omitempty"`
	HandlerAcceptanceMethod    string `json:"handler_acceptance_method,omitempty"`
	HandlerAcceptanceMethodRVA string `json:"handler_acceptance_method_rva,omitempty"`
	Calls                      uint32 `json:"calls"`
	AcceptedCalls              uint32 `json:"accepted_calls"`
}

type ClientMessageDispatchCapture struct {
	PID           uint32                       `json:"pid"`
	ElapsedMS     int64                        `json:"elapsed_ms"`
	Module        string                       `json:"module"`
	ModuleBase    string                       `json:"module_base"`
	Dispatcher    string                       `json:"dispatcher"`
	Lookup        string                       `json:"handler_lookup"`
	Capacity      uint32                       `json:"capacity"`
	TotalCalls    uint32                       `json:"total_calls"`
	RecordedCalls uint32                       `json:"recorded_calls"`
	DroppedCalls  uint32                       `json:"dropped_calls"`
	Routes        []ClientMessageDispatchRoute `json:"routes"`
	Calls         []ClientMessageDispatchCall  `json:"calls"`
	Behavior      string                       `json:"behavior"`
}

type ClientMessageDispatchTracer struct {
	pid            uint32
	process        syscall.Handle
	started        time.Time
	moduleBase     uintptr
	dispatcher     uintptr
	lookup         uintptr
	original       []byte
	patch          []byte
	page           uintptr
	counterAddress uintptr
	activeAddress  uintptr
	recordsAddress uintptr
}

// InstallClientMessageDispatchTracer replaces the complete nine-byte prefix
// of the common dispatcher with a semantically equivalent recorder. The stub
// performs the original handler lookup and vtable+8 acceptance call, while
// retaining the message ID, opaque caller argument, vtable+4 processing
// method, and vtable+8 acceptance method. Close restores the exact original
// prefix.
func InstallClientMessageDispatchTracer(pid uint32, timeout time.Duration) (*ClientMessageDispatchTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ClientMessageDispatchTracer{pid: pid, process: process, started: time.Now()}
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
	tracer.dispatcher = tracer.moduleBase + clientMessageDispatchRVA
	tracer.lookup = tracer.moduleBase + clientMessageHandlerLookupRVA
	tracer.original, _ = readRemote(process, tracer.dispatcher, len(clientMessageDispatchSignature))
	if !equalBytes(tracer.original, clientMessageDispatchSignature) {
		return nil, fmt.Errorf("Client message dispatcher signature mismatch at 0x%08X: got %X", tracer.dispatcher, tracer.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, clientMessageDispatchAllocation,
		memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx Client message-dispatch trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.counterAddress = page + 0x800
	tracer.activeAddress = page + 0x804
	tracer.recordsAddress = page + clientMessageDispatchRecordsStart
	stub := buildClientMessageDispatchTraceStub(page, tracer.counterAddress, tracer.activeAddress, tracer.recordsAddress, tracer.lookup)
	if clientMessageDispatchRecordsStart+clientMessageDispatchCapacity*clientMessageDispatchRecordSize > clientMessageDispatchAllocation {
		return nil, fmt.Errorf("Client message-dispatch trace allocation is too small")
	}
	if len(stub) >= 0x800 {
		return nil, fmt.Errorf("Client message-dispatch trace stub is unexpectedly large: %d", len(stub))
	}
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write Client message-dispatch trace stub: %w", err)
	}
	tracer.patch = relativeJumpPatch(tracer.dispatcher, page, len(tracer.original))
	if !ensureRemoteBytes(process, tracer.dispatcher, tracer.patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch Client message dispatcher at 0x%08X", tracer.dispatcher)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), tracer.dispatcher, uintptr(len(tracer.patch)))
	failed = false
	return tracer, nil
}

func buildClientMessageDispatchTraceStub(stubAddress, counterAddress, activeAddress, recordsAddress, lookup uintptr) []byte {
	stub := []byte{
		0x55,       // push ebp
		0x8B, 0xEC, // mov ebp,esp
		0x83, 0xEC, 0x04, // sub esp,4
		0x53, 0x56, 0x57, // push ebx; push esi; push edi
		0xF0, 0xFF, 0x05, // lock inc [active]
	}
	stub = binary.LittleEndian.AppendUint32(stub, uint32(activeAddress))
	stub = append(stub,
		0x8B, 0xF1, // mov esi,ecx (registry)
		0x8B, 0x7D, 0x08, // mov edi,[ebp+8] (message ID)
		0xB8, 0x01, 0, 0, 0, // mov eax,1
		0xF0, 0x0F, 0xC1, 0x05, // lock xadd [counter],eax
	)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(counterAddress))
	stub = append(stub, 0x3D)
	stub = binary.LittleEndian.AppendUint32(stub, clientMessageDispatchCapacity) // cmp eax,capacity
	stub = append(stub, 0x0F, 0x83, 0, 0, 0, 0)                                  // jae no-record
	noRecordJump := len(stub) - 4
	stub = append(stub, 0x69, 0xC0)
	stub = binary.LittleEndian.AppendUint32(stub, clientMessageDispatchRecordSize) // imul eax,eax,record-size
	stub = append(stub, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordsAddress))
	stub = append(stub,
		0x89, 0x45, 0xFC, // mov [ebp-4],eax
		0x89, 0x38, // mov [eax],edi
		0x8B, 0x55, 0x04, 0x89, 0x50, 0x04, // caller
		0x89, 0x70, 0x08, // registry
		0x8B, 0x55, 0x00, 0x8B, 0x52, 0x0C, 0x89, 0x50, 0x0C, // opaque caller argument
		0x64, 0x8B, 0x15, 0x24, 0, 0, 0, 0x89, 0x50, 0x1C, // thread ID
	)
	stub = append(stub, 0xE9, 0, 0, 0, 0) // jmp lookup
	lookupJump := len(stub) - 4
	noRecordOffset := len(stub)
	stub = append(stub, 0xC7, 0x45, 0xFC, 0, 0, 0, 0) // mov [ebp-4],0
	lookupOffset := len(stub)
	stub = append(stub,
		0xFF, 0x75, 0x08, // push [ebp+8]
		0x8B, 0xCE, // mov ecx,esi
		0xE8, 0, 0, 0, 0, // call lookup
	)
	lookupCall := len(stub) - 4
	stub = append(stub,
		0x8B, 0xD8, // mov ebx,eax
		0x8B, 0x55, 0xFC, // mov edx,[ebp-4]
		0x85, 0xD2, // test edx,edx
		0x74, 0, // jz route-ready
	)
	routeReadyJump := len(stub) - 1
	stub = append(stub,
		0x89, 0x5A, 0x10, // handler
		0x85, 0xDB, // test handler,handler
		0x74, 0, // jz route-ready
	)
	noHandlerRouteJump := len(stub) - 1
	stub = append(stub,
		0x8B, 0x0B, 0x89, 0x4A, 0x14, // vtable
		0x8B, 0x41, 0x04, 0x89, 0x42, 0x28, // vtable+4 process method
		0x8B, 0x49, 0x08, 0x89, 0x4A, 0x18, // method
	)
	routeReadyOffset := len(stub)
	patchShortJump(stub, routeReadyJump, routeReadyOffset)
	patchShortJump(stub, noHandlerRouteJump, routeReadyOffset)
	stub = append(stub,
		0x85, 0xDB, // test handler,handler
		0x75, 0, // jnz call-handler
	)
	callHandlerJump := len(stub) - 1
	stub = append(stub, 0x33, 0xC0, 0xEB, 0) // xor eax,eax; jmp finish
	finishJump := len(stub) - 1
	callHandlerOffset := len(stub)
	patchShortJump(stub, callHandlerJump, callHandlerOffset)
	stub = append(stub,
		0x8B, 0x13, // mov edx,[ebx]
		0x8B, 0xCB, // mov ecx,ebx
		0xFF, 0x52, 0x08, // call [edx+8]
	)
	finishOffset := len(stub)
	patchShortJump(stub, finishJump, finishOffset)
	stub = append(stub,
		0x8B, 0x55, 0xFC, // mov edx,[ebp-4]
		0x85, 0xD2, // test edx,edx
		0x74, 0, // jz return
	)
	returnJump := len(stub) - 1
	stub = append(stub,
		0x0F, 0xB6, 0xC8, // movzx ecx,al
		0x89, 0x4A, 0x20, // result
		0xC7, 0x42, 0x24, 0x01, 0, 0, 0, // commit=1
	)
	returnOffset := len(stub)
	patchShortJump(stub, returnJump, returnOffset)
	stub = append(stub, 0xF0, 0xFF, 0x0D) // lock dec [active]
	stub = binary.LittleEndian.AppendUint32(stub, uint32(activeAddress))
	stub = append(stub,
		0x5F, 0x5E, 0x5B, // pop edi; pop esi; pop ebx
		0x8B, 0xE5, 0x5D, // mov esp,ebp; pop ebp
		0xC2, 0x04, 0x00, // ret 4
	)

	binary.LittleEndian.PutUint32(stub[noRecordJump:noRecordJump+4], uint32(noRecordOffset-(noRecordJump+4)))
	binary.LittleEndian.PutUint32(stub[lookupJump:lookupJump+4], uint32(lookupOffset-(lookupJump+4)))
	callNext := stubAddress + uintptr(lookupCall+4)
	binary.LittleEndian.PutUint32(stub[lookupCall:lookupCall+4], uint32(int32(int64(lookup)-int64(callNext))))
	return stub
}

// TargetExited reports whether the traced client has terminated. It does not
// wait and is used by the command to persist snapshots while memory is still
// readable instead of attempting the first read after process teardown.
func (tracer *ClientMessageDispatchTracer) TargetExited() bool {
	if tracer == nil || tracer.process == 0 {
		return true
	}
	waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
	return waitResult == waitObject0
}

// Snapshot reads the records committed so far without changing the hook.
func (tracer *ClientMessageDispatchTracer) Snapshot() ClientMessageDispatchCapture {
	result := ClientMessageDispatchCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(), Module: "Client.exe",
		ModuleBase: fmt.Sprintf("0x%08X", tracer.moduleBase), Dispatcher: fmt.Sprintf("0x%08X", tracer.dispatcher),
		Lookup: fmt.Sprintf("0x%08X", tracer.lookup), Capacity: clientMessageDispatchCapacity,
		Behavior: "transparent common decoded-message dispatch trace; records vtable+4 processing and vtable+8 acceptance methods, invokes the original acceptance method, and restores the exact nine-byte prefix on close",
	}
	counter, ok := readRemote(tracer.process, tracer.counterAddress, 4)
	if !ok {
		return result
	}
	result.TotalCalls = binary.LittleEndian.Uint32(counter)
	count := result.TotalCalls
	if count > clientMessageDispatchCapacity {
		result.DroppedCalls = count - clientMessageDispatchCapacity
		count = clientMessageDispatchCapacity
	}
	if count == 0 {
		return result
	}
	records, ok := readRemote(tracer.process, tracer.recordsAddress, int(count)*clientMessageDispatchRecordSize)
	if !ok {
		return result
	}
	type routeKey struct {
		messageID uint32
		vtable    uint32
		process   uint32
		accept    uint32
	}
	routes := make(map[routeKey]*ClientMessageDispatchRoute)
	for index := uint32(0); index < count; index++ {
		record := records[index*clientMessageDispatchRecordSize : (index+1)*clientMessageDispatchRecordSize]
		if binary.LittleEndian.Uint32(record[36:40]) == 0 {
			continue
		}
		messageID := binary.LittleEndian.Uint32(record[0:4])
		vtable := binary.LittleEndian.Uint32(record[20:24])
		accept := binary.LittleEndian.Uint32(record[24:28])
		process := binary.LittleEndian.Uint32(record[40:44])
		call := ClientMessageDispatchCall{
			Sequence: index + 1, ThreadID: binary.LittleEndian.Uint32(record[28:32]),
			MessageID: messageID, MessageHex: fmt.Sprintf("0x%08X", messageID),
			Caller: formatTracePointer(record[4:8]), Registry: formatTracePointer(record[8:12]),
			DispatchArgument: formatTracePointer(record[12:16]), Handler: formatTracePointer(record[16:20]),
			HandlerVTable:           formatTracePointer(record[20:24]),
			HandlerProcessMethod:    formatTracePointer(record[40:44]),
			HandlerAcceptanceMethod: formatTracePointer(record[24:28]),
			Accepted:                binary.LittleEndian.Uint32(record[32:36]) != 0,
		}
		call.HandlerProcessMethodRVA = formatModuleRVA(tracer.moduleBase, process)
		call.HandlerAcceptanceMethodRVA = formatModuleRVA(tracer.moduleBase, accept)
		result.Calls = append(result.Calls, call)
		key := routeKey{messageID: messageID, vtable: vtable, process: process, accept: accept}
		route := routes[key]
		if route == nil {
			route = &ClientMessageDispatchRoute{
				MessageID: messageID, MessageHex: call.MessageHex,
				HandlerVTable:        call.HandlerVTable,
				HandlerProcessMethod: call.HandlerProcessMethod, HandlerProcessMethodRVA: call.HandlerProcessMethodRVA,
				HandlerAcceptanceMethod: call.HandlerAcceptanceMethod, HandlerAcceptanceMethodRVA: call.HandlerAcceptanceMethodRVA,
			}
			routes[key] = route
		}
		route.Calls++
		if call.Accepted {
			route.AcceptedCalls++
		}
	}
	result.RecordedCalls = uint32(len(result.Calls))
	for _, route := range routes {
		result.Routes = append(result.Routes, *route)
	}
	sort.Slice(result.Routes, func(i, j int) bool {
		if result.Routes[i].MessageID != result.Routes[j].MessageID {
			return result.Routes[i].MessageID < result.Routes[j].MessageID
		}
		return result.Routes[i].HandlerProcessMethod < result.Routes[j].HandlerProcessMethod
	})
	return result
}

func formatModuleRVA(moduleBase uintptr, address uint32) string {
	value := uintptr(address)
	if value < moduleBase || value >= moduleBase+0x1000000 {
		return ""
	}
	return fmt.Sprintf("0x%08X", value-moduleBase)
}

func (tracer *ClientMessageDispatchTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.dispatcher != 0 && len(tracer.original) != 0 {
		current, ok := readRemote(tracer.process, tracer.dispatcher, len(tracer.patch))
		if ok && equalBytes(current, tracer.patch) {
			ensureRemoteBytes(tracer.process, tracer.dispatcher, tracer.original, pageExecuteReadWrite)
			procFlushInstruction.Call(uintptr(tracer.process), tracer.dispatcher, uintptr(len(tracer.original)))
		}
	}
	if tracer.page != 0 {
		// Once the entry patch is restored no new calls can enter the stub. Give
		// a thread already between the JMP and the active increment one scheduler
		// turn, then free only after every in-flight call has returned.
		time.Sleep(20 * time.Millisecond)
		idle := false
		for attempt := 0; attempt < 100; attempt++ {
			active, ok := readRemote(tracer.process, tracer.activeAddress, 4)
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

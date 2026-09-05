package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"
)

const (
	qqtSectionCallbackTraceSlots       = 220
	qqtSectionCallbackTraceCapacity    = 8192
	qqtSectionCallbackTraceStackSize   = 104
	qqtSectionCallbackTracePointeeSize = 128
	qqtSectionCallbackTraceRecordSize  = qqtSectionCallbackTraceStackSize + qqtSectionCallbackTracePointeeSize
	qqtSectionCallbackThunkSize        = 10
	qqtSectionCallbackAllocationSize   = 0x1E0000
	qqtSectionCallbackRecordsOffset    = 0x3000
)

type QQTSectionCallbackTraceCall struct {
	ThreadID  uint32            `json:"thread_id"`
	Slot      uint32            `json:"slot"`
	Caller    string            `json:"caller"`
	Registers map[string]string `json:"registers"`
	Stack     []string          `json:"stack_arguments"`
	Pointee   string            `json:"pointee_hex,omitempty"`
}

type QQTSectionCallbackTraceCapture struct {
	PID            uint32                        `json:"pid"`
	ElapsedMS      int64                         `json:"elapsed_ms"`
	CallbackObject string                        `json:"callback_object"`
	OriginalVTable string                        `json:"original_vtable"`
	CloneVTable    string                        `json:"clone_vtable"`
	TotalCalls     uint32                        `json:"total_calls"`
	Calls          []QQTSectionCallbackTraceCall `json:"calls"`
	Behavior       string                        `json:"behavior"`
}

type QQTSectionCallbackTracer struct {
	pid            uint32
	process        syscall.Handle
	started        time.Time
	callbackObject uintptr
	originalVTable uintptr
	originalObject []byte
	page           uintptr
	cloneVTable    uintptr
	counterAddress uintptr
	recordsAddress uintptr
	slotCount      int
	pointeeSlot    uint32
	behavior       string
}

// ReadActiveQQTSectionCallbackTrace takes a read-only snapshot of the recorder
// installed by InstallQQTSectionCallbackTracer. It validates the cloned vtable
// layout before deriving any private addresses and never restores, frees or
// writes target-process memory; ownership remains with the installing tracer.
func ReadActiveQQTSectionCallbackTrace(pid uint32, timeout time.Duration) (QQTSectionCallbackTraceCapture, error) {
	if pid == 0 {
		return QQTSectionCallbackTraceCapture{}, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(0x0400|0x0010, false, pid)
	if err != nil {
		return QQTSectionCallbackTraceCapture{}, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)

	location, err := locateLobbyStage(process, pid, timeout)
	if err != nil {
		return QQTSectionCallbackTraceCapture{}, err
	}
	pointer, ok := readRemote(process, location.callbackObject, 4)
	if !ok || len(pointer) != 4 {
		return QQTSectionCallbackTraceCapture{}, fmt.Errorf("read callback object 0x%08X", location.callbackObject)
	}
	cloneVTable := uintptr(binary.LittleEndian.Uint32(pointer))
	methods, ok := readRemote(process, cloneVTable, qqtSectionCallbackTraceSlots*4)
	if !ok || len(methods) != qqtSectionCallbackTraceSlots*4 {
		return QQTSectionCallbackTraceCapture{}, fmt.Errorf("read active callback clone vtable at 0x%08X", cloneVTable)
	}
	page, err := activeQQTSectionCallbackTracePage(cloneVTable, methods)
	if err != nil {
		return QQTSectionCallbackTraceCapture{}, err
	}

	reader := &QQTSectionCallbackTracer{
		pid: pid, process: process, started: time.Now(), callbackObject: location.callbackObject,
		cloneVTable: cloneVTable, page: page, counterAddress: page + 0x2800,
		recordsAddress: page + qqtSectionCallbackRecordsOffset,
		slotCount:      qqtSectionCallbackTraceSlots,
		pointeeSlot:    72,
		behavior:       "read-only snapshot of active transparent QQTSection callback-vtable trace",
	}
	result := reader.capture()
	result.ElapsedMS = 0
	result.OriginalVTable = fmt.Sprintf("copy@0x%08X", page+0x2400)
	return result, nil
}

func activeQQTSectionCallbackTracePage(cloneVTable uintptr, methods []byte) (uintptr, error) {
	if cloneVTable < 0x10000+0x2000 {
		return 0, fmt.Errorf("callback clone vtable is invalid: 0x%08X", cloneVTable)
	}
	if len(methods) != qqtSectionCallbackTraceSlots*4 {
		return 0, fmt.Errorf("callback clone method bytes = %d, want %d", len(methods), qqtSectionCallbackTraceSlots*4)
	}
	page := cloneVTable - 0x2000
	for _, index := range []int{0, 1, qqtSectionCallbackTraceSlots - 1} {
		method := uintptr(binary.LittleEndian.Uint32(methods[index*4 : index*4+4]))
		want := page + uintptr(index*qqtSectionCallbackThunkSize)
		if method != want {
			return 0, fmt.Errorf("callback vtable is not an active trace clone: slot %d = 0x%08X, want 0x%08X", index, method, want)
		}
	}
	return page, nil
}

// InstallQQTSectionCallbackTracerForSlot installs the standard QQTSection
// callback recorder and copies the first argument only for the selected slot.
// Selecting the slot explicitly is important because different NetCenter
// envelopes decode to different native object layouts.
func InstallQQTSectionCallbackTracerForSlot(pid uint32, timeout time.Duration, pointeeSlot uint32) (*QQTSectionCallbackTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	if pointeeSlot >= qqtSectionCallbackTraceSlots {
		return nil, fmt.Errorf("pointee slot %d is outside 0..%d", pointeeSlot, qqtSectionCallbackTraceSlots-1)
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	location, err := locateLobbyStage(process, pid, timeout)
	if err != nil {
		syscall.CloseHandle(process)
		return nil, err
	}
	return installQQTSectionObjectTracer(pid, process, location.callbackObject,
		qqtSectionCallbackTraceSlots, pointeeSlot, 2,
		fmt.Sprintf("transparent per-object QQTSection callback-vtable trace; slot %d first-argument snapshot; original object pointer restored on close", pointeeSlot))
}

// InstallQQTSectionObjectTracer installs the same transparent vtable recorder
// on one explicitly identified 32-bit object. It is used after a separate
// read-only pointer check has established the object address and table size.
// No pointee copy is attempted because arbitrary method signatures are not
// assumed.
func InstallQQTSectionObjectTracer(pid uint32, object uintptr, slots int) (*QQTSectionCallbackTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	if object < 0x10000 || object > 0xffffffff {
		return nil, fmt.Errorf("object address is invalid: 0x%X", object)
	}
	if slots < 1 || slots > qqtSectionCallbackTraceSlots {
		return nil, fmt.Errorf("vtable slot count %d is outside 1..%d", slots, qqtSectionCallbackTraceSlots)
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	return installQQTSectionObjectTracer(pid, process, object, slots, ^uint32(0),
		0,
		"transparent explicit-object QQTSection vtable trace; original object pointer restored on close")
}

// InstallQQTSectionObjectTracerForArgument records one pointer argument for a
// selected method while preserving the exact original dispatch. Arguments are
// numbered from one, matching the method signature after the implicit this
// pointer. This keeps protocol diagnostics independent of guessed structures.
func InstallQQTSectionObjectTracerForArgument(pid uint32, object uintptr, slots int, pointeeSlot, pointeeArgument uint32) (*QQTSectionCallbackTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	if object < 0x10000 || object > 0xffffffff {
		return nil, fmt.Errorf("object address is invalid: 0x%X", object)
	}
	if slots < 1 || slots > qqtSectionCallbackTraceSlots {
		return nil, fmt.Errorf("vtable slot count %d is outside 1..%d", slots, qqtSectionCallbackTraceSlots)
	}
	if pointeeSlot >= uint32(slots) {
		return nil, fmt.Errorf("pointee slot %d is outside 0..%d", pointeeSlot, slots-1)
	}
	if pointeeArgument < 1 || pointeeArgument > 16 {
		return nil, fmt.Errorf("pointee argument %d is outside 1..16", pointeeArgument)
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	return installQQTSectionObjectTracer(pid, process, object, slots, pointeeSlot, pointeeArgument,
		fmt.Sprintf("transparent explicit-object QQTSection vtable trace; slot %d argument %d snapshot; original object pointer restored on close", pointeeSlot, pointeeArgument))
}

func installQQTSectionObjectTracer(pid uint32, process syscall.Handle, object uintptr, slots int, pointeeSlot, pointeeArgument uint32, behavior string) (*QQTSectionCallbackTracer, error) {
	tracer := &QQTSectionCallbackTracer{
		pid: pid, process: process, started: time.Now(), callbackObject: object,
		slotCount: slots, pointeeSlot: pointeeSlot, behavior: behavior,
	}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	tracer.originalObject, _ = readRemote(process, tracer.callbackObject, 4)
	if len(tracer.originalObject) != 4 {
		return nil, fmt.Errorf("read callback object 0x%08X", tracer.callbackObject)
	}
	tracer.originalVTable = uintptr(binary.LittleEndian.Uint32(tracer.originalObject))
	if tracer.originalVTable < 0x10000 {
		return nil, fmt.Errorf("callback vtable is invalid: 0x%08X", tracer.originalVTable)
	}
	originalMethods, ok := readRemote(process, tracer.originalVTable, slots*4)
	if !ok || len(originalMethods) != slots*4 {
		return nil, fmt.Errorf("read %d object methods at 0x%08X", slots, tracer.originalVTable)
	}
	for index := 0; index < slots; index++ {
		method := binary.LittleEndian.Uint32(originalMethods[index*4 : index*4+4])
		if method < 0x10000 {
			return nil, fmt.Errorf("callback method %d is invalid: 0x%08X", index, method)
		}
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, qqtSectionCallbackAllocationSize, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx QQTSection callback trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	commonAddress := page + 0x1000
	tracer.cloneVTable = page + 0x2000
	originalTable := page + 0x2400
	tracer.counterAddress = page + 0x2800
	tracer.recordsAddress = page + qqtSectionCallbackRecordsOffset

	thunks := buildQQTSectionCallbackTraceThunks(page, commonAddress, slots)
	common := buildQQTSectionCallbackTraceCommonForArgument(tracer.counterAddress, tracer.recordsAddress, originalTable, byte(pointeeSlot), byte(pointeeArgument))
	clone := make([]byte, slots*4)
	for index := 0; index < slots; index++ {
		binary.LittleEndian.PutUint32(clone[index*4:index*4+4], uint32(page+uintptr(index*qqtSectionCallbackThunkSize)))
	}
	if err := writeRemote(process, page, thunks); err != nil {
		return nil, fmt.Errorf("write callback trace thunks: %w", err)
	}
	if err := writeRemote(process, commonAddress, common); err != nil {
		return nil, fmt.Errorf("write callback trace common stub: %w", err)
	}
	if err := writeRemote(process, tracer.cloneVTable, clone); err != nil {
		return nil, fmt.Errorf("write callback clone vtable: %w", err)
	}
	if err := writeRemote(process, originalTable, originalMethods); err != nil {
		return nil, fmt.Errorf("write callback original table: %w", err)
	}
	clonePointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(clonePointer, uint32(tracer.cloneVTable))
	if !ensureRemoteBytes(process, tracer.callbackObject, clonePointer, pageReadWrite) {
		return nil, fmt.Errorf("patch callback object 0x%08X", tracer.callbackObject)
	}
	procFlushInstruction.Call(uintptr(process), page, qqtSectionCallbackAllocationSize)

	failed = false
	return tracer, nil
}

func buildQQTSectionCallbackTraceThunks(page, commonAddress uintptr, slots int) []byte {
	result := make([]byte, 0, slots*qqtSectionCallbackThunkSize)
	for index := 0; index < slots; index++ {
		thunkAddress := page + uintptr(index*qqtSectionCallbackThunkSize)
		result = append(result, 0x68)
		result = binary.LittleEndian.AppendUint32(result, uint32(index))
		result = append(result, 0xE9)
		next := thunkAddress + qqtSectionCallbackThunkSize
		result = binary.LittleEndian.AppendUint32(result, uint32(commonAddress-next))
	}
	return result
}

func buildQQTSectionCallbackTraceCommon(counterAddress, recordsAddress, originalTable uintptr, pointeeSlot byte) []byte {
	// Preserve the historical recorder layout. Its existing users select the
	// second explicit argument; new diagnostics should call the argument-aware
	// builder below instead of relying on this compatibility wrapper.
	return buildQQTSectionCallbackTraceCommonForArgument(counterAddress, recordsAddress, originalTable, pointeeSlot, 2)
}

func buildQQTSectionCallbackTraceCommonForArgument(counterAddress, recordsAddress, originalTable uintptr, pointeeSlot, pointeeArgument byte) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xB8, 0x01, 0x00, 0x00, 0x00)
	stub = append(stub, 0xF0, 0x0F, 0xC1, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(counterAddress))
	stub = append(stub, 0x3D) // cmp eax, imm32
	stub = binary.LittleEndian.AppendUint32(stub, qqtSectionCallbackTraceCapacity)
	stub = append(stub, 0x0F, 0x83, 0, 0, 0, 0) // jae dispatch
	dispatchDisplacement := len(stub) - 4
	stub = append(stub, 0x69, 0xC0) // imul eax, eax, imm32
	stub = binary.LittleEndian.AppendUint32(stub, qqtSectionCallbackTraceRecordSize)
	stub = append(stub, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(recordsAddress))
	stub = append(stub, 0x64, 0x8B, 0x15, 0x24, 0, 0, 0) // edx=thread ID
	stub = append(stub, 0x89, 0x10)
	// Injected slot and original caller follow the saved pushad/pushfd frame.
	stub = append(stub, 0x8B, 0x54, 0x24, 0x24, 0x89, 0x50, 0x04)
	stub = append(stub, 0x8B, 0x54, 0x24, 0x28, 0x89, 0x50, 0x08)
	registerStackOffsets := []byte{0x00, 0x04, 0x08, 0x10, 0x14, 0x18, 0x1C}
	for index, stackOffset := range registerStackOffsets {
		recordOffset := byte(0x0C + index*4)
		stub = append(stub, 0x8B, 0x54, 0x24, stackOffset)
		stub = append(stub, 0x89, 0x50, recordOffset)
	}
	for index := 0; index < 16; index++ {
		stackOffset := byte(0x2C + index*4)
		recordOffset := byte(0x28 + index*4)
		stub = append(stub, 0x8B, 0x54, 0x24, stackOffset)
		stub = append(stub, 0x89, 0x50, recordOffset)
	}
	// The selected callback's first argument points at its decoded native
	// object. Capture a bounded snapshot while the pointer is live so protocol
	// work is based on the client's actual input rather than guessed layouts.
	stub = append(stub, 0x83, 0x7C, 0x24, 0x24, pointeeSlot) // cmp dword ptr [esp+36], slot
	stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)              // jne skip pointee
	skipSlotDisplacement := len(stub) - 4
	argumentStackOffset := byte(0x28 + 4*pointeeArgument)
	stub = append(stub, 0x8B, 0x74, 0x24, argumentStackOffset) // mov esi, [esp+offset] (selected argument)
	stub = append(stub, 0x85, 0xF6)                            // test esi, esi
	stub = append(stub, 0x0F, 0x84, 0, 0, 0, 0)                // je skip pointee
	skipNullDisplacement := len(stub) - 4
	stub = append(stub, 0x8D, 0x78, qqtSectionCallbackTraceStackSize) // lea edi, [eax+104]
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, qqtSectionCallbackTracePointeeSize/4)
	stub = append(stub, 0xF3, 0xA5) // rep movsd
	pointeeEnd := len(stub)
	binary.LittleEndian.PutUint32(stub[skipSlotDisplacement:skipSlotDisplacement+4], uint32(pointeeEnd-(skipSlotDisplacement+4)))
	binary.LittleEndian.PutUint32(stub[skipNullDisplacement:skipNullDisplacement+4], uint32(pointeeEnd-(skipNullDisplacement+4)))
	binary.LittleEndian.PutUint32(stub[dispatchDisplacement:dispatchDisplacement+4], uint32(len(stub)-(dispatchDisplacement+4)))
	// Replace the injected slot at the original stack top with its method,
	// restore every register/flag, then RET into that method with the original
	// caller and arguments untouched.
	stub = append(stub, 0x8B, 0x54, 0x24, 0x24) // edx=[esp+36] slot
	stub = append(stub, 0x8B, 0x14, 0x95)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(originalTable))
	stub = append(stub, 0x89, 0x54, 0x24, 0x24)
	stub = append(stub, 0x61, 0x9D, 0xC3) // popad; popfd; ret-to-original
	return stub
}

func (tracer *QQTSectionCallbackTracer) Capture(duration time.Duration) QQTSectionCallbackTraceCapture {
	if duration <= 0 {
		duration = 10 * time.Second
	}
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return tracer.capture()
}

func (tracer *QQTSectionCallbackTracer) capture() QQTSectionCallbackTraceCapture {
	result := QQTSectionCallbackTraceCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		CallbackObject: fmt.Sprintf("0x%08X", tracer.callbackObject),
		OriginalVTable: fmt.Sprintf("0x%08X", tracer.originalVTable),
		CloneVTable:    fmt.Sprintf("0x%08X", tracer.cloneVTable),
		Behavior:       tracer.behavior,
	}
	counter, ok := readRemote(tracer.process, tracer.counterAddress, 4)
	if !ok {
		return result
	}
	result.TotalCalls = binary.LittleEndian.Uint32(counter)
	count := result.TotalCalls
	if count > qqtSectionCallbackTraceCapacity {
		count = qqtSectionCallbackTraceCapacity
	}
	if count == 0 {
		return result
	}
	records, ok := readRemote(tracer.process, tracer.recordsAddress, int(count)*qqtSectionCallbackTraceRecordSize)
	if !ok {
		return result
	}
	registerNames := []string{"edi", "esi", "ebp", "ebx", "edx", "ecx", "eax"}
	for index := uint32(0); index < count; index++ {
		record := records[index*qqtSectionCallbackTraceRecordSize : (index+1)*qqtSectionCallbackTraceRecordSize]
		call := QQTSectionCallbackTraceCall{
			ThreadID:  binary.LittleEndian.Uint32(record[0:4]),
			Slot:      binary.LittleEndian.Uint32(record[4:8]),
			Caller:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12])),
			Registers: make(map[string]string),
		}
		for registerIndex, name := range registerNames {
			value := binary.LittleEndian.Uint32(record[12+registerIndex*4 : 16+registerIndex*4])
			call.Registers[name] = fmt.Sprintf("0x%08X", value)
		}
		for stackIndex := 0; stackIndex < 16; stackIndex++ {
			value := binary.LittleEndian.Uint32(record[40+stackIndex*4 : 44+stackIndex*4])
			call.Stack = append(call.Stack, fmt.Sprintf("0x%08X", value))
		}
		if call.Slot == tracer.pointeeSlot {
			call.Pointee = hex.EncodeToString(record[qqtSectionCallbackTraceStackSize:qqtSectionCallbackTraceRecordSize])
		}
		result.Calls = append(result.Calls, call)
	}
	return result
}

func (tracer *QQTSectionCallbackTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.callbackObject != 0 && len(tracer.originalObject) == 4 {
		current, ok := readRemote(tracer.process, tracer.callbackObject, 4)
		if ok && len(current) == 4 && uintptr(binary.LittleEndian.Uint32(current)) == tracer.cloneVTable {
			ensureRemoteBytes(tracer.process, tracer.callbackObject, tracer.originalObject, pageReadWrite)
		}
	}
	// Restoring the object pointer prevents new virtual calls from entering the
	// cloned table, but a different thread can already have loaded one of its
	// thunk addresses.  The recorder has no in-flight counter, so freeing this
	// page here creates a use-after-free window in the target process.  Keep the
	// diagnostic allocation alive; Windows reclaims it when Client.exe exits.
	// This mirrors the safety rule used by the QQTEncoder import tracer.
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

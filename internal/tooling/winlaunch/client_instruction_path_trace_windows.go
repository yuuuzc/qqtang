package winlaunch

import (
	"encoding/binary"
	"fmt"
	"sort"
	"syscall"
	"time"
	"unsafe"
)

const (
	clientInstructionPathCapacity   = 65536
	clientInstructionPathRecordSize = 48
	clientInstructionPathPageSize   = uintptr(0x400000)
	wow64ContextControl             = 0x00010001
)

type ClientInstructionPathEvent struct {
	Index    uint32 `json:"index"`
	EIP      string `json:"eip"`
	Module   string `json:"module,omitempty"`
	RVA      string `json:"rva,omitempty"`
	ESP      string `json:"esp"`
	EBP      string `json:"ebp"`
	EAX      string `json:"eax"`
	EBX      string `json:"ebx"`
	ECX      string `json:"ecx"`
	EDX      string `json:"edx"`
	ESI      string `json:"esi"`
	EDI      string `json:"edi"`
	EFlags   string `json:"eflags"`
	ThreadID uint32 `json:"thread_id"`
}

type ClientInstructionPathCapture struct {
	PID           uint32                       `json:"pid"`
	ThreadID      uint32                       `json:"thread_id"`
	EntryAddress  string                       `json:"entry_address"`
	ReturnAddress string                       `json:"return_address"`
	ElapsedMS     int64                        `json:"elapsed_ms"`
	TotalEvents   uint32                       `json:"total_events"`
	Completed     bool                         `json:"completed"`
	ReachedReturn bool                         `json:"reached_return"`
	CapacityLimit bool                         `json:"capacity_limit"`
	Events        []ClientInstructionPathEvent `json:"events"`
	Behavior      string                       `json:"behavior"`
}

type ClientInstructionPathTracer struct {
	pid, threadID uint32
	process       syscall.Handle
	thread        syscall.Handle
	started       time.Time
	entry, ret    uintptr
	page          uintptr
	counter       uintptr
	active        uintptr
	completed     uintptr
	stopRequested uintptr
	records       uintptr
	handlerHandle uintptr
	removeHandler uintptr
	original      clientDebugRegisters
	originalFlags uint32
}

func InstallClientInstructionPathTracer(pid, threadID uint32, entry, returnAddress uintptr, timeout time.Duration) (*ClientInstructionPathTracer, error) {
	if pid == 0 || threadID == 0 || entry < 0x10000 || returnAddress < 0x10000 {
		return nil, fmt.Errorf("pid, thread ID, entry and return address are required")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess|processCreateThread, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ClientInstructionPathTracer{pid: pid, threadID: threadID, process: process, started: time.Now(), entry: entry, ret: returnAddress}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()
	addHandler, err := findRemoteExport(process, pid, "NTDLL.dll", "RtlAddVectoredExceptionHandler", timeout, 0)
	if err != nil {
		return nil, err
	}
	tracer.removeHandler, err = findRemoteExport(process, pid, "NTDLL.dll", "RtlRemoveVectoredExceptionHandler", timeout, 0)
	if err != nil {
		return nil, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, clientInstructionPathPageSize, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx instruction path trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page, tracer.counter, tracer.active, tracer.completed, tracer.stopRequested, tracer.records = page, page+0x800, page+0x804, page+0x808, page+0x80C, page+0x1000
	handler := buildClientInstructionPathVEH(tracer.counter, tracer.active, tracer.completed, tracer.stopRequested, tracer.records, threadID, returnAddress)
	installerAddress := page + clientInstructionPathPageSize - 0x1000
	installer := []byte{0x68, 0, 0, 0, 0, 0x6A, 0x01, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0xC2, 0x04, 0x00}
	binary.LittleEndian.PutUint32(installer[1:5], uint32(page))
	binary.LittleEndian.PutUint32(installer[8:12], uint32(addHandler))
	if err := writeRemote(process, page, handler); err != nil {
		return nil, err
	}
	if err := writeRemote(process, installerAddress, installer); err != nil {
		return nil, err
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(handler)))
	registered, err := remoteCallOne(process, installerAddress, 0, timeout)
	if err != nil || registered == 0 {
		return nil, fmt.Errorf("install instruction path VEH: result 0x%X: %v", registered, err)
	}
	tracer.handlerHandle = uintptr(registered)
	if err := tracer.armThread(); err != nil {
		return nil, err
	}
	failed = false
	return tracer, nil
}

func buildClientInstructionPathVEH(counter, active, completed, stopRequested, records uintptr, threadID uint32, returnAddress uintptr) []byte {
	s := []byte{0x55, 0x8B, 0xEC, 0x9C, 0x60, 0x8B, 0x45, 0x08, 0x8B, 0x10}
	s = append(s, 0x81, 0x3A, 0x04, 0, 0, 0x80, 0x0F, 0x84, 0, 0, 0, 0)
	native := len(s) - 4
	s = append(s, 0x81, 0x3A, 0x1E, 0, 0, 0x40, 0x0F, 0x85, 0, 0, 0, 0)
	forward := len(s) - 4
	handle := len(s)
	patchNearJump(s, native, handle)
	s = append(s, 0x8B, 0x70, 0x04)          // esi=WOW64_CONTEXT
	s = append(s, 0x64, 0xA1, 0x24, 0, 0, 0) // eax=current TID
	// A close can race with a queued DR0 exception after the controller has
	// already restored DR6. Once stop is requested, consume any delayed
	// single-step from the exact target thread even if DR6 bit 0 is gone.
	s = append(s, 0x83, 0x3D)
	s = binary.LittleEndian.AppendUint32(s, uint32(stopRequested))
	s = append(s, 0x00, 0x0F, 0x84, 0, 0, 0, 0)
	stopNotRequested := len(s) - 4
	s = append(s, 0x3D)
	s = binary.LittleEndian.AppendUint32(s, threadID)
	s = append(s, 0x0F, 0x84, 0, 0, 0, 0)
	stopTargetThread := len(s) - 4
	stopRequestForward := len(s)
	patchNearJump(s, stopNotRequested, stopRequestForward)
	s = append(s, 0x8B, 0x0D)
	s = binary.LittleEndian.AppendUint32(s, uint32(active))
	s = append(s, 0x85, 0xC9, 0x0F, 0x85, 0, 0, 0, 0)
	activeJump := len(s) - 4
	// Inactive: only DR0 entry breakpoints start a trace.
	s = append(s, 0xF7, 0x46, 0x14, 0x01, 0, 0, 0, 0x0F, 0x84, 0, 0, 0, 0)
	notEntry := len(s) - 4
	s = append(s, 0x83, 0x3D)
	s = binary.LittleEndian.AppendUint32(s, uint32(completed))
	s = append(s, 0x00, 0x0F, 0x85, 0, 0, 0, 0)
	alreadyDone := len(s) - 4
	s = append(s, 0xA3)
	s = binary.LittleEndian.AppendUint32(s, uint32(active))
	s = append(s, 0xE9, 0, 0, 0, 0)
	recordFromEntry := len(s) - 4
	activeOffset := len(s)
	patchNearJump(s, activeJump, activeOffset)
	s = append(s, 0x3B, 0xC8, 0x0F, 0x85, 0, 0, 0, 0)
	notActiveThread := len(s) - 4
	s = append(s, 0x83, 0x3D)
	s = binary.LittleEndian.AppendUint32(s, uint32(stopRequested))
	s = append(s, 0x00, 0x0F, 0x85, 0, 0, 0, 0)
	stopRequestedJump := len(s) - 4
	recordOffset := len(s)
	patchNearJump(s, recordFromEntry, recordOffset)
	// Reserve a record with atomic xadd.
	s = append(s, 0xB8, 0x01, 0, 0, 0, 0xF0, 0x0F, 0xC1, 0x05)
	s = binary.LittleEndian.AppendUint32(s, uint32(counter))
	s = append(s, 0x3D)
	s = binary.LittleEndian.AppendUint32(s, clientInstructionPathCapacity)
	s = append(s, 0x0F, 0x83, 0, 0, 0, 0)
	capacityStop := len(s) - 4
	s = append(s, 0x69, 0xC0)
	s = binary.LittleEndian.AppendUint32(s, clientInstructionPathRecordSize)
	s = append(s, 0x05)
	s = binary.LittleEndian.AppendUint32(s, uint32(records))
	s = append(s, 0x8B, 0xF8)
	fields := []uint32{0xB8, 0xC4, 0xB4, 0xB0, 0xA4, 0xAC, 0xA8, 0xA0, 0x9C, 0xC0}
	for index, source := range fields {
		s = append(s, 0x8B, 0x86)
		s = binary.LittleEndian.AppendUint32(s, source)
		s = append(s, 0x89, 0x47, byte(index*4))
	}
	s = append(s, 0x64, 0xA1, 0x24, 0, 0, 0, 0x89, 0x47, 0x28)
	s = append(s, 0xC7, 0x47, 0x2C, 0x01, 0, 0, 0)
	s = append(s, 0x8B, 0x86)
	s = binary.LittleEndian.AppendUint32(s, 0xB8)
	s = append(s, 0x3D)
	s = binary.LittleEndian.AppendUint32(s, uint32(returnAddress))
	s = append(s, 0x0F, 0x84, 0, 0, 0, 0)
	returnStop := len(s) - 4
	// Clear DR6 and keep TF+RF set for exactly this active thread.
	s = append(s, 0xC7, 0x46, 0x14, 0, 0, 0, 0, 0x81, 0x8E)
	s = binary.LittleEndian.AppendUint32(s, 0xC0)
	s = binary.LittleEndian.AppendUint32(s, 0x00010100)
	s = append(s, 0xE9, 0, 0, 0, 0)
	continueHandled := len(s) - 4
	stopOffset := len(s)
	patchNearJump(s, capacityStop, stopOffset)
	patchNearJump(s, returnStop, stopOffset)
	patchNearJump(s, alreadyDone, stopOffset)
	patchNearJump(s, stopRequestedJump, stopOffset)
	patchNearJump(s, stopTargetThread, stopOffset)
	s = append(s, 0xC7, 0x05)
	s = binary.LittleEndian.AppendUint32(s, uint32(completed))
	s = append(s, 0x01, 0, 0, 0, 0xC7, 0x05)
	s = binary.LittleEndian.AppendUint32(s, uint32(active))
	s = append(s, 0, 0, 0, 0)
	s = append(s, 0x81, 0xA6)
	s = binary.LittleEndian.AppendUint32(s, 0xC0)
	s = binary.LittleEndian.AppendUint32(s, 0xFFFFFEFF)
	s = append(s, 0x81, 0x8E)
	s = binary.LittleEndian.AppendUint32(s, 0xC0)
	s = binary.LittleEndian.AppendUint32(s, 0x00010000)
	s = append(s, 0x81, 0x66, 0x18, 0xFE, 0xFF, 0xFF, 0xFF, 0xC7, 0x46, 0x14, 0, 0, 0, 0)
	handledOffset := len(s)
	patchNearJump(s, continueHandled, handledOffset)
	s = append(s, 0x61, 0x9D, 0x83, 0xC8, 0xFF, 0x5D, 0xC2, 0x04, 0)
	forwardOffset := len(s)
	for _, jump := range []int{forward, notEntry, notActiveThread} {
		patchNearJump(s, jump, forwardOffset)
	}
	s = append(s, 0x61, 0x9D, 0x33, 0xC0, 0x5D, 0xC2, 0x04, 0)
	return s
}

func (tracer *ClientInstructionPathTracer) armThread() error {
	const threadAccess = 0x0002 | 0x0008 | 0x0010 | 0x0040
	handle, _, openErr := procOpenThread.Call(threadAccess, 0, uintptr(tracer.threadID))
	if handle == 0 {
		return fmt.Errorf("OpenThread %d: %v", tracer.threadID, openErr)
	}
	tracer.thread = syscall.Handle(handle)
	previous, _, _ := procSuspendThread.Call(handle)
	if previous == ^uintptr(0) {
		return fmt.Errorf("SuspendThread %d", tracer.threadID)
	}
	defer procResumeThread.Call(handle)
	context := make([]byte, 716)
	binary.LittleEndian.PutUint32(context[0:4], wow64ContextControl|wow64ContextDebugRegisters)
	got, _, getErr := procWow64GetThreadContext.Call(handle, uintptr(unsafe.Pointer(&context[0])))
	if got == 0 {
		return fmt.Errorf("Wow64GetThreadContext %d: %v", tracer.threadID, getErr)
	}
	tracer.original = clientDebugRegisters{DR0: binary.LittleEndian.Uint32(context[4:8]), DR1: binary.LittleEndian.Uint32(context[8:12]), DR2: binary.LittleEndian.Uint32(context[12:16]), DR3: binary.LittleEndian.Uint32(context[16:20]), DR6: binary.LittleEndian.Uint32(context[20:24]), DR7: binary.LittleEndian.Uint32(context[24:28])}
	tracer.originalFlags = binary.LittleEndian.Uint32(context[0xC0:0xC4])
	if tracer.original.DR7&1 != 0 {
		return fmt.Errorf("thread %d already uses DR0", tracer.threadID)
	}
	binary.LittleEndian.PutUint32(context[4:8], uint32(tracer.entry))
	binary.LittleEndian.PutUint32(context[20:24], 0)
	binary.LittleEndian.PutUint32(context[24:28], tracer.original.DR7|1)
	set, _, setErr := procWow64SetThreadContext.Call(handle, uintptr(unsafe.Pointer(&context[0])))
	if set == 0 {
		return fmt.Errorf("Wow64SetThreadContext %d: %v", tracer.threadID, setErr)
	}
	return nil
}

func (tracer *ClientInstructionPathTracer) Capture(duration time.Duration) ClientInstructionPathCapture {
	if duration <= 0 {
		duration = 10 * time.Second
	}
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if value, ok := readRemote(tracer.process, tracer.completed, 4); ok && binary.LittleEndian.Uint32(value) != 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return tracer.capture()
}

func (tracer *ClientInstructionPathTracer) capture() ClientInstructionPathCapture {
	result := ClientInstructionPathCapture{PID: tracer.pid, ThreadID: tracer.threadID, EntryAddress: fmt.Sprintf("0x%08X", tracer.entry), ReturnAddress: fmt.Sprintf("0x%08X", tracer.ret), ElapsedMS: time.Since(tracer.started).Milliseconds(), Behavior: "hardware entry breakpoint followed by thread-scoped x86 TF tracing until the declared return address; target instructions remain unchanged"}
	if value, ok := readRemote(tracer.process, tracer.completed, 4); ok {
		result.Completed = binary.LittleEndian.Uint32(value) != 0
	}
	value, ok := readRemote(tracer.process, tracer.counter, 4)
	if !ok {
		return result
	}
	result.TotalEvents = binary.LittleEndian.Uint32(value)
	result.CapacityLimit = result.TotalEvents >= clientInstructionPathCapacity
	count := result.TotalEvents
	if count > clientInstructionPathCapacity {
		count = clientInstructionPathCapacity
	}
	modules := snapshotRemoteModules(tracer.process)
	for index := uint32(0); index < count; index++ {
		record, valid := readRemote(tracer.process, tracer.records+uintptr(index)*clientInstructionPathRecordSize, clientInstructionPathRecordSize)
		if !valid || binary.LittleEndian.Uint32(record[44:48]) == 0 {
			continue
		}
		eip := binary.LittleEndian.Uint32(record[0:4])
		event := ClientInstructionPathEvent{Index: index, EIP: formatTracePointer(record[0:4]), ESP: formatTracePointer(record[4:8]), EBP: formatTracePointer(record[8:12]), EAX: formatTracePointer(record[12:16]), EBX: formatTracePointer(record[16:20]), ECX: formatTracePointer(record[20:24]), EDX: formatTracePointer(record[24:28]), ESI: formatTracePointer(record[28:32]), EDI: formatTracePointer(record[32:36]), EFlags: formatTracePointer(record[36:40]), ThreadID: binary.LittleEndian.Uint32(record[40:44])}
		for _, module := range modules {
			if eip >= module.base && eip-module.base < module.size {
				event.Module, event.RVA = module.name, fmt.Sprintf("0x%X", eip-module.base)
				break
			}
		}
		result.Events = append(result.Events, event)
		if uintptr(eip) == tracer.ret {
			result.ReachedReturn = true
		}
	}
	sort.SliceStable(result.Events, func(i, j int) bool { return result.Events[i].Index < result.Events[j].Index })
	return result
}

func (tracer *ClientInstructionPathTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	completedBeforeClose := tracer.remoteCompleted()
	if !completedBeforeClose {
		tracer.requestStop(250 * time.Millisecond)
	}
	threadRestored := true
	if tracer.thread != 0 {
		if completedBeforeClose {
			threadRestored = tracer.restoreThreadState(250 * time.Millisecond)
		}
		syscall.CloseHandle(tracer.thread)
		tracer.thread = 0
	}
	// A WOW64 thread can have a native-transition single-step pending even
	// after Wow64SetThreadContext restored its x86 TF/DR state. Only unregister
	// and free the VEH after the remote handler itself has acknowledged a stop.
	// Otherwise keep the bounded diagnostic page alive until process exit so a
	// delayed STATUS_SINGLE_STEP can still be consumed safely.
	safeToRelease := threadRestored && completedBeforeClose
	if safeToRelease && tracer.handlerHandle != 0 && tracer.removeHandler != 0 {
		_, _ = remoteCallOne(tracer.process, tracer.removeHandler, tracer.handlerHandle, 5*time.Second)
	}
	if safeToRelease && tracer.page != 0 {
		procVirtualFreeEx.Call(uintptr(tracer.process), tracer.page, 0, memRelease)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

func (tracer *ClientInstructionPathTracer) requestStop(timeout time.Duration) {
	if tracer.stopRequested == 0 || tracer.completed == 0 {
		return
	}
	if tracer.remoteCompleted() {
		return
	}
	stop := make([]byte, 4)
	binary.LittleEndian.PutUint32(stop, 1)
	if err := writeRemote(tracer.process, tracer.stopRequested, stop); err != nil {
		return
	}
	tracer.waitForRemoteCompletion(timeout)
}

func (tracer *ClientInstructionPathTracer) remoteCompleted() bool {
	completed, ok := readRemote(tracer.process, tracer.completed, 4)
	return ok && binary.LittleEndian.Uint32(completed) != 0
}

func (tracer *ClientInstructionPathTracer) waitForRemoteCompletion(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if tracer.remoteCompleted() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return tracer.remoteCompleted()
}

func (tracer *ClientInstructionPathTracer) restoreThreadState(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		previous, _, _ := procSuspendThread.Call(uintptr(tracer.thread))
		if previous == ^uintptr(0) {
			var exitCode uint32
			got, _, _ := procGetExitCodeThread.Call(uintptr(tracer.thread), uintptr(unsafe.Pointer(&exitCode)))
			return got != 0 && exitCode != 259
		}
		context := make([]byte, 716)
		binary.LittleEndian.PutUint32(context[0:4], wow64ContextControl|wow64ContextDebugRegisters)
		got, _, _ := procWow64GetThreadContext.Call(uintptr(tracer.thread), uintptr(unsafe.Pointer(&context[0])))
		if got != 0 {
			eip := uintptr(binary.LittleEndian.Uint32(context[0xB8:0xBC]))
			if eip < tracer.page || eip >= tracer.page+clientInstructionPathPageSize {
				binary.LittleEndian.PutUint32(context[4:8], tracer.original.DR0)
				binary.LittleEndian.PutUint32(context[8:12], tracer.original.DR1)
				binary.LittleEndian.PutUint32(context[12:16], tracer.original.DR2)
				binary.LittleEndian.PutUint32(context[16:20], tracer.original.DR3)
				binary.LittleEndian.PutUint32(context[20:24], tracer.original.DR6)
				binary.LittleEndian.PutUint32(context[24:28], tracer.original.DR7)
				flags := restoreInstructionPathEFlags(
					binary.LittleEndian.Uint32(context[0xC0:0xC4]), tracer.originalFlags,
				)
				binary.LittleEndian.PutUint32(context[0xC0:0xC4], flags)
				set, _, _ := procWow64SetThreadContext.Call(uintptr(tracer.thread), uintptr(unsafe.Pointer(&context[0])))
				procResumeThread.Call(uintptr(tracer.thread))
				return set != 0
			}
		}
		procResumeThread.Call(uintptr(tracer.thread))
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
}

func restoreInstructionPathEFlags(current, original uint32) uint32 {
	const traceAndResumeFlags = uint32(0x00010100)
	return current&^traceAndResumeFlags | original&traceAndResumeFlags
}

package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sort"
	"syscall"
	"time"
	"unsafe"
)

const (
	clientHardwareExecTraceCapacity   = 32
	clientHardwareExecTraceRecordSize = 0xC0
)

type ClientHardwareExecEvent struct {
	Index    uint32   `json:"index"`
	DR6      string   `json:"dr6"`
	EIP      string   `json:"eip"`
	Module   string   `json:"module,omitempty"`
	RVA      string   `json:"rva,omitempty"`
	ESP      string   `json:"esp"`
	EBP      string   `json:"ebp"`
	EAX      string   `json:"eax"`
	EBX      string   `json:"ebx"`
	ECX      string   `json:"ecx"`
	EDX      string   `json:"edx"`
	ESI      string   `json:"esi"`
	EDI      string   `json:"edi"`
	EFlags   string   `json:"eflags"`
	ThreadID uint32   `json:"thread_id"`
	Stack    []string `json:"stack"`
	CodeBase string   `json:"code_base,omitempty"`
	CodeHex  string   `json:"code_hex,omitempty"`
}

type ClientHardwareExecCapture struct {
	PID                       uint32                    `json:"pid"`
	ElapsedMS                 int64                     `json:"elapsed_ms"`
	FirstAddress              string                    `json:"first_address"`
	SecondAddress             string                    `json:"second_address"`
	ArmedThreadIDs            []uint32                  `json:"armed_thread_ids"`
	InitiallyArmedThreads     int                       `json:"initially_armed_threads"`
	ThreadsArmedDuringCapture int                       `json:"threads_armed_during_capture"`
	ThreadArmPasses           int                       `json:"thread_arm_passes"`
	TotalEvents               uint32                    `json:"total_events"`
	Events                    []ClientHardwareExecEvent `json:"events"`
	Behavior                  string                    `json:"behavior"`
}

type ClientHardwareExecTracer struct {
	pid                uint32
	process            syscall.Handle
	started            time.Time
	firstAddress       uintptr
	secondAddress      uintptr
	page               uintptr
	counter            uintptr
	records            uintptr
	handlerHandle      uintptr
	removeHandler      uintptr
	originalRegisters  map[uint32]clientDebugRegisters
	initiallyArmed     int
	armedDuringCapture int
	threadArmPasses    int
}

// InstallClientHardwareExecTracer installs x86 execution breakpoints without
// attaching a debugger or modifying the target instructions. It is intended
// for short, read-only control-flow probes in the legacy 32-bit client.
func InstallClientHardwareExecTracer(pid uint32, firstAddress, secondAddress uintptr, timeout time.Duration) (*ClientHardwareExecTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	if firstAddress < 0x10000 || secondAddress < 0x10000 || firstAddress > 0xffffffff || secondAddress > 0xffffffff {
		return nil, fmt.Errorf("probe addresses must be valid x86 pointers")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess|processCreateThread, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ClientHardwareExecTracer{
		pid: pid, process: process, started: time.Now(), firstAddress: firstAddress, secondAddress: secondAddress,
		originalRegisters: make(map[uint32]clientDebugRegisters),
	}
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
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x3000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx hardware execution trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.counter = page + 0x300
	tracer.records = page + 0x400
	handler := buildClientHardwareExecVEH(tracer.counter, tracer.records)
	installerAddress := page + 0x2800
	installer := []byte{0x68, 0, 0, 0, 0, 0x6A, 0x01, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0xC2, 0x04, 0x00}
	binary.LittleEndian.PutUint32(installer[1:5], uint32(page))
	binary.LittleEndian.PutUint32(installer[8:12], uint32(addHandler))
	if len(handler) >= 0x300 {
		return nil, fmt.Errorf("hardware execution VEH is unexpectedly large: %d", len(handler))
	}
	if err := writeRemote(process, page, handler); err != nil {
		return nil, fmt.Errorf("write hardware execution VEH: %w", err)
	}
	if err := writeRemote(process, installerAddress, installer); err != nil {
		return nil, fmt.Errorf("write hardware execution VEH installer: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(handler)))
	procFlushInstruction.Call(uintptr(process), installerAddress, uintptr(len(installer)))
	registered, err := remoteCallOne(process, installerAddress, 0, timeout)
	if err != nil {
		return nil, err
	}
	if registered == 0 {
		return nil, fmt.Errorf("RtlAddVectoredExceptionHandler returned NULL")
	}
	tracer.handlerHandle = uintptr(registered)
	armed, err := tracer.armCurrentThreads()
	if err != nil {
		return nil, err
	}
	if armed == 0 {
		return nil, fmt.Errorf("no Client.exe threads accepted x86 execution breakpoints")
	}
	tracer.initiallyArmed = armed
	failed = false
	return tracer, nil
}

func buildClientHardwareExecVEH(counter, records uintptr) []byte {
	stub := []byte{0x55, 0x8B, 0xEC, 0x9C, 0x60}
	stub = append(stub, 0x8B, 0x45, 0x08, 0x8B, 0x10)
	stub = append(stub, 0x81, 0x3A, 0x04, 0x00, 0x00, 0x80, 0x0F, 0x84, 0, 0, 0, 0)
	nativeSingleStepJump := len(stub) - 4
	stub = append(stub, 0x81, 0x3A, 0x1E, 0x00, 0x00, 0x40, 0x0F, 0x85, 0, 0, 0, 0)
	forwardJump := len(stub) - 4
	handleOffset := len(stub)
	patchNearJump(stub, nativeSingleStepJump, handleOffset)
	stub = append(stub, 0x8B, 0x70, 0x04)
	stub = append(stub, 0x8B, 0x4E, 0x14, 0xF7, 0xC1, 0x03, 0, 0, 0, 0x0F, 0x84, 0, 0, 0, 0)
	notOurBreakpointJump := len(stub) - 4
	stub = append(stub, 0xB8, 0x01, 0, 0, 0, 0xF0, 0x0F, 0xC1, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(counter))
	stub = append(stub, 0x83, 0xE0, 0x1F, 0x69, 0xC0)
	stub = binary.LittleEndian.AppendUint32(stub, clientHardwareExecTraceRecordSize)
	stub = append(stub, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(records))
	stub = append(stub, 0x8B, 0xF8)
	stub = append(stub, 0x8B, 0x46, 0x14, 0x89, 0x47, 0x00)
	contextFields := []struct {
		source uint32
		dest   byte
	}{
		{0xB8, 0x04}, {0xC4, 0x08}, {0xB4, 0x0C}, {0xB0, 0x10}, {0xA4, 0x14},
		{0xAC, 0x18}, {0xA8, 0x1C}, {0xA0, 0x20}, {0x9C, 0x24}, {0xC0, 0x28},
	}
	for _, field := range contextFields {
		stub = append(stub, 0x8B, 0x86)
		stub = binary.LittleEndian.AppendUint32(stub, field.source)
		stub = append(stub, 0x89, 0x47, field.dest)
	}
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0x89, 0x47, 0x2C)
	stub = append(stub, 0x8B, 0xB6)
	stub = binary.LittleEndian.AppendUint32(stub, 0xC4)
	stub = append(stub, 0x83, 0xC7, 0x40, 0xB9, 0x20, 0, 0, 0, 0xFC, 0xF3, 0xA5)
	stub = append(stub, 0xC7, 0x47, 0xFC, 0x01, 0, 0, 0)
	stub = append(stub, 0x8B, 0x45, 0x08, 0x8B, 0x70, 0x04, 0xC7, 0x46, 0x14, 0, 0, 0, 0)
	// Execution breakpoints fire before the instruction. Set RF so returning
	// from the VEH executes that instruction once instead of immediately
	// trapping again at the same EIP.
	stub = append(stub, 0x81, 0x8E)
	stub = binary.LittleEndian.AppendUint32(stub, 0xC0)
	stub = binary.LittleEndian.AppendUint32(stub, 0x00010000)
	stub = append(stub, 0x61, 0x9D, 0x83, 0xC8, 0xFF, 0x5D, 0xC2, 0x04, 0x00)
	forwardOffset := len(stub)
	patchNearJump(stub, forwardJump, forwardOffset)
	patchNearJump(stub, notOurBreakpointJump, forwardOffset)
	stub = append(stub, 0x61, 0x9D, 0x33, 0xC0, 0x5D, 0xC2, 0x04, 0x00)
	return stub
}

// armCurrentThreads applies the same two execution breakpoints to target
// threads that appeared after the tracer was installed. Legacy QQTang creates
// decoder/UI worker threads during login and hall entry, so arming only the
// installation-time snapshot creates false negative traces. Every original
// debug-register set is retained and restored by Close.
func (tracer *ClientHardwareExecTracer) armCurrentThreads() (int, error) {
	const threadAccess = 0x0002 | 0x0008 | 0x0010 | 0x0040
	snapshot, _, snapshotErr := procCreateToolhelp32.Call(0x00000004, 0)
	if snapshot == ^uintptr(0) || snapshot == 0 {
		return 0, fmt.Errorf("CreateToolhelp32Snapshot threads: %v", snapshotErr)
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	entry := threadEntry32{Size: uint32(unsafe.Sizeof(threadEntry32{}))}
	success, _, firstErr := procThread32First.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	if success == 0 {
		return 0, fmt.Errorf("Thread32First: %v", firstErr)
	}
	armed := 0
	for {
		_, alreadyArmed := tracer.originalRegisters[entry.ThreadID]
		if entry.OwnerProcessID == tracer.pid && !alreadyArmed {
			handle, _, _ := procOpenThread.Call(threadAccess, 0, uintptr(entry.ThreadID))
			if handle != 0 {
				previous, _, _ := procSuspendThread.Call(handle)
				if previous != ^uintptr(0) {
					context := make([]byte, 716)
					binary.LittleEndian.PutUint32(context[0:4], wow64ContextDebugRegisters)
					got, _, _ := procWow64GetThreadContext.Call(handle, uintptr(unsafe.Pointer(&context[0])))
					if got != 0 {
						original := clientDebugRegisters{
							DR0: binary.LittleEndian.Uint32(context[4:8]), DR1: binary.LittleEndian.Uint32(context[8:12]),
							DR2: binary.LittleEndian.Uint32(context[12:16]), DR3: binary.LittleEndian.Uint32(context[16:20]),
							DR6: binary.LittleEndian.Uint32(context[20:24]), DR7: binary.LittleEndian.Uint32(context[24:28]),
						}
						if original.DR7&0x0000000F == 0 {
							tracer.originalRegisters[entry.ThreadID] = original
							binary.LittleEndian.PutUint32(context[4:8], uint32(tracer.firstAddress))
							binary.LittleEndian.PutUint32(context[8:12], uint32(tracer.secondAddress))
							binary.LittleEndian.PutUint32(context[20:24], 0)
							binary.LittleEndian.PutUint32(context[24:28], original.DR7|0x00000005)
							set, _, _ := procWow64SetThreadContext.Call(handle, uintptr(unsafe.Pointer(&context[0])))
							if set == 0 {
								delete(tracer.originalRegisters, entry.ThreadID)
							} else {
								armed++
							}
						}
					}
					procResumeThread.Call(handle)
				}
				syscall.CloseHandle(syscall.Handle(handle))
			}
		}
		entry.Size = uint32(unsafe.Sizeof(threadEntry32{}))
		success, _, _ = procThread32Next.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
		if success == 0 {
			break
		}
	}
	return armed, nil
}

func (tracer *ClientHardwareExecTracer) Capture(duration time.Duration) ClientHardwareExecCapture {
	if duration <= 0 {
		duration = 30 * time.Second
	}
	deadline := time.Now().Add(duration)
	var lastTotal uint32
	var lastChange time.Time
	nextArm := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
		if !time.Now().Before(nextArm) {
			tracer.threadArmPasses++
			if armed, err := tracer.armCurrentThreads(); err == nil {
				tracer.armedDuringCapture += armed
			}
			nextArm = time.Now().Add(50 * time.Millisecond)
		}
		if value, ok := readRemote(tracer.process, tracer.counter, 4); ok {
			total := binary.LittleEndian.Uint32(value)
			if total != lastTotal {
				lastTotal = total
				lastChange = time.Now()
			}
			if total >= 2 && time.Since(lastChange) >= 500*time.Millisecond {
				break
			}
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return tracer.capture()
}

func (tracer *ClientHardwareExecTracer) capture() ClientHardwareExecCapture {
	result := ClientHardwareExecCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		FirstAddress: fmt.Sprintf("0x%08X", tracer.firstAddress), SecondAddress: fmt.Sprintf("0x%08X", tracer.secondAddress),
		InitiallyArmedThreads: tracer.initiallyArmed, ThreadsArmedDuringCapture: tracer.armedDuringCapture,
		ThreadArmPasses: tracer.threadArmPasses,
		Behavior:        "records x86 hardware execution breakpoints on existing and newly created target threads without modifying target instructions or application state",
	}
	for threadID := range tracer.originalRegisters {
		result.ArmedThreadIDs = append(result.ArmedThreadIDs, threadID)
	}
	sort.Slice(result.ArmedThreadIDs, func(i, j int) bool { return result.ArmedThreadIDs[i] < result.ArmedThreadIDs[j] })
	counterBytes, ok := readRemote(tracer.process, tracer.counter, 4)
	if !ok {
		return result
	}
	result.TotalEvents = binary.LittleEndian.Uint32(counterBytes)
	retained := result.TotalEvents
	if retained > clientHardwareExecTraceCapacity {
		retained = clientHardwareExecTraceCapacity
	}
	start := result.TotalEvents - retained
	modules := snapshotRemoteModules(tracer.process)
	for index := start; index < result.TotalEvents; index++ {
		record, recordOK := readRemote(tracer.process, tracer.records+uintptr(index%clientHardwareExecTraceCapacity)*clientHardwareExecTraceRecordSize, clientHardwareExecTraceRecordSize)
		if !recordOK || binary.LittleEndian.Uint32(record[0xBC:0xC0]) == 0 {
			continue
		}
		eip := binary.LittleEndian.Uint32(record[4:8])
		event := ClientHardwareExecEvent{
			Index: index, DR6: formatTracePointer(record[0:4]), EIP: formatTracePointer(record[4:8]),
			ESP: formatTracePointer(record[8:12]), EBP: formatTracePointer(record[12:16]),
			EAX: formatTracePointer(record[16:20]), EBX: formatTracePointer(record[20:24]), ECX: formatTracePointer(record[24:28]),
			EDX: formatTracePointer(record[28:32]), ESI: formatTracePointer(record[32:36]), EDI: formatTracePointer(record[36:40]),
			EFlags: formatTracePointer(record[40:44]), ThreadID: binary.LittleEndian.Uint32(record[44:48]), Stack: make([]string, 0, 32),
		}
		for offset := 0x40; offset < 0xC0; offset += 4 {
			event.Stack = append(event.Stack, formatTracePointer(record[offset:offset+4]))
		}
		for _, module := range modules {
			if eip >= module.base && eip-module.base < module.size {
				event.Module = module.name
				event.RVA = fmt.Sprintf("0x%X", eip-module.base)
				break
			}
		}
		if eip >= 16 {
			codeBase := uintptr(eip - 16)
			if code, codeOK := readRemote(tracer.process, codeBase, 64); codeOK {
				event.CodeBase = fmt.Sprintf("0x%08X", codeBase)
				event.CodeHex = hex.EncodeToString(code)
			}
		}
		result.Events = append(result.Events, event)
	}
	return result
}

func (tracer *ClientHardwareExecTracer) restoreThreads() {
	const threadAccess = 0x0002 | 0x0008 | 0x0010 | 0x0040
	for threadID, original := range tracer.originalRegisters {
		handle, _, _ := procOpenThread.Call(threadAccess, 0, uintptr(threadID))
		if handle == 0 {
			continue
		}
		previous, _, _ := procSuspendThread.Call(handle)
		if previous != ^uintptr(0) {
			context := make([]byte, 716)
			binary.LittleEndian.PutUint32(context[0:4], wow64ContextDebugRegisters)
			binary.LittleEndian.PutUint32(context[4:8], original.DR0)
			binary.LittleEndian.PutUint32(context[8:12], original.DR1)
			binary.LittleEndian.PutUint32(context[12:16], original.DR2)
			binary.LittleEndian.PutUint32(context[16:20], original.DR3)
			binary.LittleEndian.PutUint32(context[20:24], original.DR6)
			binary.LittleEndian.PutUint32(context[24:28], original.DR7)
			procWow64SetThreadContext.Call(handle, uintptr(unsafe.Pointer(&context[0])))
			procResumeThread.Call(handle)
		}
		syscall.CloseHandle(syscall.Handle(handle))
	}
	tracer.originalRegisters = nil
}

func (tracer *ClientHardwareExecTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	tracer.restoreThreads()
	if tracer.handlerHandle != 0 && tracer.removeHandler != 0 {
		_, _ = remoteCallOne(tracer.process, tracer.removeHandler, tracer.handlerHandle, 5*time.Second)
	}
	if tracer.page != 0 {
		procVirtualFreeEx.Call(uintptr(tracer.process), tracer.page, 0, memRelease)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

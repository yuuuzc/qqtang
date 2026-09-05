package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

const (
	clientLossWriteTraceCapacity   = 16
	clientLossWriteTraceRecordSize = 0xC0
	wow64ContextDebugRegisters     = 0x00010010
)

type ClientLossWriteEvent struct {
	Index       uint32   `json:"index"`
	DR6         string   `json:"dr6"`
	EIP         string   `json:"eip"`
	Module      string   `json:"module,omitempty"`
	RVA         string   `json:"rva,omitempty"`
	ESP         string   `json:"esp"`
	EBP         string   `json:"ebp"`
	EAX         string   `json:"eax"`
	EBX         string   `json:"ebx"`
	ECX         string   `json:"ecx"`
	EDX         string   `json:"edx"`
	ESI         string   `json:"esi"`
	EDI         string   `json:"edi"`
	EFlags      string   `json:"eflags"`
	ThreadID    uint32   `json:"thread_id"`
	ProfileLoss uint32   `json:"profile_loss"`
	RoomLoss    uint32   `json:"room_loss"`
	Stack       []string `json:"stack"`
	CodeBase    string   `json:"code_base,omitempty"`
	CodeHex     string   `json:"code_hex,omitempty"`
}

type ClientLossWriteCapture struct {
	PID                uint32                 `json:"pid"`
	ElapsedMS          int64                  `json:"elapsed_ms"`
	ProfileLossAddress string                 `json:"profile_loss_address"`
	RoomLossAddress    string                 `json:"room_loss_address"`
	BaselineLoss       uint32                 `json:"baseline_loss"`
	ExactValue         *uint32                `json:"exact_value,omitempty"`
	ArmedThreadIDs     []uint32               `json:"armed_thread_ids"`
	TotalEvents        uint32                 `json:"total_events"`
	Events             []ClientLossWriteEvent `json:"events"`
	Behavior           string                 `json:"behavior"`
}

type clientDebugRegisters struct {
	DR0 uint32
	DR1 uint32
	DR2 uint32
	DR3 uint32
	DR6 uint32
	DR7 uint32
}

type ClientLossWriteTracer struct {
	pid                uint32
	process            syscall.Handle
	started            time.Time
	profileLossAddress uintptr
	roomLossAddress    uintptr
	baselineLoss       uint32
	exactValue         *uint32
	page               uintptr
	counter            uintptr
	records            uintptr
	handlerHandle      uintptr
	removeHandler      uintptr
	originalRegisters  map[uint32]clientDebugRegisters
}

// InstallClientLossWriteTracer uses two x86 data breakpoints and an in-process
// VEH to record the exact instructions that increase the two live LossNum
// projections. It neither changes the watched values nor attaches a debugger.
func InstallClientLossWriteTracer(pid uint32, profileLossAddress, roomLossAddress uintptr, baselineLoss uint32, timeout time.Duration) (*ClientLossWriteTracer, error) {
	return installClientLossWriteTracer(pid, profileLossAddress, roomLossAddress, baselineLoss, nil, timeout)
}

// InstallClientLossWriteTracerExact records only writes that leave either
// watched DWORD equal to exactValue. This avoids false positives when a reused
// stack slot temporarily contains unrelated larger values.
func InstallClientLossWriteTracerExact(pid uint32, firstAddress, secondAddress uintptr, exactValue uint32, timeout time.Duration) (*ClientLossWriteTracer, error) {
	value := exactValue
	return installClientLossWriteTracer(pid, firstAddress, secondAddress, 0, &value, timeout)
}

func installClientLossWriteTracer(pid uint32, profileLossAddress, roomLossAddress uintptr, baselineLoss uint32, exactValue *uint32, timeout time.Duration) (*ClientLossWriteTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	if profileLossAddress < 0x10000 || roomLossAddress < 0x10000 || profileLossAddress > 0xffffffff || roomLossAddress > 0xffffffff {
		return nil, fmt.Errorf("watched addresses must be valid x86 pointers")
	}
	if profileLossAddress&3 != 0 || roomLossAddress&3 != 0 {
		return nil, fmt.Errorf("watched addresses must be DWORD aligned")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess|processCreateThread, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &ClientLossWriteTracer{
		pid: pid, process: process, started: time.Now(), profileLossAddress: profileLossAddress,
		roomLossAddress: roomLossAddress, baselineLoss: baselineLoss, exactValue: exactValue,
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
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x2000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx Client LossNum write trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.counter = page + 0x300
	tracer.records = page + 0x400
	handler := buildClientLossWriteVEH(tracer.counter, tracer.records, profileLossAddress, roomLossAddress, baselineLoss, exactValue)
	installerAddress := page + 0x1800
	installer := []byte{0x68, 0, 0, 0, 0, 0x6A, 0x01, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0xC2, 0x04, 0x00}
	binary.LittleEndian.PutUint32(installer[1:5], uint32(page))
	binary.LittleEndian.PutUint32(installer[8:12], uint32(addHandler))
	if len(handler) >= 0x300 {
		return nil, fmt.Errorf("Client LossNum VEH is unexpectedly large: %d", len(handler))
	}
	if err := writeRemote(process, page, handler); err != nil {
		return nil, fmt.Errorf("write Client LossNum VEH: %w", err)
	}
	if err := writeRemote(process, installerAddress, installer); err != nil {
		return nil, fmt.Errorf("write Client LossNum VEH installer: %w", err)
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
	if err := tracer.armExistingThreads(); err != nil {
		return nil, err
	}
	failed = false
	return tracer, nil
}

func buildClientLossWriteVEH(counter, records, profileLossAddress, roomLossAddress uintptr, baselineLoss uint32, exactValue *uint32) []byte {
	stub := []byte{0x55, 0x8B, 0xEC, 0x9C, 0x60}      // frame; preserve flags and all registers
	stub = append(stub, 0x8B, 0x45, 0x08, 0x8B, 0x10) // eax=EXCEPTION_POINTERS; edx=EXCEPTION_RECORD
	stub = append(stub, 0x81, 0x3A, 0x04, 0x00, 0x00, 0x80, 0x0F, 0x84, 0, 0, 0, 0)
	nativeSingleStepJump := len(stub) - 4
	stub = append(stub, 0x81, 0x3A, 0x1E, 0x00, 0x00, 0x40, 0x0F, 0x85, 0, 0, 0, 0)
	forwardJump := len(stub) - 4
	handleOffset := len(stub)
	patchNearJump(stub, nativeSingleStepJump, handleOffset)
	stub = append(stub, 0x8B, 0x70, 0x04) // esi=WOW64_CONTEXT
	stub = append(stub, 0x8B, 0x4E, 0x14, 0xF7, 0xC1, 0x03, 0, 0, 0, 0x0F, 0x84, 0, 0, 0, 0)
	notOurBreakpointJump := len(stub) - 4
	stub = append(stub, 0xA1)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(profileLossAddress))
	stub = append(stub, 0x3D)
	comparisonValue := baselineLoss
	firstCondition := byte(0x87)  // ja: exceeds baseline
	secondCondition := byte(0x86) // jbe: neither value exceeds baseline
	if exactValue != nil {
		comparisonValue = *exactValue
		firstCondition = 0x84  // je: exact match
		secondCondition = 0x85 // jne: neither value is an exact match
	}
	stub = binary.LittleEndian.AppendUint32(stub, comparisonValue)
	stub = append(stub, 0x0F, firstCondition, 0, 0, 0, 0)
	recordProfileJump := len(stub) - 4
	stub = append(stub, 0xA1)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(roomLossAddress))
	stub = append(stub, 0x3D)
	stub = binary.LittleEndian.AppendUint32(stub, comparisonValue)
	stub = append(stub, 0x0F, secondCondition, 0, 0, 0, 0)
	continueJump := len(stub) - 4
	recordOffset := len(stub)
	patchNearJump(stub, recordProfileJump, recordOffset)
	stub = append(stub, 0xB8, 0x01, 0, 0, 0, 0xF0, 0x0F, 0xC1, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(counter)) // eax=old event index
	stub = append(stub, 0x83, 0xE0, 0x0F, 0x8D, 0x04, 0x40, 0xC1, 0xE0, 0x06, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(records)) // slot=(index&15)*0xc0+records
	stub = append(stub, 0x8B, 0xF8)                                // edi=slot
	stub = append(stub, 0x8B, 0x46, 0x14, 0x89, 0x47, 0x00)        // DR6
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
	stub = append(stub, 0xA1)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(profileLossAddress))
	stub = append(stub, 0x89, 0x47, 0x30, 0xA1)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(roomLossAddress))
	stub = append(stub, 0x89, 0x47, 0x34)
	stub = append(stub, 0x8B, 0xB6)
	stub = binary.LittleEndian.AppendUint32(stub, 0xC4) // esi=original ESP
	stub = append(stub, 0x83, 0xC7, 0x40, 0xB9, 0x20, 0, 0, 0, 0xFC, 0xF3, 0xA5)
	stub = append(stub, 0xC7, 0x47, 0xFC, 0x01, 0, 0, 0) // committed marker at original slot+0xbc
	continueOffset := len(stub)
	patchNearJump(stub, continueJump, continueOffset)
	stub = append(stub, 0x8B, 0x45, 0x08, 0x8B, 0x70, 0x04, 0xC7, 0x46, 0x14, 0, 0, 0, 0)
	stub = append(stub, 0x61, 0x9D, 0x83, 0xC8, 0xFF, 0x5D, 0xC2, 0x04, 0x00)
	forwardOffset := len(stub)
	patchNearJump(stub, forwardJump, forwardOffset)
	patchNearJump(stub, notOurBreakpointJump, forwardOffset)
	stub = append(stub, 0x61, 0x9D, 0x33, 0xC0, 0x5D, 0xC2, 0x04, 0x00)
	return stub
}

func (tracer *ClientLossWriteTracer) armExistingThreads() error {
	const threadAccess = 0x0002 | 0x0008 | 0x0010 | 0x0040 // suspend, get/set context, query
	snapshot, _, snapshotErr := procCreateToolhelp32.Call(0x00000004, 0)
	if snapshot == ^uintptr(0) || snapshot == 0 {
		return fmt.Errorf("CreateToolhelp32Snapshot threads: %v", snapshotErr)
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	entry := threadEntry32{Size: uint32(unsafe.Sizeof(threadEntry32{}))}
	success, _, firstErr := procThread32First.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	if success == 0 {
		return fmt.Errorf("Thread32First: %v", firstErr)
	}
	for {
		if entry.OwnerProcessID == tracer.pid {
			handle, _, _ := procOpenThread.Call(threadAccess, 0, uintptr(entry.ThreadID))
			if handle != 0 {
				thread := syscall.Handle(handle)
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
							binary.LittleEndian.PutUint32(context[4:8], uint32(tracer.profileLossAddress))
							binary.LittleEndian.PutUint32(context[8:12], uint32(tracer.roomLossAddress))
							binary.LittleEndian.PutUint32(context[20:24], 0)
							binary.LittleEndian.PutUint32(context[24:28], original.DR7|0x00DD0005)
							set, _, _ := procWow64SetThreadContext.Call(handle, uintptr(unsafe.Pointer(&context[0])))
							if set == 0 {
								delete(tracer.originalRegisters, entry.ThreadID)
							}
						}
					}
					procResumeThread.Call(handle)
				}
				syscall.CloseHandle(thread)
			}
		}
		entry.Size = uint32(unsafe.Sizeof(threadEntry32{}))
		success, _, _ = procThread32Next.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
		if success == 0 {
			break
		}
	}
	if len(tracer.originalRegisters) == 0 {
		return fmt.Errorf("no Client.exe threads accepted x86 data breakpoints")
	}
	return nil
}

func (tracer *ClientLossWriteTracer) Capture(duration time.Duration) ClientLossWriteCapture {
	if duration <= 0 {
		duration = 20 * time.Minute
	}
	deadline := time.Now().Add(duration)
	var lastTotal uint32
	var lastChange time.Time
	for time.Now().Before(deadline) {
		value, ok := readRemote(tracer.process, tracer.counter, 4)
		if ok {
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

func (tracer *ClientLossWriteTracer) capture() ClientLossWriteCapture {
	result := ClientLossWriteCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		ProfileLossAddress: fmt.Sprintf("0x%08X", tracer.profileLossAddress),
		RoomLossAddress:    fmt.Sprintf("0x%08X", tracer.roomLossAddress), BaselineLoss: tracer.baselineLoss,
		Behavior: "records x86 hardware write breakpoints only when a watched live LossNum exceeds the armed baseline; does not modify profile, room, transport, or SQLite data",
	}
	if tracer.exactValue != nil {
		value := *tracer.exactValue
		result.ExactValue = &value
		result.Behavior = "records x86 hardware write breakpoints only when either watched DWORD equals the configured exact value; does not modify process or persisted data"
	}
	for threadID := range tracer.originalRegisters {
		result.ArmedThreadIDs = append(result.ArmedThreadIDs, threadID)
	}
	counterBytes, ok := readRemote(tracer.process, tracer.counter, 4)
	if !ok {
		return result
	}
	result.TotalEvents = binary.LittleEndian.Uint32(counterBytes)
	retained := result.TotalEvents
	if retained > clientLossWriteTraceCapacity {
		retained = clientLossWriteTraceCapacity
	}
	start := result.TotalEvents - retained
	modules := snapshotRemoteModules(tracer.process)
	for index := start; index < result.TotalEvents; index++ {
		record, recordOK := readRemote(tracer.process, tracer.records+uintptr(index%clientLossWriteTraceCapacity)*clientLossWriteTraceRecordSize, clientLossWriteTraceRecordSize)
		if !recordOK || binary.LittleEndian.Uint32(record[0xBC:0xC0]) == 0 {
			continue
		}
		eip := binary.LittleEndian.Uint32(record[4:8])
		event := ClientLossWriteEvent{
			Index: index, DR6: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[0:4])),
			EIP: fmt.Sprintf("0x%08X", eip), ESP: formatTracePointer(record[8:12]), EBP: formatTracePointer(record[12:16]),
			EAX: formatTracePointer(record[16:20]), EBX: formatTracePointer(record[20:24]),
			ECX: formatTracePointer(record[24:28]), EDX: formatTracePointer(record[28:32]),
			ESI: formatTracePointer(record[32:36]), EDI: formatTracePointer(record[36:40]),
			EFlags: formatTracePointer(record[40:44]), ThreadID: binary.LittleEndian.Uint32(record[44:48]),
			ProfileLoss: binary.LittleEndian.Uint32(record[48:52]), RoomLoss: binary.LittleEndian.Uint32(record[52:56]),
			Stack: make([]string, 0, 32),
		}
		for offset := 0x40; offset < 0xC0; offset += 4 {
			event.Stack = append(event.Stack, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[offset:offset+4])))
		}
		for _, module := range modules {
			if eip >= module.base && eip-module.base < module.size {
				event.Module = module.name
				event.RVA = fmt.Sprintf("0x%X", eip-module.base)
				break
			}
		}
		if eip >= 32 {
			codeBase := uintptr(eip - 32)
			if code, codeOK := readRemote(tracer.process, codeBase, 96); codeOK {
				event.CodeBase = fmt.Sprintf("0x%08X", codeBase)
				event.CodeHex = hex.EncodeToString(code)
			}
		}
		result.Events = append(result.Events, event)
	}
	return result
}

func (tracer *ClientLossWriteTracer) restoreThreads() {
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

func (tracer *ClientLossWriteTracer) Close() {
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

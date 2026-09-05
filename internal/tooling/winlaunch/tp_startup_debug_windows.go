package winlaunch

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

type StartupDebugException struct {
	Index            uint32                   `json:"index"`
	ElapsedMS        int64                    `json:"elapsed_ms"`
	FirstChance      uint32                   `json:"first_chance"`
	ThreadID         uint32                   `json:"thread_id"`
	ExceptionCode    uint32                   `json:"exception_code"`
	ExceptionFlags   uint32                   `json:"exception_flags"`
	ExceptionAddress uint32                   `json:"exception_address"`
	ParameterCount   uint32                   `json:"parameter_count"`
	Parameter0       uint32                   `json:"parameter_0"`
	Parameter1       uint32                   `json:"parameter_1"`
	EIP              uint32                   `json:"eip"`
	ESP              uint32                   `json:"esp"`
	EBP              uint32                   `json:"ebp"`
	EAX              uint32                   `json:"eax"`
	EBX              uint32                   `json:"ebx"`
	ECX              uint32                   `json:"ecx"`
	EDX              uint32                   `json:"edx"`
	ESI              uint32                   `json:"esi"`
	EDI              uint32                   `json:"edi"`
	EFlags           uint32                   `json:"eflags"`
	Module           string                   `json:"module,omitempty"`
	RVA              uint32                   `json:"rva,omitempty"`
	AccessModule     string                   `json:"access_module,omitempty"`
	AccessRVA        uint32                   `json:"access_rva,omitempty"`
	AccessBase       uint32                   `json:"access_base,omitempty"`
	AccessAllocation uint32                   `json:"access_allocation_base,omitempty"`
	AccessSize       uint32                   `json:"access_region_size,omitempty"`
	AccessState      uint32                   `json:"access_state,omitempty"`
	AccessProtect    uint32                   `json:"access_protect,omitempty"`
	AccessType       uint32                   `json:"access_type,omitempty"`
	CodeAddress      uint32                   `json:"code_address,omitempty"`
	CodeHex          string                   `json:"code_hex,omitempty"`
	EAXMemoryAddress uint32                   `json:"eax_memory_address,omitempty"`
	EAXMemoryHex     string                   `json:"eax_memory_hex,omitempty"`
	EAXPointer       uint32                   `json:"eax_pointer,omitempty"`
	EAXPointerHex    string                   `json:"eax_pointer_hex,omitempty"`
	Stack            []uint32                 `json:"stack,omitempty"`
	StackCode        []StartupDebugCodeWindow `json:"stack_code,omitempty"`
	TPContextRecord  []uint32                 `json:"tp_context_record,omitempty"`
}

type StartupDebugCodeWindow struct {
	Address uint32 `json:"address"`
	Module  string `json:"module"`
	RVA     uint32 `json:"rva"`
	Hex     string `json:"hex"`
}

type startupDebugResult struct {
	exceptions []StartupDebugException
	exitCode   *int32
	errText    string
}

type startupDebugMonitor struct {
	ready         chan error
	stop          chan struct{}
	done          chan startupDebugResult
	once          sync.Once
	recordAddress uintptr
}

func startStartupDebugMonitor(process syscall.Handle, pid uint32, started time.Time, recordAddress uintptr) *startupDebugMonitor {
	monitor := &startupDebugMonitor{
		ready:         make(chan error, 1),
		stop:          make(chan struct{}),
		done:          make(chan startupDebugResult, 1),
		recordAddress: recordAddress,
	}
	go monitor.run(process, pid, started)
	return monitor
}

func (monitor *startupDebugMonitor) stopAndWait() startupDebugResult {
	monitor.once.Do(func() { close(monitor.stop) })
	return <-monitor.done
}

func (monitor *startupDebugMonitor) run(process syscall.Handle, pid uint32, started time.Time) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	result := startupDebugResult{}
	attached, _, attachErr := procDebugActiveProcess.Call(uintptr(pid))
	if attached == 0 {
		err := fmt.Errorf("DebugActiveProcess %d: %v", pid, attachErr)
		monitor.ready <- err
		result.errText = err.Error()
		monitor.done <- result
		return
	}
	procDebugSetProcessKillOnExit.Call(0)
	monitor.ready <- nil
	defer procDebugActiveProcessStop.Call(uintptr(pid))

	attachBreakpointSeen := false
	for {
		select {
		case <-monitor.stop:
			monitor.done <- result
			return
		default:
		}
		var event [debugEventBufferSize]byte
		waited, _, waitErr := procWaitForDebugEvent.Call(uintptr(unsafe.Pointer(&event[0])), 50)
		if waited == 0 {
			if errno, ok := waitErr.(syscall.Errno); ok && errno == 121 {
				continue
			}
			result.errText = fmt.Sprintf("WaitForDebugEvent: %v", waitErr)
			monitor.done <- result
			return
		}
		eventCode := binary.LittleEndian.Uint32(event[0:4])
		eventPID := binary.LittleEndian.Uint32(event[4:8])
		eventTID := binary.LittleEndian.Uint32(event[8:12])
		continueStatus := debugContinue
		processExited := false
		if eventCode == debugEventException {
			exceptionCode := binary.LittleEndian.Uint32(event[16:20])
			if exceptionCode == statusBreakpoint && !attachBreakpointSeen {
				attachBreakpointSeen = true
			} else if exceptionCode == 0xC0000008 {
				// NtClose raises STATUS_INVALID_HANDLE only while a debugger is
				// attached. A normal launch receives an error return instead, so
				// consume this debugger artifact and keep observing real faults.
				continueStatus = debugContinue
			} else {
				continueStatus = debugExceptionNotHandled
			}
			if capture, ok := captureStartupDebugException(process, eventTID, event[:], started, monitor.recordAddress); ok {
				capture.Index = uint32(len(result.exceptions))
				result.exceptions = append(result.exceptions, capture)
			}
		} else if eventCode == debugEventExitProcess {
			code := int32(binary.LittleEndian.Uint32(event[16:20]))
			result.exitCode = &code
			processExited = true
		}
		continued, _, continueErr := procContinueDebugEvent.Call(uintptr(eventPID), uintptr(eventTID), continueStatus)
		if continued == 0 {
			result.errText = fmt.Sprintf("ContinueDebugEvent code=%d tid=%d: %v", eventCode, eventTID, continueErr)
			monitor.done <- result
			return
		}
		if processExited {
			monitor.done <- result
			return
		}
	}
}

func captureStartupDebugException(
	process syscall.Handle,
	threadID uint32,
	event []byte,
	started time.Time,
	recordAddress uintptr,
) (StartupDebugException, bool) {
	if len(event) < debugEventBufferSize {
		return StartupDebugException{}, false
	}
	const threadAccess = 0x0008 | 0x0040
	thread, _, _ := procOpenThread.Call(threadAccess, 0, uintptr(threadID))
	if thread == 0 {
		return StartupDebugException{}, false
	}
	defer syscall.CloseHandle(syscall.Handle(thread))
	context := make([]byte, 716)
	binary.LittleEndian.PutUint32(context[0:4], wow64ContextControl|wow64ContextInteger)
	read, _, _ := procWow64GetThreadContext.Call(thread, uintptr(unsafe.Pointer(&context[0])))
	if read == 0 {
		return StartupDebugException{}, false
	}
	entry := StartupDebugException{
		ElapsedMS:        time.Since(started).Milliseconds(),
		FirstChance:      binary.LittleEndian.Uint32(event[168:172]),
		ThreadID:         threadID,
		ExceptionCode:    binary.LittleEndian.Uint32(event[16:20]),
		ExceptionFlags:   binary.LittleEndian.Uint32(event[20:24]),
		ExceptionAddress: uint32(binary.LittleEndian.Uint64(event[32:40])),
		ParameterCount:   binary.LittleEndian.Uint32(event[40:44]),
		Parameter0:       uint32(binary.LittleEndian.Uint64(event[48:56])),
		Parameter1:       uint32(binary.LittleEndian.Uint64(event[56:64])),
		EDI:              binary.LittleEndian.Uint32(context[wow64ContextEDI : wow64ContextEDI+4]),
		ESI:              binary.LittleEndian.Uint32(context[wow64ContextESI : wow64ContextESI+4]),
		EBX:              binary.LittleEndian.Uint32(context[wow64ContextEBX : wow64ContextEBX+4]),
		EDX:              binary.LittleEndian.Uint32(context[wow64ContextEDX : wow64ContextEDX+4]),
		ECX:              binary.LittleEndian.Uint32(context[wow64ContextECX : wow64ContextECX+4]),
		EAX:              binary.LittleEndian.Uint32(context[wow64ContextEAX : wow64ContextEAX+4]),
		EBP:              binary.LittleEndian.Uint32(context[0xB4:0xB8]),
		EIP:              binary.LittleEndian.Uint32(context[wow64ContextEIP : wow64ContextEIP+4]),
		EFlags:           binary.LittleEndian.Uint32(context[wow64ContextEFlags : wow64ContextEFlags+4]),
		ESP:              binary.LittleEndian.Uint32(context[wow64ContextESP : wow64ContextESP+4]),
	}
	if stack, ok := readRemote(process, uintptr(entry.ESP), 64*4); ok {
		entry.Stack = make([]uint32, 0, 64)
		for offset := 0; offset+4 <= len(stack); offset += 4 {
			entry.Stack = append(entry.Stack, binary.LittleEndian.Uint32(stack[offset:offset+4]))
		}
	}
	// Protected ClientBase code is unpacked only while it is running.  Preserve
	// the live instruction window at an exception instead of relying on the
	// encrypted bytes from the on-disk image.
	if entry.EIP >= 32 {
		entry.CodeAddress = entry.EIP - 32
		if code, ok := readRemote(process, uintptr(entry.CodeAddress), 96); ok {
			entry.CodeHex = fmt.Sprintf("%X", code)
		}
	}
	modules := snapshotRemoteModules(process)
	for _, module := range modules {
		if entry.EIP >= module.base && entry.EIP-module.base < module.size {
			entry.Module = module.name
			entry.RVA = entry.EIP - module.base
			break
		}
	}
	// At the legacy early-system-write boundary EAX points at the private TP
	// bootstrap that the old client attempts to publish through a writable
	// KERNELBASE PE-header field. Preserve that runtime-only page so the
	// compatibility layer can be derived from the actual handler instead of
	// guessing how the packed on-disk image used it.
	if entry.Module == "ClientBase.dll" && entry.RVA == uint32(tpEarlySystemWriteRVA) && entry.EAX >= 0x10000 {
		entry.EAXMemoryAddress = entry.EAX &^ 0xFFF
		if memory, ok := readRemote(process, uintptr(entry.EAXMemoryAddress), 0x1000); ok {
			entry.EAXMemoryHex = fmt.Sprintf("%X", memory)
			if len(memory) >= 4 {
				entry.EAXPointer = binary.LittleEndian.Uint32(memory[:4])
				if entry.EAXPointer >= 0x10000 {
					if pointee, pointeeOK := readRemote(process, uintptr(entry.EAXPointer&^0xFFF), 0x1000); pointeeOK {
						entry.EAXPointerHex = fmt.Sprintf("%X", pointee)
					}
				}
			}
		}
	}
	if entry.Module == "ClientBase.dll" && entry.RVA == uint32(tpEarlySystemScanRVA) {
		if instruction, ok := readRemote(process, uintptr(entry.EIP), 12); ok && len(instruction) == 12 && instruction[7] == 0xE9 {
			displacement := int32(binary.LittleEndian.Uint32(instruction[8:12]))
			target := uint32(int64(entry.EIP+12) + int64(displacement))
			if target >= 32 {
				address := target - 32
				if code, codeOK := readRemote(process, uintptr(address), 128); codeOK {
					entry.StackCode = append(entry.StackCode, StartupDebugCodeWindow{
						Address: address, Module: entry.Module, RVA: target - (entry.EIP - entry.RVA), Hex: fmt.Sprintf("%X", code),
					})
				}
			}
		}
	}
	if entry.ParameterCount >= 2 {
		var memory memoryBasicInformation
		if queried, _, _ := procVirtualQueryEx.Call(
			uintptr(process), uintptr(entry.Parameter1), uintptr(unsafe.Pointer(&memory)), unsafe.Sizeof(memory),
		); queried == unsafe.Sizeof(memory) {
			entry.AccessBase = uint32(memory.BaseAddress)
			entry.AccessAllocation = uint32(memory.AllocationBase)
			entry.AccessSize = uint32(memory.RegionSize)
			entry.AccessState = memory.State
			entry.AccessProtect = memory.Protect
			entry.AccessType = memory.Type
		}
		for _, module := range modules {
			if entry.Parameter1 >= module.base && entry.Parameter1-module.base < module.size {
				entry.AccessModule = module.name
				entry.AccessRVA = entry.Parameter1 - module.base
				break
			}
		}
	}
	seenCode := make(map[uint32]struct{})
	for _, value := range entry.Stack {
		for _, module := range modules {
			if value < module.base || value-module.base >= module.size {
				continue
			}
			if _, duplicate := seenCode[value]; duplicate {
				break
			}
			seenCode[value] = struct{}{}
			address := value
			if address >= module.base+32 {
				address -= 32
			}
			if code, ok := readRemote(process, uintptr(address), 96); ok {
				entry.StackCode = append(entry.StackCode, StartupDebugCodeWindow{
					Address: address, Module: module.name, RVA: value - module.base,
					Hex: fmt.Sprintf("%X", code),
				})
			}
			break
		}
	}
	if recordAddress != 0 {
		if record, ok := readRemote(process, recordAddress, 40); ok {
			for offset := 0; offset+4 <= len(record); offset += 4 {
				entry.TPContextRecord = append(entry.TPContextRecord, binary.LittleEndian.Uint32(record[offset:offset+4]))
			}
		}
	}
	return entry, true
}

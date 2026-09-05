package winlaunch

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	debugEventCreateThread = uint32(2)
	debugEventLoadDLL      = uint32(6)
)

var procGetFinalPathNameByHandle = kernel32.NewProc("GetFinalPathNameByHandleW")

type ClientModuleDebugCapture struct {
	PID           uint32                    `json:"pid"`
	ModuleName    string                    `json:"module_name"`
	ModuleBase    string                    `json:"module_base,omitempty"`
	FirstRVA      string                    `json:"first_rva"`
	SecondRVA     string                    `json:"second_rva"`
	FirstAddress  string                    `json:"first_address,omitempty"`
	SecondAddress string                    `json:"second_address,omitempty"`
	ElapsedMS     int64                     `json:"elapsed_ms"`
	ArmedThreads  []uint32                  `json:"armed_thread_ids"`
	TotalEvents   uint32                    `json:"total_events"`
	Events        []ClientHardwareExecEvent `json:"events"`
	Exceptions    []ClientDebugException    `json:"exceptions,omitempty"`
	ProcessExited bool                      `json:"process_exited"`
	Behavior      string                    `json:"behavior"`
}

type ClientDebugException struct {
	Code          string   `json:"code"`
	FirstChance   uint32   `json:"first_chance"`
	ThreadID      uint32   `json:"thread_id"`
	EIP           string   `json:"eip"`
	ESP           string   `json:"esp"`
	EBP           string   `json:"ebp"`
	EAX           string   `json:"eax"`
	EBX           string   `json:"ebx"`
	ECX           string   `json:"ecx"`
	EDX           string   `json:"edx"`
	ESI           string   `json:"esi"`
	EDI           string   `json:"edi"`
	EFlags        string   `json:"eflags"`
	AccessType    string   `json:"access_type,omitempty"`
	AccessAddress string   `json:"access_address,omitempty"`
	Module        string   `json:"module,omitempty"`
	RVA           string   `json:"rva,omitempty"`
	Stack         []string `json:"stack"`
}

// CaptureClientModuleDebugTrace uses the Windows debug DLL-load event as the
// synchronization boundary. This avoids treating a custom crash dialog's
// heuristic stack walk as proof and arms module-relative execution probes
// while the loader thread is still stopped on LOAD_DLL_DEBUG_EVENT.
func CaptureClientModuleDebugTrace(pid uint32, moduleName string, firstRVA, secondRVA uint32, duration time.Duration) (ClientModuleDebugCapture, error) {
	started := time.Now()
	result := ClientModuleDebugCapture{
		PID: pid, ModuleName: moduleName,
		FirstRVA: fmt.Sprintf("0x%X", firstRVA), SecondRVA: fmt.Sprintf("0x%X", secondRVA),
		Behavior: "controller-side debugger synchronized by LOAD_DLL_DEBUG_EVENT; records x86 hardware execution breakpoints before the target DLL loader thread resumes",
	}
	if pid == 0 || strings.TrimSpace(moduleName) == "" {
		return result, fmt.Errorf("pid and module name are required")
	}
	if duration <= 0 {
		duration = 60 * time.Second
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return result, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	attached, _, attachErr := procDebugActiveProcess.Call(uintptr(pid))
	if attached == 0 {
		return result, fmt.Errorf("DebugActiveProcess %d: %v", pid, attachErr)
	}
	procDebugSetProcessKillOnExit.Call(0)
	defer procDebugActiveProcessStop.Call(uintptr(pid))

	armed := make(map[uint32]struct{})
	var firstAddress, secondAddress uint32
	attachBreakpointSeen := false
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		var event [debugEventBufferSize]byte
		waited, _, waitErr := procWaitForDebugEvent.Call(uintptr(unsafe.Pointer(&event[0])), 100)
		if waited == 0 {
			if errno, ok := waitErr.(syscall.Errno); ok && errno == 121 {
				continue
			}
			return result, fmt.Errorf("WaitForDebugEvent: %v", waitErr)
		}
		eventCode := binary.LittleEndian.Uint32(event[0:4])
		eventPID := binary.LittleEndian.Uint32(event[4:8])
		eventTID := binary.LittleEndian.Uint32(event[8:12])
		continueStatus := debugContinue

		switch eventCode {
		case debugEventLoadDLL:
			if firstAddress == 0 {
				moduleBase, loadedName := debugLoadDLLIdentity(process, event[:])
				if moduleBase != 0 && strings.EqualFold(loadedName, moduleName) {
					firstAddress = moduleBase + firstRVA
					secondAddress = moduleBase + secondRVA
					result.ModuleBase = fmt.Sprintf("0x%08X", moduleBase)
					result.FirstAddress = fmt.Sprintf("0x%08X", firstAddress)
					result.SecondAddress = fmt.Sprintf("0x%08X", secondAddress)
					if err := armDebugExecBreakpoints(pid, firstAddress, secondAddress, armed); err != nil {
						procContinueDebugEvent.Call(uintptr(eventPID), uintptr(eventTID), debugContinue)
						return result, err
					}
				}
			} else {
				closeDebugLoadDLLFile(event[:])
			}
		case debugEventCreateThread:
			if firstAddress != 0 {
				_ = armDebugExecBreakpointThread(eventTID, firstAddress, secondAddress, armed)
			}
		case debugEventException:
			exceptionCode := binary.LittleEndian.Uint32(event[16:20])
			if exceptionCode == statusBreakpoint && !attachBreakpointSeen {
				attachBreakpointSeen = true
				break
			}
			if exceptionCode == statusAccessViolation {
				exception, captureErr := captureClientDebugException(process, event[:], eventTID)
				if captureErr != nil {
					procContinueDebugEvent.Call(uintptr(eventPID), uintptr(eventTID), debugExceptionNotHandled)
					return result, captureErr
				}
				for _, module := range snapshotRemoteModules(process) {
					eip := parseTracePointer32(exception.EIP)
					if eip >= module.base && eip-module.base < module.size {
						exception.Module = module.name
						exception.RVA = fmt.Sprintf("0x%X", eip-module.base)
						break
					}
				}
				result.Exceptions = append(result.Exceptions, exception)
			}
			if firstAddress != 0 && (exceptionCode == statusSingleStep || exceptionCode == wow64SingleStep) {
				eventRecord, matched, captureErr := captureDebugExecEvent(process, eventTID, firstAddress, secondAddress, result.TotalEvents)
				if captureErr != nil {
					procContinueDebugEvent.Call(uintptr(eventPID), uintptr(eventTID), debugExceptionNotHandled)
					return result, captureErr
				}
				if matched {
					result.TotalEvents++
					result.Events = append(result.Events, eventRecord)
					continueStatus = debugContinue
				} else {
					continueStatus = debugExceptionNotHandled
				}
			} else {
				continueStatus = debugExceptionNotHandled
			}
		case debugEventExitProcess:
			result.ProcessExited = true
		}

		continued, _, continueErr := procContinueDebugEvent.Call(uintptr(eventPID), uintptr(eventTID), continueStatus)
		if continued == 0 {
			return result, fmt.Errorf("ContinueDebugEvent code=%d tid=%d: %v", eventCode, eventTID, continueErr)
		}
		if result.ProcessExited || result.TotalEvents >= 4 {
			break
		}
	}
	result.ElapsedMS = time.Since(started).Milliseconds()
	for threadID := range armed {
		result.ArmedThreads = append(result.ArmedThreads, threadID)
	}
	sort.Slice(result.ArmedThreads, func(i, j int) bool { return result.ArmedThreads[i] < result.ArmedThreads[j] })
	return result, nil
}

// debugLoadDLLIdentity reads the LOAD_DLL_DEBUG_EVENT itself instead of
// immediately taking a PSAPI module snapshot. Windows stops the loader thread
// before the just-mapped DLL is guaranteed to appear in that snapshot, which
// made short-lived compatibility DLLs invisible and left the probe unarmed.
// The event's file handle and base address are authoritative at this boundary.
func debugLoadDLLIdentity(process syscall.Handle, event []byte) (uint32, string) {
	if len(event) < 32 {
		return 0, ""
	}
	base64 := binary.LittleEndian.Uint64(event[24:32])
	if base64 == 0 || base64 > 0xFFFFFFFF {
		closeDebugLoadDLLFile(event)
		return 0, ""
	}
	base := uint32(base64)
	name := ""
	if file := debugLoadDLLFile(event); file != 0 {
		buffer := make([]uint16, 32768)
		length, _, _ := procGetFinalPathNameByHandle.Call(
			uintptr(file), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0,
		)
		if length > 0 && length < uintptr(len(buffer)) {
			name = filepath.Base(syscall.UTF16ToString(buffer[:length]))
		}
		syscall.CloseHandle(file)
	}
	if name == "" {
		buffer := make([]uint16, 32768)
		length, _, _ := procGetModuleFileNameEx.Call(
			uintptr(process), uintptr(base), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)),
		)
		if length > 0 && length < uintptr(len(buffer)) {
			name = filepath.Base(syscall.UTF16ToString(buffer[:length]))
		}
	}
	return base, name
}

func debugLoadDLLFile(event []byte) syscall.Handle {
	if len(event) < 24 {
		return 0
	}
	return syscall.Handle(binary.LittleEndian.Uint64(event[16:24]))
}

func closeDebugLoadDLLFile(event []byte) {
	if file := debugLoadDLLFile(event); file != 0 {
		syscall.CloseHandle(file)
	}
}

func captureClientDebugException(process syscall.Handle, event []byte, threadID uint32) (ClientDebugException, error) {
	const threadAccess = 0x0008 | 0x0010 | 0x0040
	thread, _, openErr := procOpenThread.Call(threadAccess, 0, uintptr(threadID))
	if thread == 0 {
		return ClientDebugException{}, fmt.Errorf("OpenThread %d at exception: %v", threadID, openErr)
	}
	defer syscall.CloseHandle(syscall.Handle(thread))
	context := make([]byte, 716)
	binary.LittleEndian.PutUint32(context[0:4], wow64ContextControl|wow64ContextInteger|wow64ContextDebugRegisters)
	got, _, getErr := procWow64GetThreadContext.Call(thread, uintptr(unsafe.Pointer(&context[0])))
	if got == 0 {
		return ClientDebugException{}, fmt.Errorf("Wow64GetThreadContext %d at exception: %v", threadID, getErr)
	}
	eip := binary.LittleEndian.Uint32(context[wow64ContextEIP : wow64ContextEIP+4])
	esp := binary.LittleEndian.Uint32(context[wow64ContextESP : wow64ContextESP+4])
	result := ClientDebugException{
		Code:        fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(event[16:20])),
		FirstChance: binary.LittleEndian.Uint32(event[168:172]), ThreadID: threadID,
		EIP: fmt.Sprintf("0x%08X", eip), ESP: fmt.Sprintf("0x%08X", esp),
		EBP:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0xB4:0xB8])),
		EAX:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0xB0:0xB4])),
		EBX:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0xA4:0xA8])),
		ECX:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0xAC:0xB0])),
		EDX:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0xA8:0xAC])),
		ESI:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0xA0:0xA4])),
		EDI:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0x9C:0xA0])),
		EFlags: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[wow64ContextEFlags:wow64ContextEFlags+4])),
		Stack:  make([]string, 0, 160),
	}
	if len(event) >= 64 {
		switch binary.LittleEndian.Uint64(event[48:56]) {
		case 0:
			result.AccessType = "read"
		case 1:
			result.AccessType = "write"
		case 8:
			result.AccessType = "execute"
		default:
			result.AccessType = fmt.Sprintf("0x%X", binary.LittleEndian.Uint64(event[48:56]))
		}
		result.AccessAddress = fmt.Sprintf("0x%08X", uint32(binary.LittleEndian.Uint64(event[56:64])))
	}
	if stack, ok := readRemote(process, uintptr(esp), 160*4); ok {
		for offset := 0; offset < len(stack); offset += 4 {
			result.Stack = append(result.Stack, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(stack[offset:offset+4])))
		}
	}
	return result, nil
}

func parseTracePointer32(value string) uint32 {
	var result uint32
	_, _ = fmt.Sscanf(value, "0x%X", &result)
	return result
}

func armDebugExecBreakpoints(pid, firstAddress, secondAddress uint32, armed map[uint32]struct{}) error {
	const snapshotThreads = 0x00000004
	snapshot, _, snapshotErr := procCreateToolhelp32.Call(snapshotThreads, 0)
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
		if entry.OwnerProcessID == pid {
			_ = armDebugExecBreakpointThread(entry.ThreadID, firstAddress, secondAddress, armed)
		}
		entry.Size = uint32(unsafe.Sizeof(threadEntry32{}))
		success, _, _ = procThread32Next.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
		if success == 0 {
			break
		}
	}
	if len(armed) == 0 {
		return fmt.Errorf("no Client.exe threads accepted module execution breakpoints")
	}
	return nil
}

func armDebugExecBreakpointThread(threadID, firstAddress, secondAddress uint32, armed map[uint32]struct{}) error {
	if _, ok := armed[threadID]; ok {
		return nil
	}
	const threadAccess = 0x0008 | 0x0010 | 0x0040
	thread, _, openErr := procOpenThread.Call(threadAccess, 0, uintptr(threadID))
	if thread == 0 {
		return fmt.Errorf("OpenThread %d: %v", threadID, openErr)
	}
	defer syscall.CloseHandle(syscall.Handle(thread))
	context := make([]byte, 716)
	binary.LittleEndian.PutUint32(context[0:4], wow64ContextDebugRegisters)
	got, _, getErr := procWow64GetThreadContext.Call(thread, uintptr(unsafe.Pointer(&context[0])))
	if got == 0 {
		return fmt.Errorf("Wow64GetThreadContext %d: %v", threadID, getErr)
	}
	dr7 := binary.LittleEndian.Uint32(context[24:28])
	if dr7&0xF != 0 {
		return fmt.Errorf("thread %d already uses a local x86 hardware breakpoint", threadID)
	}
	binary.LittleEndian.PutUint32(context[4:8], firstAddress)
	binary.LittleEndian.PutUint32(context[8:12], secondAddress)
	binary.LittleEndian.PutUint32(context[20:24], 0)
	binary.LittleEndian.PutUint32(context[24:28], dr7|0x5)
	set, _, setErr := procWow64SetThreadContext.Call(thread, uintptr(unsafe.Pointer(&context[0])))
	if set == 0 {
		return fmt.Errorf("Wow64SetThreadContext %d: %v", threadID, setErr)
	}
	armed[threadID] = struct{}{}
	return nil
}

func captureDebugExecEvent(process syscall.Handle, threadID, firstAddress, secondAddress, index uint32) (ClientHardwareExecEvent, bool, error) {
	const threadAccess = 0x0008 | 0x0010 | 0x0040
	thread, _, openErr := procOpenThread.Call(threadAccess, 0, uintptr(threadID))
	if thread == 0 {
		return ClientHardwareExecEvent{}, false, fmt.Errorf("OpenThread %d at execution breakpoint: %v", threadID, openErr)
	}
	defer syscall.CloseHandle(syscall.Handle(thread))
	context := make([]byte, 716)
	binary.LittleEndian.PutUint32(context[0:4], wow64ContextControl|wow64ContextInteger|wow64ContextDebugRegisters)
	got, _, getErr := procWow64GetThreadContext.Call(thread, uintptr(unsafe.Pointer(&context[0])))
	if got == 0 {
		return ClientHardwareExecEvent{}, false, fmt.Errorf("Wow64GetThreadContext %d at execution breakpoint: %v", threadID, getErr)
	}
	eip := binary.LittleEndian.Uint32(context[wow64ContextEIP : wow64ContextEIP+4])
	if eip != firstAddress && eip != secondAddress {
		return ClientHardwareExecEvent{}, false, nil
	}
	esp := binary.LittleEndian.Uint32(context[wow64ContextESP : wow64ContextESP+4])
	record := ClientHardwareExecEvent{
		Index: index, DR6: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[20:24])),
		EIP: fmt.Sprintf("0x%08X", eip), ESP: fmt.Sprintf("0x%08X", esp),
		EBP:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0xB4:0xB8])),
		EAX:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0xB0:0xB4])),
		EBX:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0xA4:0xA8])),
		ECX:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0xAC:0xB0])),
		EDX:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0xA8:0xAC])),
		ESI:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0xA0:0xA4])),
		EDI:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[0x9C:0xA0])),
		EFlags:   fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(context[wow64ContextEFlags:wow64ContextEFlags+4])),
		ThreadID: threadID, Stack: make([]string, 0, 32), Module: "QQTModules.dll",
	}
	if eip == firstAddress {
		record.RVA = "0x6BDF"
	} else {
		record.RVA = "0x6C1D"
	}
	if stack, ok := readRemote(process, uintptr(esp), 32*4); ok {
		for offset := 0; offset < len(stack); offset += 4 {
			record.Stack = append(record.Stack, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(stack[offset:offset+4])))
		}
	}
	binary.LittleEndian.PutUint32(context[20:24], 0)
	flags := binary.LittleEndian.Uint32(context[wow64ContextEFlags:wow64ContextEFlags+4]) | 0x00010000
	binary.LittleEndian.PutUint32(context[wow64ContextEFlags:wow64ContextEFlags+4], flags)
	set, _, setErr := procWow64SetThreadContext.Call(thread, uintptr(unsafe.Pointer(&context[0])))
	if set == 0 {
		return ClientHardwareExecEvent{}, false, fmt.Errorf("Wow64SetThreadContext %d after execution breakpoint: %v", threadID, setErr)
	}
	return record, true, nil
}

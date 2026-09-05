package windebug

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	debugOnlyThisProcess      = 0x00000002
	createProcessDebugEvent   = 3
	exitProcessDebugEvent     = 5
	loadDLLDebugEvent         = 6
	unloadDLLDebugEvent       = 7
	outputDebugStringEvent    = 8
	exceptionDebugEvent       = 1
	dbgContinue               = 0x00010002
	dbgExceptionNotHandled    = 0x80010001
	errorSemTimeout           = syscall.Errno(121)
	exceptionBreakpoint       = 0x80000003
	exceptionSingleStep       = 0x80000004
	exceptionWOW64SingleStep  = 0x4000001E
	threadGetContext          = 0x0008
	threadSetContext          = 0x0010
	threadQueryInformation    = 0x0040
	wow64ContextControl       = 0x00010001
	wow64ContextInteger       = 0x00010002
	memCommit                 = 0x00001000
	memReserve                = 0x00002000
	memFree                   = 0x00010000
	pageReadWrite             = 0x00000004
	maxDebugStringBytes       = 4096
	maxFramePointerStackDepth = 64
)

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procWaitForDebugEvent        = kernel32.NewProc("WaitForDebugEvent")
	procContinueDebugEvent       = kernel32.NewProc("ContinueDebugEvent")
	procReadProcessMemory        = kernel32.NewProc("ReadProcessMemory")
	procVirtualQueryEx           = kernel32.NewProc("VirtualQueryEx")
	procVirtualAllocEx           = kernel32.NewProc("VirtualAllocEx")
	procOpenThread               = kernel32.NewProc("OpenThread")
	procWow64GetThreadContext    = kernel32.NewProc("Wow64GetThreadContext")
	procWow64SetThreadContext    = kernel32.NewProc("Wow64SetThreadContext")
	procGetFinalPathNameByHandle = kernel32.NewProc("GetFinalPathNameByHandleW")
	procTerminateProcess         = kernel32.NewProc("TerminateProcess")
)

type Options struct {
	Executable      string
	WorkingDir      string
	Arguments       []string
	Output          io.Writer
	DumpDirectory   string
	TraceFromModule string
	TraceTail       int
	HealFreePages   int
	Timeout         time.Duration
}

type Runner struct {
	options       Options
	process       syscall.Handle
	processID     uint32
	modules       map[uintptr]module
	encoder       *json.Encoder
	start         time.Time
	exceptionSeen int
	dumpedModules map[uintptr]bool
	traceEnabled  bool
	traceThreadID uint32
	traceSteps    uint64
	traceTail     []traceEntry
	traceNext     int
	healedPages   int
}

type module struct {
	Base uintptr `json:"base"`
	Size uint32  `json:"size"`
	Path string  `json:"path"`
}

type debugEvent struct {
	Code      uint32
	ProcessID uint32
	ThreadID  uint32
	_         uint32
	Info      [160]byte
}

type exceptionRecord struct {
	Code       uint32
	Flags      uint32
	Nested     uintptr
	Address    uintptr
	Parameters uint32
	_          uint32
	Info       [15]uintptr
}

type exceptionDebugInfo struct {
	Record      exceptionRecord
	FirstChance uint32
	_           uint32
}

type createProcessDebugInfo struct {
	FileHandle       syscall.Handle
	ProcessHandle    syscall.Handle
	ThreadHandle     syscall.Handle
	BaseOfImage      uintptr
	DebugInfoOffset  uint32
	DebugInfoSize    uint32
	ThreadLocalBase  uintptr
	StartAddress     uintptr
	ImageNameAddress uintptr
	UnicodeImageName uint16
	_                [6]byte
}

type loadDLLDebugInfo struct {
	FileHandle       syscall.Handle
	BaseOfDLL        uintptr
	DebugInfoOffset  uint32
	DebugInfoSize    uint32
	ImageNameAddress uintptr
	UnicodeImageName uint16
	_                [6]byte
}

type outputDebugStringInfo struct {
	Address uintptr
	Unicode uint16
	Length  uint16
	_       uint32
}

type memoryBasicInformation struct {
	BaseAddress       uintptr
	AllocationBase    uintptr
	AllocationProtect uint32
	_                 uint32
	RegionSize        uintptr
	State             uint32
	Protect           uint32
	Type              uint32
	_                 uint32
}

type memoryRegion struct {
	QueryAddress      string `json:"query_address"`
	BaseAddress       string `json:"base_address"`
	AllocationBase    string `json:"allocation_base"`
	AllocationProtect string `json:"allocation_protect"`
	RegionSize        uint64 `json:"region_size"`
	State             string `json:"state"`
	Protect           string `json:"protect"`
	Type              string `json:"type"`
}

type moduleDump struct {
	Module       string `json:"module"`
	RuntimeBase  string `json:"runtime_base"`
	ImageSize    uint32 `json:"image_size"`
	OutputPath   string `json:"output_path"`
	SHA256       string `json:"sha256"`
	BytesRead    uint64 `json:"bytes_read"`
	Unreadable   uint64 `json:"unreadable_bytes"`
	MemoryLayout string `json:"memory_layout"`
}

type registerState struct {
	EAX    uint32 `json:"eax"`
	EBX    uint32 `json:"ebx"`
	ECX    uint32 `json:"ecx"`
	EDX    uint32 `json:"edx"`
	ESI    uint32 `json:"esi"`
	EDI    uint32 `json:"edi"`
	EBP    uint32 `json:"ebp"`
	ESP    uint32 `json:"esp"`
	EIP    uint32 `json:"eip"`
	EFlags uint32 `json:"eflags"`
}

type stackFrame struct {
	Index   int    `json:"index"`
	Address uint32 `json:"address"`
	Module  string `json:"module,omitempty"`
	RVA     uint32 `json:"rva,omitempty"`
	Source  string `json:"source"`
}

type traceEntry struct {
	Step     uint64 `json:"step"`
	EIP      uint32 `json:"eip"`
	Location string `json:"location"`
	EAX      uint32 `json:"eax"`
	ECX      uint32 `json:"ecx"`
	EDX      uint32 `json:"edx"`
	ESI      uint32 `json:"esi"`
	EDI      uint32 `json:"edi"`
	ESP      uint32 `json:"esp"`
	EFlags   uint32 `json:"eflags"`
}

func Run(options Options) error {
	if options.Output == nil {
		options.Output = os.Stdout
	}
	if options.Timeout == 0 {
		options.Timeout = 30 * time.Second
	}
	if options.TraceTail <= 0 {
		options.TraceTail = 8192
	}
	executable, err := filepath.Abs(options.Executable)
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}
	workingDir := options.WorkingDir
	if workingDir == "" {
		workingDir = filepath.Dir(executable)
	}
	workingDir, err = filepath.Abs(workingDir)
	if err != nil {
		return fmt.Errorf("resolve working directory: %w", err)
	}
	if _, err := os.Stat(executable); err != nil {
		return fmt.Errorf("stat executable: %w", err)
	}
	options.Executable = executable
	options.WorkingDir = workingDir
	if options.DumpDirectory != "" {
		options.DumpDirectory, err = filepath.Abs(options.DumpDirectory)
		if err != nil {
			return fmt.Errorf("resolve dump directory: %w", err)
		}
		if err := os.MkdirAll(options.DumpDirectory, 0o700); err != nil {
			return fmt.Errorf("create dump directory: %w", err)
		}
	}
	runner := &Runner{
		options: options, modules: make(map[uintptr]module), dumpedModules: make(map[uintptr]bool),
		traceTail: make([]traceEntry, 0, options.TraceTail), encoder: json.NewEncoder(options.Output), start: time.Now(),
	}
	return runner.run()
}

func (runner *Runner) run() error {
	application, err := syscall.UTF16PtrFromString(runner.options.Executable)
	if err != nil {
		return err
	}
	commandLine := quoteWindowsArgument(runner.options.Executable)
	for _, argument := range runner.options.Arguments {
		commandLine += " " + quoteWindowsArgument(argument)
	}
	commandLinePointer, err := syscall.UTF16PtrFromString(commandLine)
	if err != nil {
		return err
	}
	directory, err := syscall.UTF16PtrFromString(runner.options.WorkingDir)
	if err != nil {
		return err
	}
	var startup syscall.StartupInfo
	startup.Cb = uint32(unsafe.Sizeof(startup))
	var process syscall.ProcessInformation
	if err := syscall.CreateProcess(application, commandLinePointer, nil, nil, false, debugOnlyThisProcess, nil, directory, &startup, &process); err != nil {
		return fmt.Errorf("CreateProcess: %w", err)
	}
	runner.process = process.Process
	runner.processID = process.ProcessId
	defer syscall.CloseHandle(process.Process)
	defer syscall.CloseHandle(process.Thread)
	runner.emit(map[string]any{"event": "process_created", "pid": process.ProcessId, "thread_id": process.ThreadId, "executable": runner.options.Executable, "command_line": commandLine, "working_directory": runner.options.WorkingDir})

	deadline := time.Now().Add(runner.options.Timeout)
	for {
		if time.Now().After(deadline) {
			_, _, terminateErr := procTerminateProcess.Call(uintptr(runner.process), 0xdead)
			runner.emit(map[string]any{"event": "timeout", "terminated": terminateErr == syscall.Errno(0)})
			return fmt.Errorf("debug timeout after %s", runner.options.Timeout)
		}
		var event debugEvent
		success, _, callErr := procWaitForDebugEvent.Call(uintptr(unsafe.Pointer(&event)), 1000)
		if success == 0 {
			if errors.Is(callErr, errorSemTimeout) {
				continue
			}
			return fmt.Errorf("WaitForDebugEvent: %w", callErr)
		}
		continueStatus := uintptr(dbgContinue)
		exit := false
		switch event.Code {
		case createProcessDebugEvent:
			info := (*createProcessDebugInfo)(unsafe.Pointer(&event.Info[0]))
			runner.addModule(info.BaseOfImage, runner.options.Executable)
			runner.emit(map[string]any{"event": "create_process_debug", "thread_id": event.ThreadID, "base": hexPointer(info.BaseOfImage), "start_address": hexPointer(info.StartAddress)})
			if info.FileHandle != 0 && info.FileHandle != syscall.InvalidHandle {
				_ = syscall.CloseHandle(info.FileHandle)
			}
		case loadDLLDebugEvent:
			info := (*loadDLLDebugInfo)(unsafe.Pointer(&event.Info[0]))
			path := finalPath(info.FileHandle)
			runner.addModule(info.BaseOfDLL, path)
			runner.emit(map[string]any{"event": "load_dll", "thread_id": event.ThreadID, "base": hexPointer(info.BaseOfDLL), "path": path})
			if !runner.traceEnabled && runner.options.TraceFromModule != "" && strings.EqualFold(filepath.Base(path), runner.options.TraceFromModule) {
				if err := runner.enableSingleStep(event.ThreadID); err != nil {
					runner.emit(map[string]any{"event": "trace_start_failed", "thread_id": event.ThreadID, "module": path, "error": err.Error()})
				} else {
					runner.traceEnabled = true
					runner.traceThreadID = event.ThreadID
					runner.emit(map[string]any{"event": "trace_started", "thread_id": event.ThreadID, "module": path, "tail_capacity": runner.options.TraceTail})
				}
			}
			if info.FileHandle != 0 && info.FileHandle != syscall.InvalidHandle {
				_ = syscall.CloseHandle(info.FileHandle)
			}
		case unloadDLLDebugEvent:
			base := *(*uintptr)(unsafe.Pointer(&event.Info[0]))
			runner.emit(map[string]any{"event": "unload_dll", "thread_id": event.ThreadID, "base": hexPointer(base), "module": runner.modules[base]})
			delete(runner.modules, base)
		case outputDebugStringEvent:
			info := (*outputDebugStringInfo)(unsafe.Pointer(&event.Info[0]))
			if text := runner.readDebugString(*info); text != "" {
				runner.emit(map[string]any{"event": "debug_string", "thread_id": event.ThreadID, "text": text})
			}
		case exceptionDebugEvent:
			info := (*exceptionDebugInfo)(unsafe.Pointer(&event.Info[0]))
			if (info.Record.Code == exceptionSingleStep || info.Record.Code == exceptionWOW64SingleStep) && runner.traceEnabled && event.ThreadID == runner.traceThreadID {
				registers, err := runner.registers(event.ThreadID)
				if err != nil {
					runner.emit(map[string]any{"event": "trace_context_failed", "thread_id": event.ThreadID, "error": err.Error(), "trace_steps": runner.traceSteps})
					runner.traceEnabled = false
				} else {
					runner.appendTrace(registers)
					if info.Record.Code == exceptionSingleStep {
						if err := runner.enableSingleStep(event.ThreadID); err != nil {
							runner.emit(map[string]any{"event": "trace_rearm_failed", "thread_id": event.ThreadID, "error": err.Error(), "trace_steps": runner.traceSteps})
							runner.traceEnabled = false
						}
					}
				}
				break
			}
			runner.exceptionSeen++
			healed := false
			registers, frames, contextError := runner.context(event.ThreadID)
			parameterCount := int(info.Record.Parameters)
			if parameterCount > len(info.Record.Info) {
				parameterCount = len(info.Record.Info)
			}
			parameters := make([]string, 0, parameterCount)
			for index := 0; index < parameterCount; index++ {
				parameters = append(parameters, hexPointer(info.Record.Info[index]))
			}
			record := map[string]any{
				"event": "exception", "sequence": runner.exceptionSeen, "thread_id": event.ThreadID,
				"code": fmt.Sprintf("0x%08X", info.Record.Code), "flags": fmt.Sprintf("0x%08X", info.Record.Flags),
				"address": hexPointer(info.Record.Address), "location": runner.describeAddress(uint32(info.Record.Address)),
				"first_chance": info.FirstChance != 0, "parameters": parameters, "registers": registers, "stack": frames,
			}
			if bytes, ok := runner.readMemory(info.Record.Address, 64); ok {
				record["instruction_bytes"] = hex.EncodeToString(bytes)
			}
			if info.Record.Code == 0xC0000005 && parameterCount >= 2 {
				accessType := "read"
				if info.Record.Info[0] == 1 {
					accessType = "write"
				} else if info.Record.Info[0] == 8 {
					accessType = "execute"
				}
				record["access_type"] = accessType
				record["access_address"] = hexPointer(info.Record.Info[1])
				if region, ok := runner.queryMemory(info.Record.Info[1]); ok {
					record["access_region"] = region
				}
				if info.FirstChance != 0 && accessType != "execute" && runner.healedPages < runner.options.HealFreePages {
					if allocation, ok := runner.allocateFaultPage(info.Record.Info[1]); ok {
						runner.healedPages++
						healed = true
						record["healed_free_page"] = allocation
						record["healed_page_count"] = runner.healedPages
					}
				}
				if info.FirstChance != 0 && runner.options.DumpDirectory != "" {
					dumps, dumpErrors := runner.dumpRelevantModules(info.Record.Address)
					if len(dumps) != 0 {
						record["module_dumps"] = dumps
					}
					if len(dumpErrors) != 0 {
						record["module_dump_errors"] = dumpErrors
					}
				}
			}
			if region, ok := runner.queryMemory(info.Record.Address); ok {
				record["instruction_region"] = region
			}
			if registers.ESP != 0 {
				if region, ok := runner.queryMemory(uintptr(registers.ESP)); ok {
					record["stack_region"] = region
				}
			}
			if contextError != nil {
				record["context_error"] = contextError.Error()
			}
			if runner.traceSteps != 0 {
				record["trace_steps"] = runner.traceSteps
				record["trace_tail"] = runner.traceSnapshot()
			}
			runner.emit(record)
			if healed {
				continueStatus = dbgContinue
			} else if info.Record.Code != exceptionBreakpoint && info.Record.Code != exceptionSingleStep {
				continueStatus = dbgExceptionNotHandled
			}
		case exitProcessDebugEvent:
			exitCode := *(*uint32)(unsafe.Pointer(&event.Info[0]))
			runner.emit(map[string]any{"event": "process_exited", "exit_code": int32(exitCode), "exit_hex": fmt.Sprintf("0x%08X", exitCode), "exceptions_seen": runner.exceptionSeen, "trace_steps": runner.traceSteps})
			exit = true
		}
		continued, _, continueErr := procContinueDebugEvent.Call(uintptr(event.ProcessID), uintptr(event.ThreadID), continueStatus)
		if continued == 0 {
			return fmt.Errorf("ContinueDebugEvent: %w", continueErr)
		}
		if exit {
			return nil
		}
	}
}

func (runner *Runner) context(threadID uint32) (registerState, []stackFrame, error) {
	registers, err := runner.registers(threadID)
	if err != nil {
		return registerState{}, nil, err
	}
	frames := []stackFrame{{Index: 0, Address: registers.EIP, Source: "eip"}}
	frames[0].Module, frames[0].RVA = runner.moduleAndRVA(registers.EIP)
	ebp := registers.EBP
	for index := 1; index < maxFramePointerStackDepth && ebp != 0; index++ {
		data, ok := runner.readMemory(uintptr(ebp), 8)
		if !ok {
			break
		}
		next := binary.LittleEndian.Uint32(data[0:4])
		returnAddress := binary.LittleEndian.Uint32(data[4:8])
		frame := stackFrame{Index: index, Address: returnAddress, Source: "ebp-chain"}
		frame.Module, frame.RVA = runner.moduleAndRVA(returnAddress)
		frames = append(frames, frame)
		if next <= ebp || next-ebp > 8*1024*1024 {
			break
		}
		ebp = next
	}
	if len(frames) == 1 {
		if stack, ok := runner.readMemory(uintptr(registers.ESP), 256); ok {
			seen := make(map[uint32]struct{})
			for offset := 0; offset+4 <= len(stack) && len(frames) < 24; offset += 4 {
				candidate := binary.LittleEndian.Uint32(stack[offset : offset+4])
				moduleName, rva := runner.moduleAndRVA(candidate)
				if moduleName == "" {
					continue
				}
				if _, exists := seen[candidate]; exists {
					continue
				}
				seen[candidate] = struct{}{}
				frames = append(frames, stackFrame{Index: len(frames), Address: candidate, Module: moduleName, RVA: rva, Source: fmt.Sprintf("esp+0x%x-scan", offset)})
			}
		}
	}
	return registers, frames, nil
}

func (runner *Runner) registers(threadID uint32) (registerState, error) {
	handle, _, openErr := procOpenThread.Call(threadGetContext|threadQueryInformation, 0, uintptr(threadID))
	if handle == 0 {
		return registerState{}, fmt.Errorf("OpenThread: %w", openErr)
	}
	defer syscall.CloseHandle(syscall.Handle(handle))
	context := make([]byte, 716)
	binary.LittleEndian.PutUint32(context[0:4], wow64ContextControl|wow64ContextInteger)
	success, _, contextErr := procWow64GetThreadContext.Call(handle, uintptr(unsafe.Pointer(&context[0])))
	if success == 0 {
		return registerState{}, fmt.Errorf("Wow64GetThreadContext: %w", contextErr)
	}
	registers := registerState{
		EDI: u32(context, 156), ESI: u32(context, 160), EBX: u32(context, 164),
		EDX: u32(context, 168), ECX: u32(context, 172), EAX: u32(context, 176),
		EBP: u32(context, 180), EIP: u32(context, 184), EFlags: u32(context, 192), ESP: u32(context, 196),
	}
	return registers, nil
}

func (runner *Runner) enableSingleStep(threadID uint32) error {
	handle, _, openErr := procOpenThread.Call(threadGetContext|threadSetContext|threadQueryInformation, 0, uintptr(threadID))
	if handle == 0 {
		return fmt.Errorf("OpenThread: %w", openErr)
	}
	defer syscall.CloseHandle(syscall.Handle(handle))
	context := make([]byte, 716)
	binary.LittleEndian.PutUint32(context[0:4], wow64ContextControl)
	success, _, getErr := procWow64GetThreadContext.Call(handle, uintptr(unsafe.Pointer(&context[0])))
	if success == 0 {
		return fmt.Errorf("Wow64GetThreadContext: %w", getErr)
	}
	flags := u32(context, 192) | 0x100
	binary.LittleEndian.PutUint32(context[192:196], flags)
	success, _, setErr := procWow64SetThreadContext.Call(handle, uintptr(unsafe.Pointer(&context[0])))
	if success == 0 {
		return fmt.Errorf("Wow64SetThreadContext: %w", setErr)
	}
	return nil
}

func (runner *Runner) appendTrace(registers registerState) {
	runner.traceSteps++
	entry := traceEntry{
		Step: runner.traceSteps, EIP: registers.EIP, Location: runner.describeAddress(registers.EIP),
		EAX: registers.EAX, ECX: registers.ECX, EDX: registers.EDX, ESI: registers.ESI,
		EDI: registers.EDI, ESP: registers.ESP, EFlags: registers.EFlags,
	}
	if len(runner.traceTail) < cap(runner.traceTail) {
		runner.traceTail = append(runner.traceTail, entry)
		return
	}
	runner.traceTail[runner.traceNext] = entry
	runner.traceNext = (runner.traceNext + 1) % len(runner.traceTail)
}

func (runner *Runner) traceSnapshot() []traceEntry {
	if len(runner.traceTail) < cap(runner.traceTail) || runner.traceNext == 0 {
		return append([]traceEntry(nil), runner.traceTail...)
	}
	result := make([]traceEntry, 0, len(runner.traceTail))
	result = append(result, runner.traceTail[runner.traceNext:]...)
	result = append(result, runner.traceTail[:runner.traceNext]...)
	return result
}

func (runner *Runner) addModule(base uintptr, path string) {
	entry := module{Base: base, Path: path}
	if data, ok := runner.readMemory(base, 4096); ok && len(data) >= 0x40 && data[0] == 'M' && data[1] == 'Z' {
		peOffset := int(binary.LittleEndian.Uint32(data[0x3c:0x40]))
		sizeOffset := peOffset + 4 + 20 + 56
		if peOffset >= 0 && sizeOffset+4 <= len(data) {
			entry.Size = binary.LittleEndian.Uint32(data[sizeOffset : sizeOffset+4])
		}
	}
	runner.modules[base] = entry
}

func (runner *Runner) moduleAndRVA(address uint32) (string, uint32) {
	var candidates []module
	for _, entry := range runner.modules {
		if uintptr(address) >= entry.Base && (entry.Size == 0 || uintptr(address) < entry.Base+uintptr(entry.Size)) {
			candidates = append(candidates, entry)
		}
	}
	if len(candidates) == 0 {
		return "", 0
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Base > candidates[j].Base })
	name := filepath.Base(candidates[0].Path)
	if name == "." || name == "" {
		name = candidates[0].Path
	}
	return name, address - uint32(candidates[0].Base)
}

func (runner *Runner) describeAddress(address uint32) string {
	name, rva := runner.moduleAndRVA(address)
	if name == "" {
		return fmt.Sprintf("0x%08X", address)
	}
	return fmt.Sprintf("%s+0x%X", name, rva)
}

func (runner *Runner) readMemory(address uintptr, size int) ([]byte, bool) {
	if address == 0 || size <= 0 {
		return nil, false
	}
	buffer := make([]byte, size)
	var read uintptr
	success, _, _ := procReadProcessMemory.Call(uintptr(runner.process), address, uintptr(unsafe.Pointer(&buffer[0])), uintptr(size), uintptr(unsafe.Pointer(&read)))
	if success == 0 || read == 0 {
		return nil, false
	}
	return buffer[:read], true
}

func (runner *Runner) queryMemory(address uintptr) (memoryRegion, bool) {
	var info memoryBasicInformation
	result, _, _ := procVirtualQueryEx.Call(
		uintptr(runner.process),
		address,
		uintptr(unsafe.Pointer(&info)),
		unsafe.Sizeof(info),
	)
	if result == 0 {
		return memoryRegion{}, false
	}
	return memoryRegion{
		QueryAddress:      hexPointer(address),
		BaseAddress:       hexPointer(info.BaseAddress),
		AllocationBase:    hexPointer(info.AllocationBase),
		AllocationProtect: fmt.Sprintf("0x%X", info.AllocationProtect),
		RegionSize:        uint64(info.RegionSize),
		State:             fmt.Sprintf("0x%X", info.State),
		Protect:           fmt.Sprintf("0x%X", info.Protect),
		Type:              fmt.Sprintf("0x%X", info.Type),
	}, true
}

func (runner *Runner) allocateFaultPage(address uintptr) (map[string]any, bool) {
	if address < 0x10000 {
		return nil, false
	}
	region, ok := runner.queryMemory(address)
	if !ok || region.State != fmt.Sprintf("0x%X", memFree) {
		return nil, false
	}
	page := address &^ uintptr(0xFFF)
	allocated, _, _ := procVirtualAllocEx.Call(
		uintptr(runner.process), page, 0x1000, memReserve|memCommit, pageReadWrite,
	)
	if allocated != page {
		return nil, false
	}
	return map[string]any{
		"requested_address": hexPointer(address),
		"page_base":         hexPointer(page),
		"size":              0x1000,
		"protect":           "PAGE_READWRITE",
		"initial_contents":  "zero",
	}, true
}

func (runner *Runner) dumpRelevantModules(faultAddress uintptr) ([]moduleDump, []string) {
	var targets []module
	for _, entry := range runner.modules {
		name := filepath.Base(entry.Path)
		containsFault := faultAddress >= entry.Base && faultAddress < entry.Base+uintptr(entry.Size)
		if containsFault || strings.EqualFold(name, "Client.exe") || strings.EqualFold(name, "ClientBase.dll") {
			targets = append(targets, entry)
		}
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Base < targets[j].Base })
	var dumps []moduleDump
	var dumpErrors []string
	for _, entry := range targets {
		if entry.Size == 0 || runner.dumpedModules[entry.Base] {
			continue
		}
		dump, err := runner.dumpModule(entry)
		if err != nil {
			dumpErrors = append(dumpErrors, fmt.Sprintf("%s: %v", filepath.Base(entry.Path), err))
			continue
		}
		runner.dumpedModules[entry.Base] = true
		dumps = append(dumps, dump)
	}
	return dumps, dumpErrors
}

func (runner *Runner) dumpModule(entry module) (moduleDump, error) {
	name := filepath.Base(entry.Path)
	if name == "" || name == "." {
		name = fmt.Sprintf("module-%X", entry.Base)
	}
	baseName := fmt.Sprintf("%s.base-%08X.size-%X", name, entry.Base, entry.Size)
	dumpPath := filepath.Join(runner.options.DumpDirectory, baseName+".mem.bin")
	layoutPath := filepath.Join(runner.options.DumpDirectory, baseName+".layout.json")
	file, err := os.OpenFile(dumpPath, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600)
	if err != nil {
		return moduleDump{}, err
	}
	defer file.Close()
	if err := file.Truncate(int64(entry.Size)); err != nil {
		return moduleDump{}, err
	}

	const chunkSize = uintptr(1024 * 1024)
	var bytesRead uint64
	var regions []memoryRegion
	for offset := uintptr(0); offset < uintptr(entry.Size); {
		address := entry.Base + offset
		region, ok := runner.queryMemory(address)
		if !ok {
			offset += 0x1000
			continue
		}
		regions = append(regions, region)
		regionBase := parseHexPointer(region.BaseAddress)
		regionEnd := regionBase + uintptr(region.RegionSize)
		if regionEnd <= address {
			offset += 0x1000
			continue
		}
		end := regionEnd
		moduleEnd := entry.Base + uintptr(entry.Size)
		if end > moduleEnd {
			end = moduleEnd
		}
		for cursor := address; cursor < end; {
			amount := end - cursor
			if amount > chunkSize {
				amount = chunkSize
			}
			if data, readable := runner.readMemory(cursor, int(amount)); readable {
				if _, writeErr := file.WriteAt(data, int64(cursor-entry.Base)); writeErr != nil {
					return moduleDump{}, writeErr
				}
				bytesRead += uint64(len(data))
			}
			cursor += amount
		}
		offset = end - entry.Base
	}
	if err := file.Sync(); err != nil {
		return moduleDump{}, err
	}
	data, err := os.ReadFile(dumpPath)
	if err != nil {
		return moduleDump{}, err
	}
	sum := sha256.Sum256(data)
	layoutData, err := json.MarshalIndent(regions, "", "  ")
	if err != nil {
		return moduleDump{}, err
	}
	layoutData = append(layoutData, '\n')
	if err := os.WriteFile(layoutPath, layoutData, 0o600); err != nil {
		return moduleDump{}, err
	}
	return moduleDump{
		Module:       entry.Path,
		RuntimeBase:  hexPointer(entry.Base),
		ImageSize:    entry.Size,
		OutputPath:   dumpPath,
		SHA256:       hex.EncodeToString(sum[:]),
		BytesRead:    bytesRead,
		Unreadable:   uint64(entry.Size) - bytesRead,
		MemoryLayout: layoutPath,
	}, nil
}

func parseHexPointer(value string) uintptr {
	var result uintptr
	_, _ = fmt.Sscanf(value, "0x%X", &result)
	return result
}

func (runner *Runner) readDebugString(info outputDebugStringInfo) string {
	length := int(info.Length)
	if length <= 0 || length > maxDebugStringBytes {
		return ""
	}
	data, ok := runner.readMemory(info.Address, length)
	if !ok {
		return ""
	}
	if info.Unicode != 0 {
		values := make([]uint16, 0, len(data)/2)
		for index := 0; index+1 < len(data); index += 2 {
			value := binary.LittleEndian.Uint16(data[index : index+2])
			if value == 0 {
				break
			}
			values = append(values, value)
		}
		return syscall.UTF16ToString(values)
	}
	return strings.TrimRight(string(data), "\x00")
}

func finalPath(handle syscall.Handle) string {
	if handle == 0 || handle == syscall.InvalidHandle {
		return ""
	}
	buffer := make([]uint16, 32768)
	length, _, _ := procGetFinalPathNameByHandle.Call(uintptr(handle), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0)
	if length == 0 || length >= uintptr(len(buffer)) {
		return ""
	}
	path := syscall.UTF16ToString(buffer[:length])
	return strings.TrimPrefix(path, `\\?\`)
}

func quoteWindowsArgument(value string) string {
	if value == "" {
		return `""`
	}
	if !strings.ContainsAny(value, " \t\"") {
		return value
	}
	var builder strings.Builder
	builder.WriteByte('"')
	backslashes := 0
	for _, character := range value {
		if character == '\\' {
			backslashes++
			continue
		}
		if character == '"' {
			builder.WriteString(strings.Repeat("\\", backslashes*2+1))
			builder.WriteRune(character)
			backslashes = 0
			continue
		}
		builder.WriteString(strings.Repeat("\\", backslashes))
		backslashes = 0
		builder.WriteRune(character)
	}
	builder.WriteString(strings.Repeat("\\", backslashes*2))
	builder.WriteByte('"')
	return builder.String()
}

func (runner *Runner) emit(value map[string]any) {
	value["time"] = time.Now().UTC()
	value["elapsed_ms"] = time.Since(runner.start).Milliseconds()
	_ = runner.encoder.Encode(value)
}

func u32(data []byte, offset int) uint32 { return binary.LittleEndian.Uint32(data[offset : offset+4]) }
func hexPointer(value uintptr) string    { return fmt.Sprintf("0x%X", value) }

package winlaunch

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	createSuspended      = 0x00000004
	memCommit            = 0x00001000
	memReserve           = 0x00002000
	pageReadWrite        = 0x00000004
	pageExecuteReadWrite = 0x00000040
	waitObject0          = 0x00000000
	waitTimeout          = 0x00000102

	// Core posts WM_QUIT while QQTModules is synchronously replacing the
	// directory selector with QQTSection. The protected bootstrap consumed this
	// hand-off message; the reconstructed TP-free image has only the ordinary
	// CRT message loop, so the same message would otherwise terminate it before
	// the resolved QQTSection interface can open the lobby.
	corePostQuitWrapperReturnRVA         = uintptr(0x2622)
	coreStageTransitionPostQuitReturnRVA = uintptr(0xF83B)
)

var (
	kernel32                  = syscall.NewLazyDLL("kernel32.dll")
	procVirtualAllocEx        = kernel32.NewProc("VirtualAllocEx")
	procVirtualProtectEx      = kernel32.NewProc("VirtualProtectEx")
	procWriteProcessMemory    = kernel32.NewProc("WriteProcessMemory")
	procReadProcessMemory     = kernel32.NewProc("ReadProcessMemory")
	procVirtualQueryEx        = kernel32.NewProc("VirtualQueryEx")
	procFlushInstruction      = kernel32.NewProc("FlushInstructionCache")
	procEnumProcessModulesEx  = kernel32.NewProc("K32EnumProcessModulesEx")
	procGetModuleFileNameEx   = kernel32.NewProc("K32GetModuleFileNameExW")
	procResumeThread          = kernel32.NewProc("ResumeThread")
	procSuspendThread         = kernel32.NewProc("SuspendThread")
	procOpenThread            = kernel32.NewProc("OpenThread")
	procCreateToolhelp32      = kernel32.NewProc("CreateToolhelp32Snapshot")
	procThread32First         = kernel32.NewProc("Thread32First")
	procThread32Next          = kernel32.NewProc("Thread32Next")
	procWow64GetThreadContext = kernel32.NewProc("Wow64GetThreadContext")
	procWow64SetThreadContext = kernel32.NewProc("Wow64SetThreadContext")
	procWaitForSingle         = kernel32.NewProc("WaitForSingleObject")
	procGetExitCode           = kernel32.NewProc("GetExitCodeProcess")
	procGetExitCodeThread     = kernel32.NewProc("GetExitCodeThread")
	procCreateRemoteThread    = kernel32.NewProc("CreateRemoteThread")
	procTerminate             = kernel32.NewProc("TerminateProcess")
)

type Options struct {
	Executable              string
	WorkingDir              string
	Arguments               []string
	TPFree                  bool
	ReservePages            []uintptr
	TPContextValue          uint32
	TPContextValueSet       bool
	FixTPContextRead        bool
	FixTPEarlySystemScan    bool
	FixTPEarlySystemHeader  bool
	SuppressTPWarning       bool
	DisableIME              bool
	TrapExit                bool
	BypassNativeTerminate   bool
	HandleTPBreakpoints     bool
	DialogReturn            int32
	TrapWarningFormat       bool
	TrapTPDialogEntry       bool
	TraceTPPostDialog       bool
	TraceTPPostDialogVM     bool
	BypassTPPostReturn      bool
	TPPostReturnMode        string
	SampleTPPostDialog      bool
	SampleTPPostProcess     bool
	TrapTPPostTerminate     bool
	BypassTPPostTerminate   bool
	TrapTPPostAccessAV      bool
	BypassTPDialogEntry     bool
	ClearTPDialogFrame      bool
	TrapWarningBuffer       bool
	BypassTPFailureFrame    bool
	BypassTPEntry           bool
	TrapTPArgs              bool
	TrapTPFirstArg          bool
	SpoofWin7               bool
	BypassTPErrorHandler    bool
	TraceTPDispatcher       bool
	TraceTPPreError         bool
	TraceTPWorkerVM         bool
	BypassTPDispatcher      bool
	BypassTPSecondError     bool
	BypassTPThirdError      bool
	TPThirdReturnEAX        uint32
	TPThirdReturnEAXSet     bool
	TrapTPSecondError       bool
	TrapAccessViolation     bool
	TraceExceptions         bool
	TraceStartupDebugger    bool
	TraceTPThirdVM          bool
	TraceModuleHandle       bool
	TraceTPSystemAPIs       bool
	ClearAdjustTokenError   bool
	ClearAdjustTokenCall    uint32
	BypassTesSafeDriver     bool
	TraceTesSafeVM          bool
	TraceTesSafeIOReturn    bool
	TraceTesSafeConsumer    bool
	TraceTesSafeChecksum    bool
	TraceClientBaseEntry    bool
	TraceClientBaseThreads  bool
	BypassClientBaseDLLMain bool
	BypassTPWorkerThread    bool
	ZeroTPWorkerErrorClass  bool
	BypassTPWorkerFatal     bool
	ClearTesSafeStatus100   bool
	CaptureClientBaseRVA    uint32
	CaptureClientBaseSet    bool
	CaptureClientBaseSize   uint32
	TraceTPEarlySystemState bool
	BypassTPPostHandshake   bool
	TraceKernelETWWrapper   bool
	TraceTPETWCallerPath    bool
	BypassTPETWRegister     bool
	BypassTPFinalError      bool
	GateClientBase          bool
	GateUnpackedEntry       bool
	AllowMultiClient        bool
	FixLocalShopOrdering    bool
	HighResolutionTimer     bool
	Wait                    time.Duration
	Output                  io.Writer
}

type Result struct {
	PID                    uint32                     `json:"pid"`
	MainThreadID           uint32                     `json:"main_thread_id"`
	Executable             string                     `json:"executable"`
	OSVersion              string                     `json:"os_version,omitempty"`
	ReservedPages          []string                   `json:"reserved_pages"`
	SeededDWORDs           []string                   `json:"seeded_dwords,omitempty"`
	Running                bool                       `json:"running"`
	ExitCode               *int32                     `json:"exit_code,omitempty"`
	ElapsedMS              int64                      `json:"elapsed_ms"`
	ImportStubs            []ImportStub               `json:"import_stubs,omitempty"`
	IMEDisabled            bool                       `json:"ime_disabled"`
	TimerResolutionMS      uint32                     `json:"timer_resolution_ms,omitempty"`
	VEHInstalled           bool                       `json:"veh_installed"`
	HandledBreakpoints     uint32                     `json:"handled_breakpoints"`
	WarningBufferTrapped   bool                       `json:"warning_buffer_trapped"`
	TPEntryBypassArmed     bool                       `json:"tp_entry_bypass_armed"`
	TPEntryBypassHits      uint32                     `json:"tp_entry_bypass_hits"`
	TPArgsTrapped          bool                       `json:"tp_args_trapped"`
	TPFirstArgTrapped      bool                       `json:"tp_first_arg_trapped"`
	TPSecondErrorTrapped   bool                       `json:"tp_second_error_trapped"`
	TPSecondBypassHits     uint32                     `json:"tp_second_bypass_hits"`
	TPThirdBypassHits      uint32                     `json:"tp_third_bypass_hits"`
	AccessViolation        *AccessViolation           `json:"access_violation,omitempty"`
	ExceptionTotal         uint32                     `json:"exception_total,omitempty"`
	ExceptionTrace         []ExceptionCapture         `json:"exception_trace,omitempty"`
	StartupDebugExceptions []StartupDebugException    `json:"startup_debug_exceptions,omitempty"`
	StartupDebugExitCode   *int32                     `json:"startup_debug_exit_code,omitempty"`
	StartupDebuggerError   string                     `json:"startup_debugger_error,omitempty"`
	TPDispatcherTotal      uint32                     `json:"tp_dispatcher_total,omitempty"`
	TPDispatcherTrace      []TPDispatcherStep         `json:"tp_dispatcher_trace,omitempty"`
	TPWorkerThreadID       uint32                     `json:"tp_worker_thread_id,omitempty"`
	TPWorkerVMTotal        uint32                     `json:"tp_worker_vm_total,omitempty"`
	TPWorkerVMTrace        []TPDispatcherStep         `json:"tp_worker_vm_trace,omitempty"`
	TPWorkerFatalArmedTID  uint32                     `json:"tp_worker_fatal_armed_tid,omitempty"`
	TPWorkerFatalHits      uint32                     `json:"tp_worker_fatal_bypass_hits,omitempty"`
	TPWorkerFatalCandidate *TPWorkerFatalCapture      `json:"tp_worker_fatal_candidate,omitempty"`
	TPThirdVMTotal         uint32                     `json:"tp_third_vm_total,omitempty"`
	TPThirdVMTrace         []TPDispatcherStep         `json:"tp_third_vm_trace,omitempty"`
	TPPostDialogVMTotal    uint32                     `json:"tp_post_dialog_vm_total,omitempty"`
	TPPostDialogVMTrace    []TPDispatcherStep         `json:"tp_post_dialog_vm_trace,omitempty"`
	TPPostDialogThreadID   uint32                     `json:"tp_post_dialog_thread_id,omitempty"`
	TPPostDialogSamples    uint32                     `json:"tp_post_dialog_sample_total,omitempty"`
	TPPostDialogFailures   uint32                     `json:"tp_post_dialog_sample_failures,omitempty"`
	TPPostDialogTrace      []TPThreadSample           `json:"tp_post_dialog_thread_trace,omitempty"`
	TPPostProcessSamples   uint32                     `json:"tp_post_dialog_process_sample_total,omitempty"`
	TPPostProcessFailure   uint32                     `json:"tp_post_dialog_process_sample_failures,omitempty"`
	TPPostProcessTrace     []TPThreadSample           `json:"tp_post_dialog_process_trace,omitempty"`
	TPPostTerminateArmed   bool                       `json:"tp_post_dialog_terminate_armed,omitempty"`
	TPPostTerminateBypass  bool                       `json:"tp_post_dialog_terminate_bypass,omitempty"`
	TPPostTerminateError   string                     `json:"tp_post_dialog_terminate_error,omitempty"`
	TPPostTerminate        *NativeTerminateCapture    `json:"tp_post_dialog_terminate,omitempty"`
	TPPostAccessViolation  *AccessViolation           `json:"tp_post_dialog_access_violation,omitempty"`
	TesSafeVMTotal         uint32                     `json:"tessafe_vm_total,omitempty"`
	TesSafeVMTrace         []TPDispatcherStep         `json:"tessafe_vm_trace,omitempty"`
	TesSafeIOReturnTotal   uint32                     `json:"tessafe_io_return_total,omitempty"`
	TesSafeIOReturnTrace   []TPDispatcherStep         `json:"tessafe_io_return_trace,omitempty"`
	TesSafeConsumerTotal   uint32                     `json:"tessafe_consumer_total,omitempty"`
	TesSafeConsumerTrace   []TesSafeConsumerStep      `json:"tessafe_consumer_trace,omitempty"`
	TesSafeChecksumTotal   uint32                     `json:"tessafe_checksum_total,omitempty"`
	TesSafeChecksumTrace   []TPDispatcherStep         `json:"tessafe_checksum_trace,omitempty"`
	ClientBaseCode         *ModuleCodeCapture         `json:"clientbase_code_capture,omitempty"`
	TPEarlySystemState     *TPEarlySystemState        `json:"tp_early_system_state,omitempty"`
	TPEarlySystemHeader    *TPEarlySystemHeaderReport `json:"tp_early_system_header,omitempty"`
	TPETWCallerPath        *TPETWCallBoundaryCapture  `json:"tp_etw_caller_path,omitempty"`
	TPStartupTimeoutTrace  []TPThreadSample           `json:"tp_startup_timeout_threads,omitempty"`
	TPFinalErrorClears     uint32                     `json:"tp_final_error_clears,omitempty"`
	ClientBaseGateUsed     bool                       `json:"clientbase_gate_used,omitempty"`
	UnpackedEntryGate      *UnpackedEntryGateReport   `json:"unpacked_entry_gate,omitempty"`
}

type asynchronousStubResult struct {
	stub ImportStub
	err  error
}

type ModuleCodeCapture struct {
	Module  string `json:"module"`
	RVA     string `json:"rva"`
	Address string `json:"address"`
	Size    uint32 `json:"size"`
	Hex     string `json:"hex"`
}

type TPEarlySystemState struct {
	KernelBase       string `json:"kernelbase"`
	HeaderAddress    string `json:"header_address"`
	HeaderValue      string `json:"header_value"`
	ValueMemoryState uint32 `json:"value_memory_state,omitempty"`
	ValueMemoryBase  string `json:"value_memory_base,omitempty"`
	ValueMemorySize  uint32 `json:"value_memory_size,omitempty"`
	ValueMemoryHex   string `json:"value_memory_hex,omitempty"`
}

type TesSafeConsumerStep struct {
	Index         uint32   `json:"index"`
	ThreadID      uint32   `json:"thread_id"`
	EAX           uint32   `json:"eax"`
	EBX           uint32   `json:"ebx"`
	ECX           uint32   `json:"ecx"`
	EDX           uint32   `json:"edx"`
	ESI           uint32   `json:"esi"`
	EDI           uint32   `json:"edi"`
	EBP           uint32   `json:"ebp"`
	ESP           uint32   `json:"esp"`
	EFlags        uint32   `json:"eflags"`
	ObjectAddress string   `json:"object_address"`
	ObjectWords   []uint32 `json:"object_words"`
	ObjectHex     string   `json:"object_hex"`
	BufferAddress string   `json:"buffer_address"`
	BufferHex     string   `json:"buffer_hex"`
}

type AccessViolation struct {
	ExceptionCode    uint32   `json:"exception_code"`
	ExceptionAddress uint32   `json:"exception_address"`
	ThreadID         uint32   `json:"thread_id"`
	Operation        uint32   `json:"operation"`
	TargetAddress    uint32   `json:"target_address"`
	EIP              uint32   `json:"eip"`
	ESP              uint32   `json:"esp"`
	EBP              uint32   `json:"ebp"`
	EAX              uint32   `json:"eax"`
	EBX              uint32   `json:"ebx"`
	ECX              uint32   `json:"ecx"`
	EDX              uint32   `json:"edx"`
	ESI              uint32   `json:"esi"`
	EDI              uint32   `json:"edi"`
	EFlags           uint32   `json:"eflags"`
	CodeBase         string   `json:"code_base,omitempty"`
	CodeHex          string   `json:"code_hex,omitempty"`
	MemoryBase       string   `json:"memory_base,omitempty"`
	AllocationBase   string   `json:"allocation_base,omitempty"`
	RegionSize       string   `json:"region_size,omitempty"`
	MemoryState      string   `json:"memory_state,omitempty"`
	MemoryProtect    string   `json:"memory_protect,omitempty"`
	MemoryType       string   `json:"memory_type,omitempty"`
	Stack            []string `json:"stack,omitempty"`
}

type ExceptionCapture struct {
	Index            uint32 `json:"index"`
	ExceptionCode    uint32 `json:"exception_code"`
	ExceptionFlags   uint32 `json:"exception_flags"`
	ExceptionAddress uint32 `json:"exception_address"`
	ParameterCount   uint32 `json:"parameter_count"`
	Parameter0       uint32 `json:"parameter_0"`
	Parameter1       uint32 `json:"parameter_1"`
	EIP              uint32 `json:"eip"`
	ESP              uint32 `json:"esp"`
	EBP              uint32 `json:"ebp"`
	EAX              uint32 `json:"eax"`
	EBX              uint32 `json:"ebx"`
	ECX              uint32 `json:"ecx"`
	EDX              uint32 `json:"edx"`
	ESI              uint32 `json:"esi"`
	EDI              uint32 `json:"edi"`
	EFlags           uint32 `json:"eflags"`
	ThreadID         uint32 `json:"thread_id"`
}

type TPWorkerFatalCapture struct {
	Count            uint32 `json:"count"`
	ThreadID         uint32 `json:"thread_id"`
	ExceptionCode    uint32 `json:"exception_code"`
	ExceptionAddress uint32 `json:"exception_address"`
	EIP              uint32 `json:"eip"`
	EAX              uint32 `json:"eax"`
	ParameterCount   uint32 `json:"parameter_count"`
	Parameter0       uint32 `json:"parameter_0"`
	Parameter1       uint32 `json:"parameter_1"`
}

type TPDispatcherStep struct {
	Index       uint32   `json:"index"`
	ThreadID    uint32   `json:"thread_id,omitempty"`
	Destination uint32   `json:"destination"`
	EBX         uint32   `json:"ebx"`
	ECX         uint32   `json:"ecx"`
	EDX         uint32   `json:"edx"`
	ESI         uint32   `json:"esi"`
	EDI         uint32   `json:"edi"`
	EBP         uint32   `json:"ebp"`
	ESP         uint32   `json:"esp"`
	EFlags      uint32   `json:"eflags"`
	Stack       []uint32 `json:"stack"`
}

type TPThreadSample struct {
	Index          uint32   `json:"index"`
	ElapsedMS      int64    `json:"elapsed_ms"`
	ThreadID       uint32   `json:"thread_id"`
	EIP            uint32   `json:"eip"`
	ESP            uint32   `json:"esp"`
	EBP            uint32   `json:"ebp"`
	EAX            uint32   `json:"eax"`
	EBX            uint32   `json:"ebx"`
	ECX            uint32   `json:"ecx"`
	EDX            uint32   `json:"edx"`
	ESI            uint32   `json:"esi"`
	EDI            uint32   `json:"edi"`
	EFlags         uint32   `json:"eflags"`
	Module         string   `json:"module,omitempty"`
	RVA            uint32   `json:"rva,omitempty"`
	AllocationBase string   `json:"allocation_base,omitempty"`
	MemoryProtect  string   `json:"memory_protect,omitempty"`
	MemoryType     string   `json:"memory_type,omitempty"`
	ImagePath      string   `json:"image_path,omitempty"`
	CommandLine    string   `json:"command_line,omitempty"`
	Stack          []uint32 `json:"stack,omitempty"`
}

type remoteModuleRange struct {
	base uint32
	size uint32
	name string
}

type threadEntry32 struct {
	Size           uint32
	Usage          uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePriority   int32
	DeltaPriority  int32
	Flags          uint32
}

type tpThreadSampleBuffer struct {
	mu       sync.Mutex
	threadID uint32
	total    uint32
	failures uint32
	next     int
	samples  []TPThreadSample
}

type lateTerminateState struct {
	mu        sync.Mutex
	installed bool
	stub      ImportStub
	err       string
	shadow    atomic.Value
}

type WarningFormatCapture struct {
	Hits               uint32 `json:"hits"`
	ThreadID           uint32 `json:"thread_id"`
	CallerAddress      string `json:"caller_address"`
	DestinationAddress string `json:"destination_address"`
	FormatAddress      string `json:"format_address"`
	LabelAddress       string `json:"label_address"`
	FormatText         string `json:"format_text,omitempty"`
	LabelHex           string `json:"label_hex,omitempty"`
	Reason             uint32 `json:"reason"`
	Code               uint32 `json:"code"`
	Detail             uint32 `json:"detail"`
}

type TPDialogCapture struct {
	Hits          uint32 `json:"hits"`
	ThreadID      uint32 `json:"thread_id"`
	CallerAddress string `json:"caller_address"`
	Instance      string `json:"instance"`
	Template      string `json:"template"`
	ParentWindow  string `json:"parent_window"`
	DialogProc    string `json:"dialog_proc"`
	InitParam     string `json:"init_param"`
	EAX           uint32 `json:"eax"`
	EBX           uint32 `json:"ebx"`
	ECX           uint32 `json:"ecx"`
	EDX           uint32 `json:"edx"`
	ESI           uint32 `json:"esi"`
	EDI           uint32 `json:"edi"`
	EBP           uint32 `json:"ebp"`
	EFlags        uint32 `json:"eflags"`
	EntryESP      uint32 `json:"entry_esp"`
}

type TPPostDialogCapture struct {
	Hits          uint32 `json:"hits"`
	ThreadID      uint32 `json:"thread_id"`
	CallerAddress string `json:"caller_address"`
	EAX           uint32 `json:"eax"`
	EBX           uint32 `json:"ebx"`
	ECX           uint32 `json:"ecx"`
	EDX           uint32 `json:"edx"`
	ESI           uint32 `json:"esi"`
	EDI           uint32 `json:"edi"`
	EBP           uint32 `json:"ebp"`
	EntryESP      uint32 `json:"entry_esp"`
	EFlags        uint32 `json:"eflags"`
}

type NativeTerminateCapture struct {
	Hits           uint32   `json:"hits"`
	ThreadID       uint32   `json:"thread_id"`
	CallerAddress  string   `json:"caller_address"`
	ProcessHandle  string   `json:"process_handle"`
	ExitStatus     string   `json:"exit_status"`
	EAX            uint32   `json:"eax"`
	EBX            uint32   `json:"ebx"`
	ECX            uint32   `json:"ecx"`
	EDX            uint32   `json:"edx"`
	ESI            uint32   `json:"esi"`
	EDI            uint32   `json:"edi"`
	EBP            uint32   `json:"ebp"`
	EntryESP       uint32   `json:"entry_esp"`
	EFlags         uint32   `json:"eflags"`
	CallerCodeBase string   `json:"caller_code_base,omitempty"`
	CallerCodeHex  string   `json:"caller_code_hex,omitempty"`
	Stack          []string `json:"stack,omitempty"`
}

type tpTraceShadow struct {
	Total uint32
	Trace []byte
}

type ImportStub struct {
	Module                      string                  `json:"module"`
	ModulePath                  string                  `json:"module_path,omitempty"`
	ModuleBase                  string                  `json:"module_base,omitempty"`
	ModuleVersion               string                  `json:"module_version,omitempty"`
	Library                     string                  `json:"library"`
	Symbol                      string                  `json:"symbol"`
	IATAddress                  string                  `json:"iat_address"`
	StubAddress                 string                  `json:"stub_address"`
	Behavior                    string                  `json:"behavior"`
	Rewrites                    uint64                  `json:"rewrites"`
	TargetAddress               string                  `json:"target_address"`
	TargetRVA                   string                  `json:"target_rva,omitempty"`
	TargetSection               string                  `json:"target_section,omitempty"`
	TargetOriginal              string                  `json:"target_original_bytes"`
	TargetContext               string                  `json:"target_context,omitempty"`
	HelperAddress               string                  `json:"helper_address,omitempty"`
	HelperRVA                   string                  `json:"helper_rva,omitempty"`
	TargetRewrites              uint64                  `json:"target_rewrites"`
	CloneRewrites               uint64                  `json:"private_clone_rewrites"`
	Calls                       uint32                  `json:"calls,omitempty"`
	CorrectedCalls              uint32                  `json:"corrected_calls,omitempty"`
	BypassedCalls               uint32                  `json:"bypassed_calls,omitempty"`
	EntryStack                  []string                `json:"entry_stack,omitempty"`
	CapturedText                string                  `json:"captured_text,omitempty"`
	CapturedHex                 string                  `json:"captured_hex,omitempty"`
	CallerAddress               string                  `json:"caller_address,omitempty"`
	CallerCodeBase              string                  `json:"caller_code_base,omitempty"`
	CallerCodeHex               string                  `json:"caller_code_hex,omitempty"`
	RawEntryStack               []string                `json:"raw_entry_stack,omitempty"`
	WarningFormat               *WarningFormatCapture   `json:"warning_format,omitempty"`
	TPDialog                    *TPDialogCapture        `json:"tp_dialog,omitempty"`
	TPPostDialog                *TPPostDialogCapture    `json:"tp_post_dialog,omitempty"`
	NativeTerminate             *NativeTerminateCapture `json:"native_terminate,omitempty"`
	TesSafeDeviceCalls          []TesSafeDeviceCall     `json:"tessafe_device_calls,omitempty"`
	ThreadCreations             []ThreadCreationCall    `json:"thread_creations,omitempty"`
	iatValue                    uintptr
	stubValue                   uintptr
	targetValue                 uintptr
	targetBytes                 []byte
	originalBytes               []byte
	targetModuleBase            uintptr
	strictTargetRestore         bool
	helperValue                 uintptr
	counterValue                uintptr
	correctedValue              uintptr
	bypassedValue               uintptr
	traceValue                  uintptr
	textValue                   uintptr
	textUTF16                   bool
	binaryValue                 uintptr
	binarySize                  uintptr
	callerValue                 uintptr
	rawStackValue               uintptr
	rawStackWords               int
	formatCaptureValue          uintptr
	dialogCaptureValue          uintptr
	expectedCallerValue         uintptr
	expectedOuterCallerValue    uintptr
	tpDialogReturn              bool
	postDialogCaptureValue      uintptr
	nativeTerminateCaptureValue uintptr
	deviceRingValue             uintptr
	deviceRingEntries           int
	deviceRingStride            int
	threadRingValue             uintptr
	threadRingEntries           int
	threadRingStride            int
}

type TesSafeDeviceCall struct {
	Index                uint32 `json:"index"`
	Caller               string `json:"caller"`
	Handle               string `json:"handle"`
	ControlCode          string `json:"control_code"`
	InputAddress         string `json:"input_address"`
	InputSize            uint32 `json:"input_size"`
	OutputAddress        string `json:"output_address"`
	OutputSize           uint32 `json:"output_size"`
	BytesReturnedAddress string `json:"bytes_returned_address"`
	OverlappedAddress    string `json:"overlapped_address"`
	InputHex             string `json:"input_hex,omitempty"`
}

type ThreadCreationCall struct {
	Index            uint32 `json:"index"`
	Caller           string `json:"caller"`
	ThreadAttributes string `json:"thread_attributes"`
	StackSize        uint32 `json:"stack_size"`
	StartAddress     string `json:"start_address"`
	Parameter        string `json:"parameter"`
	CreationFlags    uint32 `json:"creation_flags"`
	ThreadIDAddress  string `json:"thread_id_address"`
	Handle           string `json:"handle"`
	ThreadID         uint32 `json:"thread_id"`
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

func Launch(options Options) (Result, error) {
	if options.Output == nil {
		options.Output = os.Stdout
	}
	if options.TraceTPPreError && (options.TraceTPDispatcher || options.BypassTPDispatcher) {
		return Result{}, fmt.Errorf("pre-error TP trace cannot share the dispatcher hook with trace/bypass modes")
	}
	if options.TraceTPWorkerVM && (options.TraceTPPreError || options.TraceTPDispatcher || options.BypassTPDispatcher || options.TraceTPPostDialogVM) {
		return Result{}, fmt.Errorf("TP worker VM trace cannot share the protected dispatcher hook with other dispatcher modes")
	}
	if options.TraceTPWorkerVM && options.BypassTPWorkerThread {
		return Result{}, fmt.Errorf("TP worker VM trace and whole-worker bypass are mutually exclusive")
	}
	if options.ZeroTPWorkerErrorClass && (options.TraceTPWorkerVM || options.BypassTPWorkerThread) {
		return Result{}, fmt.Errorf("TP worker error-class zeroing cannot share the worker-entry trace/bypass")
	}
	if options.TraceTPPostDialogVM && !options.BypassTPDialogEntry {
		return Result{}, fmt.Errorf("post-dialog VM trace requires the exact TP dialog bypass")
	}
	if options.BypassTPPostReturn && !options.BypassTPDialogEntry {
		return Result{}, fmt.Errorf("post-dialog outer-return recovery requires the exact TP dialog bypass")
	}
	if options.BypassTPPostReturn && options.TraceTPPostDialog {
		return Result{}, fmt.Errorf("post-dialog outer-return recovery and freeze trace are mutually exclusive")
	}
	if options.SampleTPPostDialog && !options.BypassTPDialogEntry {
		return Result{}, fmt.Errorf("post-dialog thread sampling requires the exact TP dialog bypass")
	}
	if options.SampleTPPostProcess && !options.BypassTPDialogEntry {
		return Result{}, fmt.Errorf("post-dialog process sampling requires the exact TP dialog bypass")
	}
	if options.TrapTPPostTerminate && !options.BypassTPDialogEntry {
		return Result{}, fmt.Errorf("post-dialog native-termination trap requires the exact TP dialog bypass")
	}
	if options.BypassTPPostTerminate && !options.BypassTPDialogEntry {
		return Result{}, fmt.Errorf("post-dialog native-termination bypass requires the exact TP dialog bypass")
	}
	if options.TrapTPPostTerminate && options.BypassTPPostTerminate {
		return Result{}, fmt.Errorf("post-dialog native-termination trap and bypass are mutually exclusive")
	}
	if options.ClearTPDialogFrame && !options.BypassTPDialogEntry {
		return Result{}, fmt.Errorf("TP dialog error-frame clearing requires the exact TP dialog bypass")
	}
	if options.TraceTPPostDialogVM && (options.TraceTPPreError || options.TraceTPDispatcher || options.BypassTPDispatcher) {
		return Result{}, fmt.Errorf("post-dialog VM trace cannot share the protected dispatcher hook with other dispatcher modes")
	}
	if options.ClearAdjustTokenCall != 0 && !options.ClearAdjustTokenError {
		return Result{}, fmt.Errorf("AdjustTokenPrivileges correction call requires the error shim")
	}
	if options.BypassTPFinalError {
		return Result{}, fmt.Errorf("final-error tuple bypass is disabled: synchronized evidence identifies that signature as python23 metadata, not a TP error object")
	}
	if options.TraceKernelETWWrapper && options.BypassTPETWRegister {
		return Result{}, fmt.Errorf("private Windows ETW wrapper tracing and ClientBase call-boundary bypass are mutually exclusive")
	}
	if options.GateUnpackedEntry && options.TraceStartupDebugger {
		return Result{}, fmt.Errorf("unpacked-entry gate and startup exception debugger cannot attach simultaneously")
	}
	if options.SuppressTPWarning && (options.TrapTPDialogEntry || options.TraceTPPostDialog || options.BypassTPDialogEntry) {
		return Result{}, fmt.Errorf("TP dialog suppression and TP dialog-entry trap cannot be enabled together")
	}
	dialogModes := 0
	for _, enabled := range []bool{options.TrapTPDialogEntry, options.TraceTPPostDialog, options.BypassTPDialogEntry} {
		if enabled {
			dialogModes++
		}
	}
	if dialogModes > 1 {
		return Result{}, fmt.Errorf("TP dialog-entry freeze, post-dialog trace, and dialog bypass are mutually exclusive")
	}
	executable, err := filepath.Abs(options.Executable)
	if err != nil {
		return Result{}, fmt.Errorf("resolve executable: %w", err)
	}
	if _, err := os.Stat(executable); err != nil {
		return Result{}, fmt.Errorf("stat executable: %w", err)
	}
	workingDir := options.WorkingDir
	if workingDir == "" {
		workingDir = filepath.Dir(executable)
	}
	workingDir, err = filepath.Abs(workingDir)
	if err != nil {
		return Result{}, fmt.Errorf("resolve working directory: %w", err)
	}

	application, err := syscall.UTF16PtrFromString(executable)
	if err != nil {
		return Result{}, err
	}
	commandLine := quoteWindowsArgument(executable)
	for _, argument := range options.Arguments {
		commandLine += " " + quoteWindowsArgument(argument)
	}
	commandLinePointer, err := syscall.UTF16PtrFromString(commandLine)
	if err != nil {
		return Result{}, err
	}
	directory, err := syscall.UTF16PtrFromString(workingDir)
	if err != nil {
		return Result{}, err
	}

	var startup syscall.StartupInfo
	startup.Cb = uint32(unsafe.Sizeof(startup))
	var process syscall.ProcessInformation
	started := time.Now()
	if err := syscall.CreateProcess(application, commandLinePointer, nil, nil, false, createSuspended, nil, directory, &startup, &process); err != nil {
		return Result{}, fmt.Errorf("CreateProcess suspended: %w", err)
	}
	defer syscall.CloseHandle(process.Process)
	defer syscall.CloseHandle(process.Thread)
	failed := true
	defer func() {
		if failed {
			_, _, _ = procTerminate.Call(uintptr(process.Process), 0xDEAD)
		}
	}()

	result := Result{
		PID:           process.ProcessId,
		MainThreadID:  process.ThreadId,
		Executable:    executable,
		OSVersion:     currentWindowsVersion(),
		ReservedPages: make([]string, 0),
	}
	var multiClientPatch chan asynchronousStubResult
	if options.TPFree && options.AllowMultiClient {
		// The reconstructed client reaches Core.dll much faster because it has no
		// protection bootstrap. Arm the module watcher before releasing its
		// initially suspended main thread so the verified singleton branch can
		// never win the race.
		multiClientPatch = make(chan asynchronousStubResult, 1)
		go func() {
			stub, patchErr := installCoreMultiClientBypass(
				process.Process, process.Thread, process.ProcessId, 30*time.Second,
			)
			multiClientPatch <- asynchronousStubResult{stub: stub, err: patchErr}
		}()
	}
	tpContextPageReserved := false
	for _, page := range options.ReservePages {
		if page < 0x10000 || page&0xFFF != 0 {
			return Result{}, fmt.Errorf("reserve page must be page-aligned and above the null guard: 0x%X", page)
		}
		var existing memoryBasicInformation
		allocated, _, callErr := procVirtualAllocEx.Call(uintptr(process.Process), page, 0x1000, memReserve|memCommit, pageReadWrite)
		if allocated != page {
			queried, _, _ := procVirtualQueryEx.Call(
				uintptr(process.Process), page, uintptr(unsafe.Pointer(&existing)), unsafe.Sizeof(existing),
			)
			if queried == unsafe.Sizeof(existing) {
				return Result{}, fmt.Errorf(
					"VirtualAllocEx at 0x%X: returned 0x%X (%v); existing base=0x%X allocation=0x%X size=0x%X state=0x%X protect=0x%X type=0x%X",
					page, allocated, callErr, existing.BaseAddress, existing.AllocationBase, existing.RegionSize,
					existing.State, existing.Protect, existing.Type,
				)
			}
			return Result{}, fmt.Errorf("VirtualAllocEx at 0x%X: returned 0x%X (%v)", page, allocated, callErr)
		}
		result.ReservedPages = append(result.ReservedPages, fmt.Sprintf("0x%X", page))
		if page == 0x21020000 {
			tpContextPageReserved = true
		}
	}
	if options.TPContextValueSet {
		const contextAddress = uintptr(0x210200F0)
		if !tpContextPageReserved {
			return Result{}, fmt.Errorf("TP context seed requires reserved page 0x21020000")
		}
		value := make([]byte, 4)
		binary.LittleEndian.PutUint32(value, options.TPContextValue)
		if err := writeRemote(process.Process, contextAddress, value); err != nil {
			return Result{}, fmt.Errorf("seed TP context dword at 0x%X: %w", contextAddress, err)
		}
		result.SeededDWORDs = append(result.SeededDWORDs,
			fmt.Sprintf("0x%08X=0x%08X", contextAddress, options.TPContextValue))
	}

	previousSuspendCount, _, resumeErr := procResumeThread.Call(uintptr(process.Thread))
	if previousSuspendCount == ^uintptr(0) {
		return Result{}, fmt.Errorf("ResumeThread: %w", resumeErr)
	}
	gateClientBase := options.GateClientBase || options.GateUnpackedEntry || options.BypassClientBaseDLLMain || options.BypassTPWorkerThread || options.TraceTPWorkerVM || options.ZeroTPWorkerErrorClass || options.BypassTPETWRegister || (options.AllowMultiClient && !options.TPFree) || options.FixTPEarlySystemHeader
	mainThreadGated := false
	if gateClientBase {
		clientBase, err := waitForModule(process.Process, process.ProcessId, "ClientBase.dll", 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("wait for early ClientBase gate: %w", err)
		}
		// A mapped module can still contain unresolved import RVAs while the
		// loader is snapping its IAT. Keep the main thread running until a
		// compatibility-critical slot becomes an executable system address,
		// then suspend before ClientBase initialization consumes it.
		adjustIAT, err := findImportAddress(process.Process, clientBase, "ADVAPI32.dll", "AdjustTokenPrivileges")
		if err != nil {
			return Result{}, fmt.Errorf("locate early ClientBase gate IAT: %w", err)
		}
		if _, err := waitForExecutablePointer(process.Process, adjustIAT, 5*time.Second); err != nil {
			return Result{}, fmt.Errorf("wait for resolved ClientBase gate IAT: %w", err)
		}
		previous, _, suspendErr := procSuspendThread.Call(uintptr(process.Thread))
		if previous == ^uintptr(0) {
			return Result{}, fmt.Errorf("suspend main thread for ClientBase gate: %w", suspendErr)
		}
		mainThreadGated = true
		result.ClientBaseGateUsed = true
	}
	gateReleased := false
	defer func() {
		if mainThreadGated && !gateReleased {
			procResumeThread.Call(uintptr(process.Thread))
		}
	}()
	var tpContextCompatibility *tpContextReadCompatibility
	if options.FixTPContextRead {
		contextStubs, compatibility, err := installTPContextReadCompatibility(
			process.Process, process.ProcessId, process.ThreadId, options.FixTPEarlySystemScan, 5*time.Second,
		)
		if err != nil {
			return Result{}, fmt.Errorf("install TP startup context-read compatibility: %w", err)
		}
		tpContextCompatibility = compatibility
		result.ImportStubs = append(result.ImportStubs, contextStubs...)
	}
	var tpEarlySystemHeader *tpEarlySystemHeaderCompatibility
	if options.FixTPEarlySystemHeader {
		compatibility, headerErr := prepareTPEarlySystemHeaderCompatibility(
			process.Process, process.ProcessId, 5*time.Second,
		)
		if headerErr != nil {
			return Result{}, fmt.Errorf("prepare TP early system-header compatibility: %w", headerErr)
		}
		tpEarlySystemHeader = compatibility
		result.TPEarlySystemHeader = compatibility.report
		defer compatibility.restore()
	}
	if options.BypassClientBaseDLLMain {
		entryStub, err := installClientBaseDLLMainBypass(process.Process, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install ClientBase PE-entry bypass: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, entryStub)
	}
	if options.BypassTPWorkerThread {
		workerStub, err := installTPWorkerThreadBypass(process.Process, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TP worker-thread bypass: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, workerStub)
	}
	if options.ZeroTPWorkerErrorClass {
		errorClassStub, err := installTPWorkerErrorClassZero(process.Process, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TP worker error-class zeroing: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, errorClassStub)
	}
	var tpWorkerThreadIDAddress uintptr
	var tpWorkerStartupGateFlag uintptr
	var tpWorkerStartupThreadID uintptr
	var tpWorkerStartupReplay uintptr
	if options.BypassTPWorkerFatal || options.BypassTPETWRegister {
		gateStub, gateFlag, gateThreadID, replayAddress, err := installTPWorkerStartupGate(process.Process, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TP worker startup gate: %w", err)
		}
		tpWorkerStartupGateFlag = gateFlag
		tpWorkerStartupThreadID = gateThreadID
		tpWorkerStartupReplay = replayAddress
		result.ImportStubs = append(result.ImportStubs, gateStub)
	}
	if options.TraceTPWorkerVM {
		workerStub, threadIDAddress, err := installTPWorkerThreadIDCapture(process.Process, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TP worker-thread ID capture: %w", err)
		}
		tpWorkerThreadIDAddress = threadIDAddress
		result.ImportStubs = append(result.ImportStubs, workerStub)
	}
	var accessViolationRecord uintptr
	var tpPostAccessViolationRecord uintptr
	var exceptionCounter uintptr
	var exceptionTrace uintptr
	var startupDebugger *startupDebugMonitor
	var unpackedEntryGateRecord uintptr
	var unpackedEntryAddress uintptr
	var tpWorkerFatalRecord uintptr
	if options.TrapAccessViolation {
		accessViolationRecord, err = installAccessViolationTrapVEH(process.Process, process.ProcessId, 0, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install access-violation trap: %w", err)
		}
	}
	if options.DisableIME {
		if err := disableRemoteIME(process.Process, process.ProcessId, 5*time.Second); err != nil {
			return Result{}, fmt.Errorf("disable remote IME: %w", err)
		}
		result.IMEDisabled = true
	}
	var breakpointCounter uintptr
	if options.HandleTPBreakpoints {
		breakpointCounter, err = installBreakpointVEH(process.Process, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TP breakpoint VEH: %w", err)
		}
		result.VEHInstalled = true
	}
	var tpEntryCounter uintptr
	if options.BypassTPEntry {
		tpEntryCounter, err = installTPEntryBypass(process.Process, process.Thread, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TP entry bypass: %w", err)
		}
		result.TPEntryBypassArmed = true
	}
	type patchCounters struct {
		pointer atomic.Uint64
		target  atomic.Uint64
	}
	var pinStops []chan struct{}
	var pinDones []chan struct{}
	var counters []*patchCounters
	var cloneRewrites atomic.Uint64
	var formatCloneRewrites atomic.Uint64
	var tpDialogCloneRewrites atomic.Uint64
	var tpFinalErrorClears atomic.Uint32
	var warningBufferTrapped atomic.Bool
	var tpArgsTrapped atomic.Bool
	var tpFirstArgTrapped atomic.Bool
	var tpSecondErrorTrapped atomic.Bool
	dialogIndex := -1
	formatIndex := -1
	tpDialogIndex := -1
	var dialogStub ImportStub
	var formatStub ImportStub
	var tpDialogStub ImportStub
	if options.SuppressTPWarning {
		stub, err := installReturnStub(process.Process, process.ProcessId, "ClientBase.dll", "USER32.dll", "MessageBoxA", 1, 16, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("suppress TP warning: %w", err)
		}
		dialogStub, err = installExportReturnStub(process.Process, process.ProcessId, "USER32.dll", "DialogBoxParamA", uint32(options.DialogReturn), 20, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("suppress TP dialog: %w", err)
		}
		dialogIndex = len(result.ImportStubs) + 1
		result.ImportStubs = append(result.ImportStubs, stub, dialogStub)
	}
	if options.TPFree && !options.TrapExit {
		handoffStub, err := installTPFreeStageTransitionGuard(process.Process, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TP-free channel-transition guard: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, handoffStub)
	}
	if options.TrapExit {
		// The reconstructed TP-free client deliberately has no ClientBase.dll.
		// Keep the legacy ClientBase IAT trap for protected-client diagnostics,
		// but do not make that obsolete module a prerequisite for TP-free exit
		// tracing. RtlExitUserProcess below catches the ordinary clean-exit path.
		if !options.TPFree {
			exitStub, err := installIATWaitStub(process.Process, process.ProcessId, "ClientBase.dll", "KERNEL32.dll", "ExitProcess", 5*time.Second)
			if err != nil {
				return Result{}, fmt.Errorf("trap ClientBase exit: %w", err)
			}
			result.ImportStubs = append(result.ImportStubs, exitStub)
		} else {
			quitStub, err := installExportWaitStub(process.Process, process.ProcessId, "USER32.dll", "PostQuitMessage", 5*time.Second)
			if err != nil {
				return Result{}, fmt.Errorf("trap TP-free PostQuitMessage: %w", err)
			}
			result.ImportStubs = append(result.ImportStubs, quitStub)
		}
		rtlExitStub, err := installExportWaitStub(process.Process, process.ProcessId, "NTDLL.dll", "RtlExitUserProcess", 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("trap process exit: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, rtlExitStub)
		// NtTerminateProcess accepts arbitrary process handles and even placing an
		// entry trampoline on it measurably changes this client's startup ordering.
		// Keep it completely untouched unless the broad legacy bypass is requested
		// explicitly; ordinary exit trapping is limited to the two self-exit APIs.
		if options.BypassNativeTerminate {
			ntTerminateStub, err := installExportReturnStub(process.Process, process.ProcessId, "NTDLL.dll", "NtTerminateProcess", 0, 8, 5*time.Second)
			if err != nil {
				return Result{}, fmt.Errorf("bypass native process termination: %w", err)
			}
			result.ImportStubs = append(result.ImportStubs, ntTerminateStub)
		}
	}
	if options.TrapWarningFormat {
		formatStub, err = installConditionalFormatWaitStub(process.Process, process.ProcessId, "ClientBase.dll", "USER32.dll", "wsprintfA", 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("trap TP warning format: %w", err)
		}
		formatIndex = len(result.ImportStubs)
		result.ImportStubs = append(result.ImportStubs, formatStub)
	}
	if options.TrapTPDialogEntry || options.TraceTPPostDialog || options.BypassTPDialogEntry || options.TrapTPPostAccessAV {
		tpDialogStub, err = installConditionalTPDialogWaitStub(
			process.Process, process.ProcessId, options.TraceTPPostDialog || options.BypassTPDialogEntry,
			options.ClearTPDialogFrame, uint32(options.DialogReturn), 5*time.Second,
		)
		if err != nil {
			return Result{}, fmt.Errorf("trap verified TP dialog entry: %w", err)
		}
		tpDialogIndex = len(result.ImportStubs)
		result.ImportStubs = append(result.ImportStubs, tpDialogStub)
		if options.TraceTPPostDialog || options.BypassTPPostReturn {
			recoveryMode := ""
			if options.BypassTPPostReturn {
				recoveryMode = options.TPPostReturnMode
				if recoveryMode == "" {
					recoveryMode = "full"
				}
				switch recoveryMode {
				case "full", "drop", "return":
				default:
					return Result{}, fmt.Errorf("invalid TP post-dialog recovery mode %q", recoveryMode)
				}
			}
			postDialogStub, err := installTPPostDialogReturnTrap(
				process.Process, process.ProcessId, recoveryMode, 5*time.Second,
			)
			if err != nil {
				return Result{}, fmt.Errorf("install TP post-dialog return trap: %w", err)
			}
			result.ImportStubs = append(result.ImportStubs, postDialogStub)
		}
		if options.TrapTPPostAccessAV {
			tpPostAccessViolationRecord, err = installAccessViolationTrapVEH(
				process.Process, process.ProcessId, tpDialogStub.dialogCaptureValue, 5*time.Second,
			)
			if err != nil {
				return Result{}, fmt.Errorf("install post-dialog access-violation trap: %w", err)
			}
		}
	}
	if options.BypassTPFailureFrame {
		recoveryStub, err := installTPFailureRecovery(process.Process, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TP failure-frame recovery: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, recoveryStub)
	}
	if options.SpoofWin7 {
		versionStub, err := installGetVersionExAWin7Stub(process.Process, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install Win7 version shim: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, versionStub)
	}
	if options.BypassTPErrorHandler {
		errorStub, err := installTPErrorHandlerBypass(process.Process, process.ProcessId, 10*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TP error-handler bypass: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, errorStub)
	}
	var tpDispatcherCounter uintptr
	var tpDispatcherTrace uintptr
	var tpWorkerVMCounter uintptr
	var tpWorkerVMTrace uintptr
	var tpThirdVMCounter uintptr
	var tpThirdVMTrace uintptr
	var tpPostDialogVMCounter uintptr
	var tpPostDialogVMTrace uintptr
	var tpPostDialogVMShadow atomic.Value
	var tpPostDialogThreadSamples tpThreadSampleBuffer
	var tpPostDialogProcessSamples tpThreadSampleBuffer
	var tpPostDialogTerminate lateTerminateState
	var tesSafeVMCounter uintptr
	var tesSafeVMTrace uintptr
	var tesSafeIOReturnCounter uintptr
	var tesSafeIOReturnTrace uintptr
	var tesSafeConsumerCounter uintptr
	var tesSafeConsumerTrace uintptr
	var tesSafeChecksumCounter uintptr
	var tesSafeChecksumTrace uintptr
	var tpSecondErrorTrapCounter uintptr
	var tpThirdErrorBypassCounter uintptr
	tpDispatcherTraceRing := false
	if options.TraceTPSystemAPIs {
		systemTraceStubs, err := installTPSystemAPITraces(
			process.Process, process.ProcessId, options.ClearAdjustTokenError, options.BypassTesSafeDriver, 5*time.Second,
		)
		if err != nil {
			return Result{}, fmt.Errorf("install TP system API traces: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, systemTraceStubs...)
	}
	if options.ClearAdjustTokenError {
		adjustStub, err := installAdjustTokenPrivilegesErrorShim(
			process.Process, process.ProcessId, options.ClearAdjustTokenCall, 5*time.Second,
		)
		if err != nil {
			return Result{}, fmt.Errorf("install AdjustTokenPrivileges error shim: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, adjustStub)
	}
	if options.BypassTesSafeDriver {
		tesSafeStubs, err := installTesSafeDriverCompatibility(process.Process, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TesSafe user-mode compatibility: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, tesSafeStubs...)
	}
	if options.TraceTesSafeVM {
		traceStub, counter, trace, err := installTesSafeVMTrace(process.Process, process.ProcessId, 10*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TesSafe VM trace: %w", err)
		}
		tesSafeVMCounter = counter
		tesSafeVMTrace = trace
		result.ImportStubs = append(result.ImportStubs, traceStub)
	}
	if options.TraceTesSafeIOReturn {
		traceStub, counter, trace, err := installTesSafeIOReturnTrace(process.Process, process.ProcessId, 10*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TesSafe IO return trace: %w", err)
		}
		tesSafeIOReturnCounter = counter
		tesSafeIOReturnTrace = trace
		result.ImportStubs = append(result.ImportStubs, traceStub)
	}
	if options.TraceTesSafeConsumer || options.ClearTesSafeStatus100 {
		traceStub, counter, trace, err := installTesSafeConsumerTrace(
			process.Process, process.ProcessId, options.ClearTesSafeStatus100, 10*time.Second,
		)
		if err != nil {
			return Result{}, fmt.Errorf("install TesSafe consumer trace: %w", err)
		}
		tesSafeConsumerCounter = counter
		tesSafeConsumerTrace = trace
		result.ImportStubs = append(result.ImportStubs, traceStub)
	}
	if options.TraceTesSafeChecksum {
		traceStub, counter, trace, err := installTesSafeChecksumTrace(process.Process, process.ProcessId, 10*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TesSafe checksum trace: %w", err)
		}
		tesSafeChecksumCounter = counter
		tesSafeChecksumTrace = trace
		result.ImportStubs = append(result.ImportStubs, traceStub)
	}
	if options.TraceClientBaseEntry {
		traceStub, err := installClientBaseEntryTrace(process.Process, process.ProcessId, 10*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install ClientBase ordinal-1 entry trace: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, traceStub)
	}
	if options.TraceClientBaseThreads {
		traceStub, err := installClientBaseCreateThreadTrace(process.Process, process.ProcessId, 10*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install ClientBase CreateThread trace: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, traceStub)
	}
	if options.BypassTPPostHandshake {
		bypassStub, err := installTPPostHandshakeBypass(process.Process, process.ProcessId, 10*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TP post-handshake bypass: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, bypassStub)
	}
	if options.TraceKernelETWWrapper {
		traceStub, err := installKernelETWWrapperTrace(
			process.Process, process.ProcessId, false, 10*time.Second,
		)
		if err != nil {
			return Result{}, fmt.Errorf("install kernel ETW wrapper trace: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, traceStub)
	}
	if options.TraceModuleHandle {
		moduleTraceStub, err := installGetModuleHandleATrace(process.Process, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install GetModuleHandleA trace: %w", err)
		}
		result.ImportStubs = append(result.ImportStubs, moduleTraceStub)
	}
	if options.TraceTPPostDialogVM {
		traceStub, counter, trace, err := installTPPostDialogVMTrace(
			process.Process, process.ProcessId, tpDialogStub.dialogCaptureValue, 10*time.Second,
		)
		if err != nil {
			return Result{}, fmt.Errorf("install post-dialog TP VM trace: %w", err)
		}
		tpPostDialogVMCounter = counter
		tpPostDialogVMTrace = trace
		result.ImportStubs = append(result.ImportStubs, traceStub)
	}
	if options.TraceTPPreError {
		traceStub, counter, trace, err := installTPPreErrorTrace(
			process.Process, process.ProcessId, process.ThreadId, 0, 10*time.Second,
		)
		if err != nil {
			return Result{}, fmt.Errorf("install pre-error TP trace: %w", err)
		}
		tpDispatcherCounter = counter
		tpDispatcherTrace = trace
		tpDispatcherTraceRing = true
		result.ImportStubs = append(result.ImportStubs, traceStub)
	}
	if options.TraceTPWorkerVM {
		traceStub, counter, trace, err := installTPPreErrorTrace(
			process.Process, process.ProcessId, 0, tpWorkerThreadIDAddress, 10*time.Second,
		)
		if err != nil {
			return Result{}, fmt.Errorf("install TP worker VM trace: %w", err)
		}
		tpWorkerVMCounter = counter
		tpWorkerVMTrace = trace
		result.ImportStubs = append(result.ImportStubs, traceStub)
	}
	if options.TraceTPDispatcher && !options.BypassTPDispatcher {
		traceStub, counter, trace, err := installTPDispatcherTrace(process.Process, process.ProcessId, 10*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TP dispatcher trace: %w", err)
		}
		tpDispatcherCounter = counter
		tpDispatcherTrace = trace
		result.ImportStubs = append(result.ImportStubs, traceStub)
	}
	if options.BypassTPDispatcher {
		bypassStub, counter, trace, secondCounter, thirdCounter, err := installTPDispatcherBypass(
			process.Process, process.ProcessId, 10*time.Second,
			options.TraceTPDispatcher,
			options.TrapTPSecondError && !options.BypassTPSecondError,
			options.BypassTPSecondError,
			options.BypassTPThirdError,
			options.TPThirdReturnEAX,
			options.TPThirdReturnEAXSet,
		)
		if err != nil {
			return Result{}, fmt.Errorf("install TP dispatcher bypass: %w", err)
		}
		if counter != 0 && trace != 0 {
			tpDispatcherCounter = counter
			tpDispatcherTrace = trace
			tpDispatcherTraceRing = true
		}
		tpSecondErrorTrapCounter = secondCounter
		tpThirdErrorBypassCounter = thirdCounter
		result.ImportStubs = append(result.ImportStubs, bypassStub)
	}
	if options.TraceTPThirdVM {
		traceStub, counter, trace, err := installTPThirdVMTrace(process.Process, process.ProcessId, 10*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TP third VM trace: %w", err)
		}
		tpThirdVMCounter = counter
		tpThirdVMTrace = trace
		result.ImportStubs = append(result.ImportStubs, traceStub)
	}
	for index := range result.ImportStubs {
		stop := make(chan struct{})
		done := make(chan struct{})
		counter := &patchCounters{}
		pinStops = append(pinStops, stop)
		pinDones = append(pinDones, done)
		counters = append(counters, counter)
		go func(stub ImportStub, counter *patchCounters, stop <-chan struct{}, done chan<- struct{}) {
			defer close(done)
			pinRemotePatches(process.Process, stub, &counter.pointer, &counter.target, stop)
		}(result.ImportStubs[index], counter, stop, done)
	}
	captureShadowBases := make([]uintptr, len(result.ImportStubs))
	captureShadows := make([]*atomic.Value, len(result.ImportStubs))
	for index := range result.ImportStubs {
		stub := result.ImportStubs[index]
		captureBase := stub.formatCaptureValue
		if captureBase == 0 {
			captureBase = stub.dialogCaptureValue
		}
		if captureBase == 0 {
			captureBase = stub.postDialogCaptureValue
		}
		if captureBase == 0 {
			captureBase = stub.nativeTerminateCaptureValue
		}
		if captureBase == 0 {
			continue
		}
		stop := make(chan struct{})
		done := make(chan struct{})
		shadow := &atomic.Value{}
		captureShadowBases[index] = captureBase
		captureShadows[index] = shadow
		pinStops = append(pinStops, stop)
		pinDones = append(pinDones, done)
		go func(base uintptr, shadow *atomic.Value, stop <-chan struct{}, done chan<- struct{}) {
			defer close(done)
			pollRemoteCapturePage(process.Process, base, shadow, stop)
		}(captureBase, shadow, stop, done)
	}
	if tpPostDialogVMCounter != 0 && tpPostDialogVMTrace != 0 {
		stop := make(chan struct{})
		done := make(chan struct{})
		pinStops = append(pinStops, stop)
		pinDones = append(pinDones, done)
		go func() {
			defer close(done)
			pollRemoteTPTrace(
				process.Process, tpPostDialogVMCounter, tpPostDialogVMTrace,
				&tpPostDialogVMShadow, stop,
			)
		}()
	}
	if options.SampleTPPostDialog {
		stop := make(chan struct{})
		done := make(chan struct{})
		pinStops = append(pinStops, stop)
		pinDones = append(pinDones, done)
		go func() {
			defer close(done)
			pollTPPostDialogThread(
				process.Process, tpDialogStub.dialogCaptureValue,
				started, &tpPostDialogThreadSamples, stop,
			)
		}()
	}
	if options.SampleTPPostProcess {
		stop := make(chan struct{})
		done := make(chan struct{})
		pinStops = append(pinStops, stop)
		pinDones = append(pinDones, done)
		go func() {
			defer close(done)
			pollTPPostDialogProcess(
				process.Process, process.ProcessId, tpDialogStub.dialogCaptureValue,
				started, &tpPostDialogProcessSamples, stop,
			)
		}()
	}
	if options.TrapTPPostTerminate || options.BypassTPPostTerminate {
		stop := make(chan struct{})
		done := make(chan struct{})
		pinStops = append(pinStops, stop)
		pinDones = append(pinDones, done)
		go func() {
			defer close(done)
			pollLateTPPostTerminate(
				process.Process, process.ProcessId, tpDialogStub.dialogCaptureValue,
				options.BypassTPPostTerminate, &tpPostDialogTerminate, stop,
			)
		}()
	}
	if dialogIndex >= 0 {
		cloneStop := make(chan struct{})
		cloneDone := make(chan struct{})
		pinStops = append(pinStops, cloneStop)
		pinDones = append(pinDones, cloneDone)
		go func() {
			defer close(cloneDone)
			pinPrivateFunctionCopies(process.Process, dialogStub.originalBytes, dialogStub.targetBytes, &cloneRewrites, cloneStop)
		}()
	}
	if formatIndex >= 0 {
		cloneStop := make(chan struct{})
		cloneDone := make(chan struct{})
		pinStops = append(pinStops, cloneStop)
		pinDones = append(pinDones, cloneDone)
		go func() {
			defer close(cloneDone)
			pinPrivateConditionalFormatCopies(
				process.Process, formatStub.originalBytes, formatStub.helperValue,
				formatStub.formatCaptureValue, &formatCloneRewrites, cloneStop,
			)
		}()
	}
	if tpDialogIndex >= 0 {
		cloneStop := make(chan struct{})
		cloneDone := make(chan struct{})
		pinStops = append(pinStops, cloneStop)
		pinDones = append(pinDones, cloneDone)
		go func() {
			defer close(cloneDone)
			pinPrivateConditionalTPDialogCopies(
				process.Process, tpDialogStub.originalBytes, tpDialogStub.helperValue,
				tpDialogStub.dialogCaptureValue, tpDialogStub.expectedCallerValue,
				tpDialogStub.expectedOuterCallerValue, tpDialogStub.tpDialogReturn,
				options.ClearTPDialogFrame, uint32(options.DialogReturn),
				&tpDialogCloneRewrites, cloneStop,
			)
		}()
	}
	if options.TrapWarningBuffer {
		trapStop := make(chan struct{})
		trapDone := make(chan struct{})
		pinStops = append(pinStops, trapStop)
		pinDones = append(pinDones, trapDone)
		go func() {
			defer close(trapDone)
			trapWarningTitleBuffer(process.Process, process.Thread, &warningBufferTrapped, trapStop)
		}()
	}
	if options.TrapTPArgs {
		trapStop := make(chan struct{})
		trapDone := make(chan struct{})
		pinStops = append(pinStops, trapStop)
		pinDones = append(pinDones, trapDone)
		go func() {
			defer close(trapDone)
			trapTPArgumentFrame(process.Process, process.Thread, process.ProcessId, &tpArgsTrapped, trapStop)
		}()
	}
	if options.TrapTPFirstArg {
		trapStop := make(chan struct{})
		trapDone := make(chan struct{})
		pinStops = append(pinStops, trapStop)
		pinDones = append(pinDones, trapDone)
		go func() {
			defer close(trapDone)
			trapTPFirstArgument(process.Process, process.Thread, &tpFirstArgTrapped, trapStop)
		}()
	}
	if options.BypassTPFinalError {
		monitorStop := make(chan struct{})
		monitorDone := make(chan struct{})
		pinStops = append(pinStops, monitorStop)
		pinDones = append(pinDones, monitorDone)
		go func() {
			defer close(monitorDone)
			monitorTPFinalError(process.Process, &tpFinalErrorClears, monitorStop)
		}()
	}
	if options.TrapTPSecondError && !options.BypassTPDispatcher {
		trapStop := make(chan struct{})
		trapDone := make(chan struct{})
		pinStops = append(pinStops, trapStop)
		pinDones = append(pinDones, trapDone)
		go func() {
			defer close(trapDone)
			trapTPSecondError(process.Process, process.Thread, &tpSecondErrorTrapped, trapStop)
		}()
	}
	if len(pinStops) != 0 {
		defer func() {
			for _, stop := range pinStops {
				close(stop)
			}
			for _, done := range pinDones {
				<-done
			}
		}()
	}
	if options.AllowMultiClient && multiClientPatch == nil {
		multiClientPatch = make(chan asynchronousStubResult, 1)
		go func() {
			stub, patchErr := installCoreMultiClientBypass(
				process.Process, process.Thread, process.ProcessId, 30*time.Second,
			)
			multiClientPatch <- asynchronousStubResult{stub: stub, err: patchErr}
		}()
	}
	var shopOrderingPatch chan asynchronousStubResult
	if options.FixLocalShopOrdering {
		shopOrderingPatch = make(chan asynchronousStubResult, 1)
		go func() {
			stub, patchErr := installClientShopConnectOrdering(
				process.Process, process.ProcessId, 45*time.Second,
			)
			shopOrderingPatch <- asynchronousStubResult{stub: stub, err: patchErr}
		}()
	}
	if options.TraceStartupDebugger {
		var contextRecordAddress uintptr
		if tpContextCompatibility != nil {
			contextRecordAddress = tpContextCompatibility.recordAddress
		}
		startupDebugger = startStartupDebugMonitor(process.Process, process.ProcessId, started, contextRecordAddress)
		if attachErr := <-startupDebugger.ready; attachErr != nil {
			startupDebugger = nil
			return Result{}, fmt.Errorf("attach startup debugger: %w", attachErr)
		}
		defer func() {
			if startupDebugger != nil {
				startupDebugger.stopAndWait()
			}
		}()
	}
	if options.GateUnpackedEntry {
		clientBaseName := filepath.Base(executable)
		clientImageBase, moduleErr := waitForModule(process.Process, process.ProcessId, clientBaseName, time.Second)
		if moduleErr != nil {
			return Result{}, fmt.Errorf("locate Client.exe for unpacked-entry gate: %w", moduleErr)
		}
		const unpackedEntryRVA = uintptr(0x2203A0)
		unpackedEntryAddress = clientImageBase + unpackedEntryRVA
		unpackedEntryGateRecord, err = installUnpackedEntryGate(
			process.Process, process.Thread, process.ProcessId, unpackedEntryAddress, 5*time.Second,
		)
		if err != nil {
			return Result{}, fmt.Errorf("install unpacked-entry gate: %w", err)
		}
	}
	if mainThreadGated {
		previous, _, resumeErr := procResumeThread.Call(uintptr(process.Thread))
		if previous == ^uintptr(0) {
			return Result{}, fmt.Errorf("release ClientBase main-thread gate: %w", resumeErr)
		}
		gateReleased = true
	}
	// The TP worker is held at its verified entry while this registration runs.
	// Do not wait for late Core/shop patches first: on slower hosts that creates
	// the exact startup race this gate is intended to close, and Core loading
	// itself may depend on ClientBase finishing its worker startup.
	if options.BypassTPWorkerFatal {
		tpWorkerFatalRecord, err = installTPWorkerFatalExceptionBypass(process.Process, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install TP worker fatal-exception bypass: %w", err)
		}
	}
	if options.BypassTPWorkerFatal || options.BypassTPETWRegister {
		if tpWorkerStartupGateFlag == 0 {
			return Result{}, fmt.Errorf("TP worker compatibility installed without a startup gate")
		}
	}
	if options.BypassTPETWRegister {
		threadID, waitErr := waitForRemoteDWORD(process.Process, tpWorkerStartupThreadID, 30*time.Second)
		if waitErr != nil {
			if startupDebugger != nil {
				debugResult := startupDebugger.stopAndWait()
				startupDebugger = nil
				result.StartupDebugExceptions = debugResult.exceptions
				result.StartupDebugExitCode = debugResult.exitCode
				result.StartupDebuggerError = debugResult.errText
			}
			if accessViolationRecord != 0 {
				result.AccessViolation = readAccessViolationCapture(process.Process, accessViolationRecord)
			}
			result.TPStartupTimeoutTrace = captureTPStartupTimeoutThreads(process.Process, process.ProcessId, started)
			if exitCode, exited := remoteProcessExitCode(process.Process); exited {
				result.ExitCode = &exitCode
			}
			result.ElapsedMS = time.Since(started).Milliseconds()
			_ = json.NewEncoder(options.Output).Encode(result)
			return result, fmt.Errorf("wait for TP worker startup thread: %w", waitErr)
		}
		if tpContextCompatibility != nil {
			if allowErr := tpContextCompatibility.allowWorkerThread(threadID); allowErr != nil {
				return Result{}, fmt.Errorf("allow TP worker in startup context compatibility: %w", allowErr)
			}
		}
		if startupDebugger != nil {
			debugResult := startupDebugger.stopAndWait()
			startupDebugger = nil
			result.StartupDebugExceptions = debugResult.exceptions
			result.StartupDebugExitCode = debugResult.exitCode
			result.StartupDebuggerError = debugResult.errText
		}
		clientBase, moduleErr := waitForModule(process.Process, process.ProcessId, "ClientBase.dll", time.Second)
		if moduleErr != nil {
			return Result{}, fmt.Errorf("locate ClientBase for TP ETW call boundary: %w", moduleErr)
		}
		capture, boundaryErr := runGatedTPETWProviderRepair(
			process.Process, process.ProcessId, threadID,
			tpWorkerStartupReplay, clientBase+0x3FBB8, tpWorkerStartupGateFlag, 30*time.Second,
		)
		result.TPETWCallerPath = &capture
		if boundaryErr != nil {
			result.ElapsedMS = time.Since(started).Milliseconds()
			_ = json.NewEncoder(options.Output).Encode(result)
			return result, fmt.Errorf("TP ETW ClientBase provider repair: %w", boundaryErr)
		}
		if tpEarlySystemHeader != nil {
			if restoreErr := tpEarlySystemHeader.restoreAfterETWRepair(); restoreErr != nil {
				result.ElapsedMS = time.Since(started).Milliseconds()
				_ = json.NewEncoder(options.Output).Encode(result)
				return result, fmt.Errorf("restore TP early system-header compatibility: %w", restoreErr)
			}
		}
		if tpContextCompatibility != nil {
			if removeErr := tpContextCompatibility.remove(5 * time.Second); removeErr != nil {
				return Result{}, fmt.Errorf("remove TP startup context handler after ETW repair: %w", removeErr)
			}
		}
	} else if options.BypassTPWorkerFatal {
		ready := make([]byte, 4)
		binary.LittleEndian.PutUint32(ready, 1)
		if err := writeRemote(process.Process, tpWorkerStartupGateFlag, ready); err != nil {
			return Result{}, fmt.Errorf("release TP worker startup gate: %w", err)
		}
	}
	if multiClientPatch != nil {
		patchResult := <-multiClientPatch
		if patchResult.err != nil {
			return Result{}, fmt.Errorf("install Core multi-client compatibility: %w", patchResult.err)
		}
		result.ImportStubs = append(result.ImportStubs, patchResult.stub)
	}
	if options.HighResolutionTimer {
		// Client's native frame limiter uses GetTickCount + Sleep and reads its
		// target from [options] limitfps. Request 1 ms timer precision inside the
		// client process so Sleep can honor that existing limiter on modern
		// Windows. No DLL or game-loop replacement is injected.
		timeBeginPeriod, timerErr := findRemoteExport(
			process.Process, process.ProcessId, "WINMM.dll", "timeBeginPeriod", 5*time.Second, 0,
		)
		if timerErr != nil {
			return Result{}, fmt.Errorf("locate client timeBeginPeriod: %w", timerErr)
		}
		status, timerErr := remoteCallOne(process.Process, timeBeginPeriod, 1, 5*time.Second)
		if timerErr != nil {
			return Result{}, fmt.Errorf("request client 1 ms timer resolution: %w", timerErr)
		}
		if status != 0 {
			return Result{}, fmt.Errorf("client timeBeginPeriod(1) returned %d", status)
		}
		result.TimerResolutionMS = 1
	}
	// RtlAddVectoredExceptionHandler may take loader-internal locks. Register
	// transparent tracing only after releasing the early ClientBase gate, whose
	// suspended thread can still own the loader lock.
	if options.TraceExceptions {
		exceptionCounter, exceptionTrace, err = installExceptionTraceVEH(process.Process, process.ProcessId, 5*time.Second)
		if err != nil {
			return Result{}, fmt.Errorf("install transparent exception trace: %w", err)
		}
	}
	if unpackedEntryGateRecord != 0 {
		_, gateErr := waitForRemoteDWORD(process.Process, unpackedEntryGateRecord, 60*time.Second)
		if gateErr != nil {
			result.ElapsedMS = time.Since(started).Milliseconds()
			_ = json.NewEncoder(options.Output).Encode(result)
			return result, gateErr
		}
		result.UnpackedEntryGate = readUnpackedEntryGateReport(process.Process, unpackedEntryGateRecord, unpackedEntryAddress)
		result.Running = true
		result.ElapsedMS = time.Since(started).Milliseconds()
		failed = false
		_ = json.NewEncoder(options.Output).Encode(result)
		return result, nil
	}
	failed = false

	waitMilliseconds := uint32(0)
	if options.Wait > 0 {
		waitMilliseconds = uint32(options.Wait / time.Millisecond)
	}
	waitResult, _, waitErr := procWaitForSingle.Call(uintptr(process.Process), uintptr(waitMilliseconds))
	switch waitResult {
	case waitObject0:
		var exitCode uint32
		success, _, exitErr := procGetExitCode.Call(uintptr(process.Process), uintptr(unsafe.Pointer(&exitCode)))
		if success == 0 {
			return Result{}, fmt.Errorf("GetExitCodeProcess: %w", exitErr)
		}
		code := int32(exitCode)
		result.ExitCode = &code
	case waitTimeout:
		result.Running = true
	default:
		return Result{}, fmt.Errorf("WaitForSingleObject result 0x%X: %w", waitResult, waitErr)
	}
	if shopOrderingPatch != nil {
		patchResult := <-shopOrderingPatch
		if patchResult.err != nil {
			_, _, _ = procTerminate.Call(uintptr(process.Process), 0xDEAD)
			return Result{}, fmt.Errorf("install local shop socket-ordering compatibility: %w", patchResult.err)
		}
		result.ImportStubs = append(result.ImportStubs, patchResult.stub)
	}
	result.ElapsedMS = time.Since(started).Milliseconds()
	if result.Running && options.CaptureClientBaseSet {
		captureSize := options.CaptureClientBaseSize
		if captureSize == 0 {
			captureSize = 128
		}
		if captureSize > 4096 {
			return Result{}, fmt.Errorf("ClientBase code capture size %d exceeds 4096", captureSize)
		}
		clientBase, moduleErr := waitForModule(process.Process, process.ProcessId, "ClientBase.dll", time.Second)
		if moduleErr != nil {
			return Result{}, fmt.Errorf("capture ClientBase code: %w", moduleErr)
		}
		address := clientBase + uintptr(options.CaptureClientBaseRVA)
		code, ok := readRemote(process.Process, address, int(captureSize))
		if !ok {
			return Result{}, fmt.Errorf("capture ClientBase code at 0x%X", address)
		}
		result.ClientBaseCode = &ModuleCodeCapture{
			Module:  "ClientBase.dll",
			RVA:     fmt.Sprintf("0x%X", options.CaptureClientBaseRVA),
			Address: fmt.Sprintf("0x%08X", address),
			Size:    captureSize,
			Hex:     fmt.Sprintf("%X", code),
		}
	}
	if result.Running && options.TraceTPEarlySystemState {
		state, captureErr := captureTPEarlySystemState(process.Process, process.ProcessId)
		if captureErr != nil {
			return Result{}, captureErr
		}
		result.TPEarlySystemState = state
	}
	result.WarningBufferTrapped = warningBufferTrapped.Load()
	result.TPArgsTrapped = tpArgsTrapped.Load()
	result.TPFirstArgTrapped = tpFirstArgTrapped.Load()
	result.TPSecondErrorTrapped = tpSecondErrorTrapped.Load()
	result.TPFinalErrorClears = tpFinalErrorClears.Load()
	if accessViolationRecord != 0 {
		result.AccessViolation = readAccessViolationCapture(process.Process, accessViolationRecord)
	}
	if exceptionCounter != 0 {
		result.ExceptionTotal, result.ExceptionTrace = readExceptionTrace(process.Process, exceptionCounter, exceptionTrace)
	}
	if tpWorkerFatalRecord != 0 {
		if record, ok := readRemote(process.Process, tpWorkerFatalRecord, 44); ok {
			result.TPWorkerFatalArmedTID = binary.LittleEndian.Uint32(record[0:4])
			result.TPWorkerFatalHits = binary.LittleEndian.Uint32(record[4:8])
			if count := binary.LittleEndian.Uint32(record[8:12]); count != 0 {
				result.TPWorkerFatalCandidate = &TPWorkerFatalCapture{
					Count:            count,
					ExceptionCode:    binary.LittleEndian.Uint32(record[12:16]),
					ExceptionAddress: binary.LittleEndian.Uint32(record[16:20]),
					EIP:              binary.LittleEndian.Uint32(record[20:24]),
					EAX:              binary.LittleEndian.Uint32(record[24:28]),
					ParameterCount:   binary.LittleEndian.Uint32(record[28:32]),
					Parameter0:       binary.LittleEndian.Uint32(record[32:36]),
					Parameter1:       binary.LittleEndian.Uint32(record[36:40]),
					ThreadID:         binary.LittleEndian.Uint32(record[40:44]),
				}
			}
		}
	}
	if tpPostAccessViolationRecord != 0 {
		result.TPPostAccessViolation = readAccessViolationCapture(process.Process, tpPostAccessViolationRecord)
	}
	if tpSecondErrorTrapCounter != 0 {
		if value, ok := readRemote(process.Process, tpSecondErrorTrapCounter, 4); ok {
			count := binary.LittleEndian.Uint32(value)
			if options.BypassTPSecondError {
				result.TPSecondBypassHits = count
			} else {
				result.TPSecondErrorTrapped = count != 0
			}
		}
	}
	if tpThirdErrorBypassCounter != 0 {
		if value, ok := readRemote(process.Process, tpThirdErrorBypassCounter, 4); ok {
			result.TPThirdBypassHits = binary.LittleEndian.Uint32(value)
		}
	}
	if breakpointCounter != 0 {
		if value, ok := readRemote(process.Process, breakpointCounter, 4); ok {
			result.HandledBreakpoints = binary.LittleEndian.Uint32(value)
		}
	}
	if tpEntryCounter != 0 {
		if value, ok := readRemote(process.Process, tpEntryCounter, 4); ok {
			result.TPEntryBypassHits = binary.LittleEndian.Uint32(value)
		}
	}
	if tpDispatcherCounter != 0 && tpDispatcherTrace != 0 {
		result.TPDispatcherTotal, result.TPDispatcherTrace = readTPTrace(
			process.Process, tpDispatcherCounter, tpDispatcherTrace, tpDispatcherTraceRing,
		)
	}
	if tpWorkerThreadIDAddress != 0 {
		if value, ok := readRemote(process.Process, tpWorkerThreadIDAddress, 4); ok {
			result.TPWorkerThreadID = binary.LittleEndian.Uint32(value)
		}
	}
	if tpWorkerVMCounter != 0 && tpWorkerVMTrace != 0 {
		result.TPWorkerVMTotal, result.TPWorkerVMTrace = readTPTrace(
			process.Process, tpWorkerVMCounter, tpWorkerVMTrace, true,
		)
	}
	if tpThirdVMCounter != 0 && tpThirdVMTrace != 0 {
		result.TPThirdVMTotal, result.TPThirdVMTrace = readTPTrace(
			process.Process, tpThirdVMCounter, tpThirdVMTrace, true,
		)
	}
	if tpPostDialogVMCounter != 0 && tpPostDialogVMTrace != 0 {
		result.TPPostDialogVMTotal, result.TPPostDialogVMTrace = readTPPostDialogVMTrace(
			process.Process, tpPostDialogVMCounter, tpPostDialogVMTrace, &tpPostDialogVMShadow,
		)
	}
	result.TPPostDialogThreadID, result.TPPostDialogSamples,
		result.TPPostDialogFailures, result.TPPostDialogTrace = tpPostDialogThreadSamples.snapshot()
	_, result.TPPostProcessSamples, result.TPPostProcessFailure,
		result.TPPostProcessTrace = tpPostDialogProcessSamples.snapshot()
	result.TPPostTerminateArmed, result.TPPostTerminateError,
		result.TPPostTerminate = readLateTerminateCapture(process.Process, &tpPostDialogTerminate)
	result.TPPostTerminateBypass = options.BypassTPPostTerminate && result.TPPostTerminateArmed
	if tesSafeVMCounter != 0 && tesSafeVMTrace != 0 {
		result.TesSafeVMTotal, result.TesSafeVMTrace = readTesSafeVMTrace(
			process.Process, tesSafeVMCounter, tesSafeVMTrace,
		)
	}
	if tesSafeIOReturnCounter != 0 && tesSafeIOReturnTrace != 0 {
		result.TesSafeIOReturnTotal, result.TesSafeIOReturnTrace = readTesSafeVMTrace(
			process.Process, tesSafeIOReturnCounter, tesSafeIOReturnTrace,
		)
	}
	if tesSafeConsumerCounter != 0 && tesSafeConsumerTrace != 0 {
		result.TesSafeConsumerTotal, result.TesSafeConsumerTrace = readTesSafeConsumerTrace(
			process.Process, tesSafeConsumerCounter, tesSafeConsumerTrace,
		)
	}
	if tesSafeChecksumCounter != 0 && tesSafeChecksumTrace != 0 {
		result.TesSafeChecksumTotal, result.TesSafeChecksumTrace = readTesSafeChecksumTrace(
			process.Process, tesSafeChecksumCounter, tesSafeChecksumTrace,
		)
	}
	// Late startup patches (for example the Core.dll multi-client branch)
	// are appended after the long-lived pin counters have been created.
	for index := range counters {
		readStubValue := func(address uintptr, size int) ([]byte, bool) {
			if value, ok := readRemote(process.Process, address, size); ok {
				return value, true
			}
			if index >= len(captureShadows) || captureShadows[index] == nil {
				return nil, false
			}
			stored := captureShadows[index].Load()
			if stored == nil {
				return nil, false
			}
			page, ok := stored.([]byte)
			base := captureShadowBases[index]
			if !ok || address < base || uintptr(size) > uintptr(len(page)) || address-base > uintptr(len(page)-size) {
				return nil, false
			}
			offset := int(address - base)
			return append([]byte(nil), page[offset:offset+size]...), true
		}
		result.ImportStubs[index].Rewrites = counters[index].pointer.Load()
		result.ImportStubs[index].TargetRewrites = counters[index].target.Load()
		if result.ImportStubs[index].counterValue != 0 {
			if value, ok := readStubValue(result.ImportStubs[index].counterValue, 4); ok {
				result.ImportStubs[index].Calls = binary.LittleEndian.Uint32(value)
			}
		}
		if result.ImportStubs[index].correctedValue != 0 {
			if value, ok := readStubValue(result.ImportStubs[index].correctedValue, 4); ok {
				result.ImportStubs[index].CorrectedCalls = binary.LittleEndian.Uint32(value)
			}
		}
		if result.ImportStubs[index].bypassedValue != 0 {
			if value, ok := readStubValue(result.ImportStubs[index].bypassedValue, 4); ok {
				result.ImportStubs[index].BypassedCalls = binary.LittleEndian.Uint32(value)
			}
		}
		if result.ImportStubs[index].traceValue != 0 {
			if value, ok := readStubValue(result.ImportStubs[index].traceValue, 32); ok {
				for offset := 0; offset < len(value); offset += 4 {
					result.ImportStubs[index].EntryStack = append(result.ImportStubs[index].EntryStack,
						fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[offset:offset+4])))
				}
			}
		}
		if result.ImportStubs[index].textValue != 0 {
			if value, ok := readStubValue(result.ImportStubs[index].textValue, 256); ok {
				if result.ImportStubs[index].textUTF16 {
					words := make([]uint16, 0, len(value)/2)
					for offset := 0; offset+1 < len(value); offset += 2 {
						word := binary.LittleEndian.Uint16(value[offset : offset+2])
						if word == 0 {
							break
						}
						words = append(words, word)
					}
					result.ImportStubs[index].CapturedText = syscall.UTF16ToString(words)
				} else if end := bytes.IndexByte(value, 0); end >= 0 {
					result.ImportStubs[index].CapturedText = string(value[:end])
				}
			}
		}
		if result.ImportStubs[index].binaryValue != 0 && result.ImportStubs[index].binarySize != 0 {
			if sizeBytes, ok := readStubValue(result.ImportStubs[index].binarySize, 4); ok {
				size := binary.LittleEndian.Uint32(sizeBytes)
				if size > 64 {
					size = 64
				}
				if size != 0 {
					if value, ok := readStubValue(result.ImportStubs[index].binaryValue, int(size)); ok {
						result.ImportStubs[index].CapturedHex = fmt.Sprintf("%X", value)
					}
				}
			}
		}
		if result.ImportStubs[index].callerValue != 0 {
			if value, ok := readStubValue(result.ImportStubs[index].callerValue, 4); ok {
				caller := uintptr(binary.LittleEndian.Uint32(value))
				result.ImportStubs[index].CallerAddress = fmt.Sprintf("0x%08X", caller)
				// The protected ClientBase image decrypts some call sites only while
				// they are executing. Capture the live window around the return
				// address instead of relying on a stale or still-encrypted dump.
				if caller >= 64 {
					codeBase := caller - 64
					if code, codeOK := readRemote(process.Process, codeBase, 128); codeOK {
						result.ImportStubs[index].CallerCodeBase = fmt.Sprintf("0x%08X", codeBase)
						result.ImportStubs[index].CallerCodeHex = fmt.Sprintf("%X", code)
					}
				}
			}
		}
		if result.ImportStubs[index].rawStackValue != 0 && result.ImportStubs[index].rawStackWords > 0 {
			if value, ok := readStubValue(
				result.ImportStubs[index].rawStackValue,
				result.ImportStubs[index].rawStackWords*4,
			); ok {
				for offset := 0; offset < len(value); offset += 4 {
					result.ImportStubs[index].RawEntryStack = append(
						result.ImportStubs[index].RawEntryStack,
						fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[offset:offset+4])),
					)
				}
			}
		}
		if result.ImportStubs[index].threadRingValue != 0 &&
			result.ImportStubs[index].threadRingEntries > 0 && result.ImportStubs[index].threadRingStride >= 40 {
			entries := result.ImportStubs[index].threadRingEntries
			stride := result.ImportStubs[index].threadRingStride
			if ring, ok := readStubValue(result.ImportStubs[index].threadRingValue, entries*stride); ok {
				total := result.ImportStubs[index].Calls
				retained := total
				if retained > uint32(entries) {
					retained = uint32(entries)
				}
				first := total - retained
				for sequence := first; sequence < total; sequence++ {
					offset := int(sequence%uint32(entries)) * stride
					record := ring[offset : offset+stride]
					if binary.LittleEndian.Uint32(record[0:4]) != sequence {
						continue
					}
					result.ImportStubs[index].ThreadCreations = append(result.ImportStubs[index].ThreadCreations, ThreadCreationCall{
						Index:            sequence,
						Caller:           fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[4:8])),
						ThreadAttributes: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12])),
						StackSize:        binary.LittleEndian.Uint32(record[12:16]),
						StartAddress:     fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[16:20])),
						Parameter:        fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[20:24])),
						CreationFlags:    binary.LittleEndian.Uint32(record[24:28]),
						ThreadIDAddress:  fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[28:32])),
						Handle:           fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[32:36])),
						ThreadID:         binary.LittleEndian.Uint32(record[36:40]),
					})
				}
			}
		}
		if result.ImportStubs[index].deviceRingValue != 0 &&
			result.ImportStubs[index].deviceRingEntries > 0 && result.ImportStubs[index].deviceRingStride >= 0x68 {
			entries := result.ImportStubs[index].deviceRingEntries
			stride := result.ImportStubs[index].deviceRingStride
			if ring, ok := readStubValue(result.ImportStubs[index].deviceRingValue, entries*stride); ok {
				total := result.ImportStubs[index].Calls
				retained := total
				if retained > uint32(entries) {
					retained = uint32(entries)
				}
				first := total - retained
				for sequence := first; sequence < total; sequence++ {
					offset := int(sequence%uint32(entries)) * stride
					record := ring[offset : offset+stride]
					if binary.LittleEndian.Uint32(record[0:4]) != sequence {
						continue
					}
					inputSize := binary.LittleEndian.Uint32(record[20:24])
					capturedSize := inputSize
					if capturedSize > 64 {
						capturedSize = 64
					}
					call := TesSafeDeviceCall{
						Index:                sequence,
						Caller:               fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[4:8])),
						Handle:               fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12])),
						ControlCode:          fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[12:16])),
						InputAddress:         fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[16:20])),
						InputSize:            inputSize,
						OutputAddress:        fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[24:28])),
						OutputSize:           binary.LittleEndian.Uint32(record[28:32]),
						BytesReturnedAddress: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[32:36])),
						OverlappedAddress:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[36:40])),
					}
					if capturedSize != 0 {
						call.InputHex = fmt.Sprintf("%X", record[40:40+int(capturedSize)])
					}
					result.ImportStubs[index].TesSafeDeviceCalls = append(result.ImportStubs[index].TesSafeDeviceCalls, call)
				}
			}
		}
		if result.ImportStubs[index].formatCaptureValue != 0 {
			if value, ok := readStubValue(result.ImportStubs[index].formatCaptureValue, 9*4); ok {
				hits := binary.LittleEndian.Uint32(value[0:4])
				if hits != 0 {
					formatPointer := uintptr(binary.LittleEndian.Uint32(value[16:20]))
					labelPointer := uintptr(binary.LittleEndian.Uint32(value[20:24]))
					capture := &WarningFormatCapture{
						Hits:               hits,
						ThreadID:           binary.LittleEndian.Uint32(value[4:8]),
						CallerAddress:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[8:12])),
						DestinationAddress: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[12:16])),
						FormatAddress:      fmt.Sprintf("0x%08X", formatPointer),
						LabelAddress:       fmt.Sprintf("0x%08X", labelPointer),
						Reason:             binary.LittleEndian.Uint32(value[24:28]),
						Code:               binary.LittleEndian.Uint32(value[28:32]),
						Detail:             binary.LittleEndian.Uint32(value[32:36]),
					}
					if formatBytes, formatOK := readRemote(process.Process, formatPointer, 32); formatOK {
						if end := bytes.IndexByte(formatBytes, 0); end >= 0 {
							capture.FormatText = string(formatBytes[:end])
						}
					}
					if labelBytes, labelOK := readRemote(process.Process, labelPointer, 64); labelOK {
						if end := bytes.IndexByte(labelBytes, 0); end >= 0 {
							capture.LabelHex = fmt.Sprintf("%X", labelBytes[:end])
						}
					}
					result.ImportStubs[index].WarningFormat = capture
				}
			}
		}
		if result.ImportStubs[index].dialogCaptureValue != 0 {
			if value, ok := readStubValue(result.ImportStubs[index].dialogCaptureValue, 17*4); ok {
				hits := binary.LittleEndian.Uint32(value[0:4])
				if hits != 0 {
					result.ImportStubs[index].TPDialog = &TPDialogCapture{
						Hits:          hits,
						ThreadID:      binary.LittleEndian.Uint32(value[4:8]),
						CallerAddress: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[8:12])),
						Instance:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[12:16])),
						Template:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[16:20])),
						ParentWindow:  fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[20:24])),
						DialogProc:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[24:28])),
						InitParam:     fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[28:32])),
						EAX:           binary.LittleEndian.Uint32(value[32:36]),
						EBX:           binary.LittleEndian.Uint32(value[36:40]),
						ECX:           binary.LittleEndian.Uint32(value[40:44]),
						EDX:           binary.LittleEndian.Uint32(value[44:48]),
						ESI:           binary.LittleEndian.Uint32(value[48:52]),
						EDI:           binary.LittleEndian.Uint32(value[52:56]),
						EBP:           binary.LittleEndian.Uint32(value[56:60]),
						EFlags:        binary.LittleEndian.Uint32(value[60:64]),
						EntryESP:      binary.LittleEndian.Uint32(value[64:68]),
					}
				}
			}
		}
		if result.ImportStubs[index].postDialogCaptureValue != 0 {
			if value, ok := readStubValue(result.ImportStubs[index].postDialogCaptureValue, 12*4); ok {
				hits := binary.LittleEndian.Uint32(value[0:4])
				if hits != 0 {
					result.ImportStubs[index].TPPostDialog = &TPPostDialogCapture{
						Hits:          hits,
						ThreadID:      binary.LittleEndian.Uint32(value[4:8]),
						CallerAddress: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[8:12])),
						EAX:           binary.LittleEndian.Uint32(value[12:16]),
						EBX:           binary.LittleEndian.Uint32(value[16:20]),
						ECX:           binary.LittleEndian.Uint32(value[20:24]),
						EDX:           binary.LittleEndian.Uint32(value[24:28]),
						ESI:           binary.LittleEndian.Uint32(value[28:32]),
						EDI:           binary.LittleEndian.Uint32(value[32:36]),
						EBP:           binary.LittleEndian.Uint32(value[36:40]),
						EntryESP:      binary.LittleEndian.Uint32(value[40:44]),
						EFlags:        binary.LittleEndian.Uint32(value[44:48]),
					}
				}
			}
		}
		if result.ImportStubs[index].nativeTerminateCaptureValue != 0 {
			if value, ok := readStubValue(result.ImportStubs[index].nativeTerminateCaptureValue, 14*4); ok {
				hits := binary.LittleEndian.Uint32(value[0:4])
				if hits != 0 {
					result.ImportStubs[index].NativeTerminate = &NativeTerminateCapture{
						Hits:          hits,
						ThreadID:      binary.LittleEndian.Uint32(value[4:8]),
						CallerAddress: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[8:12])),
						ProcessHandle: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[12:16])),
						ExitStatus:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[16:20])),
						EAX:           binary.LittleEndian.Uint32(value[20:24]),
						EBX:           binary.LittleEndian.Uint32(value[24:28]),
						ECX:           binary.LittleEndian.Uint32(value[28:32]),
						EDX:           binary.LittleEndian.Uint32(value[32:36]),
						ESI:           binary.LittleEndian.Uint32(value[36:40]),
						EDI:           binary.LittleEndian.Uint32(value[40:44]),
						EBP:           binary.LittleEndian.Uint32(value[44:48]),
						EFlags:        binary.LittleEndian.Uint32(value[48:52]),
						EntryESP:      binary.LittleEndian.Uint32(value[52:56]),
					}
				}
			}
		}
	}
	if dialogIndex >= 0 {
		result.ImportStubs[dialogIndex].CloneRewrites = cloneRewrites.Load()
	}
	if formatIndex >= 0 {
		result.ImportStubs[formatIndex].CloneRewrites = formatCloneRewrites.Load()
	}
	if tpDialogIndex >= 0 {
		result.ImportStubs[tpDialogIndex].CloneRewrites = tpDialogCloneRewrites.Load()
	}
	if err := json.NewEncoder(options.Output).Encode(result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func captureTPEarlySystemState(process syscall.Handle, pid uint32) (*TPEarlySystemState, error) {
	kernelBase, headerAddress, err := resolveTPEarlySystemHeaderField(process, pid, time.Second)
	if err != nil {
		return nil, fmt.Errorf("capture TP early system state: %w", err)
	}
	header, ok := readRemote(process, headerAddress, 4)
	if !ok {
		return nil, fmt.Errorf("capture TP early system header at 0x%X", headerAddress)
	}
	value := uintptr(binary.LittleEndian.Uint32(header))
	state := &TPEarlySystemState{
		KernelBase:    fmt.Sprintf("0x%08X", kernelBase),
		HeaderAddress: fmt.Sprintf("0x%08X", headerAddress),
		HeaderValue:   fmt.Sprintf("0x%08X", value),
	}
	if value < 0x10000 {
		return state, nil
	}
	var memory memoryBasicInformation
	if queried, _, _ := procVirtualQueryEx.Call(
		uintptr(process), value, uintptr(unsafe.Pointer(&memory)), unsafe.Sizeof(memory),
	); queried != unsafe.Sizeof(memory) {
		return state, nil
	}
	state.ValueMemoryState = memory.State
	state.ValueMemoryBase = fmt.Sprintf("0x%08X", memory.BaseAddress)
	state.ValueMemorySize = uint32(memory.RegionSize)
	if memory.State == memCommit && memory.Protect&0x101 == 0 {
		size := int(memory.RegionSize)
		if size > 0x1000 {
			size = 0x1000
		}
		if data, readable := readRemote(process, memory.BaseAddress, size); readable {
			state.ValueMemoryHex = fmt.Sprintf("%X", data)
		}
	}
	return state, nil
}

// installCoreMultiClientBypass changes only Core.dll's handling of
// ERROR_ALREADY_EXISTS for the verified QQTangWinClass startup mutex. The
// mutex is still created and retained normally; the legacy branch that
// activates the existing window and ends the new process is skipped.
func installCoreMultiClientBypass(process, mainThread syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, error) {
	// Core's singleton branch runs almost immediately after the image is mapped.
	// The generic 10 ms module poll can lose that race intermittently, producing
	// a clean second-instance exit even though the patch is installed later.
	coreBase, err := waitForModuleInterval(process, pid, "Core.dll", timeout, 200*time.Microsecond)
	if err != nil {
		return ImportStub{}, err
	}
	previous, _, suspendErr := procSuspendThread.Call(uintptr(mainThread))
	if previous == ^uintptr(0) {
		return ImportStub{}, fmt.Errorf("suspend main thread before Core singleton patch: %w", suspendErr)
	}
	defer procResumeThread.Call(uintptr(mainThread))

	const signatureRVA = uintptr(0x26F0)
	const branchRVA = uintptr(0x2701)
	expected := []byte{
		0x3B, 0xC6, 0x89, 0x07, 0x74, 0x63, 0xFF, 0x15, 0xB4, 0xC4, 0x00, 0x10,
		0x3D, 0xB7, 0x00, 0x00, 0x00, 0x75, 0x56,
	}
	// The absolute CreateMutexA IAT operand is relocated with Core.dll.
	binary.LittleEndian.PutUint32(expected[8:12], uint32(coreBase+0xC4B4))
	actual, ok := readRemote(process, coreBase+signatureRVA, len(expected))
	if !ok {
		return ImportStub{}, fmt.Errorf("read Core.dll singleton signature at 0x%X", coreBase+signatureRVA)
	}
	if !bytes.Equal(actual, expected) {
		return ImportStub{}, fmt.Errorf(
			"Core.dll singleton signature mismatch at 0x%X: got %X, want %X",
			coreBase+signatureRVA, actual, expected,
		)
	}
	target := coreBase + branchRVA
	patch := []byte{0xEB}
	if !ensureRemoteBytes(process, target, patch, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch Core.dll singleton branch at 0x%X", target)
	}
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(patch)))
	return ImportStub{
		Module:         "Core.dll",
		Library:        "KERNEL32.dll",
		Symbol:         "CreateMutexA(QQTangWinClass)/ERROR_ALREADY_EXISTS",
		StubAddress:    fmt.Sprintf("0x%X", target),
		Behavior:       "preserve the QQTangWinClass mutex, but skip only the legacy existing-instance activation/exit branch",
		TargetAddress:  fmt.Sprintf("0x%X", target),
		TargetOriginal: fmt.Sprintf("75 (context %X)", actual),
		targetValue:    target,
		targetBytes:    patch,
		originalBytes:  []byte{0x75},
	}, nil
}

func readTPTrace(process syscall.Handle, counterAddress, traceAddress uintptr, ring bool) (uint32, []TPDispatcherStep) {
	value, ok := readRemote(process, counterAddress, 4)
	if !ok {
		return 0, nil
	}
	total := binary.LittleEndian.Uint32(value)
	count := total
	if count > 128 {
		count = 128
	}
	if count == 0 {
		return total, nil
	}
	const recordSize = 17 * 4
	readCount := count
	if ring {
		readCount = 128
	}
	trace, ok := readRemote(process, traceAddress, int(readCount)*recordSize)
	if !ok {
		return total, nil
	}
	steps := make([]TPDispatcherStep, 0, count)
	for index := uint32(0); index < count; index++ {
		slot := index
		stepIndex := index
		if ring {
			stepIndex = total - count + index
			if total > 128 {
				slot = (total + index) & 127
			}
		}
		offset := slot * recordSize
		step := TPDispatcherStep{
			Index:       stepIndex,
			Destination: binary.LittleEndian.Uint32(trace[offset : offset+4]),
			EBX:         binary.LittleEndian.Uint32(trace[offset+4 : offset+8]),
			ECX:         binary.LittleEndian.Uint32(trace[offset+8 : offset+12]),
			EDX:         binary.LittleEndian.Uint32(trace[offset+12 : offset+16]),
			ESI:         binary.LittleEndian.Uint32(trace[offset+16 : offset+20]),
			EDI:         binary.LittleEndian.Uint32(trace[offset+20 : offset+24]),
			EBP:         binary.LittleEndian.Uint32(trace[offset+24 : offset+28]),
			ESP:         binary.LittleEndian.Uint32(trace[offset+28 : offset+32]),
			EFlags:      binary.LittleEndian.Uint32(trace[offset+32 : offset+36]),
		}
		for stackOffset := uint32(36); stackOffset < recordSize; stackOffset += 4 {
			step.Stack = append(step.Stack, binary.LittleEndian.Uint32(trace[offset+stackOffset:offset+stackOffset+4]))
		}
		steps = append(steps, step)
	}
	return total, steps
}

func readTPPostDialogVMTrace(
	process syscall.Handle,
	counterAddress, traceAddress uintptr,
	shadow *atomic.Value,
) (uint32, []TPDispatcherStep) {
	const traceSize = 128 * 17 * 4
	if counter, ok := readRemote(process, counterAddress, 4); ok {
		total := binary.LittleEndian.Uint32(counter)
		if trace, traceOK := readRemote(process, traceAddress, traceSize); traceOK {
			return decodeTPPostDialogVMTrace(total, trace)
		}
	}
	if shadow == nil {
		return 0, nil
	}
	stored := shadow.Load()
	if stored == nil {
		return 0, nil
	}
	snapshot, ok := stored.(tpTraceShadow)
	if !ok {
		return 0, nil
	}
	return decodeTPPostDialogVMTrace(snapshot.Total, snapshot.Trace)
}

func decodeTPPostDialogVMTrace(total uint32, trace []byte) (uint32, []TPDispatcherStep) {
	const recordSize = 17 * 4
	if len(trace) < 128*recordSize {
		return total, nil
	}
	count := total
	if count > 128 {
		count = 128
	}
	steps := make([]TPDispatcherStep, 0, count)
	for index := uint32(0); index < count; index++ {
		slot := index
		stepIndex := index
		if total > 128 {
			stepIndex = total - count + index
			slot = (total + index) & 127
		}
		offset := int(slot) * recordSize
		step := TPDispatcherStep{
			Index:       stepIndex,
			ThreadID:    binary.LittleEndian.Uint32(trace[offset+64 : offset+68]),
			Destination: binary.LittleEndian.Uint32(trace[offset : offset+4]),
			EBX:         binary.LittleEndian.Uint32(trace[offset+4 : offset+8]),
			ECX:         binary.LittleEndian.Uint32(trace[offset+8 : offset+12]),
			EDX:         binary.LittleEndian.Uint32(trace[offset+12 : offset+16]),
			ESI:         binary.LittleEndian.Uint32(trace[offset+16 : offset+20]),
			EDI:         binary.LittleEndian.Uint32(trace[offset+20 : offset+24]),
			EBP:         binary.LittleEndian.Uint32(trace[offset+24 : offset+28]),
			ESP:         binary.LittleEndian.Uint32(trace[offset+28 : offset+32]),
			EFlags:      binary.LittleEndian.Uint32(trace[offset+32 : offset+36]),
		}
		for stackOffset := 36; stackOffset < 64; stackOffset += 4 {
			step.Stack = append(step.Stack, binary.LittleEndian.Uint32(trace[offset+stackOffset:offset+stackOffset+4]))
		}
		steps = append(steps, step)
	}
	return total, steps
}

func pollRemoteTPTrace(
	process syscall.Handle,
	counterAddress, traceAddress uintptr,
	shadow *atomic.Value,
	stop <-chan struct{},
) {
	if counterAddress == 0 || traceAddress == 0 || shadow == nil {
		return
	}
	const traceSize = 128 * 17 * 4
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			counter, ok := readRemote(process, counterAddress, 4)
			if !ok {
				return
			}
			total := binary.LittleEndian.Uint32(counter)
			if total == 0 {
				continue
			}
			trace, ok := readRemote(process, traceAddress, traceSize)
			if !ok {
				return
			}
			shadow.Store(tpTraceShadow{Total: total, Trace: append([]byte(nil), trace...)})
		}
	}
}

func readTesSafeVMTrace(process syscall.Handle, counterAddress, traceAddress uintptr) (uint32, []TPDispatcherStep) {
	total, steps := readTPTrace(process, counterAddress, traceAddress, true)
	for index := range steps {
		if len(steps[index].Stack) == 0 {
			continue
		}
		steps[index].ThreadID = steps[index].Stack[0]
		steps[index].Stack = steps[index].Stack[1:]
	}
	return total, steps
}

func readTesSafeChecksumTrace(process syscall.Handle, counterAddress, traceAddress uintptr) (uint32, []TPDispatcherStep) {
	value, ok := readRemote(process, counterAddress, 4)
	if !ok {
		return 0, nil
	}
	total := binary.LittleEndian.Uint32(value)
	count := total
	if count > 128 {
		count = 128
	}
	if count == 0 {
		return total, nil
	}
	const recordSize = 42 * 4
	trace, ok := readRemote(process, traceAddress, 128*recordSize)
	if !ok {
		return total, nil
	}
	steps := make([]TPDispatcherStep, 0, count)
	for index := uint32(0); index < count; index++ {
		slot := index
		stepIndex := total - count + index
		if total > 128 {
			slot = (total + index) & 127
		}
		offset := slot * recordSize
		step := TPDispatcherStep{
			Index:       stepIndex,
			ThreadID:    binary.LittleEndian.Uint32(trace[offset+36 : offset+40]),
			Destination: binary.LittleEndian.Uint32(trace[offset : offset+4]),
			EBX:         binary.LittleEndian.Uint32(trace[offset+4 : offset+8]),
			ECX:         binary.LittleEndian.Uint32(trace[offset+8 : offset+12]),
			EDX:         binary.LittleEndian.Uint32(trace[offset+12 : offset+16]),
			ESI:         binary.LittleEndian.Uint32(trace[offset+16 : offset+20]),
			EDI:         binary.LittleEndian.Uint32(trace[offset+20 : offset+24]),
			EBP:         binary.LittleEndian.Uint32(trace[offset+24 : offset+28]),
			ESP:         binary.LittleEndian.Uint32(trace[offset+28 : offset+32]),
			EFlags:      binary.LittleEndian.Uint32(trace[offset+32 : offset+36]),
		}
		for stackOffset := uint32(40); stackOffset < recordSize; stackOffset += 4 {
			step.Stack = append(step.Stack, binary.LittleEndian.Uint32(trace[offset+stackOffset:offset+stackOffset+4]))
		}
		steps = append(steps, step)
	}
	return total, steps
}

func readTesSafeConsumerTrace(process syscall.Handle, counterAddress, traceAddress uintptr) (uint32, []TesSafeConsumerStep) {
	value, ok := readRemote(process, counterAddress, 4)
	if !ok {
		return 0, nil
	}
	total := binary.LittleEndian.Uint32(value)
	count := total
	if count > 16 {
		count = 16
	}
	if count == 0 {
		return total, nil
	}
	const recordSize = 1344
	trace, ok := readRemote(process, traceAddress, 16*recordSize)
	if !ok {
		return total, nil
	}
	steps := make([]TesSafeConsumerStep, 0, count)
	for index := uint32(0); index < count; index++ {
		slot := index
		stepIndex := index
		if total > 16 {
			stepIndex = total - count + index
			slot = (total + index) & 15
		}
		offset := slot * recordSize
		step := TesSafeConsumerStep{
			Index:         stepIndex,
			ThreadID:      binary.LittleEndian.Uint32(trace[offset : offset+4]),
			EAX:           binary.LittleEndian.Uint32(trace[offset+4 : offset+8]),
			EBX:           binary.LittleEndian.Uint32(trace[offset+8 : offset+12]),
			ECX:           binary.LittleEndian.Uint32(trace[offset+12 : offset+16]),
			EDX:           binary.LittleEndian.Uint32(trace[offset+16 : offset+20]),
			ESI:           binary.LittleEndian.Uint32(trace[offset+20 : offset+24]),
			EDI:           binary.LittleEndian.Uint32(trace[offset+24 : offset+28]),
			EBP:           binary.LittleEndian.Uint32(trace[offset+28 : offset+32]),
			ESP:           binary.LittleEndian.Uint32(trace[offset+32 : offset+36]),
			EFlags:        binary.LittleEndian.Uint32(trace[offset+36 : offset+40]),
			ObjectAddress: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(trace[offset+40:offset+44])),
			BufferAddress: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(trace[offset+60:offset+64])),
			BufferHex:     fmt.Sprintf("%X", trace[offset+64:offset+320]),
			ObjectHex:     fmt.Sprintf("%X", trace[offset+320:offset+1344]),
		}
		for wordOffset := uint32(44); wordOffset < 60; wordOffset += 4 {
			step.ObjectWords = append(step.ObjectWords, binary.LittleEndian.Uint32(trace[offset+wordOffset:offset+wordOffset+4]))
		}
		steps = append(steps, step)
	}
	return total, steps
}

func trapTPFirstArgument(process, thread syscall.Handle, hit *atomic.Bool, stop <-chan struct{}) {
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	select {
	case <-stop:
		return
	case <-timer.C:
	}
	for {
		select {
		case <-stop:
			return
		default:
		}
		value, ok := readRemote(process, 0x0132C1B4, 4)
		if !ok {
			return
		}
		if binary.LittleEndian.Uint32(value) == 540 {
			previous, _, _ := procSuspendThread.Call(uintptr(thread))
			if previous != ^uintptr(0) {
				hit.Store(true)
				<-stop
				procResumeThread.Call(uintptr(thread))
			}
			return
		}
	}
}

func monitorTPFinalError(process syscall.Handle, clears *atomic.Uint32, stop <-chan struct{}) {
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	select {
	case <-stop:
		return
	case <-timer.C:
	}
	for {
		select {
		case <-stop:
			return
		default:
		}
		count := clearTPFinalErrorRecords(process, stop)
		if count != 0 {
			clears.Add(uint32(count))
		}
		timer.Reset(100 * time.Millisecond)
		select {
		case <-stop:
			return
		case <-timer.C:
		}
	}
}

func clearTPFinalErrorRecords(process syscall.Handle, stop <-chan struct{}) int {
	pattern := make([]byte, 12)
	binary.LittleEndian.PutUint32(pattern[0:4], 3)
	binary.LittleEndian.PutUint32(pattern[4:8], 1008)
	binary.LittleEndian.PutUint32(pattern[8:12], 31000)
	marker := []byte("Error_SetReason")
	const (
		memCommit   = 0x1000
		pageGuard   = 0x100
		chunkSize   = 1024 * 1024
		minimumHeap = 0x10000
	)
	cleared := 0
	for address := uintptr(0x10000); address < uintptr(0x80000000); {
		select {
		case <-stop:
			return cleared
		default:
		}
		var info memoryBasicInformation
		queried, _, _ := procVirtualQueryEx.Call(
			uintptr(process), address, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
		)
		if queried == 0 || info.RegionSize == 0 {
			break
		}
		next := info.BaseAddress + info.RegionSize
		if next <= address {
			break
		}
		if next > uintptr(0x80000000) {
			next = uintptr(0x80000000)
		}
		if info.State == memCommit && info.Protect&pageGuard == 0 && info.Protect&0xFF == pageReadWrite && info.RegionSize >= minimumHeap {
			var overlap []byte
			for chunkAddress := info.BaseAddress; chunkAddress < next; {
				size := chunkSize
				if remaining := next - chunkAddress; remaining < uintptr(size) {
					size = int(remaining)
				}
				data, ok := readRemote(process, chunkAddress, size)
				if !ok {
					break
				}
				combined := append(append(make([]byte, 0, len(overlap)+len(data)), overlap...), data...)
				combinedBase := chunkAddress - uintptr(len(overlap))
				for searchAt := 0; searchAt+len(pattern) <= len(combined); {
					relative := bytes.Index(combined[searchAt:], pattern)
					if relative < 0 {
						break
					}
					relative += searchAt
					matchAddress := combinedBase + uintptr(relative)
					contextBase := matchAddress
					if matchAddress >= info.BaseAddress+64 {
						contextBase = matchAddress - 64
					}
					contextSize := 320
					if remaining := next - contextBase; remaining < uintptr(contextSize) {
						contextSize = int(remaining)
					}
					context, contextOK := readRemote(process, contextBase, contextSize)
					if contextOK && bytes.Contains(context, marker) {
						if err := writeRemote(process, matchAddress, make([]byte, len(pattern))); err == nil {
							cleared++
						}
					}
					searchAt = relative + len(pattern)
				}
				keep := len(pattern) - 1
				if len(combined) < keep {
					keep = len(combined)
				}
				overlap = append(overlap[:0], combined[len(combined)-keep:]...)
				chunkAddress += uintptr(len(data))
			}
		}
		address = next
	}
	return cleared
}

func trapTPSecondError(process, thread syscall.Handle, hit *atomic.Bool, stop <-chan struct{}) {
	// This is the stable pre-UI frame observed after the verified 0/5/540
	// virtual return.  The older 0132C46C probe was a transient value in the
	// later USER32 dialog stack and could suspend an unrelated instruction.
	const frameAddress = uintptr(0x01329464)
	seenClear := false
	for {
		select {
		case <-stop:
			return
		default:
		}
		value, ok := readRemote(process, frameAddress, 12)
		if !ok {
			time.Sleep(200 * time.Microsecond)
			continue
		}
		matches := binary.LittleEndian.Uint32(value[0:4]) == 3 &&
			binary.LittleEndian.Uint32(value[4:8]) == 1006 &&
			binary.LittleEndian.Uint32(value[8:12]) == 19008
		if !matches {
			seenClear = true
		}
		if seenClear && matches {
			previous, _, _ := procSuspendThread.Call(uintptr(thread))
			if previous != ^uintptr(0) {
				hit.Store(true)
				<-stop
				procResumeThread.Call(uintptr(thread))
			}
			return
		}
		time.Sleep(100 * time.Microsecond)
	}
}

func trapTPArgumentFrame(process, thread syscall.Handle, pid uint32, hit *atomic.Bool, stop <-chan struct{}) {
	timer := time.NewTimer(8 * time.Second)
	defer timer.Stop()
	select {
	case <-stop:
		return
	case <-timer.C:
	}
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", time.Second)
	if err != nil {
		return
	}
	expectedReturn := uint32(clientBase + 0x8D1E74)
	for {
		select {
		case <-stop:
			return
		default:
		}
		frame, ok := readRemote(process, 0x0132C1A4, 20)
		if !ok {
			return
		}
		if binary.LittleEndian.Uint32(frame[0:4]) == expectedReturn &&
			binary.LittleEndian.Uint32(frame[8:12]) == 0 &&
			binary.LittleEndian.Uint32(frame[12:16]) == 5 &&
			binary.LittleEndian.Uint32(frame[16:20]) == 540 {
			previous, _, _ := procSuspendThread.Call(uintptr(thread))
			if previous != ^uintptr(0) {
				hit.Store(true)
				<-stop
				procResumeThread.Call(uintptr(thread))
			}
			return
		}
	}
}

func installTPFailureRecovery(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, err
	}
	target, err := findRemoteExport(process, pid, "NTDLL.dll", "NtTerminateProcess", timeout, 0)
	if err != nil {
		return ImportStub{}, err
	}
	const originalSize = 15
	original, ok := readRemote(process, target, originalSize)
	if !ok {
		return ImportStub{}, fmt.Errorf("read NtTerminateProcess at 0x%X", target)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx TP recovery: 0x%X (%v)", page, allocErr)
	}
	expectedReturn := clientBase + 0x8D1E74
	trampoline := page + 0x100
	stub := []byte{
		0x81, 0x3D, 0xA4, 0xC1, 0x32, 0x01, 0, 0, 0, 0, 0x75, 0x38,
		0x81, 0x3D, 0xAC, 0xC1, 0x32, 0x01, 0x00, 0x00, 0x00, 0x00, 0x75, 0x2C,
		0x81, 0x3D, 0xB0, 0xC1, 0x32, 0x01, 0x05, 0x00, 0x00, 0x00, 0x75, 0x20,
		0x81, 0x3D, 0xB4, 0xC1, 0x32, 0x01, 0x1C, 0x02, 0x00, 0x00, 0x75, 0x14,
		0xBC, 0xB8, 0xC1, 0x32, 0x01,
		0x8B, 0x2D, 0xA0, 0xC1, 0x32, 0x01,
		0x33, 0xC0,
		0xBA, 0, 0, 0, 0,
		0xFF, 0xE2,
		0xB8, 0, 0, 0, 0,
		0xFF, 0xE0,
	}
	binary.LittleEndian.PutUint32(stub[6:10], uint32(expectedReturn))
	// The recovery path starts at offset 48. Its mov edx immediate begins at 62,
	// and the non-matching forward path's mov eax immediate begins at 69.
	binary.LittleEndian.PutUint32(stub[62:66], uint32(expectedReturn))
	binary.LittleEndian.PutUint32(stub[69:73], uint32(trampoline))
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write TP recovery stub: %w", err)
	}
	if err := writeRemote(process, trampoline, original); err != nil {
		return ImportStub{}, fmt.Errorf("write NtTerminateProcess trampoline: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0}
	displacement := int64(page) - int64(target+uintptr(len(targetPatch)))
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("TP recovery displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(displacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch NtTerminateProcess for TP recovery")
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), trampoline, uintptr(len(original)))
	return ImportStub{
		Module: "NTDLL.dll", Library: "NTDLL.dll", Symbol: "NtTerminateProcess(error 0,5,540 only)",
		StubAddress: fmt.Sprintf("0x%X", page), Behavior: "unwind verified ClientBase TP failure frame; forward all non-matches",
		TargetAddress: fmt.Sprintf("0x%X", target), TargetOriginal: fmt.Sprintf("%X", original),
		targetValue: target, targetBytes: targetPatch, originalBytes: original,
	}, nil
}

func trapWarningTitleBuffer(process, thread syscall.Handle, hit *atomic.Bool, stop <-chan struct{}) {
	signature := []byte{0xBE, 0xAF, 0xB8, 0xE6, 0xC2, 0xEB}
	for {
		select {
		case <-stop:
			return
		default:
		}
		data, ok := readRemote(process, 0x0132BFC4, len(signature))
		if !ok {
			return
		}
		if bytes.Equal(data, signature) {
			previous, _, _ := procSuspendThread.Call(uintptr(thread))
			if previous != ^uintptr(0) {
				hit.Store(true)
				<-stop
				procResumeThread.Call(uintptr(thread))
			}
			return
		}
		time.Sleep(200 * time.Microsecond)
	}
}

func installConditionalFormatWaitStub(process syscall.Handle, pid uint32, moduleName, libraryName, symbolName string, timeout time.Duration) (ImportStub, error) {
	base, err := waitForModule(process, pid, moduleName, timeout)
	if err != nil {
		return ImportStub{}, err
	}
	iatAddress, err := findImportAddress(process, base, libraryName, symbolName)
	if err != nil {
		return ImportStub{}, err
	}
	targetAddress, err := waitForExecutablePointer(process, iatAddress, timeout)
	if err != nil {
		return ImportStub{}, err
	}
	sleepAddress, err := findRemoteExport(process, pid, "KERNEL32.dll", "Sleep", timeout, 0)
	if err != nil {
		return ImportStub{}, err
	}
	stubAddress, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if stubAddress == 0 || stubAddress > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx conditional format stub: 0x%X (%v)", stubAddress, allocErr)
	}
	const stolenSize = 5
	original, ok := readRemote(process, targetAddress, 15)
	if !ok {
		return ImportStub{}, fmt.Errorf("read %s target at 0x%X", symbolName, targetAddress)
	}
	trampolineAddress := stubAddress + 0x200
	captureAddress := stubAddress + 0x400
	trampoline := append([]byte{}, original[:stolenSize]...)
	trampoline = append(trampoline, 0xE9, 0, 0, 0, 0)
	trampolineDisplacement := int64(targetAddress+stolenSize) - int64(trampolineAddress+uintptr(len(trampoline)))
	if trampolineDisplacement < -0x80000000 || trampolineDisplacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("wsprintf trampoline displacement out of range")
	}
	binary.LittleEndian.PutUint32(trampoline[stolenSize+1:], uint32(int32(trampolineDisplacement)))
	stub := buildConditionalFormatTrapStub(captureAddress, sleepAddress, trampolineAddress)
	if err := writeRemote(process, stubAddress, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write conditional format stub: %w", err)
	}
	if err := writeRemote(process, trampolineAddress, trampoline); err != nil {
		return ImportStub{}, fmt.Errorf("write conditional format trampoline: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0}
	targetDisplacement := int64(stubAddress) - int64(targetAddress+uintptr(len(targetPatch)))
	if targetDisplacement < -0x80000000 || targetDisplacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("wsprintf target displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(targetDisplacement)))
	if !ensureRemoteBytes(process, targetAddress, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch conditional %s target at 0x%X", symbolName, targetAddress)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(stubAddress))
	if !ensureRemoteBytes(process, iatAddress, pointer, pageReadWrite) {
		return ImportStub{}, fmt.Errorf("patch conditional %s!%s IAT at 0x%X", moduleName, symbolName, iatAddress)
	}
	procFlushInstruction.Call(uintptr(process), stubAddress, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), trampolineAddress, uintptr(len(trampoline)))
	return ImportStub{
		Module: moduleName, Library: libraryName, Symbol: symbolName,
		IATAddress: fmt.Sprintf("0x%X", iatAddress), StubAddress: fmt.Sprintf("0x%X", stubAddress),
		Behavior:      "forward unless format is %s(%u, %u, %u) and values are (3,1008,31000); record context, then Sleep(INFINITE)",
		TargetAddress: fmt.Sprintf("0x%X", targetAddress), TargetOriginal: fmt.Sprintf("%X", original),
		iatValue: iatAddress, stubValue: stubAddress, targetValue: targetAddress,
		targetBytes: targetPatch, originalBytes: original, helperValue: sleepAddress,
		counterValue: captureAddress, callerValue: captureAddress + 8,
		rawStackValue: captureAddress + 0x100, rawStackWords: 64,
		formatCaptureValue: captureAddress,
	}, nil
}

func buildConditionalFormatTrapStub(captureAddress, sleepAddress, trampolineAddress uintptr) []byte {
	stub := make([]byte, 0, 256)
	var forwardJumps []int
	appendUint32 := func(value uint32) {
		stub = binary.LittleEndian.AppendUint32(stub, value)
	}
	appendForwardJump := func() {
		stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0) // jne forward
		forwardJumps = append(forwardJumps, len(stub)-4)
	}
	compareFormatDWORD := func(displacement byte, value uint32) {
		if displacement == 0 {
			stub = append(stub, 0x81, 0x38) // cmp dword ptr [eax],value
		} else {
			stub = append(stub, 0x81, 0x78, displacement) // cmp dword ptr [eax+disp8],value
		}
		appendUint32(value)
		appendForwardJump()
	}
	compareStackDWORD := func(displacement byte, value uint32) {
		stub = append(stub, 0x81, 0x7C, 0x24, displacement) // cmp dword ptr [esp+disp8],value
		appendUint32(value)
		appendForwardJump()
	}
	storeStackDWORD := func(displacement byte, address uintptr) {
		if displacement == 0 {
			stub = append(stub, 0x8B, 0x06) // mov eax,[esi]
		} else {
			stub = append(stub, 0x8B, 0x46, displacement) // mov eax,[esi+disp8]
		}
		stub = append(stub, 0xA3) // mov [absolute],eax
		appendUint32(uint32(address))
	}

	stub = append(stub, 0x8B, 0x44, 0x24, 0x08)             // mov eax,[esp+8] (format)
	compareFormatDWORD(0x00, 0x25287325)                    // "%s(%"
	compareFormatDWORD(0x04, 0x25202C75)                    // "u, %"
	compareFormatDWORD(0x08, 0x25202C75)                    // "u, %"
	stub = append(stub, 0x66, 0x81, 0x78, 0x0C, 0x75, 0x29) // cmp word ptr [eax+0Ch],"u)"
	appendForwardJump()
	stub = append(stub, 0x80, 0x78, 0x0E, 0x00) // cmp byte ptr [eax+0Eh],0
	appendForwardJump()
	compareStackDWORD(0x10, 3)
	compareStackDWORD(0x14, 1008)
	compareStackDWORD(0x18, 31000)

	stub = append(stub, 0x9C, 0x60)             // pushfd; pushad
	stub = append(stub, 0x8D, 0x74, 0x24, 0x24) // lea esi,[esp+24h] (original entry ESP)
	stub = append(stub, 0xFF, 0x05)             // inc dword ptr [capture.hits]
	appendUint32(uint32(captureAddress))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0) // mov eax,fs:[24h] (thread id)
	stub = append(stub, 0xA3)
	appendUint32(uint32(captureAddress + 4))
	storeStackDWORD(0x00, captureAddress+8)  // caller
	storeStackDWORD(0x04, captureAddress+12) // destination
	storeStackDWORD(0x08, captureAddress+16) // format
	storeStackDWORD(0x0C, captureAddress+20) // label
	storeStackDWORD(0x10, captureAddress+24) // reason
	storeStackDWORD(0x14, captureAddress+28) // code
	storeStackDWORD(0x18, captureAddress+32) // detail
	stub = append(stub, 0xBF)
	appendUint32(uint32(captureAddress + 0x100)) // mov edi,raw stack record
	stub = append(stub, 0xB9)
	appendUint32(64)                                  // mov ecx,64
	stub = append(stub, 0xFC, 0xF3, 0xA5, 0x61, 0x9D) // cld; rep movsd; popad; popfd
	stub = append(stub, 0x68, 0xFF, 0xFF, 0xFF, 0xFF) // push INFINITE
	stub = append(stub, 0xB8)
	appendUint32(uint32(sleepAddress))
	stub = append(stub, 0xFF, 0xD0, 0xEB, 0xFE) // call eax; spin if Sleep returns

	forwardOffset := len(stub)
	stub = append(stub, 0xB8)
	appendUint32(uint32(trampolineAddress))
	stub = append(stub, 0xFF, 0xE0) // jmp eax
	for _, displacementOffset := range forwardJumps {
		next := displacementOffset + 4
		binary.LittleEndian.PutUint32(
			stub[displacementOffset:displacementOffset+4],
			uint32(int32(forwardOffset-next)),
		)
	}
	return stub
}

func installConditionalTPDialogWaitStub(
	process syscall.Handle,
	pid uint32,
	returnInstead, clearErrorFrame bool,
	returnValue uint32,
	timeout time.Duration,
) (ImportStub, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, err
	}
	targetAddress, err := findRemoteExport(process, pid, "USER32.dll", "DialogBoxParamA", timeout, 0)
	if err != nil {
		return ImportStub{}, err
	}
	sleepAddress, err := findRemoteExport(process, pid, "KERNEL32.dll", "Sleep", timeout, 0)
	if err != nil {
		return ImportStub{}, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx TP dialog trap: 0x%X (%v)", page, allocErr)
	}
	const stolenSize = 5
	original, ok := readRemote(process, targetAddress, 15)
	if !ok {
		return ImportStub{}, fmt.Errorf("read DialogBoxParamA target at 0x%X", targetAddress)
	}
	trampolineAddress := page + 0x200
	captureAddress := page + 0x400
	expectedCaller := clientBase + 0x7BBEF4
	expectedOuterCaller := clientBase + 0x6CFF15
	trampoline := append([]byte{}, original[:stolenSize]...)
	trampoline = append(trampoline, 0xE9, 0, 0, 0, 0)
	displacement := int64(targetAddress+stolenSize) - int64(trampolineAddress+uintptr(len(trampoline)))
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("DialogBoxParamA trampoline displacement out of range")
	}
	binary.LittleEndian.PutUint32(trampoline[stolenSize+1:], uint32(int32(displacement)))
	stub := buildConditionalTPDialogTrapStub(
		captureAddress, sleepAddress, trampolineAddress,
		expectedCaller, expectedOuterCaller, returnInstead, clearErrorFrame, returnValue,
	)
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write TP dialog trap: %w", err)
	}
	if err := writeRemote(process, trampolineAddress, trampoline); err != nil {
		return ImportStub{}, fmt.Errorf("write TP dialog trampoline: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0}
	targetDisplacement := int64(page) - int64(targetAddress+uintptr(len(targetPatch)))
	if targetDisplacement < -0x80000000 || targetDisplacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("DialogBoxParamA target displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(targetDisplacement)))
	if !ensureRemoteBytes(process, targetAddress, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch DialogBoxParamA target at 0x%X", targetAddress)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), trampolineAddress, uintptr(len(trampoline)))
	behavior := "forward every call except verified TP 1008 frame; record DialogBoxParamA arguments, registers, and 64 stack DWORDs, then Sleep(INFINITE)"
	if returnInstead {
		behavior = fmt.Sprintf("forward every call except verified TP 1008 frame; record context and return %d so ClientBase unwinds to +0x6CFF15", int32(returnValue))
		if clearErrorFrame {
			behavior = "forward every call except verified TP 1008 frame; record original context, clear only the verified 31000/1008/3 stack slots, and return IDOK"
		}
	}
	return ImportStub{
		Module: "USER32.dll", Library: "USER32.dll", Symbol: "DialogBoxParamA(TP 3,1008,31000 caller)",
		StubAddress:   fmt.Sprintf("0x%X", page),
		Behavior:      behavior,
		TargetAddress: fmt.Sprintf("0x%X", targetAddress), TargetOriginal: fmt.Sprintf("%X", original),
		stubValue: page, targetValue: targetAddress, targetBytes: targetPatch, originalBytes: original,
		helperValue: sleepAddress, counterValue: captureAddress, callerValue: captureAddress + 8,
		rawStackValue: captureAddress + 0x100, rawStackWords: 64,
		dialogCaptureValue: captureAddress, expectedCallerValue: expectedCaller,
		expectedOuterCallerValue: expectedOuterCaller, tpDialogReturn: returnInstead,
	}, nil
}

func buildConditionalTPDialogTrapStub(
	captureAddress, sleepAddress, trampolineAddress, expectedCaller, expectedOuterCaller uintptr,
	returnInstead, clearErrorFrame bool,
	returnValue uint32,
) []byte {
	stub := make([]byte, 0, 256)
	var forwardJumps []int
	appendUint32 := func(value uint32) {
		stub = binary.LittleEndian.AppendUint32(stub, value)
	}
	appendForwardJump := func() {
		stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)
		forwardJumps = append(forwardJumps, len(stub)-4)
	}
	compareStackDWORD := func(displacement byte, value uint32) {
		stub = append(stub, 0x81, 0x7C, 0x24, displacement)
		appendUint32(value)
		appendForwardJump()
	}
	storeStackDWORD := func(displacement byte, address uintptr) {
		if displacement == 0 {
			stub = append(stub, 0x8B, 0x06) // mov eax,[esi]
		} else {
			stub = append(stub, 0x8B, 0x46, displacement) // mov eax,[esi+disp8]
		}
		stub = append(stub, 0xA3) // mov [absolute],eax
		appendUint32(uint32(address))
	}
	storeSavedDWORD := func(displacement byte, address uintptr) {
		stub = append(stub, 0x8B, 0x44, 0x24, displacement) // mov eax,[esp+disp8]
		stub = append(stub, 0xA3)
		appendUint32(uint32(address))
	}

	stub = append(stub, 0x81, 0x3C, 0x24) // cmp dword ptr [esp],expected caller
	appendUint32(uint32(expectedCaller))
	appendForwardJump()
	stub = append(stub, 0x81, 0x7D, 0x04) // cmp dword ptr [ebp+4],outer caller
	appendUint32(uint32(expectedOuterCaller))
	appendForwardJump()
	compareStackDWORD(0x50, 31000)
	compareStackDWORD(0x78, 1008)
	compareStackDWORD(0x7C, 3)
	stub = append(stub, 0x9C, 0x60)             // pushfd; pushad
	stub = append(stub, 0x8D, 0x74, 0x24, 0x24) // lea esi,[esp+24h] (original entry ESP)
	stub = append(stub, 0xFF, 0x05)             // inc dword ptr [capture.hits]
	appendUint32(uint32(captureAddress))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0) // mov eax,fs:[24h] (thread id)
	stub = append(stub, 0xA3)
	appendUint32(uint32(captureAddress + 4))
	storeStackDWORD(0x00, captureAddress+8)  // caller
	storeStackDWORD(0x04, captureAddress+12) // hInstance
	storeStackDWORD(0x08, captureAddress+16) // template
	storeStackDWORD(0x0C, captureAddress+20) // parent HWND
	storeStackDWORD(0x10, captureAddress+24) // dialog proc
	storeStackDWORD(0x14, captureAddress+28) // init param
	storeSavedDWORD(0x1C, captureAddress+32) // original EAX
	storeSavedDWORD(0x10, captureAddress+36) // original EBX
	storeSavedDWORD(0x18, captureAddress+40) // original ECX
	storeSavedDWORD(0x14, captureAddress+44) // original EDX
	storeSavedDWORD(0x04, captureAddress+48) // original ESI
	storeSavedDWORD(0x00, captureAddress+52) // original EDI
	storeSavedDWORD(0x08, captureAddress+56) // original EBP
	storeSavedDWORD(0x20, captureAddress+60) // original EFLAGS
	stub = append(stub, 0x8B, 0xC6, 0xA3)    // mov eax,esi; mov [entry ESP],eax
	appendUint32(uint32(captureAddress + 64))
	stub = append(stub, 0xBF)
	appendUint32(uint32(captureAddress + 0x100))
	stub = append(stub, 0xB9)
	appendUint32(64)
	stub = append(stub, 0xFC, 0xF3, 0xA5) // cld; rep movsd (ESI advanced by 100h)
	if clearErrorFrame {
		stub = append(stub, 0x81, 0xEE, 0x00, 0x01, 0x00, 0x00) // sub esi,100h
		for _, displacement := range []byte{0x50, 0x78, 0x7C} {
			stub = append(stub, 0xC7, 0x46, displacement, 0, 0, 0, 0) // mov dword ptr [esi+disp],0
		}
	}
	stub = append(stub, 0x61, 0x9D) // popad; popfd
	if returnInstead {
		stub = append(stub, 0xB8)
		appendUint32(returnValue)
		stub = append(stub, 0xC2, 0x14, 0x00) // return selected value and pop five DialogBoxParamA args
	} else {
		stub = append(stub, 0x68, 0xFF, 0xFF, 0xFF, 0xFF) // push INFINITE
		stub = append(stub, 0xB8)
		appendUint32(uint32(sleepAddress))
		stub = append(stub, 0xFF, 0xD0, 0xEB, 0xFE)
	}
	forwardOffset := len(stub)
	stub = append(stub, 0xB8)
	appendUint32(uint32(trampolineAddress))
	stub = append(stub, 0xFF, 0xE0)
	for _, displacementOffset := range forwardJumps {
		binary.LittleEndian.PutUint32(
			stub[displacementOffset:displacementOffset+4],
			uint32(int32(forwardOffset-(displacementOffset+4))),
		)
	}
	return stub
}

func installTPPostDialogReturnTrap(process syscall.Handle, pid uint32, recoveryMode string, timeout time.Duration) (ImportStub, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, err
	}
	sleepAddress, err := findRemoteExport(process, pid, "KERNEL32.dll", "Sleep", timeout, 0)
	if err != nil {
		return ImportStub{}, err
	}
	targetAddress := clientBase + 0x6CFF15
	original, ok := readRemote(process, targetAddress, 15)
	if !ok {
		return ImportStub{}, fmt.Errorf("read ClientBase+0x6CFF15")
	}
	expected := []byte{0x68, 0x55, 0x40, 0xA9, 0x45}
	if !bytes.Equal(original[:len(expected)], expected) {
		return ImportStub{}, fmt.Errorf("ClientBase+0x6CFF15 signature mismatch: %X", original[:len(expected)])
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx TP post-dialog trap: 0x%X (%v)", page, allocErr)
	}
	trampolineAddress := page + 0x200
	captureAddress := page + 0x400
	trampoline := append([]byte{}, original[:5]...)
	trampoline = append(trampoline, 0xE9, 0, 0, 0, 0)
	trampolineDisplacement := int64(targetAddress+5) - int64(trampolineAddress+uintptr(len(trampoline)))
	if trampolineDisplacement < -0x80000000 || trampolineDisplacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("TP post-dialog trampoline displacement out of range")
	}
	binary.LittleEndian.PutUint32(trampoline[6:], uint32(int32(trampolineDisplacement)))
	stub := buildTPPostDialogTrapStub(
		captureAddress, sleepAddress, trampolineAddress, clientBase+0x70708B, recoveryMode,
	)
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write TP post-dialog trap: %w", err)
	}
	if err := writeRemote(process, trampolineAddress, trampoline); err != nil {
		return ImportStub{}, fmt.Errorf("write TP post-dialog trampoline: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0}
	displacement := int64(page) - int64(targetAddress+uintptr(len(targetPatch)))
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("TP post-dialog target displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(displacement)))
	if !ensureRemoteBytes(process, targetAddress, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch ClientBase+0x6CFF15")
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), trampolineAddress, uintptr(len(trampoline)))
	behavior := "after the exact TP dialog returns IDOK and the real protected frame unwinds, record registers and 64 stack DWORDs, then Sleep(INFINITE)"
	switch recoveryMode {
	case "return":
		behavior = "only when the verified outer stack contains object,+0x70708B,+0x70708B: record context, discard object, and ret through the first marker"
	case "drop":
		behavior = "only when the verified outer stack contains object,+0x70708B,+0x70708B: record context, discard object, then transfer both markers to +0x70708B"
	case "full":
		behavior = "only when the verified outer stack contains object,+0x70708B,+0x70708B: record context, then transfer the complete stack to +0x70708B"
	}
	return ImportStub{
		Module: "ClientBase.dll", Library: "ClientBase.dll", Symbol: "TPPostDialogReturn+0x6CFF15",
		StubAddress:   fmt.Sprintf("0x%X", page),
		Behavior:      behavior,
		TargetAddress: fmt.Sprintf("0x%X", targetAddress), TargetOriginal: fmt.Sprintf("%X", original),
		stubValue: page, targetValue: targetAddress, targetBytes: targetPatch, originalBytes: original,
		counterValue: captureAddress, callerValue: captureAddress + 8,
		rawStackValue: captureAddress + 0x100, rawStackWords: 64,
		postDialogCaptureValue: captureAddress,
	}, nil
}

func buildTPPostDialogTrapStub(
	captureAddress, sleepAddress, trampolineAddress, expectedOuter uintptr,
	recoveryMode string,
) []byte {
	stub := make([]byte, 0, 160)
	var forwardJumps []int
	appendUint32 := func(value uint32) {
		stub = binary.LittleEndian.AppendUint32(stub, value)
	}
	storeSavedDWORD := func(displacement byte, address uintptr) {
		stub = append(stub, 0x8B, 0x44, 0x24, displacement)
		stub = append(stub, 0xA3)
		appendUint32(uint32(address))
	}
	if recoveryMode != "" {
		for _, offset := range []byte{0x04, 0x08} {
			stub = append(stub, 0x81, 0x7C, 0x24, offset)
			appendUint32(uint32(expectedOuter))
			stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)
			forwardJumps = append(forwardJumps, len(stub)-4)
		}
		stub = append(stub, 0x39, 0x2C, 0x24) // cmp [esp],ebp
		stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)
		forwardJumps = append(forwardJumps, len(stub)-4)
	}

	stub = append(stub, 0x9C, 0x60)             // pushfd; pushad
	stub = append(stub, 0x8D, 0x74, 0x24, 0x24) // lea esi,[esp+24h] (original entry ESP)
	stub = append(stub, 0xFF, 0x05)
	appendUint32(uint32(captureAddress))
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0)
	stub = append(stub, 0xA3)
	appendUint32(uint32(captureAddress + 4))
	stub = append(stub, 0x8B, 0x06, 0xA3) // caller at original [esp]
	appendUint32(uint32(captureAddress + 8))
	storeSavedDWORD(0x1C, captureAddress+12) // EAX
	storeSavedDWORD(0x10, captureAddress+16) // EBX
	storeSavedDWORD(0x18, captureAddress+20) // ECX
	storeSavedDWORD(0x14, captureAddress+24) // EDX
	storeSavedDWORD(0x04, captureAddress+28) // ESI
	storeSavedDWORD(0x00, captureAddress+32) // EDI
	storeSavedDWORD(0x08, captureAddress+36) // EBP
	stub = append(stub, 0x8B, 0xC6, 0xA3)
	appendUint32(uint32(captureAddress + 40))
	storeSavedDWORD(0x20, captureAddress+44) // EFLAGS
	stub = append(stub, 0xBF)
	appendUint32(uint32(captureAddress + 0x100))
	stub = append(stub, 0xB9)
	appendUint32(64)
	stub = append(stub, 0xFC, 0xF3, 0xA5, 0x61, 0x9D)
	if recoveryMode != "" {
		switch recoveryMode {
		case "return":
			stub = append(stub, 0x83, 0xC4, 0x04, 0xC3)
		case "drop":
			stub = append(stub, 0x83, 0xC4, 0x04, 0x68)
			appendUint32(uint32(expectedOuter))
			stub = append(stub, 0xC3)
		case "full":
			// push/ret is a register-transparent absolute jump and leaves the
			// complete captured stack intact for the alternate protected entry.
			stub = append(stub, 0x68)
			appendUint32(uint32(expectedOuter))
			stub = append(stub, 0xC3)
		}
		forwardOffset := len(stub)
		stub = append(stub, 0xB8)
		appendUint32(uint32(trampolineAddress))
		stub = append(stub, 0xFF, 0xE0)
		for _, displacementOffset := range forwardJumps {
			binary.LittleEndian.PutUint32(
				stub[displacementOffset:displacementOffset+4],
				uint32(int32(forwardOffset-(displacementOffset+4))),
			)
		}
	} else {
		stub = append(stub, 0x68, 0xFF, 0xFF, 0xFF, 0xFF)
		stub = append(stub, 0xB8)
		appendUint32(uint32(sleepAddress))
		stub = append(stub, 0xFF, 0xD0, 0xEB, 0xFE)
	}
	return stub
}

func installBreakpointVEH(process syscall.Handle, pid uint32, timeout time.Duration) (uintptr, error) {
	addHandler, err := findRemoteExport(process, pid, "NTDLL.dll", "RtlAddVectoredExceptionHandler", timeout, 0)
	if err != nil {
		return 0, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return 0, fmt.Errorf("VirtualAllocEx breakpoint VEH: 0x%X (%v)", page, allocErr)
	}
	handlerAddress := page
	counterAddress := page + 0x100
	installerAddress := page + 0x200
	handler := []byte{
		0x55, 0x8B, 0xEC, 0x8B, 0x45, 0x08, 0x8B, 0x10, 0x8B, 0x0A,
		0x81, 0xF9, 0x1F, 0x00, 0x00, 0x40, 0x74, 0x08,
		0x81, 0xF9, 0x03, 0x00, 0x00, 0x80, 0x75, 0x21,
		0xFF, 0x05, 0, 0, 0, 0,
		0x8B, 0x48, 0x04, 0x8B, 0x52, 0x0C,
		0x39, 0x91, 0xB8, 0x00, 0x00, 0x00, 0x75, 0x06,
		0xFF, 0x81, 0xB8, 0x00, 0x00, 0x00,
		0x83, 0xC8, 0xFF, 0x5D, 0xC2, 0x04, 0x00,
		0x33, 0xC0, 0x5D, 0xC2, 0x04, 0x00,
	}
	binary.LittleEndian.PutUint32(handler[28:32], uint32(counterAddress))
	installer := []byte{0x68, 0, 0, 0, 0, 0x6A, 0x01, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0xC2, 0x04, 0x00}
	binary.LittleEndian.PutUint32(installer[1:5], uint32(handlerAddress))
	binary.LittleEndian.PutUint32(installer[8:12], uint32(addHandler))
	if err := writeRemote(process, handlerAddress, handler); err != nil {
		return 0, fmt.Errorf("write breakpoint handler: %w", err)
	}
	if err := writeRemote(process, installerAddress, installer); err != nil {
		return 0, fmt.Errorf("write breakpoint installer: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), handlerAddress, uintptr(len(handler)))
	procFlushInstruction.Call(uintptr(process), installerAddress, uintptr(len(installer)))
	registered, err := remoteCallOne(process, installerAddress, 0, timeout)
	if err != nil {
		return 0, err
	}
	if registered == 0 {
		return 0, fmt.Errorf("RtlAddVectoredExceptionHandler returned NULL")
	}
	return counterAddress, nil
}

func readAccessViolationCapture(process syscall.Handle, recordAddress uintptr) *AccessViolation {
	value, ok := readRemote(process, recordAddress, 15*4)
	if !ok || binary.LittleEndian.Uint32(value[0:4]) == 0 {
		return nil
	}
	capture := &AccessViolation{
		ExceptionCode:    0xC0000005,
		ExceptionAddress: binary.LittleEndian.Uint32(value[4:8]),
		ThreadID:         binary.LittleEndian.Uint32(value[56:60]),
		Operation:        binary.LittleEndian.Uint32(value[8:12]),
		TargetAddress:    binary.LittleEndian.Uint32(value[12:16]),
		EIP:              binary.LittleEndian.Uint32(value[16:20]),
		ESP:              binary.LittleEndian.Uint32(value[20:24]),
		EBP:              binary.LittleEndian.Uint32(value[24:28]),
		EAX:              binary.LittleEndian.Uint32(value[28:32]),
		EBX:              binary.LittleEndian.Uint32(value[32:36]),
		ECX:              binary.LittleEndian.Uint32(value[36:40]),
		EDX:              binary.LittleEndian.Uint32(value[40:44]),
		ESI:              binary.LittleEndian.Uint32(value[44:48]),
		EDI:              binary.LittleEndian.Uint32(value[48:52]),
		EFlags:           binary.LittleEndian.Uint32(value[52:56]),
	}
	if stack, stackOK := readRemote(process, recordAddress+0x100, 64*4); stackOK {
		for offset := 0; offset < len(stack); offset += 4 {
			capture.Stack = append(capture.Stack, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(stack[offset:offset+4])))
		}
	}
	eip := uintptr(capture.EIP)
	if eip >= 64 {
		codeBase := eip - 64
		if code, codeOK := readRemote(process, codeBase, 128); codeOK {
			capture.CodeBase = fmt.Sprintf("0x%08X", codeBase)
			capture.CodeHex = fmt.Sprintf("%X", code)
		}
	}
	var info memoryBasicInformation
	if queried, _, _ := procVirtualQueryEx.Call(
		uintptr(process), eip, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
	); queried == unsafe.Sizeof(info) {
		capture.MemoryBase = fmt.Sprintf("0x%08X", info.BaseAddress)
		capture.AllocationBase = fmt.Sprintf("0x%08X", info.AllocationBase)
		capture.RegionSize = fmt.Sprintf("0x%X", info.RegionSize)
		capture.MemoryState = fmt.Sprintf("0x%X", info.State)
		capture.MemoryProtect = fmt.Sprintf("0x%X", info.Protect)
		capture.MemoryType = fmt.Sprintf("0x%X", info.Type)
	}
	return capture
}

func installAccessViolationTrapVEH(process syscall.Handle, pid uint32, gateAddress uintptr, timeout time.Duration) (uintptr, error) {
	addHandler, err := findRemoteExport(process, pid, "NTDLL.dll", "RtlAddVectoredExceptionHandler", timeout, 0)
	if err != nil {
		return 0, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return 0, fmt.Errorf("VirtualAllocEx access-violation VEH: 0x%X (%v)", page, allocErr)
	}
	handlerAddress := page
	recordAddress := page + 0x400
	installerAddress := page + 0x800
	handler := []byte{
		0x55, 0x8B, 0xEC,
		0x8B, 0x45, 0x08,
		0x8B, 0x10,
		0x81, 0x3A, 0x05, 0x00, 0x00, 0xC0,
		0x0F, 0x85, 0, 0, 0, 0,
	}
	forwardJumps := []int{len(handler) - 4}
	if gateAddress != 0 {
		// cmp dword ptr [gateAddress],0; je forward. The gate is the exact TP
		// dialog capture count, so ordinary pre-dialog first-chance probes keep
		// their original exception-search behavior.
		handler = append(handler, 0x83, 0x3D)
		handler = binary.LittleEndian.AppendUint32(handler, uint32(gateAddress))
		handler = append(handler, 0x00, 0x0F, 0x84, 0, 0, 0, 0)
		forwardJumps = append(forwardJumps, len(handler)-4)
	}
	appendImmediateStore := func(address uintptr, value uint32) {
		handler = append(handler, 0xC7, 0x05, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(handler[len(handler)-8:len(handler)-4], uint32(address))
		binary.LittleEndian.PutUint32(handler[len(handler)-4:], value)
	}
	appendEAXStore := func(address uintptr) {
		handler = append(handler, 0xA3, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(handler[len(handler)-4:], uint32(address))
	}
	appendImmediateStore(recordAddress, 1)
	handler = append(handler, 0x8B, 0x42, 0x0C)
	appendEAXStore(recordAddress + 4)
	handler = append(handler, 0x8B, 0x42, 0x14)
	appendEAXStore(recordAddress + 8)
	handler = append(handler, 0x8B, 0x42, 0x18)
	appendEAXStore(recordAddress + 12)
	handler = append(handler, 0x8B, 0x4D, 0x08, 0x8B, 0x49, 0x04)
	appendContextStore := func(contextOffset uint32, recordOffset uintptr) {
		handler = append(handler, 0x8B, 0x81, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(handler[len(handler)-4:], contextOffset)
		appendEAXStore(recordAddress + recordOffset)
	}
	appendContextStore(0xB8, 16)
	appendContextStore(0xC4, 20)
	appendContextStore(0xB4, 24)
	appendContextStore(0xB0, 28)
	appendContextStore(0xA4, 32)
	appendContextStore(0xAC, 36)
	appendContextStore(0xA8, 40)
	appendContextStore(0xA0, 44)
	appendContextStore(0x9C, 48)
	appendContextStore(0xC0, 52)
	handler = append(handler, 0x64, 0xA1, 0x24, 0x00, 0x00, 0x00) // mov eax,fs:[TEB.ClientId.UniqueThread]
	appendEAXStore(recordAddress + 56)
	// Capture the first 64 DWORDs at the original faulting ESP before entering
	// the infinite diagnostic freeze. The source is a committed thread stack;
	// 256 bytes stays well within the committed stack region for the observed TP fault.
	handler = append(handler,
		0x8B, 0x4D, 0x08, // mov ecx,[ebp+8] (EXCEPTION_POINTERS)
		0x8B, 0x49, 0x04, // mov ecx,[ecx+4] (CONTEXT)
		0x8B, 0xB1, 0xC4, 0x00, 0x00, 0x00, // mov esi,[ecx+C4h] (original ESP)
		0xBF, 0, 0, 0, 0, // mov edi,stack record
		0xB9, 0x40, 0x00, 0x00, 0x00, // mov ecx,64
		0xFC, 0xF3, 0xA5, // cld; rep movsd
	)
	binary.LittleEndian.PutUint32(handler[len(handler)-12:len(handler)-8], uint32(recordAddress+0x100))
	handler = append(handler, 0xEB, 0xFE)
	forwardOffset := len(handler)
	handler = append(handler, 0x33, 0xC0, 0x5D, 0xC2, 0x04, 0x00)
	for _, forwardJump := range forwardJumps {
		binary.LittleEndian.PutUint32(handler[forwardJump:forwardJump+4], uint32(int32(forwardOffset-(forwardJump+4))))
	}
	installer := []byte{0x68, 0, 0, 0, 0, 0x6A, 0x01, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0xC2, 0x04, 0x00}
	binary.LittleEndian.PutUint32(installer[1:5], uint32(handlerAddress))
	binary.LittleEndian.PutUint32(installer[8:12], uint32(addHandler))
	if err := writeRemote(process, handlerAddress, handler); err != nil {
		return 0, fmt.Errorf("write access-violation handler: %w", err)
	}
	if err := writeRemote(process, installerAddress, installer); err != nil {
		return 0, fmt.Errorf("write access-violation installer: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), handlerAddress, uintptr(len(handler)))
	procFlushInstruction.Call(uintptr(process), installerAddress, uintptr(len(installer)))
	registered, err := remoteCallOne(process, installerAddress, 0, timeout)
	if err != nil {
		return 0, err
	}
	if registered == 0 {
		return 0, fmt.Errorf("RtlAddVectoredExceptionHandler for access violations returned NULL")
	}
	return recordAddress, nil
}

const (
	exceptionTraceCapacity   = 32
	exceptionTraceRecordSize = 0x50
)

func readExceptionTrace(process syscall.Handle, counterAddress, traceAddress uintptr) (uint32, []ExceptionCapture) {
	counterBytes, ok := readRemote(process, counterAddress, 4)
	if !ok {
		return 0, nil
	}
	total := binary.LittleEndian.Uint32(counterBytes)
	retained := total
	if retained > exceptionTraceCapacity {
		retained = exceptionTraceCapacity
	}
	start := total - retained
	result := make([]ExceptionCapture, 0, retained)
	for index := start; index < total; index++ {
		address := traceAddress + uintptr(index%exceptionTraceCapacity)*exceptionTraceRecordSize
		record, ok := readRemote(process, address, exceptionTraceRecordSize)
		if !ok {
			continue
		}
		result = append(result, ExceptionCapture{
			Index:            index,
			ExceptionCode:    binary.LittleEndian.Uint32(record[0:4]),
			ExceptionFlags:   binary.LittleEndian.Uint32(record[4:8]),
			ExceptionAddress: binary.LittleEndian.Uint32(record[8:12]),
			ParameterCount:   binary.LittleEndian.Uint32(record[12:16]),
			Parameter0:       binary.LittleEndian.Uint32(record[16:20]),
			Parameter1:       binary.LittleEndian.Uint32(record[20:24]),
			EIP:              binary.LittleEndian.Uint32(record[24:28]),
			ESP:              binary.LittleEndian.Uint32(record[28:32]),
			EBP:              binary.LittleEndian.Uint32(record[32:36]),
			EAX:              binary.LittleEndian.Uint32(record[36:40]),
			EBX:              binary.LittleEndian.Uint32(record[40:44]),
			ECX:              binary.LittleEndian.Uint32(record[44:48]),
			EDX:              binary.LittleEndian.Uint32(record[48:52]),
			ESI:              binary.LittleEndian.Uint32(record[52:56]),
			EDI:              binary.LittleEndian.Uint32(record[56:60]),
			EFlags:           binary.LittleEndian.Uint32(record[60:64]),
			ThreadID:         binary.LittleEndian.Uint32(record[64:68]),
		})
	}
	return total, result
}

// installExceptionTraceVEH installs a transparent, read-only-in-effect VEH. It
// retains the last 32 x86 exception contexts and always returns
// EXCEPTION_CONTINUE_SEARCH, so the client's original SEH/UEF chain remains in
// complete control of every exception.
func installExceptionTraceVEH(process syscall.Handle, pid uint32, timeout time.Duration) (uintptr, uintptr, error) {
	addHandler, err := findRemoteExport(process, pid, "NTDLL.dll", "RtlAddVectoredExceptionHandler", timeout, 0)
	if err != nil {
		return 0, 0, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x2000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return 0, 0, fmt.Errorf("VirtualAllocEx exception trace: 0x%X (%v)", page, allocErr)
	}
	handlerAddress := page
	counterAddress := page + 0x200
	traceAddress := page + 0x400
	installerAddress := page + 0xE00

	handler := []byte{0x55, 0x8B, 0xEC, 0x56, 0x57}         // prologue; preserve ESI/EDI
	handler = append(handler, 0xB8, 0x01, 0x00, 0x00, 0x00) // mov eax,1
	handler = append(handler, 0xF0, 0x0F, 0xC1, 0x05)       // lock xadd [counter],eax
	handler = binary.LittleEndian.AppendUint32(handler, uint32(counterAddress))
	handler = append(handler,
		0x83, 0xE0, 0x1F, // and eax,31
		0x8D, 0x04, 0x80, // lea eax,[eax+eax*4]
		0xC1, 0xE0, 0x04, // shl eax,4 (slot size 0x50)
		0x05, 0, 0, 0, 0, // add eax,traceAddress
		0x8B, 0xF8, // mov edi,eax
		0x8B, 0x75, 0x08, // mov esi,[ebp+8] (EXCEPTION_POINTERS)
		0x8B, 0x16, // mov edx,[esi] (EXCEPTION_RECORD)
	)
	binary.LittleEndian.PutUint32(handler[28:32], uint32(traceAddress))
	appendEDXStore := func(sourceOffset byte, destinationOffset byte) {
		handler = append(handler, 0x8B, 0x42, sourceOffset, 0x89, 0x47, destinationOffset)
	}
	appendEDXStore(0x00, 0x00)
	appendEDXStore(0x04, 0x04)
	appendEDXStore(0x0C, 0x08)
	appendEDXStore(0x10, 0x0C)
	appendEDXStore(0x14, 0x10)
	appendEDXStore(0x18, 0x14)
	handler = append(handler, 0x8B, 0x76, 0x04) // mov esi,[esi+4] (CONTEXT)
	appendESIStore := func(sourceOffset uint32, destinationOffset byte) {
		handler = append(handler, 0x8B, 0x86)
		handler = binary.LittleEndian.AppendUint32(handler, sourceOffset)
		handler = append(handler, 0x89, 0x47, destinationOffset)
	}
	appendESIStore(0xB8, 0x18)
	appendESIStore(0xC4, 0x1C)
	appendESIStore(0xB4, 0x20)
	appendESIStore(0xB0, 0x24)
	appendESIStore(0xA4, 0x28)
	appendESIStore(0xAC, 0x2C)
	appendESIStore(0xA8, 0x30)
	appendESIStore(0xA0, 0x34)
	appendESIStore(0x9C, 0x38)
	appendESIStore(0xC0, 0x3C)
	handler = append(handler,
		0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, // mov eax,fs:[24h] (thread ID)
		0x89, 0x47, 0x40, // mov [edi+40h],eax
		0x5F, 0x5E, 0x33, 0xC0, 0x5D, 0xC2, 0x04, 0x00, // return CONTINUE_SEARCH
	)
	installer := []byte{0x68, 0, 0, 0, 0, 0x6A, 0x01, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0xC2, 0x04, 0x00}
	binary.LittleEndian.PutUint32(installer[1:5], uint32(handlerAddress))
	binary.LittleEndian.PutUint32(installer[8:12], uint32(addHandler))
	if err := writeRemote(process, handlerAddress, handler); err != nil {
		return 0, 0, fmt.Errorf("write exception trace handler: %w", err)
	}
	if err := writeRemote(process, installerAddress, installer); err != nil {
		return 0, 0, fmt.Errorf("write exception trace installer: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), handlerAddress, uintptr(len(handler)))
	procFlushInstruction.Call(uintptr(process), installerAddress, uintptr(len(installer)))
	registered, err := remoteCallOne(process, installerAddress, 0, timeout)
	if err != nil {
		return 0, 0, err
	}
	if registered == 0 {
		return 0, 0, fmt.Errorf("RtlAddVectoredExceptionHandler for exception trace returned NULL")
	}
	return counterAddress, traceAddress, nil
}

// installTPWorkerFatalExceptionBypass recognizes the fully observed terminal
// TP sequences without relying on an ASLR-sensitive target address. The same
// thread must first raise STATUS_PRIVILEGED_INSTRUCTION at the exact ClientBase
// VM site. It is then ended only when it either executes the verified BOUND at
// EAX+3, or deliberately jumps to an unreadable address described identically
// by ExceptionAddress, EIP, EAX, and the read-fault target.
func installTPWorkerFatalExceptionBypass(process syscall.Handle, pid uint32, timeout time.Duration) (uintptr, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return 0, err
	}
	addHandler, err := findRemoteExport(process, pid, "NTDLL.dll", "RtlAddVectoredExceptionHandler", timeout, 0)
	if err != nil {
		return 0, err
	}
	exitThread, err := findRemoteExport(process, pid, "KERNEL32.dll", "ExitThread", timeout, 0)
	if err != nil {
		return 0, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return 0, fmt.Errorf("VirtualAllocEx TP worker fatal bypass: 0x%X (%v)", page, allocErr)
	}
	handlerAddress := page
	recordAddress := page + 0x200 // armed TID, bypass count, and the last same-thread terminal candidate
	exitStubAddress := page + 0x300
	installerAddress := page + 0x380
	expectedPrivilegedAddress := clientBase + 0x6C6F02

	handler := []byte{
		0x55, 0x8B, 0xEC, // push ebp; mov ebp,esp
		0x8B, 0x45, 0x08, // mov eax,[ebp+8] (EXCEPTION_POINTERS)
		0x8B, 0x10, // mov edx,[eax] (EXCEPTION_RECORD)
		0x81, 0x3A, 0x96, 0x00, 0x00, 0xC0, // cmp [edx],C0000096h
		0x0F, 0x84, // je arm
	}
	armJump := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	forwardJumps := make([]int, 0, 16)
	appendConditionalForwardJump := func(condition byte) {
		handler = append(handler, 0x0F, condition)
		forwardJumps = append(forwardJumps, len(handler))
		handler = binary.LittleEndian.AppendUint32(handler, 0)
	}
	handler = append(handler, 0x64, 0x8B, 0x0D, 0x24, 0x00, 0x00, 0x00) // mov ecx,fs:[24h]
	handler = append(handler, 0x3B, 0x0D)                               // cmp ecx,[armed TID]
	armedCompare := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	appendConditionalForwardJump(0x85)                            // jne forward
	handler = append(handler, 0x81, 0x3A, 0x06, 0x00, 0x01, 0x40) // cmp [edx],DBG_PRINTEXCEPTION_C
	appendConditionalForwardJump(0x84)                            // je forward

	// Retain the exact exception that follows the late privileged-instruction
	// arm on the same TP worker. This record is diagnostic even when neither
	// verified terminal predicate accepts the candidate.
	handler = append(handler, 0xFF, 0x05) // inc dword ptr [candidate count]
	candidateCounter := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	handler = append(handler, 0x8B, 0x02, 0xA3) // mov eax,[edx]; mov [code],eax
	candidateCode := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	handler = append(handler, 0x8B, 0x42, 0x0C, 0xA3) // mov eax,[edx+0Ch]; mov [address],eax
	candidateAddress := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	handler = append(handler, 0x8B, 0x45, 0x08, 0x8B, 0x40, 0x04) // reload CONTEXT
	handler = append(handler, 0x8B, 0x88, 0xB8, 0x00, 0x00, 0x00, 0x89, 0x0D)
	candidateEIP := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	handler = append(handler, 0x8B, 0x88, 0xB0, 0x00, 0x00, 0x00, 0x89, 0x0D)
	candidateEAX := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	handler = append(handler, 0x8B, 0x4A, 0x10, 0x89, 0x0D)
	candidateParameterCount := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	handler = append(handler, 0x8B, 0x4A, 0x14, 0x89, 0x0D)
	candidateParameter0 := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	handler = append(handler, 0x8B, 0x4A, 0x18, 0x89, 0x0D)
	candidateParameter1 := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	handler = append(handler, 0x64, 0x8B, 0x0D, 0x24, 0x00, 0x00, 0x00, 0x89, 0x0D)
	candidateThreadID := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)

	// The currently observed Win11 terminal form is a read access violation in
	// which TP transfers control to the same deliberately invalid value held in
	// EAX and reported as ExceptionAddress and ExceptionInformation[1]. Branch
	// to its exact predicate; otherwise retain the earlier verified BOUND path.
	handler = append(handler, 0x81, 0x3A, 0x05, 0x00, 0x00, 0xC0) // cmp [edx],C0000005h
	handler = append(handler, 0x0F, 0x84)                         // je accessViolation
	accessViolationJump := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	handler = append(handler, 0x81, 0x3A, 0x8C, 0x00, 0x00, 0xC0) // cmp [edx],C000008Ch
	appendConditionalForwardJump(0x85)                            // jne forward
	handler = append(handler,
		0x8B, 0x45, 0x08, // mov eax,[ebp+8] (EXCEPTION_POINTERS)
		0x8B, 0x48, 0x04, // mov ecx,[eax+4] (CONTEXT)
		0x8B, 0x91, 0xB8, 0x00, 0x00, 0x00, // mov edx,[ecx+B8h] (EIP)
		0x8B, 0x81, 0xB0, 0x00, 0x00, 0x00, // mov eax,[ecx+B0h] (EAX)
		0x83, 0xC0, 0x03, // add eax,3
		0x3B, 0xD0, // cmp edx,eax
	)
	appendConditionalForwardJump(0x85)
	handler = append(handler, 0x80, 0x3A, 0x62) // cmp byte ptr [edx],62h (BOUND)
	appendConditionalForwardJump(0x85)
	handler = append(handler, 0xE9) // jmp bypass
	boundBypassJump := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)

	accessViolationOffset := len(handler)
	handler = append(handler,
		0x8B, 0x45, 0x08, // mov eax,[ebp+8] (EXCEPTION_POINTERS)
		0x8B, 0x10, // mov edx,[eax] (EXCEPTION_RECORD)
		0x8B, 0x48, 0x04, // mov ecx,[eax+4] (CONTEXT)
		0x83, 0x7A, 0x10, 0x02, // cmp dword ptr [edx+10h],2 (parameter count)
	)
	appendConditionalForwardJump(0x85)
	handler = append(handler, 0x83, 0x7A, 0x14, 0x00) // cmp dword ptr [edx+14h],0 (read)
	appendConditionalForwardJump(0x85)
	handler = append(handler,
		0x8B, 0x81, 0xB8, 0x00, 0x00, 0x00, // mov eax,[ecx+B8h] (EIP)
		0x3B, 0x81, 0xB0, 0x00, 0x00, 0x00, // cmp eax,[ecx+B0h] (EAX)
	)
	appendConditionalForwardJump(0x85)
	handler = append(handler, 0x3B, 0x42, 0x0C) // cmp eax,[edx+0Ch] (ExceptionAddress)
	appendConditionalForwardJump(0x85)
	handler = append(handler, 0x3B, 0x42, 0x18) // cmp eax,[edx+18h] (read target)
	appendConditionalForwardJump(0x85)

	bypassOffset := len(handler)
	handler = append(handler, 0xFF, 0x05) // inc dword ptr [bypass count]
	bypassCounter := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	handler = append(handler, 0xC7, 0x81, 0xB8, 0x00, 0x00, 0x00) // mov [ecx+B8h],exit stub
	exitStubImmediate := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	handler = append(handler,
		0x83, 0xC8, 0xFF, // mov eax,-1 (EXCEPTION_CONTINUE_EXECUTION)
		0x5D, 0xC2, 0x04, 0x00, // pop ebp; ret 4
	)
	armOffset := len(handler)
	handler = append(handler, 0x81, 0x7A, 0x0C) // cmp dword ptr [edx+0Ch],expected address
	expectedAddressImmediate := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	appendConditionalForwardJump(0x85)
	handler = append(handler, 0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0xA3) // mov eax,fs:[24h]; mov [armed TID],eax
	armedStore := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	handler = append(handler, 0xC7, 0x05) // mov dword ptr [candidate count],0
	armCandidateCounter := len(handler)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	handler = binary.LittleEndian.AppendUint32(handler, 0)
	forwardOffset := len(handler)
	handler = append(handler, 0x33, 0xC0, 0x5D, 0xC2, 0x04, 0x00)

	binary.LittleEndian.PutUint32(handler[armJump:armJump+4], uint32(int32(armOffset-(armJump+4))))
	binary.LittleEndian.PutUint32(
		handler[accessViolationJump:accessViolationJump+4],
		uint32(int32(accessViolationOffset-(accessViolationJump+4))),
	)
	binary.LittleEndian.PutUint32(
		handler[boundBypassJump:boundBypassJump+4],
		uint32(int32(bypassOffset-(boundBypassJump+4))),
	)
	for _, displacementOffset := range forwardJumps {
		binary.LittleEndian.PutUint32(
			handler[displacementOffset:displacementOffset+4],
			uint32(int32(forwardOffset-(displacementOffset+4))),
		)
	}
	binary.LittleEndian.PutUint32(handler[armedCompare:armedCompare+4], uint32(recordAddress))
	binary.LittleEndian.PutUint32(handler[candidateCounter:candidateCounter+4], uint32(recordAddress+8))
	binary.LittleEndian.PutUint32(handler[candidateCode:candidateCode+4], uint32(recordAddress+12))
	binary.LittleEndian.PutUint32(handler[candidateAddress:candidateAddress+4], uint32(recordAddress+16))
	binary.LittleEndian.PutUint32(handler[candidateEIP:candidateEIP+4], uint32(recordAddress+20))
	binary.LittleEndian.PutUint32(handler[candidateEAX:candidateEAX+4], uint32(recordAddress+24))
	binary.LittleEndian.PutUint32(handler[candidateParameterCount:candidateParameterCount+4], uint32(recordAddress+28))
	binary.LittleEndian.PutUint32(handler[candidateParameter0:candidateParameter0+4], uint32(recordAddress+32))
	binary.LittleEndian.PutUint32(handler[candidateParameter1:candidateParameter1+4], uint32(recordAddress+36))
	binary.LittleEndian.PutUint32(handler[candidateThreadID:candidateThreadID+4], uint32(recordAddress+40))
	binary.LittleEndian.PutUint32(handler[bypassCounter:bypassCounter+4], uint32(recordAddress+4))
	binary.LittleEndian.PutUint32(handler[exitStubImmediate:exitStubImmediate+4], uint32(exitStubAddress))
	binary.LittleEndian.PutUint32(handler[expectedAddressImmediate:expectedAddressImmediate+4], uint32(expectedPrivilegedAddress))
	binary.LittleEndian.PutUint32(handler[armedStore:armedStore+4], uint32(recordAddress))
	binary.LittleEndian.PutUint32(handler[armCandidateCounter:armCandidateCounter+4], uint32(recordAddress+8))
	if len(handler) > int(recordAddress-handlerAddress) {
		return 0, fmt.Errorf("TP worker fatal handler is %d bytes and overlaps its record page", len(handler))
	}

	exitStub := []byte{0x6A, 0x00, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0xEB, 0xFE}
	binary.LittleEndian.PutUint32(exitStub[3:7], uint32(exitThread))
	installer := []byte{0x68, 0, 0, 0, 0, 0x6A, 0x01, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0xC2, 0x04, 0x00}
	binary.LittleEndian.PutUint32(installer[1:5], uint32(handlerAddress))
	binary.LittleEndian.PutUint32(installer[8:12], uint32(addHandler))
	if err := writeRemote(process, handlerAddress, handler); err != nil {
		return 0, fmt.Errorf("write TP worker fatal handler: %w", err)
	}
	if err := writeRemote(process, exitStubAddress, exitStub); err != nil {
		return 0, fmt.Errorf("write TP worker exit stub: %w", err)
	}
	if err := writeRemote(process, installerAddress, installer); err != nil {
		return 0, fmt.Errorf("write TP worker fatal installer: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), handlerAddress, uintptr(len(handler)))
	procFlushInstruction.Call(uintptr(process), exitStubAddress, uintptr(len(exitStub)))
	procFlushInstruction.Call(uintptr(process), installerAddress, uintptr(len(installer)))
	registered, err := remoteCallOne(process, installerAddress, 0, timeout)
	if err != nil {
		return 0, err
	}
	if registered == 0 {
		return 0, fmt.Errorf("RtlAddVectoredExceptionHandler for TP worker fatal bypass returned NULL")
	}
	return recordAddress, nil
}

func installTPEntryBypass(process, mainThread syscall.Handle, pid uint32, timeout time.Duration) (uintptr, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return 0, err
	}
	addHandler, err := findRemoteExport(process, pid, "NTDLL.dll", "RtlAddVectoredExceptionHandler", timeout, 0)
	if err != nil {
		return 0, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return 0, fmt.Errorf("VirtualAllocEx TP entry VEH: 0x%X (%v)", page, allocErr)
	}
	handlerAddress := page
	counterAddress := page + 0x200
	installerAddress := page + 0x300
	expectedReturn := clientBase + 0x8D1E74
	handler := []byte{
		0x55, 0x8B, 0xEC, 0x8B, 0x45, 0x08, 0x8B, 0x10, 0x8B, 0x0A,
		0x81, 0xF9, 0x04, 0x00, 0x00, 0x80, 0x74, 0x08,
		0x81, 0xF9, 0x1E, 0x00, 0x00, 0x40, 0x75, 0x7D,
		0x81, 0x3D, 0xA4, 0xC1, 0x32, 0x01, 0, 0, 0, 0, 0x75, 0x60,
		0x81, 0x3D, 0xAC, 0xC1, 0x32, 0x01, 0, 0, 0, 0, 0x75, 0x54,
		0x81, 0x3D, 0xB0, 0xC1, 0x32, 0x01, 0x05, 0x00, 0x00, 0x00, 0x75, 0x48,
		0x81, 0x3D, 0xB4, 0xC1, 0x32, 0x01, 0x1C, 0x02, 0x00, 0x00, 0x75, 0x3C,
		0x8B, 0x48, 0x04,
		0xC7, 0x81, 0xB8, 0x00, 0x00, 0x00, 0, 0, 0, 0,
		0xC7, 0x81, 0xC4, 0x00, 0x00, 0x00, 0xB8, 0xC1, 0x32, 0x01,
		0xC7, 0x81, 0xB0, 0x00, 0x00, 0x00, 0, 0, 0, 0,
		0xC7, 0x41, 0x18, 0, 0, 0, 0,
		0xC7, 0x41, 0x14, 0, 0, 0, 0,
		0xFF, 0x05, 0, 0, 0, 0,
		0x83, 0xC8, 0xFF, 0x5D, 0xC2, 0x04, 0x00,
		0x8B, 0x48, 0x04, 0xC7, 0x41, 0x14, 0, 0, 0, 0,
		0x83, 0xC8, 0xFF, 0x5D, 0xC2, 0x04, 0x00,
		0x33, 0xC0, 0x5D, 0xC2, 0x04, 0x00,
	}
	binary.LittleEndian.PutUint32(handler[32:36], uint32(expectedReturn))
	binary.LittleEndian.PutUint32(handler[83:87], uint32(expectedReturn))
	binary.LittleEndian.PutUint32(handler[123:127], uint32(counterAddress))
	installer := []byte{0x68, 0, 0, 0, 0, 0x6A, 0x01, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0xC2, 0x04, 0x00}
	binary.LittleEndian.PutUint32(installer[1:5], uint32(handlerAddress))
	binary.LittleEndian.PutUint32(installer[8:12], uint32(addHandler))
	if err := writeRemote(process, handlerAddress, handler); err != nil {
		return 0, fmt.Errorf("write TP entry handler: %w", err)
	}
	if err := writeRemote(process, installerAddress, installer); err != nil {
		return 0, fmt.Errorf("write TP entry installer: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), handlerAddress, uintptr(len(handler)))
	procFlushInstruction.Call(uintptr(process), installerAddress, uintptr(len(installer)))
	registered, err := remoteCallOne(process, installerAddress, 0, timeout)
	if err != nil {
		return 0, err
	}
	if registered == 0 {
		return 0, fmt.Errorf("RtlAddVectoredExceptionHandler for TP entry returned NULL")
	}

	// The verified failure call occurs at about 10.8 seconds on this build.
	// Arming late avoids unrelated early calls that reuse the same stack slot.
	time.Sleep(8 * time.Second)
	previous, _, suspendErr := procSuspendThread.Call(uintptr(mainThread))
	if previous == ^uintptr(0) {
		return 0, fmt.Errorf("SuspendThread for TP entry breakpoint: %w", suspendErr)
	}
	defer procResumeThread.Call(uintptr(mainThread))
	context := make([]byte, 716)
	binary.LittleEndian.PutUint32(context[0:4], 0x00010013)
	success, _, contextErr := procWow64GetThreadContext.Call(uintptr(mainThread), uintptr(unsafe.Pointer(&context[0])))
	if success == 0 {
		return 0, fmt.Errorf("Wow64GetThreadContext for TP entry: %w", contextErr)
	}
	binary.LittleEndian.PutUint32(context[4:8], 0x0132C1A4)
	binary.LittleEndian.PutUint32(context[20:24], 0)
	binary.LittleEndian.PutUint32(context[24:28], 0x000D0001)
	success, _, contextErr = procWow64SetThreadContext.Call(uintptr(mainThread), uintptr(unsafe.Pointer(&context[0])))
	if success == 0 {
		return 0, fmt.Errorf("Wow64SetThreadContext for TP entry: %w", contextErr)
	}
	return counterAddress, nil
}

func installExportWaitStub(process syscall.Handle, pid uint32, moduleName, symbolName string, timeout time.Duration) (ImportStub, error) {
	targetAddress, err := findRemoteExport(process, pid, moduleName, symbolName, timeout, 0)
	if err != nil {
		return ImportStub{}, err
	}
	sleepAddress, err := findRemoteExport(process, pid, "KERNEL32.dll", "Sleep", timeout, 0)
	if err != nil {
		return ImportStub{}, err
	}
	stub := makeWaitStub(sleepAddress)
	original, ok := readRemote(process, targetAddress, len(stub))
	if !ok {
		return ImportStub{}, fmt.Errorf("read %s!%s at 0x%X", moduleName, symbolName, targetAddress)
	}
	if !ensureRemoteBytes(process, targetAddress, stub, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch %s!%s at 0x%X", moduleName, symbolName, targetAddress)
	}
	return ImportStub{
		Module: moduleName, Library: moduleName, Symbol: symbolName,
		StubAddress: fmt.Sprintf("0x%X", targetAddress), Behavior: "Sleep(INFINITE) preserving caller stack",
		TargetAddress: fmt.Sprintf("0x%X", targetAddress), TargetOriginal: fmt.Sprintf("%X", original),
		targetValue: targetAddress, targetBytes: stub, originalBytes: original,
	}, nil
}

func installTPFreeStageTransitionGuard(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, error) {
	coreBase, err := waitForModule(process, pid, "Core.dll", timeout)
	if err != nil {
		return ImportStub{}, err
	}

	// Hook Core.dll's own PostQuitMessage import slot instead of inspecting or
	// modifying USER32 code. System DLL export thunks legitimately differ across
	// Windows 10/11 builds, while this call instruction belongs to the fixed game
	// module shipped with the release. The on-disk client remains untouched.
	const postQuitCallLength = uintptr(6)
	postQuitCallAddress := coreBase + corePostQuitWrapperReturnRVA - postQuitCallLength
	postQuitCall, ok := readRemote(process, postQuitCallAddress, int(postQuitCallLength))
	if !ok {
		return ImportStub{}, fmt.Errorf("read Core.dll PostQuitMessage call at 0x%X", postQuitCallAddress)
	}
	postQuitIAT, validCall := parseX86AbsoluteIndirectCall(postQuitCall)
	if !validCall {
		return ImportStub{}, fmt.Errorf("unsupported Core.dll PostQuitMessage call at 0x%X: %X", postQuitCallAddress, postQuitCall)
	}
	originalPointer, ok := readRemote(process, postQuitIAT, 4)
	if !ok {
		return ImportStub{}, fmt.Errorf("read Core.dll PostQuitMessage IAT at 0x%X", postQuitIAT)
	}
	forwardTarget := uintptr(binary.LittleEndian.Uint32(originalPointer))
	if forwardTarget == 0 {
		return ImportStub{}, fmt.Errorf("Core.dll PostQuitMessage IAT at 0x%X is null", postQuitIAT)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx TP-free channel-transition guard: 0x%X (%v)", page, allocErr)
	}

	counterAddress := page + 0x100
	stub := buildTPFreeStageTransitionGuardStub(
		coreBase+corePostQuitWrapperReturnRVA,
		coreBase+coreStageTransitionPostQuitReturnRVA,
		forwardTarget,
		counterAddress,
	)
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write TP-free channel-transition guard: %w", err)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(page))
	if !ensureRemoteBytes(process, postQuitIAT, pointer, pageReadWrite) {
		return ImportStub{}, fmt.Errorf("patch Core.dll PostQuitMessage IAT at 0x%X", postQuitIAT)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	return ImportStub{
		Module: "Core.dll", Library: "USER32.dll", Symbol: "PostQuitMessage(QQTDir-to-QQTSection handoff)",
		IATAddress:     fmt.Sprintf("0x%X", postQuitIAT),
		StubAddress:    fmt.Sprintf("0x%X", page),
		Behavior:       "suppress only Core+0x2622 called from Core+0xF83B during QQTDir-to-QQTSection handoff; forward every other Core PostQuitMessage unchanged",
		TargetAddress:  fmt.Sprintf("0x%X", postQuitIAT),
		TargetOriginal: fmt.Sprintf("%X", originalPointer),
		iatValue:       postQuitIAT, stubValue: page,
		counterValue: counterAddress,
	}, nil
}

func parseX86AbsoluteIndirectCall(instruction []byte) (uintptr, bool) {
	if len(instruction) != 6 || instruction[0] != 0xFF || instruction[1] != 0x15 {
		return 0, false
	}
	address := uintptr(binary.LittleEndian.Uint32(instruction[2:6]))
	return address, address != 0
}

func buildTPFreeStageTransitionGuardStub(coreWrapperReturn, coreTransitionReturn, forwardTarget, counterAddress uintptr) []byte {
	stub := make([]byte, 0, 48)
	appendUint32 := func(value uintptr) {
		stub = binary.LittleEndian.AppendUint32(stub, uint32(value))
	}
	stub = append(stub, 0x81, 0x3C, 0x24) // cmp dword ptr [esp],coreWrapperReturn
	appendUint32(coreWrapperReturn)
	stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0) // jne forward
	firstForwardJump := len(stub) - 4
	stub = append(stub, 0x81, 0x7D, 0x04) // cmp dword ptr [ebp+4],coreTransitionReturn
	appendUint32(coreTransitionReturn)
	stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0) // jne forward
	secondForwardJump := len(stub) - 4
	stub = append(stub, 0xFF, 0x05) // inc dword ptr [counterAddress]
	appendUint32(counterAddress)
	stub = append(stub, 0xC2, 0x04, 0x00) // ret 4
	forwardOffset := len(stub)
	stub = append(stub, 0xB8) // mov eax,forwardTarget; jmp eax
	appendUint32(forwardTarget)
	stub = append(stub, 0xFF, 0xE0)
	for _, displacementOffset := range []int{firstForwardJump, secondForwardJump} {
		displacement := forwardOffset - (displacementOffset + 4)
		binary.LittleEndian.PutUint32(stub[displacementOffset:displacementOffset+4], uint32(int32(displacement)))
	}
	return stub
}

func installConditionalZeroStatusTerminateStub(process syscall.Handle, pid uint32, returnInstead bool, timeout time.Duration) (ImportStub, error) {
	targetAddress, err := findRemoteExport(process, pid, "NTDLL.dll", "NtTerminateProcess", timeout, 0)
	if err != nil {
		return ImportStub{}, err
	}
	sleepAddress, err := findRemoteExport(process, pid, "KERNEL32.dll", "Sleep", timeout, 0)
	if err != nil {
		return ImportStub{}, err
	}
	const stolenSize = 5
	original, ok := readRemote(process, targetAddress, 15)
	if !ok {
		return ImportStub{}, fmt.Errorf("read NTDLL!NtTerminateProcess at 0x%X", targetAddress)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx self-terminate trap: 0x%X (%v)", page, allocErr)
	}
	trampolineAddress := page + 0x300
	captureAddress := page + 0x400
	trampoline := append([]byte{}, original[:stolenSize]...)
	trampoline = append(trampoline, 0xE9, 0, 0, 0, 0)
	trampolineDisplacement := int64(targetAddress+stolenSize) - int64(trampolineAddress+uintptr(len(trampoline)))
	if trampolineDisplacement < -0x80000000 || trampolineDisplacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("NtTerminateProcess trampoline displacement out of range")
	}
	binary.LittleEndian.PutUint32(trampoline[stolenSize+1:], uint32(int32(trampolineDisplacement)))
	stub := buildConditionalZeroStatusTerminateStub(captureAddress, sleepAddress, trampolineAddress, returnInstead)
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write self-terminate trap: %w", err)
	}
	if err := writeRemote(process, trampolineAddress, trampoline); err != nil {
		return ImportStub{}, fmt.Errorf("write NtTerminateProcess trampoline: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0}
	targetDisplacement := int64(page) - int64(targetAddress+uintptr(len(targetPatch)))
	if targetDisplacement < -0x80000000 || targetDisplacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("NtTerminateProcess target displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(targetDisplacement)))
	if !ensureRemoteBytes(process, targetAddress, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch NtTerminateProcess at 0x%X", targetAddress)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), trampolineAddress, uintptr(len(trampoline)))
	behavior := "record and freeze only status-0 termination; forward every other status unchanged"
	if returnInstead {
		behavior = "record and return STATUS_SUCCESS only for post-dialog status-0 termination; forward every other status unchanged"
	}
	return ImportStub{
		Module: "NTDLL.dll", Library: "NTDLL.dll", Symbol: "NtTerminateProcess(status 0 only)",
		StubAddress:   fmt.Sprintf("0x%X", page),
		Behavior:      behavior,
		TargetAddress: fmt.Sprintf("0x%X", targetAddress), TargetOriginal: fmt.Sprintf("%X", original),
		stubValue: page, targetValue: targetAddress, targetBytes: targetPatch, originalBytes: original,
		counterValue: captureAddress, callerValue: captureAddress + 8,
		rawStackValue: captureAddress + 0x100, rawStackWords: 64,
		nativeTerminateCaptureValue: captureAddress,
	}, nil
}

func buildConditionalZeroStatusTerminateStub(captureAddress, sleepAddress, trampolineAddress uintptr, returnInstead bool) []byte {
	stub := make([]byte, 0, 256)
	appendUint32 := func(value uint32) {
		stub = binary.LittleEndian.AppendUint32(stub, value)
	}
	storeEntryDWORD := func(displacement byte, address uintptr) {
		if displacement == 0 {
			stub = append(stub, 0x8B, 0x06) // mov eax,[esi]
		} else {
			stub = append(stub, 0x8B, 0x46, displacement)
		}
		stub = append(stub, 0xA3)
		appendUint32(uint32(address))
	}
	storeSavedDWORD := func(displacement byte, address uintptr) {
		stub = append(stub, 0x8B, 0x44, 0x24, displacement)
		stub = append(stub, 0xA3)
		appendUint32(uint32(address))
	}

	stub = append(stub, 0x9C, 0x60)                   // pushfd; pushad
	stub = append(stub, 0x83, 0x7C, 0x24, 0x2C, 0x00) // cmp dword ptr [esp+2Ch],0 (exit status)
	stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)       // jne forward
	nonSelfJump := len(stub) - 4

	stub = append(stub, 0x8D, 0x74, 0x24, 0x24) // lea esi,[esp+24h] (original entry ESP)
	stub = append(stub, 0xFF, 0x05)
	appendUint32(uint32(captureAddress))           // inc capture.hits
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0) // mov eax,fs:[24h]
	stub = append(stub, 0xA3)
	appendUint32(uint32(captureAddress + 4))
	storeEntryDWORD(0x00, captureAddress+8)  // caller
	storeEntryDWORD(0x04, captureAddress+12) // target process handle
	storeEntryDWORD(0x08, captureAddress+16) // exit status
	storeSavedDWORD(0x1C, captureAddress+20) // EAX
	storeSavedDWORD(0x10, captureAddress+24) // EBX
	storeSavedDWORD(0x18, captureAddress+28) // ECX
	storeSavedDWORD(0x14, captureAddress+32) // EDX
	storeSavedDWORD(0x04, captureAddress+36) // ESI
	storeSavedDWORD(0x00, captureAddress+40) // EDI
	storeSavedDWORD(0x08, captureAddress+44) // EBP
	storeSavedDWORD(0x20, captureAddress+48) // EFLAGS
	stub = append(stub, 0x8B, 0xC6, 0xA3)
	appendUint32(uint32(captureAddress + 52)) // entry ESP
	stub = append(stub, 0xBF)
	appendUint32(uint32(captureAddress + 0x100))
	stub = append(stub, 0xB9)
	appendUint32(64)
	stub = append(stub, 0xFC, 0xF3, 0xA5, 0x61, 0x9D) // cld; rep movsd; popad; popfd
	if returnInstead {
		stub = append(stub, 0x33, 0xC0, 0xC2, 0x08, 0x00) // STATUS_SUCCESS; ret 8
	} else {
		stub = append(stub, 0x68, 0xFF, 0xFF, 0xFF, 0xFF)
		stub = append(stub, 0xB8)
		appendUint32(uint32(sleepAddress))
		stub = append(stub, 0xFF, 0xD0, 0xEB, 0xFE) // Sleep(INFINITE); loop if it returns
	}

	forwardOffset := len(stub)
	stub = append(stub, 0x61, 0x9D) // popad; popfd
	stub = append(stub, 0xB8)
	appendUint32(uint32(trampolineAddress))
	stub = append(stub, 0xFF, 0xE0)
	binary.LittleEndian.PutUint32(stub[nonSelfJump:nonSelfJump+4], uint32(int32(forwardOffset-(nonSelfJump+4))))
	return stub
}

func installIATWaitStub(process syscall.Handle, pid uint32, moduleName, libraryName, symbolName string, timeout time.Duration) (ImportStub, error) {
	base, err := waitForModule(process, pid, moduleName, timeout)
	if err != nil {
		return ImportStub{}, err
	}
	iatAddress, err := findImportAddress(process, base, libraryName, symbolName)
	if err != nil {
		return ImportStub{}, err
	}
	original, ok := readRemote(process, iatAddress, 4)
	if !ok {
		return ImportStub{}, fmt.Errorf("read %s!%s IAT at 0x%X", moduleName, symbolName, iatAddress)
	}
	sleepAddress, err := findRemoteExport(process, pid, "KERNEL32.dll", "Sleep", timeout, 0)
	if err != nil {
		return ImportStub{}, err
	}
	stubAddress, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if stubAddress == 0 || stubAddress > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx wait stub: 0x%X (%v)", stubAddress, allocErr)
	}
	stub := makeWaitStub(sleepAddress)
	if err := writeRemote(process, stubAddress, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write wait stub: %w", err)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(stubAddress))
	if !ensureRemoteBytes(process, iatAddress, pointer, pageReadWrite) {
		return ImportStub{}, fmt.Errorf("patch %s!%s IAT at 0x%X", moduleName, symbolName, iatAddress)
	}
	procFlushInstruction.Call(uintptr(process), stubAddress, uintptr(len(stub)))
	return ImportStub{
		Module: moduleName, Library: libraryName, Symbol: symbolName,
		IATAddress: fmt.Sprintf("0x%X", iatAddress), StubAddress: fmt.Sprintf("0x%X", stubAddress),
		Behavior: "Sleep(INFINITE) preserving ExitProcess caller stack", TargetOriginal: fmt.Sprintf("%X", original),
		iatValue: iatAddress, stubValue: stubAddress,
	}, nil
}

func makeWaitStub(sleepAddress uintptr) []byte {
	stub := []byte{0x68, 0xFF, 0xFF, 0xFF, 0xFF, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0xEB, 0xFE}
	binary.LittleEndian.PutUint32(stub[6:10], uint32(sleepAddress))
	return stub
}

func installExportReturnStub(process syscall.Handle, pid uint32, moduleName, symbolName string, returnValue uint32, stackBytes uint16, timeout time.Duration) (ImportStub, error) {
	targetAddress, err := findRemoteExport(process, pid, moduleName, symbolName, timeout, 0)
	if err != nil {
		return ImportStub{}, err
	}
	stub := []byte{0xB8, byte(returnValue), byte(returnValue >> 8), byte(returnValue >> 16), byte(returnValue >> 24), 0xC2, byte(stackBytes), byte(stackBytes >> 8)}
	const signatureSize = 19
	original, ok := readRemote(process, targetAddress, signatureSize)
	if !ok {
		return ImportStub{}, fmt.Errorf("read %s!%s at 0x%X", moduleName, symbolName, targetAddress)
	}
	if !ensureRemoteBytes(process, targetAddress, stub, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch %s!%s at 0x%X", moduleName, symbolName, targetAddress)
	}
	return ImportStub{
		Module: moduleName, Library: moduleName, Symbol: symbolName,
		StubAddress: fmt.Sprintf("0x%X", targetAddress), Behavior: fmt.Sprintf("return %d; ret %d", returnValue, stackBytes),
		TargetAddress: fmt.Sprintf("0x%X", targetAddress), TargetOriginal: fmt.Sprintf("%X", original),
		targetValue: targetAddress, targetBytes: stub, originalBytes: original,
	}, nil
}

func disableRemoteIME(process syscall.Handle, pid uint32, timeout time.Duration) error {
	loadLibrary, err := findRemoteExport(process, pid, "KERNEL32.dll", "LoadLibraryW", timeout, 0)
	if err != nil {
		return err
	}
	name, err := syscall.UTF16FromString("imm32.dll")
	if err != nil {
		return err
	}
	nameBytes := make([]byte, len(name)*2)
	for index, word := range name {
		binary.LittleEndian.PutUint16(nameBytes[index*2:], word)
	}
	remoteName, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, uintptr(len(nameBytes)), memReserve|memCommit, pageReadWrite,
	)
	if remoteName == 0 || remoteName > 0xFFFFFFFF {
		return fmt.Errorf("VirtualAllocEx imm32 name: 0x%X (%v)", remoteName, allocErr)
	}
	if err := writeRemote(process, remoteName, nameBytes); err != nil {
		return fmt.Errorf("write imm32 name: %w", err)
	}
	moduleResult, err := remoteCallOne(process, loadLibrary, remoteName, timeout)
	if err != nil {
		return fmt.Errorf("remote LoadLibraryW: %w", err)
	}
	if moduleResult == 0 {
		return fmt.Errorf("remote LoadLibraryW returned NULL")
	}
	immDisable, err := findRemoteExport(process, pid, "IMM32.dll", "ImmDisableIME", timeout, 0)
	if err != nil {
		return err
	}
	disabled, err := remoteCallOne(process, immDisable, uintptr(0xFFFFFFFF), timeout)
	if err != nil {
		return fmt.Errorf("remote ImmDisableIME: %w", err)
	}
	if disabled == 0 {
		return fmt.Errorf("remote ImmDisableIME returned FALSE")
	}
	return nil
}

func remoteCallOne(process syscall.Handle, function, parameter uintptr, timeout time.Duration) (uint32, error) {
	var threadID uint32
	threadValue, _, createErr := procCreateRemoteThread.Call(
		uintptr(process), 0, 0, function, parameter, 0, uintptr(unsafe.Pointer(&threadID)),
	)
	if threadValue == 0 {
		return 0, fmt.Errorf("CreateRemoteThread at 0x%X: %w", function, createErr)
	}
	thread := syscall.Handle(threadValue)
	defer syscall.CloseHandle(thread)
	waitMilliseconds := uint32(timeout / time.Millisecond)
	waitResult, _, waitErr := procWaitForSingle.Call(uintptr(thread), uintptr(waitMilliseconds))
	if waitResult != waitObject0 {
		return 0, fmt.Errorf("wait remote thread %d result 0x%X: %w", threadID, waitResult, waitErr)
	}
	var exitCode uint32
	success, _, exitErr := procGetExitCodeThread.Call(uintptr(thread), uintptr(unsafe.Pointer(&exitCode)))
	if success == 0 {
		return 0, fmt.Errorf("GetExitCodeThread %d: %w", threadID, exitErr)
	}
	return exitCode, nil
}

func findRemoteExport(process syscall.Handle, pid uint32, moduleName, symbolName string, timeout time.Duration, depth int) (uintptr, error) {
	if depth > 8 {
		return 0, fmt.Errorf("export forwarder recursion for %s!%s", moduleName, symbolName)
	}
	base, err := waitForModule(process, pid, moduleName, timeout)
	if err != nil {
		return 0, err
	}
	header, ok := readRemote(process, base, 4096)
	if !ok || len(header) < 0x100 || header[0] != 'M' || header[1] != 'Z' {
		return 0, fmt.Errorf("invalid PE header for %s", moduleName)
	}
	peOffset := int(binary.LittleEndian.Uint32(header[0x3C:0x40]))
	optional := peOffset + 24
	if optional+104 > len(header) || binary.LittleEndian.Uint16(header[optional:optional+2]) != 0x10B {
		return 0, fmt.Errorf("remote module %s is not PE32", moduleName)
	}
	exportRVA := binary.LittleEndian.Uint32(header[optional+96 : optional+100])
	exportSize := binary.LittleEndian.Uint32(header[optional+100 : optional+104])
	if exportRVA == 0 {
		return 0, fmt.Errorf("remote module %s has no export directory", moduleName)
	}
	directory, ok := readRemote(process, base+uintptr(exportRVA), 40)
	if !ok {
		return 0, fmt.Errorf("read %s export directory", moduleName)
	}
	functionCount := binary.LittleEndian.Uint32(directory[20:24])
	nameCount := binary.LittleEndian.Uint32(directory[24:28])
	functionsRVA := binary.LittleEndian.Uint32(directory[28:32])
	namesRVA := binary.LittleEndian.Uint32(directory[32:36])
	ordinalsRVA := binary.LittleEndian.Uint32(directory[36:40])
	if functionCount == 0 || nameCount == 0 || functionCount > 1<<20 || nameCount > 1<<20 {
		return 0, fmt.Errorf("invalid %s export counts", moduleName)
	}
	names, ok := readRemote(process, base+uintptr(namesRVA), int(nameCount)*4)
	if !ok {
		return 0, fmt.Errorf("read %s export names", moduleName)
	}
	ordinals, ok := readRemote(process, base+uintptr(ordinalsRVA), int(nameCount)*2)
	if !ok {
		return 0, fmt.Errorf("read %s export ordinals", moduleName)
	}
	functions, ok := readRemote(process, base+uintptr(functionsRVA), int(functionCount)*4)
	if !ok {
		return 0, fmt.Errorf("read %s export functions", moduleName)
	}
	for index := uint32(0); index < nameCount; index++ {
		nameRVA := binary.LittleEndian.Uint32(names[index*4 : index*4+4])
		name, ok := readCString(process, base+uintptr(nameRVA), 1024)
		if !ok || name != symbolName {
			continue
		}
		ordinal := uint32(binary.LittleEndian.Uint16(ordinals[index*2 : index*2+2]))
		if ordinal >= functionCount {
			return 0, fmt.Errorf("invalid ordinal %d for %s!%s", ordinal, moduleName, symbolName)
		}
		functionRVA := binary.LittleEndian.Uint32(functions[ordinal*4 : ordinal*4+4])
		if functionRVA >= exportRVA && functionRVA < exportRVA+exportSize {
			forwarder, ok := readCString(process, base+uintptr(functionRVA), 1024)
			if !ok {
				return 0, fmt.Errorf("read forwarder for %s!%s", moduleName, symbolName)
			}
			separator := strings.LastIndexByte(forwarder, '.')
			if separator <= 0 || separator == len(forwarder)-1 || forwarder[separator+1] == '#' {
				return 0, fmt.Errorf("unsupported forwarder %q", forwarder)
			}
			forwardModule := forwarder[:separator]
			if !strings.HasSuffix(strings.ToLower(forwardModule), ".dll") {
				forwardModule += ".dll"
			}
			return findRemoteExport(process, pid, forwardModule, forwarder[separator+1:], timeout, depth+1)
		}
		return base + uintptr(functionRVA), nil
	}
	return 0, fmt.Errorf("export %s not found in %s", symbolName, moduleName)
}

func installReturnStub(process syscall.Handle, pid uint32, moduleName, libraryName, symbolName string, returnValue uint32, stackBytes uint16, timeout time.Duration) (ImportStub, error) {
	base, err := waitForModule(process, pid, moduleName, timeout)
	if err != nil {
		return ImportStub{}, err
	}
	iatAddress, err := findImportAddress(process, base, libraryName, symbolName)
	if err != nil {
		return ImportStub{}, err
	}
	stubAddress, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if stubAddress == 0 || stubAddress > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx stub: 0x%X (%v)", stubAddress, allocErr)
	}
	stub := []byte{0xB8, byte(returnValue), byte(returnValue >> 8), byte(returnValue >> 16), byte(returnValue >> 24), 0xC2, byte(stackBytes), byte(stackBytes >> 8)}
	targetAddress, err := waitForExecutablePointer(process, iatAddress, timeout)
	if err != nil {
		return ImportStub{}, err
	}
	targetOriginal, ok := readRemote(process, targetAddress, len(stub))
	if !ok {
		return ImportStub{}, fmt.Errorf("read target function at 0x%X", targetAddress)
	}
	if err := writeRemote(process, stubAddress, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write return stub: %w", err)
	}
	var oldProtect uint32
	success, _, protectErr := procVirtualProtectEx.Call(uintptr(process), iatAddress, 4, pageReadWrite, uintptr(unsafe.Pointer(&oldProtect)))
	if success == 0 {
		return ImportStub{}, fmt.Errorf("VirtualProtectEx IAT: %w", protectErr)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(stubAddress))
	writeErr := writeRemote(process, iatAddress, pointer)
	var ignored uint32
	procVirtualProtectEx.Call(uintptr(process), iatAddress, 4, uintptr(oldProtect), uintptr(unsafe.Pointer(&ignored)))
	if writeErr != nil {
		return ImportStub{}, fmt.Errorf("patch IAT: %w", writeErr)
	}
	var targetOldProtect uint32
	success, _, protectErr = procVirtualProtectEx.Call(uintptr(process), targetAddress, uintptr(len(stub)), pageExecuteReadWrite, uintptr(unsafe.Pointer(&targetOldProtect)))
	if success == 0 {
		return ImportStub{}, fmt.Errorf("VirtualProtectEx target 0x%X: %w", targetAddress, protectErr)
	}
	targetWriteErr := writeRemote(process, targetAddress, stub)
	procVirtualProtectEx.Call(uintptr(process), targetAddress, uintptr(len(stub)), uintptr(targetOldProtect), uintptr(unsafe.Pointer(&ignored)))
	if targetWriteErr != nil {
		return ImportStub{}, fmt.Errorf("patch target function: %w", targetWriteErr)
	}
	procFlushInstruction.Call(uintptr(process), stubAddress, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), targetAddress, uintptr(len(stub)))
	return ImportStub{
		Module: moduleName, Library: libraryName, Symbol: symbolName,
		IATAddress: fmt.Sprintf("0x%X", iatAddress), StubAddress: fmt.Sprintf("0x%X", stubAddress),
		Behavior:      fmt.Sprintf("return %d; ret %d", returnValue, stackBytes),
		TargetAddress: fmt.Sprintf("0x%X", targetAddress), TargetOriginal: fmt.Sprintf("%X", targetOriginal),
		iatValue: iatAddress, stubValue: stubAddress, targetValue: targetAddress, targetBytes: stub,
	}, nil
}

func installGetVersionExAWin7Stub(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, error) {
	base, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, err
	}
	iatAddress, err := findImportAddress(process, base, "KERNEL32.dll", "GetVersionExA")
	if err != nil {
		return ImportStub{}, err
	}
	targetAddress, err := waitForExecutablePointer(process, iatAddress, timeout)
	if err != nil {
		return ImportStub{}, err
	}
	stubAddress, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if stubAddress == 0 || stubAddress > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx GetVersionExA shim: 0x%X (%v)", stubAddress, allocErr)
	}
	// BOOL WINAPI GetVersionExA(LPOSVERSIONINFOA): report Windows 7 SP1
	// (6.1.7601) while preserving the caller-provided structure size.
	stub := []byte{
		0x8B, 0x44, 0x24, 0x04, // mov eax,[esp+4]
		0x85, 0xC0, // test eax,eax
		0x74, 0x00, // jz fail
		0xC7, 0x40, 0x04, 0x06, 0x00, 0x00, 0x00,
		0xC7, 0x40, 0x08, 0x01, 0x00, 0x00, 0x00,
		0xC7, 0x40, 0x0C, 0xB1, 0x1D, 0x00, 0x00,
		0xC7, 0x40, 0x10, 0x02, 0x00, 0x00, 0x00,
		0xC7, 0x40, 0x14, 0x53, 0x65, 0x72, 0x76, // "Serv"
		0xC7, 0x40, 0x18, 0x69, 0x63, 0x65, 0x20, // "ice "
		0xC7, 0x40, 0x1C, 0x50, 0x61, 0x63, 0x6B, // "Pack"
		0xC7, 0x40, 0x20, 0x20, 0x31, 0x00, 0x00, // " 1"
		0x81, 0x38, 0x9C, 0x00, 0x00, 0x00, // cmp dword [eax],156
		0x72, 0x00, // jb success
		0x66, 0xC7, 0x80, 0x94, 0x00, 0x00, 0x00, 0x01, 0x00,
		0x66, 0xC7, 0x80, 0x96, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x66, 0xC7, 0x80, 0x98, 0x00, 0x00, 0x00, 0x00, 0x00,
		0xC6, 0x80, 0x9A, 0x00, 0x00, 0x00, 0x01,
		0xC6, 0x80, 0x9B, 0x00, 0x00, 0x00, 0x00,
	}
	successOffset := len(stub)
	stub = append(stub, 0xB8, 0x01, 0x00, 0x00, 0x00, 0xC2, 0x04, 0x00)
	failOffset := len(stub)
	stub = append(stub, 0x33, 0xC0, 0xC2, 0x04, 0x00)
	if failOffset-8 > 0x7F || successOffset-72 > 0x7F {
		return ImportStub{}, fmt.Errorf("GetVersionExA shim branch exceeds short-jump range")
	}
	stub[7] = byte(failOffset - 8)
	stub[71] = byte(successOffset - 72)
	if err := writeRemote(process, stubAddress, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write GetVersionExA shim: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0}
	displacement := int64(stubAddress) - int64(targetAddress+uintptr(len(targetPatch)))
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("GetVersionExA shim displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(displacement)))
	targetOriginal, ok := readRemote(process, targetAddress, len(targetPatch))
	if !ok {
		return ImportStub{}, fmt.Errorf("read GetVersionExA at 0x%X", targetAddress)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(stubAddress))
	if !ensureRemoteBytes(process, iatAddress, pointer, pageReadWrite) {
		return ImportStub{}, fmt.Errorf("patch ClientBase GetVersionExA IAT")
	}
	if !ensureRemoteBytes(process, targetAddress, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch GetVersionExA entry")
	}
	procFlushInstruction.Call(uintptr(process), stubAddress, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), targetAddress, uintptr(len(targetPatch)))
	return ImportStub{
		Module: "ClientBase.dll", Library: "KERNEL32.dll", Symbol: "GetVersionExA",
		IATAddress: fmt.Sprintf("0x%X", iatAddress), StubAddress: fmt.Sprintf("0x%X", stubAddress),
		Behavior:      "report Windows 7 SP1 (6.1.7601) in this process",
		TargetAddress: fmt.Sprintf("0x%X", targetAddress), TargetOriginal: fmt.Sprintf("%X", targetOriginal),
		iatValue: iatAddress, stubValue: stubAddress, targetValue: targetAddress,
		targetBytes: targetPatch, originalBytes: targetOriginal,
	}, nil
}

func installTPErrorHandlerBypass(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, err
	}
	const (
		entryRVA      = uintptr(0x8BF4BC)
		callRVA       = uintptr(0x8D1E6E)
		normalReturn  = uintptr(0x8D1E73)
		patchedLength = 8
	)
	entry := clientBase + entryRVA
	entrySignature := []byte{0x8D, 0x64, 0x24, 0x04, 0x89, 0x04, 0x24, 0x57}
	deadline := time.Now().Add(timeout)
	for {
		current, ok := readRemote(process, entry, len(entrySignature))
		if ok && bytes.Equal(current, entrySignature) {
			break
		}
		if time.Now().After(deadline) {
			return ImportStub{}, fmt.Errorf("TP handler signature not available at 0x%X", entry)
		}
		time.Sleep(200 * time.Microsecond)
	}
	callSignature := []byte{0xE8, 0x49, 0xD6, 0xFE, 0xFF, 0x7B}
	callBytes, ok := readRemote(process, clientBase+callRVA, len(callSignature))
	if !ok || !bytes.Equal(callBytes, callSignature) {
		return ImportStub{}, fmt.Errorf("TP handler call-site signature mismatch at 0x%X", clientBase+callRVA)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx TP handler bypass: 0x%X (%v)", page, allocErr)
	}
	traceAddress := page + 0x200
	counterAddress := page + 0x240
	stub := make([]byte, 0, 256)
	// Capture the most recent eight entry-stack dwords without perturbing the
	// protected caller's registers or flags. The fatal call is the final entry
	// before its modal warning, so the last trace is the useful one.
	stub = append(stub, 0x9C, 0x60, 0x8D, 0x74, 0x24, 0x24) // pushfd; pushad; lea esi,[esp+36]
	for index := 0; index < 8; index++ {
		if index == 0 {
			stub = append(stub, 0x8B, 0x06) // mov eax,[esi]
		} else {
			stub = append(stub, 0x8B, 0x46, byte(index*4)) // mov eax,[esi+offset]
		}
		stub = append(stub, 0xA3, 0, 0, 0, 0) // mov [trace+offset],eax
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+uintptr(index*4)))
	}
	stub = append(stub, 0xFF, 0x05, 0, 0, 0, 0, 0x61, 0x9D) // inc [counter]; popad; popfd
	binary.LittleEndian.PutUint32(stub[len(stub)-6:len(stub)-2], uint32(counterAddress))
	var forwardJumps []int
	appendForwardJump := func() {
		stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)
		forwardJumps = append(forwardJumps, len(stub)-4)
	}
	stub = append(stub, 0x81, 0x3C, 0x24, 0, 0, 0, 0) // cmp [esp],normal return
	binary.LittleEndian.PutUint32(stub[3:7], uint32(clientBase+normalReturn))
	appendForwardJump()
	stub = append(stub, 0x83, 0x7C, 0x24, 0x08, 0x00) // cmp [esp+8],0
	appendForwardJump()
	stub = append(stub, 0x83, 0x7C, 0x24, 0x0C, 0x05) // cmp [esp+0xc],5
	appendForwardJump()
	stub = append(stub, 0x81, 0x7C, 0x24, 0x10, 0x1C, 0x02, 0x00, 0x00) // cmp [esp+0x10],540
	appendForwardJump()
	// Preserve the caller's flags and registers, skip the one-byte junk after
	// the protected call, and clean the verified four stdcall arguments.
	stub = append(stub, 0x9C, 0xFF, 0x44, 0x24, 0x04, 0x9D, 0xC2, 0x10, 0x00)
	forwardOffset := len(stub)
	trampoline := page + 0x100
	stub = append(stub, 0xB8, 0, 0, 0, 0, 0xFF, 0xE0)
	binary.LittleEndian.PutUint32(stub[forwardOffset+1:forwardOffset+5], uint32(trampoline))
	for _, displacementOffset := range forwardJumps {
		next := displacementOffset + 4
		binary.LittleEndian.PutUint32(stub[displacementOffset:displacementOffset+4], uint32(int32(forwardOffset-next)))
	}
	trampolineBytes := append([]byte{}, entrySignature...)
	trampolineBytes = append(trampolineBytes, 0xE9, 0, 0, 0, 0)
	backDisplacement := int64(entry+patchedLength) - int64(trampoline+uintptr(len(trampolineBytes)))
	if backDisplacement < -0x80000000 || backDisplacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("TP handler trampoline displacement out of range")
	}
	binary.LittleEndian.PutUint32(trampolineBytes[len(trampolineBytes)-4:], uint32(int32(backDisplacement)))
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write TP handler bypass: %w", err)
	}
	if err := writeRemote(process, trampoline, trampolineBytes); err != nil {
		return ImportStub{}, fmt.Errorf("write TP handler trampoline: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0, 0x90, 0x90, 0x90}
	targetDisplacement := int64(page) - int64(entry+5)
	if targetDisplacement < -0x80000000 || targetDisplacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("TP handler bypass displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:5], uint32(int32(targetDisplacement)))
	if !ensureRemoteBytes(process, entry, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch TP handler entry at 0x%X", entry)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), trampoline, uintptr(len(trampolineBytes)))
	procFlushInstruction.Call(uintptr(process), entry, uintptr(len(targetPatch)))
	return ImportStub{
		Module: "ClientBase.dll", Library: "ClientBase.dll", Symbol: "TPError(0,5,540)",
		StubAddress:   fmt.Sprintf("0x%X", page),
		Behavior:      "preserve caller state; skip protected fatal handler only for return+0,5,540; forward all non-matches",
		TargetAddress: fmt.Sprintf("0x%X", entry), TargetOriginal: fmt.Sprintf("%X", entrySignature),
		targetValue: entry, targetBytes: targetPatch, originalBytes: entrySignature,
		counterValue: counterAddress, traceValue: traceAddress,
	}, nil
}

func installTPSystemAPITraces(
	process syscall.Handle,
	pid uint32,
	skipAdjustTokenPrivileges bool,
	skipTesSafeAPIs bool,
	timeout time.Duration,
) ([]ImportStub, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return nil, err
	}
	getLastErrorIAT, err := findImportAddress(process, clientBase, "KERNEL32.dll", "GetLastError")
	if err != nil {
		return nil, err
	}
	getLastErrorTarget, err := waitForExecutablePointer(process, getLastErrorIAT, timeout)
	if err != nil {
		return nil, err
	}
	type apiSpec struct {
		library string
		symbol  string
		args    int
	}
	specs := []apiSpec{
		{library: "ADVAPI32.dll", symbol: "OpenSCManagerA", args: 3},
		{library: "ADVAPI32.dll", symbol: "OpenServiceW", args: 3},
		{library: "ADVAPI32.dll", symbol: "CreateServiceW", args: 13},
		{library: "ADVAPI32.dll", symbol: "StartServiceA", args: 3},
		{library: "ADVAPI32.dll", symbol: "OpenProcessToken", args: 3},
		{library: "ADVAPI32.dll", symbol: "LookupPrivilegeValueA", args: 3},
		{library: "ADVAPI32.dll", symbol: "AdjustTokenPrivileges", args: 6},
		{library: "KERNEL32.dll", symbol: "CreateFileA", args: 7},
		{library: "KERNEL32.dll", symbol: "CreateFileW", args: 7},
		{library: "KERNEL32.dll", symbol: "GetProcAddress", args: 2},
		{library: "KERNEL32.dll", symbol: "SetUnhandledExceptionFilter", args: 1},
		{library: "KERNEL32.dll", symbol: "DeviceIoControl", args: 8},
		{library: "KERNEL32.dll", symbol: "CreateEventA", args: 4},
		{library: "KERNEL32.dll", symbol: "OpenEventA", args: 3},
		{library: "KERNEL32.dll", symbol: "CreateMutexA", args: 3},
		{library: "KERNEL32.dll", symbol: "CreateMutexW", args: 3},
		{library: "KERNEL32.dll", symbol: "OpenMutexA", args: 3},
		{library: "KERNEL32.dll", symbol: "CreateFileMappingA", args: 6},
		{library: "KERNEL32.dll", symbol: "OpenFileMappingA", args: 3},
		{library: "KERNEL32.dll", symbol: "ReleaseMutex", args: 1},
		{library: "KERNEL32.dll", symbol: "WaitForSingleObject", args: 2},
		{library: "KERNEL32.dll", symbol: "ReadProcessMemory", args: 5},
		{library: "KERNEL32.dll", symbol: "VirtualProtect", args: 4},
		{library: "KERNEL32.dll", symbol: "VirtualAlloc", args: 4},
		{library: "KERNEL32.dll", symbol: "VirtualFree", args: 3},
		{library: "KERNEL32.dll", symbol: "VirtualQuery", args: 3},
		{library: "KERNEL32.dll", symbol: "GetThreadContext", args: 2},
		{library: "KERNEL32.dll", symbol: "SetThreadContext", args: 2},
		{library: "KERNEL32.dll", symbol: "OpenProcess", args: 3},
		{library: "KERNEL32.dll", symbol: "OpenThread", args: 3},
		{library: "KERNEL32.dll", symbol: "SuspendThread", args: 1},
		{library: "KERNEL32.dll", symbol: "CreateToolhelp32Snapshot", args: 2},
		{library: "KERNEL32.dll", symbol: "Process32First", args: 2},
		{library: "KERNEL32.dll", symbol: "Process32Next", args: 2},
		{library: "KERNEL32.dll", symbol: "IsDebuggerPresent", args: 0},
		{library: "KERNEL32.dll", symbol: "IsWow64Process", args: 2},
		{library: "USER32.dll", symbol: "FindWindowExA", args: 4},
		{library: "USER32.dll", symbol: "EnumWindows", args: 2},
		{library: "WINTRUST.dll", symbol: "WinVerifyTrust", args: 3},
		{library: "ntdll.dll", symbol: "NtQueryInformationProcess", args: 5},
		{library: "ntdll.dll", symbol: "NtQueryInformationThread", args: 5},
		{library: "ntdll.dll", symbol: "NtGetContextThread", args: 2},
		{library: "ntdll.dll", symbol: "NtProtectVirtualMemory", args: 5},
		{library: "ntdll.dll", symbol: "NtWriteVirtualMemory", args: 5},
	}
	stubs := make([]ImportStub, 0, len(specs))
	for _, spec := range specs {
		if skipAdjustTokenPrivileges && spec.symbol == "AdjustTokenPrivileges" {
			continue
		}
		if skipTesSafeAPIs && (spec.symbol == "CreateFileA" || spec.symbol == "DeviceIoControl") {
			continue
		}
		stub, err := installSystemAPIReturnTrace(
			process, clientBase, spec.library, spec.symbol, spec.args, getLastErrorTarget, timeout,
		)
		if err != nil {
			return nil, err
		}
		stubs = append(stubs, stub)
	}
	return stubs, nil
}

// installAdjustTokenPrivilegesErrorShim is a narrow diagnostic for the TP
// startup path. AdjustTokenPrivileges can return TRUE while setting
// ERROR_NOT_ALL_ASSIGNED when the token does not contain the requested
// privilege. The legacy protection checks this path before its first warning.
// This shim preserves the real BOOL result and all arguments, but changes the
// thread's last-error value from ERROR_NOT_ALL_ASSIGNED to ERROR_SUCCESS only
// for a successful call. It also records enough state to prove whether the
// experimental correction was exercised.
func installAdjustTokenPrivilegesErrorShim(
	process syscall.Handle,
	pid uint32,
	correctionCall uint32,
	timeout time.Duration,
) (ImportStub, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, err
	}
	adjustIAT, err := findImportAddress(process, clientBase, "ADVAPI32.dll", "AdjustTokenPrivileges")
	if err != nil {
		return ImportStub{}, err
	}
	adjustTarget, err := waitForExecutablePointer(process, adjustIAT, timeout)
	if err != nil {
		return ImportStub{}, err
	}
	getLastErrorIAT, err := findImportAddress(process, clientBase, "KERNEL32.dll", "GetLastError")
	if err != nil {
		return ImportStub{}, err
	}
	getLastErrorTarget, err := waitForExecutablePointer(process, getLastErrorIAT, timeout)
	if err != nil {
		return ImportStub{}, err
	}
	setLastErrorIAT, err := findImportAddress(process, clientBase, "KERNEL32.dll", "SetLastError")
	if err != nil {
		return ImportStub{}, err
	}
	setLastErrorTarget, err := waitForExecutablePointer(process, setLastErrorIAT, timeout)
	if err != nil {
		return ImportStub{}, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx AdjustTokenPrivileges error shim: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x100
	correctedAddress := page + 0x104
	traceAddress := page + 0x200
	stub := []byte{0x56, 0x8D, 0x74, 0x24, 0x04} // push esi; lea esi,[esp+4] (original ESP)
	for argument := 6; argument >= 1; argument-- {
		stub = append(stub, 0xFF, 0x76, byte(argument*4)) // push [esi+argument*4]
	}
	appendEAXImmediate := func(value uintptr) {
		stub = append(stub, 0xB8, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(value))
	}
	appendStoreEAX := func(address uintptr) {
		stub = append(stub, 0xA3, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(address))
	}
	appendEAXImmediate(adjustTarget)
	stub = append(stub, 0xFF, 0xD0) // call real AdjustTokenPrivileges
	appendStoreEAX(traceAddress)
	stub = append(stub, 0x50) // preserve the real BOOL result
	appendEAXImmediate(getLastErrorTarget)
	stub = append(stub, 0xFF, 0xD0) // call GetLastError
	appendStoreEAX(traceAddress + 4)
	stub = append(stub, 0x83, 0x3C, 0x24, 0x00) // cmp dword ptr [esp],0
	skipCorrectionJZ := len(stub)
	stub = append(stub, 0x74, 0)                      // je skipCorrection
	stub = append(stub, 0x3D, 0x14, 0x05, 0x00, 0x00) // cmp eax,ERROR_NOT_ALL_ASSIGNED
	skipCorrectionJNE := len(stub)
	stub = append(stub, 0x75, 0) // jne skipCorrection
	var skipCallJNE int
	if correctionCall != 0 {
		stub = append(stub, 0x81, 0x3D, 0, 0, 0, 0, 0, 0, 0, 0) // cmp dword ptr [counter],call-1
		binary.LittleEndian.PutUint32(stub[len(stub)-8:len(stub)-4], uint32(counterAddress))
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], correctionCall-1)
		skipCallJNE = len(stub)
		stub = append(stub, 0x75, 0) // jne skipCorrection
	}
	stub = append(stub, 0x6A, 0x00) // push ERROR_SUCCESS
	appendEAXImmediate(setLastErrorTarget)
	stub = append(stub, 0xFF, 0xD0, 0xFF, 0x05, 0, 0, 0, 0) // call eax; inc [corrected]
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(correctedAddress))
	skipCorrection := len(stub)
	stub[skipCorrectionJZ+1] = byte(skipCorrection - (skipCorrectionJZ + 2))
	stub[skipCorrectionJNE+1] = byte(skipCorrection - (skipCorrectionJNE + 2))
	if skipCallJNE != 0 {
		stub[skipCallJNE+1] = byte(skipCorrection - (skipCallJNE + 2))
	}
	stub = append(stub, 0x58, 0x52) // restore API BOOL; preserve edx while logging arguments
	for argument := 1; argument <= 6; argument++ {
		stub = append(stub, 0x8B, 0x56, byte(argument*4))
		stub = append(stub, 0x89, 0x15, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+8+uintptr((argument-1)*4)))
	}
	stub = append(stub, 0x5A, 0xFF, 0x05, 0, 0, 0, 0, 0x5E, 0xC2, 0x18, 0x00)
	binary.LittleEndian.PutUint32(stub[len(stub)-8:len(stub)-4], uint32(counterAddress))
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write AdjustTokenPrivileges error shim: %w", err)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(page))
	if !ensureRemoteBytes(process, adjustIAT, pointer, pageReadWrite) {
		return ImportStub{}, fmt.Errorf("patch ClientBase AdjustTokenPrivileges IAT")
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	behavior := "diagnostic: preserve the real BOOL result; clear ERROR_NOT_ALL_ASSIGNED only after a successful call"
	if correctionCall != 0 {
		behavior += fmt.Sprintf("; correction limited to call %d", correctionCall)
	}
	return ImportStub{
		Module: "ClientBase.dll", Library: "ADVAPI32.dll", Symbol: "AdjustTokenPrivileges",
		IATAddress: fmt.Sprintf("0x%X", adjustIAT), StubAddress: fmt.Sprintf("0x%X", page),
		Behavior:      behavior,
		TargetAddress: fmt.Sprintf("0x%X", adjustTarget),
		iatValue:      adjustIAT, stubValue: page, counterValue: counterAddress, correctedValue: correctedAddress, traceValue: traceAddress,
	}, nil
}

// installTesSafeDriverCompatibility isolates the obsolete TesSafe kernel path
// used by the bundled TP client. ClientBase is denied access to the Service
// Control Manager, so an elevated Win10 process cannot install or start the
// bundled driver. An exact \\.\TesSafe open is redirected to the Windows NUL
// device so the returned value is a real, closeable handle. DeviceIoControl is
// synthesized only for that handle; request buffers are retained unchanged and
// the most recent request is recorded for analysis. All unrelated calls jump
// directly to their original kernel32 targets.
func installTesSafeDriverCompatibility(process syscall.Handle, pid uint32, timeout time.Duration) ([]ImportStub, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return nil, err
	}
	openSCManagerIAT, err := findImportAddress(process, clientBase, "ADVAPI32.dll", "OpenSCManagerA")
	if err != nil {
		return nil, err
	}
	openSCManagerTarget, err := waitForExecutablePointer(process, openSCManagerIAT, timeout)
	if err != nil {
		return nil, err
	}
	createIAT, err := findImportAddress(process, clientBase, "KERNEL32.dll", "CreateFileA")
	if err != nil {
		return nil, err
	}
	createTarget, err := waitForExecutablePointer(process, createIAT, timeout)
	if err != nil {
		return nil, err
	}
	deviceIAT, err := findImportAddress(process, clientBase, "KERNEL32.dll", "DeviceIoControl")
	if err != nil {
		return nil, err
	}
	deviceTarget, err := waitForExecutablePointer(process, deviceIAT, timeout)
	if err != nil {
		return nil, err
	}
	findCloseIAT, err := findImportAddress(process, clientBase, "KERNEL32.dll", "FindClose")
	if err != nil {
		return nil, err
	}
	findCloseTarget, err := waitForExecutablePointer(process, findCloseIAT, timeout)
	if err != nil {
		return nil, err
	}
	closeHandleIAT, err := findImportAddress(process, clientBase, "KERNEL32.dll", "CloseHandle")
	if err != nil {
		return nil, err
	}
	closeHandleTarget, err := waitForExecutablePointer(process, closeHandleIAT, timeout)
	if err != nil {
		return nil, err
	}
	setLastErrorIAT, err := findImportAddress(process, clientBase, "KERNEL32.dll", "SetLastError")
	if err != nil {
		return nil, err
	}
	setLastErrorTarget, err := waitForExecutablePointer(process, setLastErrorIAT, timeout)
	if err != nil {
		return nil, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x5000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx TesSafe compatibility: 0x%X (%v)", page, allocErr)
	}
	const (
		createStubOffset          = uintptr(0x000)
		nulStringOffset           = uintptr(0x180)
		fakeHandleOffset          = uintptr(0x184)
		openCountOffset           = uintptr(0x188)
		deviceStubOffset          = uintptr(0x200)
		deviceCountOffset         = uintptr(0x800)
		deviceTraceOffset         = uintptr(0x820)
		binaryDataOffset          = uintptr(0x900)
		binarySizeOffset          = uintptr(0x980)
		createCallerOffset        = uintptr(0x9C0)
		deviceCallerOffset        = uintptr(0x9C4)
		rawStackOffset            = uintptr(0xA00)
		handleRingOffset          = uintptr(0xB00)
		findCloseStubOffset       = uintptr(0xC00)
		findCloseCountOffset      = uintptr(0xD00)
		findCloseCallerOffset     = uintptr(0xD04)
		deviceRingOffset          = uintptr(0x1000)
		openSCManagerStubOffset   = uintptr(0x3000)
		openSCManagerCountOffset  = uintptr(0x3100)
		openSCManagerCallerOffset = uintptr(0x3104)
		deviceRingEntries         = 32
		deviceRingStride          = 0x80
	)
	createStubAddress := page + createStubOffset
	nulStringAddress := page + nulStringOffset
	fakeHandleAddress := page + fakeHandleOffset
	openCountAddress := page + openCountOffset
	deviceStubAddress := page + deviceStubOffset
	deviceCountAddress := page + deviceCountOffset
	deviceTraceAddress := page + deviceTraceOffset
	binaryDataAddress := page + binaryDataOffset
	binarySizeAddress := page + binarySizeOffset
	createCallerAddress := page + createCallerOffset
	deviceCallerAddress := page + deviceCallerOffset
	rawStackAddress := page + rawStackOffset
	handleRingAddress := page + handleRingOffset
	findCloseStubAddress := page + findCloseStubOffset
	findCloseCountAddress := page + findCloseCountOffset
	findCloseCallerAddress := page + findCloseCallerOffset
	deviceRingAddress := page + deviceRingOffset
	openSCManagerStubAddress := page + openSCManagerStubOffset
	openSCManagerCountAddress := page + openSCManagerCountOffset
	openSCManagerCallerAddress := page + openSCManagerCallerOffset

	// Historical traces show ClientBase requests SC_MANAGER_ALL_ACCESS before
	// touching TesSafe. Non-elevated systems already return this exact failure
	// and continue into the user-mode device fallback. Make that safe behavior
	// deterministic even when the launcher itself is elevated.
	openSCManagerStub := []byte{
		0x8B, 0x04, 0x24, 0xA3, 0, 0, 0, 0, // mov eax,[esp]; mov [caller],eax
		0xFF, 0x05, 0, 0, 0, 0, // inc [count]
		0x6A, 0x05, // push ERROR_ACCESS_DENIED
		0xB8, 0, 0, 0, 0, // mov eax,SetLastError
		0xFF, 0xD0, // call eax
		0x33, 0xC0, // xor eax,eax (NULL SC_HANDLE)
		0xC2, 0x0C, 0x00, // ret 12
	}
	binary.LittleEndian.PutUint32(openSCManagerStub[4:8], uint32(openSCManagerCallerAddress))
	binary.LittleEndian.PutUint32(openSCManagerStub[10:14], uint32(openSCManagerCountAddress))
	binary.LittleEndian.PutUint32(openSCManagerStub[17:21], uint32(setLastErrorTarget))

	createStub := []byte{
		0x56, 0x8D, 0x74, 0x24, 0x04, // push esi; lea esi,[esp+4] (original ESP)
		0x8B, 0x06, 0xA3, 0, 0, 0, 0, // mov eax,[esi]; mov [caller],eax
		0x8B, 0x46, 0x04, // mov eax,[esi+4] (path)
		0x85, 0xC0, // test eax,eax
	}
	binary.LittleEndian.PutUint32(createStub[8:12], uint32(createCallerAddress))
	var createForwardJumps []int
	appendCreateForwardJump := func(condition byte) {
		createStub = append(createStub, 0x0F, condition, 0, 0, 0, 0)
		createForwardJumps = append(createForwardJumps, len(createStub)-4)
	}
	appendCreateForwardJump(0x84)                           // jz forward
	createStub = append(createStub, 0x81, 0x38, 0, 0, 0, 0) // cmp dword ptr [eax],"\\\\.\\"
	binary.LittleEndian.PutUint32(createStub[len(createStub)-4:], 0x5C2E5C5C)
	appendCreateForwardJump(0x85)
	createStub = append(createStub, 0x81, 0x78, 0x04, 0, 0, 0, 0) // cmp [eax+4],"TesS"
	binary.LittleEndian.PutUint32(createStub[len(createStub)-4:], 0x53736554)
	appendCreateForwardJump(0x85)
	createStub = append(createStub, 0x81, 0x78, 0x08, 0, 0, 0, 0) // cmp [eax+8],"afe\0"
	binary.LittleEndian.PutUint32(createStub[len(createStub)-4:], 0x00656661)
	appendCreateForwardJump(0x85)
	for argument := 7; argument >= 2; argument-- {
		createStub = append(createStub, 0xFF, 0x76, byte(argument*4))
	}
	createStub = append(createStub, 0x68, 0, 0, 0, 0) // push nulStringAddress
	binary.LittleEndian.PutUint32(createStub[len(createStub)-4:], uint32(nulStringAddress))
	createStub = append(createStub, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0) // mov eax,CreateFileA; call eax
	binary.LittleEndian.PutUint32(createStub[len(createStub)-6:len(createStub)-2], uint32(createTarget))
	createStub = append(createStub, 0xA3, 0, 0, 0, 0) // mov [fakeHandle],eax
	binary.LittleEndian.PutUint32(createStub[len(createStub)-4:], uint32(fakeHandleAddress))
	createStub = append(createStub, 0x8B, 0xD0)             // mov edx,eax
	createStub = append(createStub, 0x8B, 0x0D, 0, 0, 0, 0) // mov ecx,[openCount]
	binary.LittleEndian.PutUint32(createStub[len(createStub)-4:], uint32(openCountAddress))
	createStub = append(createStub, 0x83, 0xE1, 0x1F)             // and ecx,31
	createStub = append(createStub, 0x89, 0x14, 0x8D, 0, 0, 0, 0) // mov [handleRing+ecx*4],edx
	binary.LittleEndian.PutUint32(createStub[len(createStub)-4:], uint32(handleRingAddress))
	createStub = append(createStub, 0xFF, 0x05, 0, 0, 0, 0) // inc [openCount]
	binary.LittleEndian.PutUint32(createStub[len(createStub)-4:], uint32(openCountAddress))
	createStub = append(createStub, 0x8B, 0xC2)                                           // mov eax,edx
	createStub = append(createStub, 0x50, 0x6A, 0x00, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0x58) // preserve handle; SetLastError(0)
	binary.LittleEndian.PutUint32(createStub[len(createStub)-7:len(createStub)-3], uint32(setLastErrorTarget))
	createStub = append(createStub, 0x5E, 0xC2, 0x1C, 0x00) // pop esi; ret 28
	createForward := len(createStub)
	createStub = append(createStub, 0x5E, 0xB8, 0, 0, 0, 0, 0xFF, 0xE0) // pop esi; jmp real CreateFileA
	binary.LittleEndian.PutUint32(createStub[len(createStub)-6:len(createStub)-2], uint32(createTarget))
	for _, displacementOffset := range createForwardJumps {
		binary.LittleEndian.PutUint32(
			createStub[displacementOffset:displacementOffset+4],
			uint32(int32(createForward-(displacementOffset+4))),
		)
	}

	deviceStub := []byte{
		0x56, 0x8D, 0x74, 0x24, 0x04, // push esi; lea esi,[esp+4]
		0x8B, 0x46, 0x04, // mov eax,[esi+4] (handle)
		0x85, 0xC0, // test eax,eax
	}
	var deviceForwardJumps []int
	appendDeviceForwardJump := func(condition byte) {
		deviceStub = append(deviceStub, 0x0F, condition, 0, 0, 0, 0)
		deviceForwardJumps = append(deviceForwardJumps, len(deviceStub)-4)
	}
	appendDeviceForwardJump(0x84)
	deviceStub = append(deviceStub, 0x83, 0xF8, 0xFF) // cmp eax,INVALID_HANDLE_VALUE
	appendDeviceForwardJump(0x84)
	deviceStub = append(deviceStub, 0xB9, 0x20, 0, 0, 0) // mov ecx,32
	deviceStub = append(deviceStub, 0xBA, 0, 0, 0, 0)    // mov edx,handleRing
	binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-4:], uint32(handleRingAddress))
	handleSearch := len(deviceStub)
	deviceStub = append(deviceStub, 0x3B, 0x02, 0x74, 0) // cmp eax,[edx]; je matchedHandle
	matchedHandleJump := len(deviceStub) - 1
	deviceStub = append(deviceStub, 0x83, 0xC2, 0x04, 0xE2, 0) // add edx,4; loop handleSearch
	deviceStub[len(deviceStub)-1] = byte(int8(handleSearch - len(deviceStub)))
	deviceStub = append(deviceStub, 0xE9, 0, 0, 0, 0) // no ring match: forward
	deviceForwardJumps = append(deviceForwardJumps, len(deviceStub)-4)
	matchedHandle := len(deviceStub)
	deviceStub[matchedHandleJump] = byte(matchedHandle - (matchedHandleJump + 1))
	deviceStub = append(deviceStub, 0x8B, 0x06, 0xA3, 0, 0, 0, 0) // capture caller
	binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-4:], uint32(deviceCallerAddress))
	// Preserve the matched IAT-entry stack before the VM bridge consumes it.
	for index := 0; index < 20; index++ {
		if index == 0 {
			deviceStub = append(deviceStub, 0x8B, 0x06)
		} else {
			deviceStub = append(deviceStub, 0x8B, 0x46, byte(index*4))
		}
		deviceStub = append(deviceStub, 0xA3, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-4:], uint32(rawStackAddress+uintptr(index*4)))
	}
	// Retain a bounded request history before synthesizing the user-mode result.
	// Each 0x80-byte slot contains ten DWORDs of metadata followed by at most
	// 64 bytes from the input buffer. ECX/EDX/EAX are volatile across the real
	// API, while ESI (the original entry stack) and EDI are preserved.
	deviceStub = append(deviceStub, 0x8B, 0x0D, 0, 0, 0, 0) // mov ecx,[deviceCount]
	binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-4:], uint32(deviceCountAddress))
	deviceStub = append(deviceStub,
		0x8B, 0xD1, // mov edx,ecx
		0x83, 0xE2, byte(deviceRingEntries-1), // and edx,31
		0xC1, 0xE2, 0x07, // shl edx,7 (0x80-byte slot)
		0x81, 0xC2, 0, 0, 0, 0, // add edx,deviceRingAddress
		0x89, 0x0A, // mov [edx],ecx (zero-based call index)
	)
	binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-6:len(deviceStub)-2], uint32(deviceRingAddress))
	appendDeviceRingDWORD := func(stackOffset, recordOffset byte) {
		if stackOffset == 0 {
			deviceStub = append(deviceStub, 0x8B, 0x06) // mov eax,[esi]
		} else {
			deviceStub = append(deviceStub, 0x8B, 0x46, stackOffset) // mov eax,[esi+offset]
		}
		deviceStub = append(deviceStub, 0x89, 0x42, recordOffset) // mov [edx+offset],eax
	}
	for index := byte(0); index < 9; index++ {
		appendDeviceRingDWORD(index*4, 4+index*4)
	}
	deviceStub = append(deviceStub, 0x8B, 0x46, 0x0C, 0x85, 0xC0) // eax=input pointer; test eax,eax
	var ringCopyDoneJumps []int
	appendRingCopyDoneJump := func(condition byte) {
		deviceStub = append(deviceStub, 0x0F, condition, 0, 0, 0, 0)
		ringCopyDoneJumps = append(ringCopyDoneJumps, len(deviceStub)-4)
	}
	appendRingCopyDoneJump(0x84)
	deviceStub = append(deviceStub, 0x8B, 0x4E, 0x10, 0x85, 0xC9) // ecx=input length; test ecx,ecx
	appendRingCopyDoneJump(0x84)
	deviceStub = append(deviceStub, 0x83, 0xF9, 0x40, 0x76, 0) // cmp ecx,64; jbe ringLengthReady
	ringLengthReadyJump := len(deviceStub) - 1
	deviceStub = append(deviceStub, 0xB9, 0x40, 0, 0, 0) // mov ecx,64
	ringLengthReady := len(deviceStub)
	deviceStub[ringLengthReadyJump] = byte(ringLengthReady - (ringLengthReadyJump + 1))
	deviceStub = append(deviceStub,
		0x56, 0x57, // push esi; push edi
		0x8B, 0xF0, // mov esi,eax
		0x8D, 0x7A, 0x28, // lea edi,[edx+28h]
		0xF3, 0xA4, // rep movsb
		0x5F, 0x5E, // pop edi; pop esi
	)
	ringCopyDone := len(deviceStub)
	for _, displacementOffset := range ringCopyDoneJumps {
		binary.LittleEndian.PutUint32(
			deviceStub[displacementOffset:displacementOffset+4],
			uint32(int32(ringCopyDone-(displacementOffset+4))),
		)
	}
	deviceStub = append(deviceStub, 0xFF, 0x05, 0, 0, 0, 0) // inc [deviceCount]
	binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-4:], uint32(deviceCountAddress))
	deviceStub = append(deviceStub, 0xB8, 0x01, 0, 0, 0) // mov eax,TRUE
	deviceStub = append(deviceStub, 0xA3, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-4:], uint32(deviceTraceAddress))
	deviceStub = append(deviceStub, 0x33, 0xC0, 0xA3, 0, 0, 0, 0) // xor eax,eax; store ERROR_SUCCESS
	binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-4:], uint32(deviceTraceAddress+4))
	for argument := 1; argument <= 6; argument++ {
		deviceStub = append(deviceStub, 0x8B, 0x46, byte(argument*4), 0xA3, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-4:], uint32(deviceTraceAddress+8+uintptr((argument-1)*4)))
	}
	deviceStub = append(deviceStub, 0xC7, 0x05, 0, 0, 0, 0, 0, 0, 0, 0) // captured size = 0
	binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-8:len(deviceStub)-4], uint32(binarySizeAddress))
	deviceStub = append(deviceStub, 0x50, 0x51, 0x52, 0x57)       // push eax/ecx/edx/edi
	deviceStub = append(deviceStub, 0x8B, 0x46, 0x0C, 0x85, 0xC0) // input pointer
	var binaryDoneJumps []int
	appendBinaryDoneJump := func(condition byte) {
		deviceStub = append(deviceStub, 0x0F, condition, 0, 0, 0, 0)
		binaryDoneJumps = append(binaryDoneJumps, len(deviceStub)-4)
	}
	appendBinaryDoneJump(0x84)
	deviceStub = append(deviceStub, 0x8B, 0x4E, 0x10, 0x85, 0xC9) // input length
	appendBinaryDoneJump(0x84)
	deviceStub = append(deviceStub, 0x83, 0xF9, 0x40) // cmp ecx,64
	deviceStub = append(deviceStub, 0x76, 0)          // jbe lengthReady
	lengthReadyJump := len(deviceStub) - 1
	deviceStub = append(deviceStub, 0xB9, 0x40, 0, 0, 0)
	lengthReady := len(deviceStub)
	deviceStub[lengthReadyJump] = byte(lengthReady - (lengthReadyJump + 1))
	deviceStub = append(deviceStub, 0x89, 0x0D, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-4:], uint32(binarySizeAddress))
	deviceStub = append(deviceStub, 0xBF, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-4:], uint32(binaryDataAddress))
	binaryCopyLoop := len(deviceStub)
	deviceStub = append(deviceStub, 0x8A, 0x10, 0x88, 0x17, 0x40, 0x47, 0xE2, 0)
	deviceStub[len(deviceStub)-1] = byte(int8(binaryCopyLoop - len(deviceStub)))
	binaryDone := len(deviceStub)
	for _, displacementOffset := range binaryDoneJumps {
		binary.LittleEndian.PutUint32(
			deviceStub[displacementOffset:displacementOffset+4],
			uint32(int32(binaryDone-(displacementOffset+4))),
		)
	}
	deviceStub = append(deviceStub, 0x5F, 0x5A, 0x59, 0x58)       // restore copy registers
	deviceStub = append(deviceStub, 0x8B, 0x56, 0x1C, 0x85, 0xD2) // lpBytesReturned
	deviceStub = append(deviceStub, 0x74, 0)                      // jz bytesDone
	bytesDoneJump := len(deviceStub) - 1
	deviceStub = append(deviceStub, 0x8B, 0x46, 0x14, 0x85, 0xC0) // output pointer
	deviceStub = append(deviceStub, 0x74, 0)                      // jz noOutput
	noOutputJump := len(deviceStub) - 1
	deviceStub = append(deviceStub, 0x8B, 0x46, 0x18, 0xEB, 0x02) // eax=output length; jmp writeBytes
	noOutput := len(deviceStub)
	deviceStub = append(deviceStub, 0x33, 0xC0) // xor eax,eax
	deviceStub[noOutputJump] = byte(noOutput - (noOutputJump + 1))
	deviceStub = append(deviceStub, 0x89, 0x02) // mov [edx],eax
	bytesDone := len(deviceStub)
	deviceStub[bytesDoneJump] = byte(bytesDone - (bytesDoneJump + 1))
	deviceStub = append(deviceStub, 0x6A, 0x00, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0) // SetLastError(0)
	binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-6:len(deviceStub)-2], uint32(setLastErrorTarget))
	deviceStub = append(deviceStub, 0xB8, 0x01, 0, 0, 0, 0x5E, 0xC2, 0x20, 0x00) // TRUE; pop esi; ret 32
	deviceForward := len(deviceStub)
	deviceStub = append(deviceStub, 0x5E, 0xB8, 0, 0, 0, 0, 0xFF, 0xE0)
	binary.LittleEndian.PutUint32(deviceStub[len(deviceStub)-6:len(deviceStub)-2], uint32(deviceTarget))
	for _, displacementOffset := range deviceForwardJumps {
		binary.LittleEndian.PutUint32(
			deviceStub[displacementOffset:displacementOffset+4],
			uint32(int32(deviceForward-(displacementOffset+4))),
		)
	}

	findCloseStub := []byte{
		0x56, 0x8D, 0x74, 0x24, 0x04, // push esi; lea esi,[esp+4] (original ESP)
		0x8B, 0x06, 0xA3, 0, 0, 0, 0, // mov eax,[esi]; mov [caller],eax
		0x8B, 0x46, 0x04, // mov eax,[esi+4] (handle)
		0x85, 0xC0, // test eax,eax
	}
	binary.LittleEndian.PutUint32(findCloseStub[8:12], uint32(findCloseCallerAddress))
	var findCloseForwardJumps []int
	appendFindCloseForwardJump := func(condition byte) {
		findCloseStub = append(findCloseStub, 0x0F, condition, 0, 0, 0, 0)
		findCloseForwardJumps = append(findCloseForwardJumps, len(findCloseStub)-4)
	}
	appendFindCloseForwardJump(0x84)                        // jz forward
	findCloseStub = append(findCloseStub, 0x83, 0xF8, 0xFF) // cmp eax,INVALID_HANDLE_VALUE
	appendFindCloseForwardJump(0x84)
	findCloseStub = append(findCloseStub, 0x81, 0x3E, 0, 0, 0, 0) // cmp dword [esi],verified TP cleanup caller
	binary.LittleEndian.PutUint32(findCloseStub[len(findCloseStub)-4:], uint32(clientBase+0xAADD79))
	findCloseStub = append(findCloseStub, 0x0F, 0x84, 0, 0, 0, 0) // je closeMatched
	findCloseCallerMatchedJump := len(findCloseStub) - 4
	findCloseStub = append(findCloseStub, 0xB9, 0x20, 0, 0, 0) // mov ecx,32
	findCloseStub = append(findCloseStub, 0xBA, 0, 0, 0, 0)    // mov edx,handleRing
	binary.LittleEndian.PutUint32(findCloseStub[len(findCloseStub)-4:], uint32(handleRingAddress))
	findCloseSearch := len(findCloseStub)
	findCloseStub = append(findCloseStub, 0x3B, 0x02, 0x74, 0) // cmp eax,[edx]; je matchedHandle
	findCloseMatchedJump := len(findCloseStub) - 1
	findCloseStub = append(findCloseStub, 0x83, 0xC2, 0x04, 0xE2, 0) // add edx,4; loop search
	findCloseStub[len(findCloseStub)-1] = byte(int8(findCloseSearch - len(findCloseStub)))
	findCloseStub = append(findCloseStub, 0xE9, 0, 0, 0, 0) // no ring match: forward
	findCloseForwardJumps = append(findCloseForwardJumps, len(findCloseStub)-4)
	findCloseMatched := len(findCloseStub)
	findCloseStub[findCloseMatchedJump] = byte(findCloseMatched - (findCloseMatchedJump + 1))
	findCloseStub = append(findCloseStub, 0xC7, 0x02, 0, 0, 0, 0) // clear the active ring slot before CloseHandle
	findCloseCallerMatched := len(findCloseStub)
	binary.LittleEndian.PutUint32(
		findCloseStub[findCloseCallerMatchedJump:findCloseCallerMatchedJump+4],
		uint32(int32(findCloseCallerMatched-(findCloseCallerMatchedJump+4))),
	)
	findCloseStub = append(findCloseStub, 0x50) // push compatibility handle
	findCloseStub = append(findCloseStub, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0)
	binary.LittleEndian.PutUint32(findCloseStub[len(findCloseStub)-6:len(findCloseStub)-2], uint32(closeHandleTarget))
	findCloseStub = append(findCloseStub, 0xFF, 0x05, 0, 0, 0, 0) // inc [findCloseCount]
	binary.LittleEndian.PutUint32(findCloseStub[len(findCloseStub)-4:], uint32(findCloseCountAddress))
	findCloseStub = append(findCloseStub, 0x5E, 0xC2, 0x04, 0x00) // pop esi; ret 4
	findCloseForward := len(findCloseStub)
	findCloseStub = append(findCloseStub, 0x5E, 0xB8, 0, 0, 0, 0, 0xFF, 0xE0) // pop esi; jmp real FindClose
	binary.LittleEndian.PutUint32(findCloseStub[len(findCloseStub)-6:len(findCloseStub)-2], uint32(findCloseTarget))
	for _, displacementOffset := range findCloseForwardJumps {
		binary.LittleEndian.PutUint32(
			findCloseStub[displacementOffset:displacementOffset+4],
			uint32(int32(findCloseForward-(displacementOffset+4))),
		)
	}
	if len(createStub) > int(nulStringOffset-createStubOffset) {
		return nil, fmt.Errorf("TesSafe CreateFileA stub overlaps data: %d bytes", len(createStub))
	}
	if len(deviceStub) > int(deviceCountOffset-deviceStubOffset) {
		return nil, fmt.Errorf("TesSafe DeviceIoControl stub overlaps data: %d bytes", len(deviceStub))
	}
	if len(findCloseStub) > int(findCloseCountOffset-findCloseStubOffset) {
		return nil, fmt.Errorf("TesSafe FindClose stub overlaps data: %d bytes", len(findCloseStub))
	}
	if len(openSCManagerStub) > int(openSCManagerCountOffset-openSCManagerStubOffset) {
		return nil, fmt.Errorf("TesSafe OpenSCManagerA stub overlaps data: %d bytes", len(openSCManagerStub))
	}
	if err := writeRemote(process, createStubAddress, createStub); err != nil {
		return nil, fmt.Errorf("write TesSafe CreateFileA stub: %w", err)
	}
	if err := writeRemote(process, nulStringAddress, []byte{'N', 'U', 'L', 0}); err != nil {
		return nil, fmt.Errorf("write TesSafe NUL path: %w", err)
	}
	if err := writeRemote(process, deviceStubAddress, deviceStub); err != nil {
		return nil, fmt.Errorf("write TesSafe DeviceIoControl stub: %w", err)
	}
	if err := writeRemote(process, findCloseStubAddress, findCloseStub); err != nil {
		return nil, fmt.Errorf("write TesSafe FindClose stub: %w", err)
	}
	if err := writeRemote(process, openSCManagerStubAddress, openSCManagerStub); err != nil {
		return nil, fmt.Errorf("write TesSafe OpenSCManagerA isolation stub: %w", err)
	}
	createPointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(createPointer, uint32(createStubAddress))
	if !ensureRemoteBytes(process, createIAT, createPointer, pageReadWrite) {
		return nil, fmt.Errorf("patch ClientBase CreateFileA IAT for TesSafe")
	}
	devicePointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(devicePointer, uint32(deviceStubAddress))
	if !ensureRemoteBytes(process, deviceIAT, devicePointer, pageReadWrite) {
		return nil, fmt.Errorf("patch ClientBase DeviceIoControl IAT for TesSafe")
	}
	findClosePointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(findClosePointer, uint32(findCloseStubAddress))
	if !ensureRemoteBytes(process, findCloseIAT, findClosePointer, pageReadWrite) {
		return nil, fmt.Errorf("patch ClientBase FindClose IAT for TesSafe")
	}
	openSCManagerPointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(openSCManagerPointer, uint32(openSCManagerStubAddress))
	if !ensureRemoteBytes(process, openSCManagerIAT, openSCManagerPointer, pageReadWrite) {
		return nil, fmt.Errorf("patch ClientBase OpenSCManagerA IAT for TesSafe isolation")
	}
	procFlushInstruction.Call(uintptr(process), createStubAddress, uintptr(len(createStub)))
	procFlushInstruction.Call(uintptr(process), deviceStubAddress, uintptr(len(deviceStub)))
	procFlushInstruction.Call(uintptr(process), findCloseStubAddress, uintptr(len(findCloseStub)))
	procFlushInstruction.Call(uintptr(process), openSCManagerStubAddress, uintptr(len(openSCManagerStub)))
	return []ImportStub{
		{
			Module: "ClientBase.dll", Library: "ADVAPI32.dll", Symbol: "OpenSCManagerA(TesSafe isolation)",
			IATAddress: fmt.Sprintf("0x%X", openSCManagerIAT), StubAddress: fmt.Sprintf("0x%X", openSCManagerStubAddress),
			Behavior:      "deny ClientBase service-manager access with ERROR_ACCESS_DENIED so the obsolete TesSafe kernel driver cannot be installed or started",
			TargetAddress: fmt.Sprintf("0x%X", openSCManagerTarget), iatValue: openSCManagerIAT, stubValue: openSCManagerStubAddress,
			counterValue: openSCManagerCountAddress, callerValue: openSCManagerCallerAddress,
		},
		{
			Module: "ClientBase.dll", Library: "KERNEL32.dll", Symbol: "CreateFileA(\\\\.\\TesSafe)",
			IATAddress: fmt.Sprintf("0x%X", createIAT), StubAddress: fmt.Sprintf("0x%X", createStubAddress),
			Behavior:      "exact TesSafe path only: open NUL as a real closeable compatibility handle; forward all other paths",
			TargetAddress: fmt.Sprintf("0x%X", createTarget), iatValue: createIAT, stubValue: createStubAddress,
			counterValue: openCountAddress, callerValue: createCallerAddress,
		},
		{
			Module: "ClientBase.dll", Library: "KERNEL32.dll", Symbol: "DeviceIoControl(TesSafe)",
			IATAddress: fmt.Sprintf("0x%X", deviceIAT), StubAddress: fmt.Sprintf("0x%X", deviceStubAddress),
			Behavior:      "compatibility handle only: preserve buffers, report output length and success, and retain the last 32 request records; forward every other handle",
			TargetAddress: fmt.Sprintf("0x%X", deviceTarget), iatValue: deviceIAT, stubValue: deviceStubAddress,
			counterValue: deviceCountAddress, traceValue: deviceTraceAddress,
			binaryValue: binaryDataAddress, binarySize: binarySizeAddress,
			callerValue:   deviceCallerAddress,
			rawStackValue: rawStackAddress, rawStackWords: 20,
			deviceRingValue: deviceRingAddress, deviceRingEntries: deviceRingEntries,
			deviceRingStride: deviceRingStride,
		},
		{
			Module: "ClientBase.dll", Library: "KERNEL32.dll", Symbol: "FindClose(TesSafe)",
			IATAddress: fmt.Sprintf("0x%X", findCloseIAT), StubAddress: fmt.Sprintf("0x%X", findCloseStubAddress),
			Behavior:      "compatibility handles or verified TP cleanup caller ClientBase+0xAADD79 only: translate legacy FindClose into CloseHandle; forward every other find handle",
			TargetAddress: fmt.Sprintf("0x%X", findCloseTarget), iatValue: findCloseIAT, stubValue: findCloseStubAddress,
			counterValue: findCloseCountAddress, callerValue: findCloseCallerAddress,
		},
	}, nil
}

func installSystemAPIReturnTrace(
	process syscall.Handle,
	clientBase uintptr,
	library string,
	symbol string,
	argumentCount int,
	getLastErrorTarget uintptr,
	timeout time.Duration,
) (ImportStub, error) {
	iatAddress, err := findImportAddress(process, clientBase, library, symbol)
	if err != nil {
		return ImportStub{}, err
	}
	targetAddress, err := waitForExecutablePointer(process, iatAddress, timeout)
	if err != nil {
		return ImportStub{}, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx %s trace: 0x%X (%v)", symbol, page, allocErr)
	}
	counterAddress := page + 0x100
	traceAddress := page + 0x200
	callerAddress := page + 0x4C0
	stub := []byte{0x56, 0x8D, 0x74, 0x24, 0x04}      // push esi; lea esi,[esp+4] (original ESP)
	stub = append(stub, 0x8B, 0x06, 0xA3, 0, 0, 0, 0) // capture the API return address
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(callerAddress))
	textAddress := uintptr(0)
	textUTF16 := false
	textArgument := 0
	if symbol == "CreateFileA" || symbol == "CreateFileW" {
		textArgument = 1
		textUTF16 = symbol == "CreateFileW"
	} else if symbol == "LookupPrivilegeValueA" || symbol == "GetProcAddress" {
		textArgument = 2
	} else if symbol == "CreateMutexA" || symbol == "CreateMutexW" || symbol == "OpenMutexA" ||
		symbol == "OpenEventA" || symbol == "OpenFileMappingA" || symbol == "FindWindowExA" {
		textArgument = 3
		textUTF16 = symbol == "CreateMutexW"
	} else if symbol == "CreateEventA" {
		textArgument = 4
	} else if symbol == "CreateFileMappingA" {
		textArgument = 6
	}
	if textArgument != 0 {
		textAddress = page + 0x300
		stub = append(stub,
			0x50,                             // push eax
			0x51,                             // push ecx
			0x52,                             // push edx
			0x57,                             // push edi
			0x8B, 0x46, byte(textArgument*4), // mov eax,[esi+text argument]
			0x85, 0xC0, // test eax,eax
			0x74, 0, // jz copyDone
			0x3D, 0x00, 0x00, 0x01, 0x00, // cmp eax,10000h (integer atom, not a string pointer)
			0x72, 0, // jb copyDone
			0xBF, 0, 0, 0, 0, // mov edi,textAddress
			0xB9, 0, 0, 0, 0, // mov ecx,max characters
		)
		nullJumpOffset := len(stub) - 18
		atomJumpOffset := len(stub) - 11
		binary.LittleEndian.PutUint32(stub[len(stub)-9:len(stub)-5], uint32(textAddress))
		characterLimit := uint32(255)
		if textUTF16 {
			characterLimit = 127
		}
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], characterLimit)
		copyLoop := len(stub)
		if textUTF16 {
			stub = append(stub,
				0x66, 0x8B, 0x10, // mov dx,[eax]
				0x66, 0x89, 0x17, // mov [edi],dx
				0x83, 0xC0, 0x02, // add eax,2
				0x83, 0xC7, 0x02, // add edi,2
				0x66, 0x85, 0xD2, // test dx,dx
			)
		} else {
			stub = append(stub,
				0x8A, 0x10, // mov dl,[eax]
				0x88, 0x17, // mov [edi],dl
				0x40,       // inc eax
				0x47,       // inc edi
				0x84, 0xD2, // test dl,dl
			)
		}
		stub = append(stub, 0x74, 0) // jz copyDone
		terminatedJumpOffset := len(stub) - 1
		stub = append(stub, 0xE2, 0) // loop copyLoop
		loopDisplacement := copyLoop - len(stub)
		if loopDisplacement < -128 {
			return ImportStub{}, fmt.Errorf("%s string-copy loop displacement out of range", symbol)
		}
		stub[len(stub)-1] = byte(int8(loopDisplacement))
		copyDone := len(stub)
		stub[nullJumpOffset] = byte(copyDone - (nullJumpOffset + 1))
		stub[atomJumpOffset] = byte(copyDone - (atomJumpOffset + 1))
		stub[terminatedJumpOffset] = byte(copyDone - (terminatedJumpOffset + 1))
		stub = append(stub, 0x5F, 0x5A, 0x59, 0x58) // pop edi; pop edx; pop ecx; pop eax
	}
	binaryAddress := uintptr(0)
	binarySizeAddress := uintptr(0)
	if symbol == "DeviceIoControl" {
		binaryAddress = page + 0x400
		binarySizeAddress = page + 0x480
		stub = append(stub,
			0x50,             // push eax
			0x51,             // push ecx
			0x52,             // push edx
			0x57,             // push edi
			0x8B, 0x46, 0x0C, // mov eax,[esi+12] (input buffer)
			0x85, 0xC0, // test eax,eax
			0x74, 0, // jz binaryCopyDone
			0x8B, 0x4E, 0x10, // mov ecx,[esi+16] (input length)
			0x85, 0xC9, // test ecx,ecx
			0x74, 0, // jz binaryCopyDone
			0x83, 0xF9, 0x40, // cmp ecx,64
			0x76, 0, // jbe binaryLengthReady
			0xB9, 0x40, 0, 0, 0, // mov ecx,64
			0x89, 0x0D, 0, 0, 0, 0, // mov [binarySizeAddress],ecx
			0xBF, 0, 0, 0, 0, // mov edi,binaryAddress
		)
		nullBufferJump := len(stub) - 29
		zeroLengthJump := len(stub) - 22
		lengthReadyJump := len(stub) - 17
		lengthReady := len(stub) - 11
		stub[lengthReadyJump] = byte(lengthReady - (lengthReadyJump + 1))
		binary.LittleEndian.PutUint32(stub[len(stub)-9:len(stub)-5], uint32(binarySizeAddress))
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(binaryAddress))
		copyLoop := len(stub)
		stub = append(stub,
			0x8A, 0x10, // mov dl,[eax]
			0x88, 0x17, // mov [edi],dl
			0x40,    // inc eax
			0x47,    // inc edi
			0xE2, 0, // loop copyLoop
		)
		loopDisplacement := copyLoop - len(stub)
		if loopDisplacement < -128 {
			return ImportStub{}, fmt.Errorf("%s binary-copy loop displacement out of range", symbol)
		}
		stub[len(stub)-1] = byte(int8(loopDisplacement))
		binaryCopyDone := len(stub)
		stub[nullBufferJump] = byte(binaryCopyDone - (nullBufferJump + 1))
		stub[zeroLengthJump] = byte(binaryCopyDone - (zeroLengthJump + 1))
		stub = append(stub, 0x5F, 0x5A, 0x59, 0x58) // pop edi; pop edx; pop ecx; pop eax
	}
	for argument := argumentCount; argument >= 1; argument-- {
		stub = append(stub, 0xFF, 0x76, byte(argument*4)) // push [esi+argument*4]
	}
	appendEAXImmediate := func(value uintptr) {
		stub = append(stub, 0xB8, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(value))
	}
	appendStoreEAX := func(address uintptr) {
		stub = append(stub, 0xA3, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(address))
	}
	appendEAXImmediate(targetAddress)
	stub = append(stub, 0xFF, 0xD0) // call original API
	appendStoreEAX(traceAddress)
	stub = append(stub, 0x50) // preserve API return value
	appendEAXImmediate(getLastErrorTarget)
	stub = append(stub, 0xFF, 0xD0)
	appendStoreEAX(traceAddress + 4)
	stub = append(stub, 0x58, 0x52) // restore API result; preserve edx while copying arguments
	loggedArguments := argumentCount
	if loggedArguments > 6 {
		loggedArguments = 6
	}
	for argument := 1; argument <= loggedArguments; argument++ {
		stub = append(stub, 0x8B, 0x56, byte(argument*4)) // mov edx,[esi+argument*4]
		stub = append(stub, 0x89, 0x15, 0, 0, 0, 0)       // mov [trace+...],edx
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+8+uintptr((argument-1)*4)))
	}
	stub = append(stub, 0x5A, 0xFF, 0x05, 0, 0, 0, 0, 0x5E) // pop edx; inc [counter]; pop esi
	binary.LittleEndian.PutUint32(stub[len(stub)-5:len(stub)-1], uint32(counterAddress))
	stackBytes := argumentCount * 4
	stub = append(stub, 0xC2, byte(stackBytes), byte(stackBytes>>8))
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write %s trace: %w", symbol, err)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(page))
	if !ensureRemoteBytes(process, iatAddress, pointer, pageReadWrite) {
		return ImportStub{}, fmt.Errorf("patch ClientBase %s IAT", symbol)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	behavior := "transparent last-call trace: return, GetLastError, and first six arguments"
	if textAddress != 0 {
		behavior += "; copy selected string argument at call time"
	}
	if binaryAddress != 0 {
		behavior += "; copy up to 64 input-buffer bytes before the call"
	}
	return ImportStub{
		Module: "ClientBase.dll", Library: library, Symbol: symbol,
		IATAddress: fmt.Sprintf("0x%X", iatAddress), StubAddress: fmt.Sprintf("0x%X", page),
		Behavior:      behavior,
		TargetAddress: fmt.Sprintf("0x%X", targetAddress),
		iatValue:      iatAddress, stubValue: page, counterValue: counterAddress, traceValue: traceAddress,
		textValue: textAddress, textUTF16: textUTF16,
		binaryValue: binaryAddress, binarySize: binarySizeAddress,
		callerValue: callerAddress,
	}, nil
}

func installClientBaseCreateThreadTrace(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, err
	}
	createThreadIAT, err := findImportAddress(process, clientBase, "KERNEL32.dll", "CreateThread")
	if err != nil {
		return ImportStub{}, err
	}
	createThreadTarget, err := waitForExecutablePointer(process, createThreadIAT, timeout)
	if err != nil {
		return ImportStub{}, err
	}
	getThreadIDTarget, err := findRemoteExport(process, pid, "KERNEL32.dll", "GetThreadId", timeout, 0)
	if err != nil {
		return ImportStub{}, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx CreateThread ring trace: 0x%X (%v)", page, allocErr)
	}
	const (
		counterOffset = uintptr(0x200)
		ringOffset    = uintptr(0x300)
		ringEntries   = 16
		ringStride    = 40
	)
	counterAddress := page + counterOffset
	ringAddress := page + ringOffset
	stub := []byte{
		0x56,                   // push esi
		0x57,                   // push edi
		0x53,                   // push ebx
		0x8D, 0x74, 0x24, 0x0C, // lea esi,[esp+12] (original ESP)
		0x8B, 0x0D, 0, 0, 0, 0, // mov ecx,[counter]
		0x8B, 0xD1, // mov edx,ecx
		0x83, 0xE2, 0x0F, // and edx,15
		0x6B, 0xD2, byte(ringStride), // imul edx,edx,40
		0xBF, 0, 0, 0, 0, // mov edi,ring
		0x03, 0xFA, // add edi,edx
		0x89, 0x0F, // mov [edi],ecx
		0x8B, 0x06, // mov eax,[esi]
		0x89, 0x47, 0x04, // mov [edi+4],eax
	}
	binary.LittleEndian.PutUint32(stub[9:13], uint32(counterAddress))
	binary.LittleEndian.PutUint32(stub[22:26], uint32(ringAddress))
	for argument := 1; argument <= 6; argument++ {
		stub = append(stub, 0x8B, 0x46, byte(argument*4))
		stub = append(stub, 0x89, 0x47, byte(4+argument*4))
	}
	for argument := 6; argument >= 1; argument-- {
		stub = append(stub, 0xFF, 0x76, byte(argument*4))
	}
	stub = append(stub, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0)
	binary.LittleEndian.PutUint32(stub[len(stub)-6:len(stub)-2], uint32(createThreadTarget))
	stub = append(stub,
		0x89, 0x47, 0x20, // mov [edi+32],eax
		0x50,       // push eax (preserve CreateThread result)
		0x85, 0xC0, // test eax,eax
		0x74, 0x0C, // jz noThreadID
		0x50,             // push eax (thread handle)
		0xB8, 0, 0, 0, 0, // mov eax,GetThreadId
		0xFF, 0xD0, // call eax
		0x8B, 0xD0, // mov edx,eax
		0xEB, 0x02, // jmp storeThreadID
		0x33, 0xD2, // noThreadID: xor edx,edx
		0x89, 0x57, 0x24, // storeThreadID: mov [edi+36],edx
		0xF0, 0xFF, 0x05, 0, 0, 0, 0, // lock inc dword ptr [counter]
		0x58,             // pop eax
		0x5B,             // pop ebx
		0x5F,             // pop edi
		0x5E,             // pop esi
		0xC2, 0x18, 0x00, // ret 24
	)
	binary.LittleEndian.PutUint32(stub[len(stub)-29:len(stub)-25], uint32(getThreadIDTarget))
	binary.LittleEndian.PutUint32(stub[len(stub)-11:len(stub)-7], uint32(counterAddress))
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write CreateThread ring trace: %w", err)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(page))
	if !ensureRemoteBytes(process, createThreadIAT, pointer, pageReadWrite) {
		return ImportStub{}, fmt.Errorf("patch ClientBase CreateThread IAT")
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	return ImportStub{
		Module: "ClientBase.dll", Library: "KERNEL32.dll", Symbol: "CreateThread",
		IATAddress: fmt.Sprintf("0x%X", createThreadIAT), StubAddress: fmt.Sprintf("0x%X", page),
		Behavior:        "transparent 16-call ring trace: caller, all arguments, returned handle, and created thread ID",
		TargetAddress:   fmt.Sprintf("0x%X", createThreadTarget),
		iatValue:        createThreadIAT,
		stubValue:       page,
		counterValue:    counterAddress,
		threadRingValue: ringAddress, threadRingEntries: ringEntries, threadRingStride: ringStride,
	}, nil
}

func installGetModuleHandleATrace(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, err
	}
	iatAddress, err := findImportAddress(process, clientBase, "KERNEL32.dll", "GetModuleHandleA")
	if err != nil {
		return ImportStub{}, err
	}
	targetAddress, err := waitForExecutablePointer(process, iatAddress, timeout)
	if err != nil {
		return ImportStub{}, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx GetModuleHandleA trace: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x100
	traceAddress := page + 0x200
	stub := []byte{
		0x51,                   // push ecx
		0x52,                   // push edx
		0x8B, 0x0D, 0, 0, 0, 0, // mov ecx,[counter]
		0x8B, 0xD1, // mov edx,ecx
		0x83, 0xE2, 0x0F, // and edx,15
		0xC1, 0xE2, 0x03, // shl edx,3
		0x8B, 0x44, 0x24, 0x0C, // mov eax,[esp+12] (name)
		0x89, 0x04, 0x15, 0, 0, 0, 0, // mov [trace+edx],eax; edx is slot*8
		0x5A,                   // pop edx
		0x59,                   // pop ecx
		0xFF, 0x74, 0x24, 0x04, // push [esp+4]
		0xB8, 0, 0, 0, 0, // mov eax,original GetModuleHandleA
		0xFF, 0xD0, // call eax
		0x51,                   // push ecx
		0x52,                   // push edx
		0x8B, 0x0D, 0, 0, 0, 0, // mov ecx,[counter]
		0x8B, 0xD1, // mov edx,ecx
		0x83, 0xE2, 0x0F, // and edx,15
		0xC1, 0xE2, 0x03, // shl edx,3
		0x89, 0x04, 0x15, 0, 0, 0, 0, // mov [trace+4+edx],eax
		0x41,                   // inc ecx
		0x89, 0x0D, 0, 0, 0, 0, // mov [counter],ecx
		0x5A,             // pop edx
		0x59,             // pop ecx
		0xC2, 0x04, 0x00, // ret 4
	}
	binary.LittleEndian.PutUint32(stub[4:8], uint32(counterAddress))
	binary.LittleEndian.PutUint32(stub[23:27], uint32(traceAddress))
	binary.LittleEndian.PutUint32(stub[34:38], uint32(targetAddress))
	binary.LittleEndian.PutUint32(stub[44:48], uint32(counterAddress))
	binary.LittleEndian.PutUint32(stub[59:63], uint32(traceAddress+4))
	binary.LittleEndian.PutUint32(stub[66:70], uint32(counterAddress))
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write GetModuleHandleA trace: %w", err)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(page))
	if !ensureRemoteBytes(process, iatAddress, pointer, pageReadWrite) {
		return ImportStub{}, fmt.Errorf("patch ClientBase GetModuleHandleA IAT")
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	return ImportStub{
		Module: "ClientBase.dll", Library: "KERNEL32.dll", Symbol: "GetModuleHandleA",
		IATAddress: fmt.Sprintf("0x%X", iatAddress), StubAddress: fmt.Sprintf("0x%X", page),
		Behavior:      "transparent 16-entry ring trace of module-name pointers and returned handles",
		TargetAddress: fmt.Sprintf("0x%X", targetAddress),
		iatValue:      iatAddress, stubValue: page, counterValue: counterAddress, traceValue: traceAddress,
	}, nil
}

func installTPPostDialogVMTrace(
	process syscall.Handle,
	pid uint32,
	dialogHitAddress uintptr,
	timeout time.Duration,
) (ImportStub, uintptr, uintptr, error) {
	if dialogHitAddress == 0 {
		return ImportStub{}, 0, 0, fmt.Errorf("post-dialog VM trace has no dialog-hit gate")
	}
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, 0, 0, err
	}
	sleepAddress, err := findRemoteExport(process, pid, "KERNEL32.dll", "Sleep", timeout, 0)
	if err != nil {
		return ImportStub{}, 0, 0, err
	}
	const targetRVA = uintptr(0xA0FF1D)
	target := clientBase + targetRVA
	signature := []byte{
		0x8B, 0x16,
		0x81, 0xC2, 0xB9, 0x18, 0x01, 0x4A,
		0x81, 0xC6, 0x04, 0x00, 0x00, 0x00,
		0x03, 0x17,
		0xFF, 0xE2,
	}
	deadline := time.Now().Add(timeout)
	for {
		current, ok := readRemote(process, target, len(signature))
		if ok && bytes.Equal(current, signature) {
			break
		}
		if time.Now().After(deadline) {
			return ImportStub{}, 0, 0, fmt.Errorf("post-dialog TP dispatcher signature not available at 0x%X", target)
		}
		time.Sleep(200 * time.Microsecond)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x5000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("VirtualAllocEx post-dialog TP trace: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x800
	traceAddress := page + 0x1000
	stub := append([]byte{}, signature[:16]...)
	stub = append(stub, 0x9C, 0x60)                   // pushfd; pushad after resolving destination
	stub = append(stub, 0x83, 0x3D, 0, 0, 0, 0, 0x00) // cmp dword ptr [dialog hits],0
	binary.LittleEndian.PutUint32(stub[len(stub)-5:], uint32(dialogHitAddress))
	stub = append(stub, 0x0F, 0x84, 0, 0, 0, 0) // je traceDone
	traceDoneJump := len(stub) - 4
	stub = append(stub, 0x8B, 0x0D, 0, 0, 0, 0) // mov ecx,[counter]
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x8B, 0xD1, 0x83, 0xE2, 0x7F)             // edx=counter&127
	stub = append(stub, 0x8B, 0xEA, 0xC1, 0xE2, 0x04, 0x03, 0xD5) // edx=slot*17
	appendStored := func(load []byte, recordOffset uintptr) {
		stub = append(stub, load...)
		stub = append(stub, 0x89, 0x04, 0x95, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+recordOffset))
	}
	appendStored([]byte{0x8B, 0x44, 0x24, 0x14}, 0)  // decoded destination
	appendStored([]byte{0x8B, 0x44, 0x24, 0x10}, 4)  // EBX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x18}, 8)  // ECX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x14}, 12) // EDX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x04}, 16) // ESI
	appendStored([]byte{0x8B, 0x04, 0x24}, 20)       // EDI
	appendStored([]byte{0x8B, 0x44, 0x24, 0x08}, 24) // EBP
	stub = append(stub, 0x8B, 0x44, 0x24, 0x0C, 0x83, 0xC0, 0x04)
	appendStored(nil, 28)                            // entry ESP
	appendStored([]byte{0x8B, 0x44, 0x24, 0x20}, 32) // EFLAGS
	stub = append(stub, 0x8B, 0x6C, 0x24, 0x0C, 0x83, 0xC5, 0x04)
	for index := 0; index < 7; index++ {
		appendStored([]byte{0x8B, 0x45, byte(index * 4)}, uintptr(36+index*4))
	}
	appendStored([]byte{0x64, 0xA1, 0x24, 0x00, 0x00, 0x00}, 64) // thread ID
	stub = append(stub, 0x41, 0x89, 0x0D, 0, 0, 0, 0)            // inc ecx; mov [counter],ecx
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x6A, 0x01, 0xB8) // one-millisecond diagnostic sampling barrier
	stub = binary.LittleEndian.AppendUint32(stub, uint32(sleepAddress))
	stub = append(stub, 0xFF, 0xD0)
	traceDone := len(stub)
	binary.LittleEndian.PutUint32(stub[traceDoneJump:traceDoneJump+4], uint32(int32(traceDone-(traceDoneJump+4))))
	stub = append(stub, 0x61, 0x9D, 0xFF, 0xE2) // popad; popfd; jmp decoded destination
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, 0, 0, fmt.Errorf("write post-dialog TP trace: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0, 0x90}
	displacement := int64(page) - int64(target+5)
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("post-dialog TP trace displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(displacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, 0, 0, fmt.Errorf("patch post-dialog TP dispatcher at 0x%X", target)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(targetPatch)))
	return ImportStub{
		Module: "ClientBase.dll", Library: "ClientBase.dll", Symbol: "TPPostDialogVMDispatcher+0xA0FF1D",
		StubAddress:   fmt.Sprintf("0x%X", page),
		Behavior:      "all-thread ring trace enabled only after the exact TP 1008 dialog frame returns IDOK; preserve context and add a 1 ms diagnostic sampling barrier per transition",
		TargetAddress: fmt.Sprintf("0x%X", target), TargetOriginal: fmt.Sprintf("%X", signature[:6]),
		targetValue: target, targetBytes: targetPatch, originalBytes: signature[:6], counterValue: counterAddress,
	}, counterAddress, traceAddress, nil
}

func installTPPreErrorTrace(
	process syscall.Handle,
	pid uint32,
	mainThreadID uint32,
	threadIDAddress uintptr,
	timeout time.Duration,
) (ImportStub, uintptr, uintptr, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, 0, 0, err
	}
	const targetRVA = uintptr(0xA0FF1D)
	target := clientBase + targetRVA
	signature := []byte{
		0x8B, 0x16, // mov edx,[esi]
		0x81, 0xC2, 0xB9, 0x18, 0x01, 0x4A, // add edx,4A0118B9h
		0x81, 0xC6, 0x04, 0x00, 0x00, 0x00, // add esi,4
		0x03, 0x17, // add edx,[edi]
		0xFF, 0xE2, // jmp edx
	}
	deadline := time.Now().Add(timeout)
	for {
		current, ok := readRemote(process, target, len(signature))
		if ok && bytes.Equal(current, signature) {
			break
		}
		if time.Now().After(deadline) {
			return ImportStub{}, 0, 0, fmt.Errorf("TP dispatcher signature not available at 0x%X", target)
		}
		time.Sleep(200 * time.Microsecond)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x5000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("VirtualAllocEx pre-error TP trace: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x800
	traceAddress := page + 0x1000
	stub := append([]byte{}, signature[:16]...)
	stub = append(stub, 0x9C, 0x60)                         // pushfd; pushad after resolving the native destination
	stub = append(stub, 0x64, 0xA1, 0x24, 0x00, 0x00, 0x00) // mov eax,fs:[TEB.ClientId.UniqueThread]
	if threadIDAddress != 0 {
		stub = append(stub, 0x3B, 0x05, 0, 0, 0, 0) // cmp eax,[captured worker thread ID]
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(threadIDAddress))
	} else {
		stub = append(stub, 0x3D, 0, 0, 0, 0) // cmp eax,mainThreadID
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], mainThreadID)
	}
	stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0) // jne traceDone
	traceDoneJump := len(stub) - 4
	stub = append(stub, 0x8B, 0x0D, 0, 0, 0, 0) // mov ecx,[counter]
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x8B, 0xD1, 0x83, 0xE2, 0x7F)             // edx=counter&127
	stub = append(stub, 0x8B, 0xEA, 0xC1, 0xE2, 0x04, 0x03, 0xD5) // edx=slot*17
	appendStored := func(load []byte, recordOffset uintptr) {
		stub = append(stub, load...)
		stub = append(stub, 0x89, 0x04, 0x95, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+recordOffset))
	}
	appendStored([]byte{0x8B, 0x44, 0x24, 0x14}, 0)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x10}, 4)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x18}, 8)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x14}, 12)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x04}, 16)
	appendStored([]byte{0x8B, 0x04, 0x24}, 20)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x08}, 24)
	stub = append(stub, 0x8B, 0x44, 0x24, 0x0C, 0x83, 0xC0, 0x04)
	appendStored(nil, 28)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x20}, 32)
	stub = append(stub, 0x8B, 0x6C, 0x24, 0x0C, 0x83, 0xC5, 0x04)
	for index := 0; index < 8; index++ {
		appendStored([]byte{0x8B, 0x45, byte(index * 4)}, uintptr(36+index*4))
	}
	stub = append(stub, 0x41, 0x89, 0x0D, 0, 0, 0, 0) // inc ecx; mov [counter],ecx
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	traceDone := len(stub)
	binary.LittleEndian.PutUint32(stub[traceDoneJump:traceDoneJump+4], uint32(int32(traceDone-(traceDoneJump+4))))
	stub = append(stub, 0x61, 0x9D, 0xFF, 0xE2) // popad; popfd; jmp decoded destination
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, 0, 0, fmt.Errorf("write pre-error TP trace: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0, 0x90}
	displacement := int64(page) - int64(target+5)
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("pre-error TP trace displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(displacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, 0, 0, fmt.Errorf("patch TP dispatcher at 0x%X", target)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(targetPatch)))
	behavior := "transparent main-thread ring trace of the last 128 dispatcher transitions before the first TP warning"
	symbol := "TPPreErrorDispatcher+0xA0FF1D"
	if threadIDAddress != 0 {
		behavior = "transparent ring trace of the last 128 dispatcher transitions for the exact ClientBase+0x70708B TP worker thread"
		symbol = "TPWorkerVMDispatcher+0xA0FF1D"
	}
	return ImportStub{
		Module: "ClientBase.dll", Library: "ClientBase.dll", Symbol: symbol,
		StubAddress:   fmt.Sprintf("0x%X", page),
		Behavior:      behavior,
		TargetAddress: fmt.Sprintf("0x%X", target), TargetOriginal: fmt.Sprintf("%X", signature[:6]),
		targetValue: target, targetBytes: targetPatch, originalBytes: signature[:6], counterValue: counterAddress,
	}, counterAddress, traceAddress, nil
}

func installTesSafeVMTrace(
	process syscall.Handle,
	pid uint32,
	timeout time.Duration,
) (ImportStub, uintptr, uintptr, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, 0, 0, err
	}
	const targetRVA = uintptr(0xAB3447)
	target := clientBase + targetRVA
	signature := []byte{
		0x8D, 0x64, 0x24, 0x04, // lea esp,[esp+4]
		0x03, 0x17, // add edx,[edi]
		0x81, 0xC2, 0x68, 0xDB, 0xFF, 0x71, // add edx,71FFDB68h
		0x81, 0xC6, 0x04, 0x00, 0x00, 0x00, // add esi,4
		0x8D, 0xA4, 0x24, 0xFC, 0xFF, 0xFF, 0xFF, // lea esp,[esp-4]
		0x89, 0x14, 0x24, // mov [esp],edx
		0x51,                                     // push ecx
		0x8B, 0x8C, 0x24, 0x04, 0x00, 0x00, 0x00, // mov ecx,[esp+4]
		0x51,                                     // push ecx
		0x8B, 0x8C, 0x24, 0x04, 0x00, 0x00, 0x00, // mov ecx,[esp+4]
		0xC2, 0x08, 0x00, // ret 8
	}
	deadline := time.Now().Add(timeout)
	for {
		current, ok := readRemote(process, target, len(signature))
		if ok && bytes.Equal(current, signature) {
			break
		}
		if time.Now().After(deadline) {
			return ImportStub{}, 0, 0, fmt.Errorf("TesSafe VM bridge signature not available at 0x%X", target)
		}
		time.Sleep(200 * time.Microsecond)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x5000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("VirtualAllocEx TesSafe VM trace: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x800
	traceAddress := page + 0x1000

	// Resolve the native destination exactly as the original bridge does. The
	// trace is inserted between the arithmetic and the stack/ret bridge so the
	// saved flags and every general register can be restored before continuing.
	stub := append([]byte{}, signature[:18]...)
	stub = append(stub, 0x9C, 0x60)             // pushfd; pushad
	stub = append(stub, 0x8B, 0x0D, 0, 0, 0, 0) // mov ecx,[counter]
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x8B, 0xD1, 0x83, 0xE2, 0x7F)             // edx=counter&127
	stub = append(stub, 0x8B, 0xEA, 0xC1, 0xE2, 0x04, 0x03, 0xD5) // edx=slot*17
	appendStored := func(load []byte, recordOffset uintptr) {
		stub = append(stub, load...)
		stub = append(stub, 0x89, 0x04, 0x95, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+recordOffset))
	}
	appendStored([]byte{0x8B, 0x44, 0x24, 0x14}, 0)  // resolved EDX destination
	appendStored([]byte{0x8B, 0x44, 0x24, 0x10}, 4)  // EBX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x18}, 8)  // ECX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x14}, 12) // EDX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x04}, 16) // ESI
	appendStored([]byte{0x8B, 0x04, 0x24}, 20)       // EDI
	appendStored([]byte{0x8B, 0x44, 0x24, 0x08}, 24) // EBP
	stub = append(stub, 0x8B, 0x44, 0x24, 0x0C, 0x83, 0xC0, 0x04)
	appendStored(nil, 28)                                        // ESP before the bridge discarded its incoming return address
	appendStored([]byte{0x8B, 0x44, 0x24, 0x20}, 32)             // EFLAGS
	appendStored([]byte{0x64, 0xA1, 0x24, 0x00, 0x00, 0x00}, 36) // thread ID
	stub = append(stub, 0x8B, 0x6C, 0x24, 0x0C, 0x83, 0xC5, 0x04)
	for index := 0; index < 7; index++ {
		appendStored([]byte{0x8B, 0x45, byte(index * 4)}, uintptr(40+index*4))
	}
	stub = append(stub, 0x41, 0x89, 0x0D, 0, 0, 0, 0) // inc ecx; mov [counter],ecx
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x61, 0x9D)        // popad; popfd
	stub = append(stub, signature[18:]...) // original stack/ret bridge
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, 0, 0, fmt.Errorf("write TesSafe VM trace: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0, 0x90}
	displacement := int64(page) - int64(target+5)
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("TesSafe VM trace displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(displacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, 0, 0, fmt.Errorf("patch TesSafe VM bridge at 0x%X", target)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(targetPatch)))
	return ImportStub{
		Module: "ClientBase.dll", Library: "ClientBase.dll", Symbol: "TesSafeVMBridge+0xAB3447",
		StubAddress:   fmt.Sprintf("0x%X", page),
		Behavior:      "transparent all-thread ring trace of the last 128 TesSafe VM call-bridge transitions with thread IDs",
		TargetAddress: fmt.Sprintf("0x%X", target), TargetOriginal: fmt.Sprintf("%X", signature[:6]),
		targetValue: target, targetBytes: targetPatch, originalBytes: signature[:6], counterValue: counterAddress,
	}, counterAddress, traceAddress, nil
}

func installTesSafeIOReturnTrace(
	process syscall.Handle,
	pid uint32,
	timeout time.Duration,
) (ImportStub, uintptr, uintptr, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, 0, 0, err
	}
	const targetRVA = uintptr(0xAB34A6)
	target := clientBase + targetRVA
	signature := []byte{
		0x85, 0xC0, // test eax,eax
		0x0F, 0x85, 0x68, 0xA9, 0x5F, 0xFF, // jnz ClientBase+0xADE16
		0xE9, 0x24, 0x24, 0x00, 0x00, // jmp ClientBase+0xAB58D7
	}
	deadline := time.Now().Add(timeout)
	for {
		current, ok := readRemote(process, target, len(signature))
		if ok && bytes.Equal(current, signature) {
			break
		}
		if time.Now().After(deadline) {
			return ImportStub{}, 0, 0, fmt.Errorf("TesSafe IO return signature not available at 0x%X", target)
		}
		time.Sleep(200 * time.Microsecond)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x5000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("VirtualAllocEx TesSafe IO return trace: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x800
	traceAddress := page + 0x1000
	successTarget := clientBase + 0xADE16
	failureTarget := clientBase + 0xAB58D7

	stub := []byte{0x9C, 0x60}                  // pushfd; pushad
	stub = append(stub, 0x8B, 0x0D, 0, 0, 0, 0) // mov ecx,[counter]
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x8B, 0xD1, 0x83, 0xE2, 0x7F)             // edx=counter&127
	stub = append(stub, 0x8B, 0xEA, 0xC1, 0xE2, 0x04, 0x03, 0xD5) // edx=slot*17
	appendStored := func(load []byte, recordOffset uintptr) {
		stub = append(stub, load...)
		stub = append(stub, 0x89, 0x04, 0x95, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+recordOffset))
	}
	appendStored([]byte{0x8B, 0x44, 0x24, 0x1C}, 0)  // DeviceIoControl EAX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x10}, 4)  // EBX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x18}, 8)  // ECX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x14}, 12) // EDX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x04}, 16) // ESI
	appendStored([]byte{0x8B, 0x04, 0x24}, 20)       // EDI
	appendStored([]byte{0x8B, 0x44, 0x24, 0x08}, 24) // EBP
	stub = append(stub, 0x8B, 0x44, 0x24, 0x0C, 0x83, 0xC0, 0x04)
	appendStored(nil, 28)                                        // original ESP
	appendStored([]byte{0x8B, 0x44, 0x24, 0x20}, 32)             // EFLAGS
	appendStored([]byte{0x64, 0xA1, 0x24, 0x00, 0x00, 0x00}, 36) // thread ID
	stub = append(stub, 0x8B, 0x6C, 0x24, 0x0C, 0x83, 0xC5, 0x04)
	for index := 0; index < 7; index++ {
		appendStored([]byte{0x8B, 0x45, byte(index * 4)}, uintptr(40+index*4))
	}
	stub = append(stub, 0x41, 0x89, 0x0D, 0, 0, 0, 0) // inc ecx; mov [counter],ecx
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x61, 0x9D, 0x85, 0xC0) // popad; popfd; test eax,eax
	stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0) // jnz success
	successNext := page + uintptr(len(stub))
	successDisplacement := int64(successTarget) - int64(successNext)
	if successDisplacement < -0x80000000 || successDisplacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("TesSafe IO success displacement out of range")
	}
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(int32(successDisplacement)))
	stub = append(stub, 0xE9, 0, 0, 0, 0) // jmp failure
	failureNext := page + uintptr(len(stub))
	failureDisplacement := int64(failureTarget) - int64(failureNext)
	if failureDisplacement < -0x80000000 || failureDisplacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("TesSafe IO failure displacement out of range")
	}
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(int32(failureDisplacement)))
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, 0, 0, fmt.Errorf("write TesSafe IO return trace: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0, 0x90, 0x90, 0x90}
	patchDisplacement := int64(page) - int64(target+5)
	if patchDisplacement < -0x80000000 || patchDisplacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("TesSafe IO return trace displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(patchDisplacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, 0, 0, fmt.Errorf("patch TesSafe IO return at 0x%X", target)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(targetPatch)))
	return ImportStub{
		Module: "ClientBase.dll", Library: "ClientBase.dll", Symbol: "TesSafeIOReturn+0xAB34A6",
		StubAddress:   fmt.Sprintf("0x%X", page),
		Behavior:      "transparent all-thread ring trace after DeviceIoControl; replay test EAX and both original branches",
		TargetAddress: fmt.Sprintf("0x%X", target), TargetOriginal: fmt.Sprintf("%X", signature[:8]),
		targetValue: target, targetBytes: targetPatch, originalBytes: signature[:8], counterValue: counterAddress,
	}, counterAddress, traceAddress, nil
}

func installTesSafeConsumerTrace(
	process syscall.Handle,
	pid uint32,
	clearStatusMinus100 bool,
	timeout time.Duration,
) (ImportStub, uintptr, uintptr, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, 0, 0, err
	}
	const targetRVA = uintptr(0xAB542A)
	target := clientBase + targetRVA
	signature := []byte{
		0x8B, 0x7E, 0x08, // mov edi,[esi+8]
		0x3B, 0xFB, // cmp edi,ebx
	}
	deadline := time.Now().Add(timeout)
	for {
		current, ok := readRemote(process, target, len(signature))
		if ok && bytes.Equal(current, signature) {
			break
		}
		if time.Now().After(deadline) {
			return ImportStub{}, 0, 0, fmt.Errorf("TesSafe consumer signature not available at 0x%X", target)
		}
		time.Sleep(200 * time.Microsecond)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x8000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("VirtualAllocEx TesSafe consumer trace: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x400
	clearCounterAddress := page + 0x404
	traceAddress := page + 0x1000

	stub := []byte{0x9C, 0x60}                  // pushfd; pushad
	stub = append(stub, 0x8B, 0x0D, 0, 0, 0, 0) // mov ecx,[counter]
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub,
		0x8B, 0xD1, // mov edx,ecx
		0x83, 0xE2, 0x0F, // and edx,15
		0x69, 0xD2, 0x40, 0x05, 0x00, 0x00, // imul edx,edx,1344
	)
	appendStored := func(load []byte, recordOffset uintptr) {
		stub = append(stub, load...)
		stub = append(stub, 0x89, 0x82, 0, 0, 0, 0) // mov [edx+trace+offset],eax
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+recordOffset))
	}
	appendStored([]byte{0x64, 0xA1, 0x24, 0x00, 0x00, 0x00}, 0) // thread ID
	appendStored([]byte{0x8B, 0x44, 0x24, 0x1C}, 4)             // EAX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x10}, 8)             // EBX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x18}, 12)            // ECX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x14}, 16)            // EDX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x04}, 20)            // ESI
	appendStored([]byte{0x8B, 0x04, 0x24}, 24)                  // EDI
	appendStored([]byte{0x8B, 0x44, 0x24, 0x08}, 28)            // EBP
	stub = append(stub, 0x8B, 0x44, 0x24, 0x0C, 0x83, 0xC0, 0x04)
	appendStored(nil, 32)                                        // original ESP
	appendStored([]byte{0x8B, 0x44, 0x24, 0x20}, 36)             // EFLAGS
	appendStored([]byte{0x8B, 0x44, 0x24, 0x04}, 40)             // object pointer (ESI)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x04, 0x8B, 0x00}, 44) // object[0]
	appendStored([]byte{0x8B, 0x44, 0x24, 0x04, 0x8B, 0x40, 0x04}, 48)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x04, 0x8B, 0x40, 0x08}, 52)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x04, 0x8B, 0x40, 0x0C}, 56)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x08}, 60) // buffer pointer (EBP)
	stub = append(stub,
		0x8B, 0x74, 0x24, 0x08, // mov esi,saved EBP (buffer)
		0x8D, 0xBA, 0, 0, 0, 0, // lea edi,[edx+trace+64]
		0xB9, 0x40, 0x00, 0x00, 0x00, // mov ecx,64 DWORDs (256 bytes)
		0xFC, 0xF3, 0xA5, // cld; rep movsd
	)
	binary.LittleEndian.PutUint32(stub[len(stub)-12:len(stub)-8], uint32(traceAddress+64))
	stub = append(stub,
		0x8B, 0x74, 0x24, 0x04, // mov esi,saved ESI (decoded object)
		0x8D, 0xBA, 0, 0, 0, 0, // lea edi,[edx+trace+320]
		0xB9, 0x00, 0x01, 0x00, 0x00, // mov ecx,256 DWORDs (1024 bytes)
		0xFC, 0xF3, 0xA5, // cld; rep movsd
	)
	binary.LittleEndian.PutUint32(stub[len(stub)-12:len(stub)-8], uint32(traceAddress+320))
	stub = append(stub, 0xFF, 0x05, 0, 0, 0, 0) // inc [counter]
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	if clearStatusMinus100 {
		stub = append(stub,
			0x8B, 0x44, 0x24, 0x08, // mov eax,saved EBP (context)
			0x81, 0x78, 0x10, 0x9C, 0xFF, 0xFF, 0xFF, // cmp dword [eax+10h],-100
			0x75, 0x0D, // jne restore
			0xC7, 0x40, 0x10, 0x00, 0x00, 0x00, 0x00, // mov dword [eax+10h],0
			0xFF, 0x05, 0, 0, 0, 0, // inc [clearCounter]
		)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(clearCounterAddress))
	}
	stub = append(stub, 0x61, 0x9D)       // popad; popfd
	stub = append(stub, signature...)     // replay mov edi,[esi+8]; cmp edi,ebx
	stub = append(stub, 0xE9, 0, 0, 0, 0) // jmp target+5
	continuation := target + uintptr(len(signature))
	stubNext := page + uintptr(len(stub))
	displacement := int64(continuation) - int64(stubNext)
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("TesSafe consumer continuation displacement out of range")
	}
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(int32(displacement)))
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, 0, 0, fmt.Errorf("write TesSafe consumer trace: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0}
	patchDisplacement := int64(page) - int64(target+5)
	if patchDisplacement < -0x80000000 || patchDisplacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("TesSafe consumer patch displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(patchDisplacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, 0, 0, fmt.Errorf("patch TesSafe consumer at 0x%X", target)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(targetPatch)))
	behavior := "transparent ring trace after the protected response helper; capture 256 bytes of context and the complete 1024-byte decoded object, then replay original instructions"
	if clearStatusMinus100 {
		behavior += "; clear only context+0x10 values exactly equal to signed -100"
	}
	return ImportStub{
		Module: "ClientBase.dll", Library: "ClientBase.dll", Symbol: "TesSafeConsumer+0xAB542A",
		StubAddress:   fmt.Sprintf("0x%X", page),
		Behavior:      behavior,
		TargetAddress: fmt.Sprintf("0x%X", target), TargetOriginal: fmt.Sprintf("%X", signature),
		targetValue: target, targetBytes: targetPatch, originalBytes: signature, counterValue: counterAddress,
		correctedValue: clearCounterAddress,
	}, counterAddress, traceAddress, nil
}

func installTesSafeChecksumTrace(
	process syscall.Handle,
	pid uint32,
	timeout time.Duration,
) (ImportStub, uintptr, uintptr, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, 0, 0, err
	}
	const targetRVA = uintptr(0xAB60A9)
	target := clientBase + targetRVA
	signature := []byte{
		0x59,                         // pop ecx
		0x59,                         // pop ecx
		0x5F,                         // pop edi
		0xE9, 0x4A, 0xEE, 0x5E, 0xFF, // jmp ClientBase+0xA4EFB
	}
	deadline := time.Now().Add(timeout)
	for {
		current, ok := readRemote(process, target, len(signature))
		if ok && bytes.Equal(current, signature) {
			break
		}
		if time.Now().After(deadline) {
			return ImportStub{}, 0, 0, fmt.Errorf("TesSafe checksum cleanup signature not available at 0x%X", target)
		}
		time.Sleep(200 * time.Microsecond)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x8000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("VirtualAllocEx TesSafe checksum trace: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x800
	traceAddress := page + 0x1000
	continuation := clientBase + 0xA4EFB

	stub := []byte{0x9C, 0x60}                  // pushfd; pushad
	stub = append(stub, 0x8B, 0x0D, 0, 0, 0, 0) // mov ecx,[counter]
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x8B, 0xD1, 0x83, 0xE2, 0x7F)       // edx=counter&127
	stub = append(stub, 0x69, 0xD2, 0x2A, 0x00, 0x00, 0x00) // edx=slot*42 DWORDs; SIB applies *4
	appendStored := func(load []byte, recordOffset uintptr) {
		stub = append(stub, load...)
		stub = append(stub, 0x89, 0x04, 0x95, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+recordOffset))
	}
	appendStored([]byte{0x8B, 0x44, 0x24, 0x1C}, 0)  // checksum result EAX (0 or -7)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x10}, 4)  // EBX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x18}, 8)  // ECX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x14}, 12) // EDX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x04}, 16) // ESI
	appendStored([]byte{0x8B, 0x04, 0x24}, 20)       // EDI
	appendStored([]byte{0x8B, 0x44, 0x24, 0x08}, 24) // EBP
	stub = append(stub, 0x8B, 0x44, 0x24, 0x0C, 0x83, 0xC0, 0x04)
	appendStored(nil, 28)                                        // original ESP
	appendStored([]byte{0x8B, 0x44, 0x24, 0x20}, 32)             // EFLAGS
	appendStored([]byte{0x64, 0xA1, 0x24, 0x00, 0x00, 0x00}, 36) // thread ID
	stub = append(stub, 0x8B, 0x6C, 0x24, 0x0C, 0x83, 0xC5, 0x04)
	for index := 0; index < 32; index++ {
		appendStored([]byte{0x8B, 0x45, byte(index * 4)}, uintptr(40+index*4))
	}
	stub = append(stub, 0x41, 0x89, 0x0D, 0, 0, 0, 0) // inc ecx; mov [counter],ecx
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x61, 0x9D, 0x59, 0x59, 0x5F) // popad; popfd; replay cleanup pops
	stub = append(stub, 0xE9, 0, 0, 0, 0)             // jmp ClientBase+0xA4EFB
	stubNext := page + uintptr(len(stub))
	displacement := int64(continuation) - int64(stubNext)
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("TesSafe checksum continuation displacement out of range")
	}
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(int32(displacement)))
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, 0, 0, fmt.Errorf("write TesSafe checksum trace: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0, 0x90, 0x90, 0x90}
	patchDisplacement := int64(page) - int64(target+5)
	if patchDisplacement < -0x80000000 || patchDisplacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("TesSafe checksum trace displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(patchDisplacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, 0, 0, fmt.Errorf("patch TesSafe checksum cleanup at 0x%X", target)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(targetPatch)))
	return ImportStub{
		Module: "ClientBase.dll", Library: "ClientBase.dll", Symbol: "TesSafeChecksum+0xAB60A9",
		StubAddress:   fmt.Sprintf("0x%X", page),
		Behavior:      "transparent all-thread ring trace of checksum EAX and 32 stack DWORDs before replaying the verified cleanup tail",
		TargetAddress: fmt.Sprintf("0x%X", target), TargetOriginal: fmt.Sprintf("%X", signature),
		targetValue: target, targetBytes: targetPatch, originalBytes: signature, counterValue: counterAddress,
	}, counterAddress, traceAddress, nil
}

func installClientBaseDLLMainBypass(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, error) {
	base, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, err
	}
	header, ok := readRemote(process, base, 4096)
	if !ok || len(header) < 0x100 || header[0] != 'M' || header[1] != 'Z' {
		return ImportStub{}, fmt.Errorf("invalid ClientBase PE header")
	}
	peOffset := int(binary.LittleEndian.Uint32(header[0x3C:0x40]))
	optional := peOffset + 24
	if optional+20 > len(header) || binary.LittleEndian.Uint16(header[optional:optional+2]) != 0x10B {
		return ImportStub{}, fmt.Errorf("ClientBase is not PE32")
	}
	entryRVA := binary.LittleEndian.Uint32(header[optional+16 : optional+20])
	if entryRVA == 0 {
		return ImportStub{}, fmt.Errorf("ClientBase has no PE entry point")
	}
	target := base + uintptr(entryRVA)
	patch := []byte{0xB8, 0x01, 0x00, 0x00, 0x00, 0xC2, 0x0C, 0x00} // mov eax,1; ret 12
	original, ok := readRemote(process, target, len(patch))
	if !ok {
		return ImportStub{}, fmt.Errorf("read ClientBase PE entry at 0x%X", target)
	}
	if !ensureRemoteBytes(process, target, patch, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch ClientBase PE entry at 0x%X", target)
	}
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(patch)))
	return ImportStub{
		Module:         "ClientBase.dll",
		Library:        "ClientBase.dll",
		Symbol:         "AddressOfEntryPoint",
		StubAddress:    fmt.Sprintf("0x%X", target),
		TargetAddress:  fmt.Sprintf("0x%X", target),
		TargetOriginal: fmt.Sprintf("%X", original),
		Behavior:       fmt.Sprintf("diagnostic in-memory only: PE entry RVA 0x%X returns TRUE with stdcall ret 12 while the early loader gate is held", entryRVA),
		targetValue:    target,
		targetBytes:    patch,
		originalBytes:  original,
	}, nil
}

func installTPWorkerThreadIDCapture(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, uintptr, error) {
	base, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, 0, err
	}
	const workerRVA = uintptr(0x70708B)
	target := base + workerRVA
	expected := []byte{0x68, 0x7E, 0x77, 0x29, 0x6E, 0x51, 0x52, 0x53, 0x50, 0x56, 0x9C, 0x55}
	original, ok := readRemote(process, target, len(expected))
	if !ok {
		return ImportStub{}, 0, fmt.Errorf("read ClientBase+0x%X at 0x%X", workerRVA, target)
	}
	if !bytes.Equal(original, expected) {
		return ImportStub{}, 0, fmt.Errorf("ClientBase+0x%X signature mismatch: %X", workerRVA, original)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, 0, fmt.Errorf("VirtualAllocEx TP worker TID capture: 0x%X (%v)", page, allocErr)
	}
	threadIDAddress := page + 0x200
	stub := []byte{
		0x50,                               // push eax
		0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, // mov eax,fs:[TEB.ClientId.UniqueThread]
		0xA3, 0, 0, 0, 0, // mov [threadIDAddress],eax
		0x58, // pop eax
	}
	binary.LittleEndian.PutUint32(stub[8:12], uint32(threadIDAddress))
	stub = append(stub, original...)
	jumpAddress := page + uintptr(len(stub))
	stub = append(stub, 0xE9, 0, 0, 0, 0)
	displacement := int64(target+uintptr(len(original))) - int64(jumpAddress+5)
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, 0, fmt.Errorf("TP worker TID capture return displacement out of range")
	}
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(int32(displacement)))
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, 0, fmt.Errorf("write TP worker TID capture: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90}
	patchDisplacement := int64(page) - int64(target+5)
	if patchDisplacement < -0x80000000 || patchDisplacement > 0x7FFFFFFF {
		return ImportStub{}, 0, fmt.Errorf("TP worker TID capture displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:5], uint32(int32(patchDisplacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, 0, fmt.Errorf("patch ClientBase+0x%X at 0x%X", workerRVA, target)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(targetPatch)))
	return ImportStub{
		Module:         "ClientBase.dll",
		Library:        "ClientBase.dll",
		Symbol:         "TP worker thread ID capture ClientBase+0x70708B",
		StubAddress:    fmt.Sprintf("0x%X", page),
		TargetAddress:  fmt.Sprintf("0x%X", target),
		TargetOriginal: fmt.Sprintf("%X", original),
		Behavior:       "transparent: record the worker TID, replay the exact 12-byte protected prologue, and continue at ClientBase+0x707097",
		stubValue:      page,
		targetValue:    target,
		targetBytes:    targetPatch,
		originalBytes:  original,
	}, threadIDAddress, nil
}

func installTPWorkerThreadBypass(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, error) {
	base, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, err
	}
	const workerRVA = uintptr(0x70708B)
	target := base + workerRVA
	expected := []byte{0x68, 0x7E, 0x77, 0x29, 0x6E, 0x51, 0x52, 0x53, 0x50, 0x56, 0x9C, 0x55}
	original, ok := readRemote(process, target, len(expected))
	if !ok {
		return ImportStub{}, fmt.Errorf("read ClientBase+0x%X at 0x%X", workerRVA, target)
	}
	if !bytes.Equal(original, expected) {
		return ImportStub{}, fmt.Errorf("ClientBase+0x%X signature mismatch: %X", workerRVA, original)
	}
	patch := []byte{0x31, 0xC0, 0xC2, 0x04, 0x00, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90} // xor eax,eax; ret 4
	if !ensureRemoteBytes(process, target, patch, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch ClientBase+0x%X at 0x%X", workerRVA, target)
	}
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(patch)))
	return ImportStub{
		Module:         "ClientBase.dll",
		Library:        "ClientBase.dll",
		Symbol:         "TP worker thread ClientBase+0x70708B",
		StubAddress:    fmt.Sprintf("0x%X", target),
		TargetAddress:  fmt.Sprintf("0x%X", target),
		TargetOriginal: fmt.Sprintf("%X", original),
		Behavior:       "diagnostic in-memory only: the repeatedly verified TP dialog worker returns zero immediately; CRT and all other ClientBase initialization remain intact",
		targetValue:    target,
		targetBytes:    patch,
		originalBytes:  original,
	}, nil
}

// installTPWorkerStartupGate closes the only scheduling window between
// releasing ClientBase's loader thread and registering the narrowly scoped TP
// worker VEH. The verified worker entry is redirected to a tiny remote stub
// that waits on a dword, replays the exact protected prologue, and then
// continues at the original function. Once the VEH is registered Launch sets
// the dword to one. This preserves the worker instead of bypassing it and does
// not depend on host timing or a retry loop.
func installTPWorkerStartupGate(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, uintptr, uintptr, uintptr, error) {
	base, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, 0, 0, 0, err
	}
	const workerRVA = uintptr(0x70708B)
	target := base + workerRVA
	expected := []byte{0x68, 0x7E, 0x77, 0x29, 0x6E, 0x51, 0x52, 0x53, 0x50, 0x56, 0x9C, 0x55}
	original, ok := readRemote(process, target, len(expected))
	if !ok {
		return ImportStub{}, 0, 0, 0, fmt.Errorf("read ClientBase+0x%X at 0x%X", workerRVA, target)
	}
	if !bytes.Equal(original, expected) {
		return ImportStub{}, 0, 0, 0, fmt.Errorf("ClientBase+0x%X signature mismatch: %X", workerRVA, original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, 0, 0, 0, fmt.Errorf("VirtualAllocEx TP worker startup gate: 0x%X (%v)", page, allocErr)
	}
	flagAddress := page
	threadIDAddress := page + 4
	stubAddress := page + 0x10
	stub := []byte{
		0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, // mov eax,fs:[TEB.ClientId.UniqueThread]
		0xA3, 0, 0, 0, 0, // mov [threadID],eax
		0x83, 0x3D, 0, 0, 0, 0, 0, // cmp dword ptr [flag],0
		0x74, 0xF7, // je stub start
	}
	binary.LittleEndian.PutUint32(stub[7:11], uint32(threadIDAddress))
	binary.LittleEndian.PutUint32(stub[13:17], uint32(flagAddress))
	// The retry jumps back to the cmp, not to the TID store.
	stub[19] = 0xF7
	replayAddress := stubAddress + uintptr(len(stub))
	stub = append(stub, original...)
	stub = append(stub, 0xE9, 0, 0, 0, 0)
	continuation := target + uintptr(len(original))
	displacement := int64(continuation) - int64(stubAddress+uintptr(len(stub)))
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, 0, fmt.Errorf("TP worker startup gate continuation is out of rel32 range")
	}
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(int32(displacement)))
	if err := writeRemote(process, stubAddress, stub); err != nil {
		return ImportStub{}, 0, 0, 0, fmt.Errorf("write TP worker startup gate: %w", err)
	}

	patch := []byte{0xE9, 0, 0, 0, 0, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90, 0x90}
	patchDisplacement := int64(stubAddress) - int64(target+5)
	if patchDisplacement < -0x80000000 || patchDisplacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, 0, fmt.Errorf("TP worker startup gate entry is out of rel32 range")
	}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(int32(patchDisplacement)))
	if !ensureRemoteBytes(process, target, patch, pageExecuteReadWrite) {
		return ImportStub{}, 0, 0, 0, fmt.Errorf("patch ClientBase+0x%X at 0x%X", workerRVA, target)
	}
	procFlushInstruction.Call(uintptr(process), stubAddress, uintptr(len(stub)))

	return ImportStub{
		Module:         "ClientBase.dll",
		Library:        "ClientBase.dll",
		Symbol:         "TP worker startup gate ClientBase+0x70708B",
		StubAddress:    fmt.Sprintf("0x%X", stubAddress),
		TargetAddress:  fmt.Sprintf("0x%X", target),
		TargetOriginal: fmt.Sprintf("%X", original),
		Behavior:       "compatibility in-memory only: wait until the scoped TP worker VEH is registered, replay the exact 12-byte prologue, and continue normally",
		stubValue:      stubAddress,
		targetValue:    target,
		targetBytes:    patch,
		originalBytes:  original,
	}, flagAddress, threadIDAddress, replayAddress, nil
}

func waitForRemoteDWORD(process syscall.Handle, address uintptr, timeout time.Duration) (uint32, error) {
	deadline := time.Now().Add(timeout)
	for {
		if value, ok := readRemote(process, address, 4); ok {
			if result := binary.LittleEndian.Uint32(value); result != 0 {
				return result, nil
			}
		}
		if exitCode, exited := remoteProcessExitCode(process); exited {
			return 0, fmt.Errorf("process exited with code %d (0x%08X) before remote DWORD at 0x%X became nonzero", exitCode, uint32(exitCode), address)
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("remote DWORD at 0x%X remained zero", address)
		}
		time.Sleep(time.Millisecond)
	}
}

func remoteProcessExitCode(process syscall.Handle) (int32, bool) {
	const stillActive = 259
	var exitCode uint32
	success, _, _ := procGetExitCode.Call(uintptr(process), uintptr(unsafe.Pointer(&exitCode)))
	if success == 0 || exitCode == stillActive {
		return 0, false
	}
	return int32(exitCode), true
}

func installTPWorkerErrorClassZero(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, error) {
	base, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, err
	}
	const (
		contextRVA = uintptr(0x9F5061)
		operandRVA = uintptr(0x9F5069)
	)
	expected := []byte{0xB5, 0xFD, 0x9B, 0xC6, 0x49, 0xA7, 0x65, 0xC6, 0x76, 0x17, 0x0F, 0x92, 0xDE, 0x37, 0xDC, 0xBA}
	context, ok := readRemote(process, base+contextRVA, len(expected))
	if !ok {
		return ImportStub{}, fmt.Errorf("read ClientBase+0x%X", contextRVA)
	}
	if !bytes.Equal(context, expected) {
		return ImportStub{}, fmt.Errorf("ClientBase+0x%X signature mismatch: %X", contextRVA, context)
	}
	target := base + operandRVA
	original := []byte{expected[operandRVA-contextRVA]}
	patch := []byte{0xF5}
	if !ensureRemoteBytes(process, target, patch, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch ClientBase+0x%X", operandRVA)
	}
	procFlushInstruction.Call(uintptr(process), target, 1)
	return ImportStub{
		Module:         "ClientBase.dll",
		Library:        "ClientBase.dll",
		Symbol:         "TP worker final error-class operand ClientBase+0x9F5069",
		StubAddress:    fmt.Sprintf("0x%X", target),
		TargetAddress:  fmt.Sprintf("0x%X", target),
		TargetOriginal: fmt.Sprintf("%X (context %X)", original, context),
		Behavior:       "diagnostic in-memory only: change the uniquely traced VM operand from 0x76 to the algebraically derived 0xF5 so the decoded pushed immediate changes from 3 to 0",
		targetValue:    target,
		targetBytes:    patch,
		originalBytes:  original,
	}, nil
}

func installClientBaseEntryTrace(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, error) {
	client, err := waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return ImportStub{}, err
	}
	iatAddress, err := findImportAddressOrdinal(process, client, "ClientBase.dll", 1)
	if err != nil {
		return ImportStub{}, err
	}
	target, err := waitForExecutablePointer(process, iatAddress, timeout)
	if err != nil {
		return ImportStub{}, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx ClientBase entry trace: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x400
	callerAddress := page + 0x404
	rawStackAddress := page + 0x500

	stub := []byte{
		0x9C, 0x60, // pushfd; pushad
		0x8B, 0x74, 0x24, 0x0C, // mov esi,[esp+0Ch] (pushad-saved ESP after pushfd)
		0x83, 0xC6, 0x04, // add esi,4 (original entry ESP)
		0x8B, 0x06, 0xA3, 0, 0, 0, 0, // mov eax,[esi]; mov [caller],eax
	}
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(callerAddress))
	stub = append(stub, 0xBF)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(rawStackAddress)) // mov edi,rawStack
	stub = append(stub,
		0xB9, 0x20, 0x00, 0x00, 0x00, // mov ecx,32
		0xFC, 0xF3, 0xA5, // cld; rep movsd
		0xFF, 0x05, 0, 0, 0, 0, // inc dword [counter]
	)
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x61, 0x9D, 0xB8) // popad; popfd; mov eax,target
	stub = binary.LittleEndian.AppendUint32(stub, uint32(target))
	stub = append(stub, 0xFF, 0xE0) // jmp eax
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write ClientBase entry trace: %w", err)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(page))
	if !ensureRemoteBytes(process, iatAddress, pointer, pageReadWrite) {
		return ImportStub{}, fmt.Errorf("patch Client.exe ClientBase ordinal-1 IAT at 0x%X", iatAddress)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	return ImportStub{
		Module: "Client.exe", Library: "ClientBase.dll", Symbol: "ordinal_1",
		IATAddress:     fmt.Sprintf("0x%X", iatAddress),
		StubAddress:    fmt.Sprintf("0x%X", page),
		TargetAddress:  fmt.Sprintf("0x%X", target),
		TargetOriginal: fmt.Sprintf("%08X", uint32(target)),
		Behavior:       "transparent trace of Client.exe calls through its sole ClientBase import; capture caller and 32 entry-stack DWORDs, then tail-jump to the real ordinal-1 export",
		iatValue:       iatAddress,
		stubValue:      page,
		counterValue:   counterAddress,
		callerValue:    callerAddress,
		rawStackValue:  rawStackAddress,
		rawStackWords:  32,
	}, nil
}

func installTPPostHandshakeBypass(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, err
	}
	const targetRVA = uintptr(0x6A8D16)
	target := clientBase + targetRVA
	signature := []byte{
		0xFF, 0xD1, // call ecx (verified TP post-handshake subroutine)
		0x7F,                         // protected return marker skipped by the callee
		0x68, 0x90, 0x73, 0xA9, 0x49, // push 49A97390h at the native continuation
	}
	deadline := time.Now().Add(timeout)
	for {
		current, ok := readRemote(process, target, len(signature))
		if ok && bytes.Equal(current, signature) {
			break
		}
		if time.Now().After(deadline) {
			return ImportStub{}, fmt.Errorf("TP post-handshake signature not available at 0x%X", target)
		}
		time.Sleep(200 * time.Microsecond)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx TP post-handshake bypass: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x200
	stub := []byte{0x9C, 0xFF, 0x05, 0, 0, 0, 0, 0x9D} // pushfd; inc [counter]; popfd
	binary.LittleEndian.PutUint32(stub[3:7], uint32(counterAddress))
	stub = append(stub,
		0x8D, 0x64, 0x24, 0x10, // lea esp,[esp+16] (four protected arguments)
		0x68, 0x90, 0x73, 0xA9, 0x49, // replay push 49A97390h
		0xE9, 0, 0, 0, 0, // jmp native continuation after the replayed push
	)
	continuation := target + uintptr(len(signature))
	stubEnd := page + uintptr(len(stub))
	displacement := int64(continuation) - int64(stubEnd)
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("TP post-handshake continuation displacement out of range")
	}
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(int32(displacement)))
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write TP post-handshake bypass: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0}
	patchDisplacement := int64(page) - int64(target+5)
	if patchDisplacement < -0x80000000 || patchDisplacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("TP post-handshake patch displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(patchDisplacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, fmt.Errorf("patch TP post-handshake call at 0x%X", target)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(targetPatch)))
	return ImportStub{
		Module: "ClientBase.dll", Library: "ClientBase.dll", Symbol: "TPPostHandshakeCall+0x6A8D16",
		StubAddress:   fmt.Sprintf("0x%X", page),
		Behavior:      "diagnostic: preserve flags, discard four TP-only arguments, replay the verified native continuation push, and skip unavailable driver-state consumption",
		TargetAddress: fmt.Sprintf("0x%X", target), TargetOriginal: fmt.Sprintf("%X", signature[:5]),
		targetValue: target, targetBytes: targetPatch, originalBytes: signature[:5], counterValue: counterAddress,
	}, nil
}

func installKernelETWWrapperTrace(
	process syscall.Handle,
	pid uint32,
	bypassTPRegistration bool,
	timeout time.Duration,
) (ImportStub, error) {
	// Older Win10 WOW64 builds place this internal wrapper in KERNELBASE,
	// while current Win11 builds expose the same bounded shape in KERNEL32.
	// Resolve both remote modules by ASLR base and accept only the first module
	// with one unique, fully validated wrapper; never fall back to a fixed RVA.
	var (
		kernelImage remoteExecutableImage
		candidate   kernelETWWrapperCandidate
		kernelBase  uintptr
		attempts    []string
	)
	for _, moduleName := range []string{"KERNEL32.DLL", "KERNELBASE.DLL"} {
		moduleBase, moduleErr := waitForModule(process, pid, moduleName, timeout)
		if moduleErr != nil {
			attempts = append(attempts, fmt.Sprintf("%s: %v", moduleName, moduleErr))
			continue
		}
		image, imageErr := loadRemoteExecutableImage(process, moduleBase)
		if imageErr != nil {
			attempts = append(attempts, fmt.Sprintf("%s base=0x%X: %v", moduleName, moduleBase, imageErr))
			continue
		}
		located, locateErr := locateKernelETWWrapper(image)
		if locateErr != nil {
			sections := make([]string, 0, len(image.sections))
			for _, section := range image.sections {
				sections = append(sections, fmt.Sprintf("%s@0x%X+0x%X", section.name, section.virtualAddress, section.virtualSize))
			}
			attempts = append(attempts, fmt.Sprintf(
				"%s path=%q version=%q base=0x%X size=0x%X executable_sections=[%s]: %v",
				moduleName, image.path, image.version, moduleBase, image.size, strings.Join(sections, ","), locateErr,
			))
			continue
		}
		kernelImage, candidate, kernelBase = image, located, moduleBase
		break
	}
	if kernelBase == 0 {
		return ImportStub{}, fmt.Errorf(
			"locate Windows ETW wrapper safely: os=%s attempts=[%s]",
			currentWindowsVersion(), strings.Join(attempts, " | "),
		)
	}
	var tpCaller uintptr
	if bypassTPRegistration {
		clientBase, clientErr := waitForModule(process, pid, "ClientBase.dll", timeout)
		if clientErr != nil {
			return ImportStub{}, clientErr
		}
		tpCaller = clientBase + 0x3FBB8
	}
	target := candidate.address
	signature := candidate.signature
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, fmt.Errorf("VirtualAllocEx kernel ETW wrapper trace: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x300
	bypassCounterAddress := page + 0x304
	callerAddress := page + 0x320
	traceAddress := page + 0x340
	rawStackAddress := page + 0x400
	stub := []byte{0x9C, 0x60}                  // pushfd; pushad
	stub = append(stub, 0xFF, 0x05, 0, 0, 0, 0) // inc [counter]
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x0C, 0x83, 0xC0, 0x04, 0x8B, 0x00) // eax=original [esp]
	stub = append(stub, 0xA3, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(callerAddress))
	stub = append(stub, 0xA3, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x18, 0xA3, 0, 0, 0, 0) // saved ECX
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+4))
	stub = append(stub, 0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0xA3, 0, 0, 0, 0) // thread ID
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+8))
	stub = append(stub,
		0x8B, 0x74, 0x24, 0x0C, 0x83, 0xC6, 0x04, // esi=original ESP
		0xBF, 0, 0, 0, 0, // edi=raw stack
		0xB9, 0x10, 0x00, 0x00, 0x00, // ecx=16 DWORDs
		0xFC, 0xF3, 0xA5, // cld; rep movsd
	)
	binary.LittleEndian.PutUint32(stub[len(stub)-12:len(stub)-8], uint32(rawStackAddress))
	stub = append(stub, 0x61, 0x9D) // popad; popfd
	if bypassTPRegistration {
		stub = append(stub,
			0x81, 0x3C, 0x24, 0, 0, 0, 0, // cmp dword ptr [esp],ClientBase+0x3FBB8
			0x75, 0x09, // jne replayOriginal
			0xFF, 0x05, 0, 0, 0, 0, // inc [bypassCounter]
			0x31, 0xC0, // xor eax,eax (ETW registration success)
			0xC3, // ret; the wrapper has no caller arguments to clean
		)
		binary.LittleEndian.PutUint32(stub[len(stub)-15:len(stub)-11], uint32(tpCaller))
		binary.LittleEndian.PutUint32(stub[len(stub)-7:len(stub)-3], uint32(bypassCounterAddress))
	}
	stub = append(stub, signature[:4]...) // mov edi,edi; push ecx; push ecx
	stub = append(stub, 0xE8, 0, 0, 0, 0) // call original helper
	callNext := page + uintptr(len(stub))
	callTarget := candidate.helper
	if err := validateRemoteExecutableAddress(process, kernelBase, callTarget, 1); err != nil {
		return ImportStub{}, fmt.Errorf("validate kernel ETW helper 0x%X: %w", callTarget, err)
	}
	callDisplacement := int64(callTarget) - int64(callNext)
	if callDisplacement < -0x80000000 || callDisplacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("kernel ETW helper displacement out of range")
	}
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(int32(callDisplacement)))
	stub = append(stub, 0xE9, 0, 0, 0, 0) // continue through this build's native cleanup/return tail
	continuationNext := page + uintptr(len(stub))
	continuationDisplacement := int64(candidate.continuation) - int64(continuationNext)
	if continuationDisplacement < -0x80000000 || continuationDisplacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("kernel ETW wrapper continuation displacement out of range")
	}
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(int32(continuationDisplacement)))
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, fmt.Errorf("write kernel ETW wrapper trace: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0}
	displacement := int64(page) - int64(target+5)
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, fmt.Errorf("kernel ETW wrapper trace displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(displacement)))
	if err := patchRemoteExecutableBytes(process, kernelBase, target, signature[:5], targetPatch); err != nil {
		return ImportStub{}, fmt.Errorf("patch kernel ETW wrapper at 0x%X: %w", target, err)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(targetPatch)))
	behavior := "transparent trace of the real caller, ECX object, thread ID, and 16 entry-stack DWORDs; replay the verified hotpatch prefix and helper call, then continue through the Windows-build-native cleanup/return tail"
	if bypassTPRegistration {
		behavior += "; only caller ClientBase+0x3FBB8 skips the invalid TP ETW provider object and returns success"
	}
	return ImportStub{
		Module:         filepath.Base(kernelImage.path),
		ModulePath:     kernelImage.path,
		ModuleBase:     fmt.Sprintf("0x%X", kernelImage.base),
		ModuleVersion:  kernelImage.version,
		Library:        filepath.Base(kernelImage.path),
		Symbol:         fmt.Sprintf("InternalETWWrapper(dynamic RVA 0x%X)", candidate.rva),
		StubAddress:    fmt.Sprintf("0x%X", page),
		Behavior:       behavior,
		TargetAddress:  fmt.Sprintf("0x%X", target),
		TargetRVA:      fmt.Sprintf("0x%X", candidate.rva),
		TargetSection:  candidate.sectionName,
		TargetOriginal: fmt.Sprintf("%X", signature[:5]),
		TargetContext:  fmt.Sprintf("%X", candidate.context),
		HelperAddress:  fmt.Sprintf("0x%X", candidate.helper),
		HelperRVA:      fmt.Sprintf("0x%X", candidate.helperRVA),
		targetValue:    target, targetBytes: targetPatch, originalBytes: signature[:5], targetModuleBase: kernelBase,
		strictTargetRestore: true, counterValue: counterAddress,
		traceValue: traceAddress, callerValue: callerAddress, rawStackValue: rawStackAddress, rawStackWords: 16,
		bypassedValue: bypassCounterAddress,
	}, nil
}

func installTPDispatcherTrace(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, uintptr, uintptr, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, 0, 0, err
	}
	const targetRVA = uintptr(0x9D2E9A)
	target := clientBase + targetRVA
	signature := []byte{
		0x81, 0xC6, 0x04, 0x00, 0x00, 0x00, // add esi,4
		0x03, 0x07, // add eax,[edi]
		0x81, 0xC0, 0xE5, 0xB7, 0x75, 0x4D, // add eax,4D75B7E5h
		0xFF, 0xE0, // jmp eax
	}
	deadline := time.Now().Add(timeout)
	for {
		current, ok := readRemote(process, target, len(signature))
		if ok && bytes.Equal(current, signature) {
			break
		}
		if time.Now().After(deadline) {
			return ImportStub{}, 0, 0, fmt.Errorf("TP dispatcher signature not available at 0x%X", target)
		}
		time.Sleep(200 * time.Microsecond)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x5000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("VirtualAllocEx TP dispatcher trace: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x800
	traceAddress := page + 0x1000
	// Execute the original arithmetic first, then trace the resolved native
	// destination while preserving exactly the flags produced by those adds.
	stub := append([]byte{}, signature[:14]...)
	stub = append(stub, 0x9C, 0x60) // pushfd; pushad
	var skipJumps []int
	appendSkipJump := func() {
		stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)
		skipJumps = append(skipJumps, len(stub)-4)
	}
	stub = append(stub, 0x81, 0x3D, 0xA4, 0xC1, 0x32, 0x01, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(clientBase+0x8D1E74))
	appendSkipJump()
	stub = append(stub, 0x81, 0x3D, 0xAC, 0xC1, 0x32, 0x01, 0, 0, 0, 0)
	appendSkipJump()
	stub = append(stub, 0x81, 0x3D, 0xB0, 0xC1, 0x32, 0x01, 0x05, 0x00, 0x00, 0x00)
	appendSkipJump()
	stub = append(stub, 0x81, 0x3D, 0xB4, 0xC1, 0x32, 0x01, 0x1C, 0x02, 0x00, 0x00)
	appendSkipJump()
	stub = append(stub, 0x8B, 0x0D, 0, 0, 0, 0) // mov ecx,[counter]
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x81, 0xF9, 0x80, 0x00, 0x00, 0x00) // cmp ecx,128
	stub = append(stub, 0x0F, 0x83, 0, 0, 0, 0)             // jae skip
	skipJumps = append(skipJumps, len(stub)-4)
	stub = append(stub, 0x8B, 0xD1, 0xC1, 0xE2, 0x04, 0x03, 0xD1) // edx=ecx*17
	appendStored := func(load []byte, recordOffset uintptr) {
		stub = append(stub, load...)
		stub = append(stub, 0x89, 0x04, 0x95, 0, 0, 0, 0) // mov [trace+edx*4],eax
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+recordOffset))
	}
	appendStored([]byte{0x8B, 0x44, 0x24, 0x1C}, 0)               // destination EAX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x10}, 4)               // EBX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x18}, 8)               // ECX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x14}, 12)              // EDX
	appendStored([]byte{0x8B, 0x44, 0x24, 0x04}, 16)              // ESI after add esi,4
	appendStored([]byte{0x8B, 0x04, 0x24}, 20)                    // EDI
	appendStored([]byte{0x8B, 0x44, 0x24, 0x08}, 24)              // EBP
	stub = append(stub, 0x8B, 0x44, 0x24, 0x0C, 0x83, 0xC0, 0x04) // original ESP
	appendStored(nil, 28)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x20}, 32)              // EFLAGS from original adds
	stub = append(stub, 0x8B, 0x6C, 0x24, 0x0C, 0x83, 0xC5, 0x04) // ebp=original ESP
	for index := 0; index < 8; index++ {
		appendStored([]byte{0x8B, 0x45, byte(index * 4)}, uintptr(36+index*4))
	}
	stub = append(stub, 0x41, 0x89, 0x0D, 0, 0, 0, 0) // inc ecx; mov [counter],ecx
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	skipOffset := len(stub)
	stub = append(stub, 0x61, 0x9D, 0xFF, 0xE0) // popad; popfd; jmp eax
	for _, displacementOffset := range skipJumps {
		next := displacementOffset + 4
		binary.LittleEndian.PutUint32(stub[displacementOffset:displacementOffset+4], uint32(int32(skipOffset-next)))
	}
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, 0, 0, fmt.Errorf("write TP dispatcher trace: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0, 0x90}
	displacement := int64(page) - int64(target+5)
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("TP dispatcher trace displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(displacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, 0, 0, fmt.Errorf("patch TP dispatcher at 0x%X", target)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(targetPatch)))
	return ImportStub{
		Module: "ClientBase.dll", Library: "ClientBase.dll", Symbol: "TPDispatcher+0x9D2E9A",
		StubAddress: fmt.Sprintf("0x%X", page), Behavior: "semantic-equivalent dispatcher trace while frame is 0,5,540",
		TargetAddress: fmt.Sprintf("0x%X", target), TargetOriginal: fmt.Sprintf("%X", signature[:6]),
		targetValue: target, targetBytes: targetPatch, originalBytes: signature[:6],
	}, counterAddress, traceAddress, nil
}

func installTPThirdVMTrace(process syscall.Handle, pid uint32, timeout time.Duration) (ImportStub, uintptr, uintptr, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, 0, 0, err
	}
	const targetRVA = uintptr(0x209D7)
	target := clientBase + targetRVA
	signature := []byte{
		0x8A, 0xC9, // mov cl,cl
		0xC7, 0xC0, 0, 0, 0, 0, // mov eax,0
	}
	deadline := time.Now().Add(timeout)
	for {
		current, ok := readRemote(process, target, len(signature))
		if ok && bytes.Equal(current, signature) {
			break
		}
		if time.Now().After(deadline) {
			return ImportStub{}, 0, 0, fmt.Errorf("TP third VM pop-state handler signature not available at 0x%X", target)
		}
		time.Sleep(200 * time.Microsecond)
	}
	const tailRVA = uintptr(0x5F46D4)
	tail, ok := readRemote(process, clientBase+tailRVA, 15)
	if !ok || !bytes.Equal(tail[:7], []byte{0x8B, 0x0E, 0x8D, 0x76, 0x05, 0x81, 0xC1}) ||
		!bytes.Equal(tail[11:], []byte{0x03, 0x0F, 0xFF, 0xE1}) {
		return ImportStub{}, 0, 0, fmt.Errorf("TP third VM direct-dispatch tail signature not available at 0x%X", clientBase+tailRVA)
	}
	decodeConstant := binary.LittleEndian.Uint32(tail[7:11])
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x5000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("VirtualAllocEx TP third VM trace: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x800
	traceAddress := page + 0x1000
	stub := []byte{0x9C, 0x60} // pushfd; pushad before the pop-state handler mutates anything
	stub = append(stub, 0x8B, 0x0D, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x8B, 0xD1, 0x83, 0xE2, 0x7F)             // edx=counter&127
	stub = append(stub, 0x8B, 0xEA, 0xC1, 0xE2, 0x04, 0x03, 0xD5) // edx=slot*17
	appendStored := func(load []byte, recordOffset uintptr) {
		stub = append(stub, load...)
		stub = append(stub, 0x89, 0x04, 0x95, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+recordOffset))
	}
	// Decode the direct successor that the handler tail will jump to after it
	// pops one value into EDI's state table.
	stub = append(stub,
		0x8B, 0x74, 0x24, 0x04, // mov esi,[esp+4]
		0x8B, 0x06, // mov eax,[esi]
		0x8B, 0x3C, 0x24, // mov edi,[esp]
		0x03, 0x07, // add eax,[edi]
		0x05, 0, 0, 0, 0, // add eax,decodeConstant
	)
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], decodeConstant)
	appendStored(nil, 0)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x10}, 4)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x18}, 8)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x14}, 12)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x04}, 16)
	appendStored([]byte{0x8B, 0x04, 0x24}, 20)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x08}, 24)
	stub = append(stub, 0x8B, 0x44, 0x24, 0x0C, 0x83, 0xC0, 0x04)
	appendStored(nil, 28)
	appendStored([]byte{0x8B, 0x44, 0x24, 0x20}, 32)
	stub = append(stub, 0x8B, 0x6C, 0x24, 0x0C, 0x83, 0xC5, 0x04)
	for index := 0; index < 8; index++ {
		appendStored([]byte{0x8B, 0x45, byte(index * 4)}, uintptr(36+index*4))
	}
	stub = append(stub, 0x41, 0x89, 0x0D, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, signature...)
	stub = append(stub, 0xE9, 0, 0, 0, 0)
	resumeDisplacement := int64(target+uintptr(len(signature))) - int64(page+uintptr(len(stub)))
	if resumeDisplacement < -0x80000000 || resumeDisplacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("TP third VM trace resume displacement out of range")
	}
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(int32(resumeDisplacement)))
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, 0, 0, fmt.Errorf("write TP third VM trace: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0, 0x90, 0x90, 0x90}
	displacement := int64(page) - int64(target+5)
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, fmt.Errorf("TP third VM trace displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(displacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, 0, 0, fmt.Errorf("patch TP third VM dispatcher at 0x%X", target)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(targetPatch)))
	return ImportStub{
		Module: "ClientBase.dll", Library: "ClientBase.dll", Symbol: "TPThirdVM+0x209D7",
		StubAddress:   fmt.Sprintf("0x%X", page),
		Behavior:      "semantic-equivalent 128-step ring trace for the third-error VM's direct pop-state loop",
		TargetAddress: fmt.Sprintf("0x%X", target), TargetOriginal: fmt.Sprintf("%X", signature),
		targetValue: target, targetBytes: targetPatch, originalBytes: signature, counterValue: counterAddress,
	}, counterAddress, traceAddress, nil
}

func installTPDispatcherBypass(
	process syscall.Handle,
	pid uint32,
	timeout time.Duration,
	traceAfterBypass bool,
	trapSecondError bool,
	bypassSecondError bool,
	bypassThirdError bool,
	thirdReturnEAX uint32,
	thirdReturnEAXSet bool,
) (ImportStub, uintptr, uintptr, uintptr, uintptr, error) {
	clientBase, err := waitForModule(process, pid, "ClientBase.dll", timeout)
	if err != nil {
		return ImportStub{}, 0, 0, 0, 0, err
	}
	const targetRVA = uintptr(0x9D2E9A)
	target := clientBase + targetRVA
	signature := []byte{
		0x81, 0xC6, 0x04, 0x00, 0x00, 0x00,
		0x03, 0x07,
		0x81, 0xC0, 0xE5, 0xB7, 0x75, 0x4D,
		0xFF, 0xE0,
	}
	deadline := time.Now().Add(timeout)
	for {
		current, ok := readRemote(process, target, len(signature))
		if ok && bytes.Equal(current, signature) {
			break
		}
		if time.Now().After(deadline) {
			return ImportStub{}, 0, 0, 0, 0, fmt.Errorf("TP dispatcher signature not available at 0x%X", target)
		}
		time.Sleep(200 * time.Microsecond)
	}
	allocationSize := uintptr(0x1000)
	if traceAfterBypass || trapSecondError || bypassSecondError || bypassThirdError {
		allocationSize = 0x5000
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, allocationSize, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return ImportStub{}, 0, 0, 0, 0, fmt.Errorf("VirtualAllocEx TP dispatcher bypass: 0x%X (%v)", page, allocErr)
	}
	counterAddress := page + 0x800
	traceCounterAddress := uintptr(0)
	traceAddress := uintptr(0)
	trapCounterAddress := uintptr(0)
	thirdCounterAddress := uintptr(0)
	armedAddress := page + 0x80C
	if traceAfterBypass {
		traceCounterAddress = page + 0x804
		traceAddress = page + 0x1000
	}
	if trapSecondError || bypassSecondError {
		trapCounterAddress = page + 0x808
	}
	if bypassThirdError {
		thirdCounterAddress = page + 0x810
	}
	stub := []byte{0x9C} // preserve the exact pre-dispatcher flags
	var forwardJumps []int
	appendForwardJump := func() {
		stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)
		forwardJumps = append(forwardJumps, len(stub)-4)
	}
	stub = append(stub, 0x81, 0x3D, 0xA4, 0xC1, 0x32, 0x01, 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(clientBase+0x8D1E74))
	appendForwardJump()
	stub = append(stub, 0x81, 0x3D, 0xAC, 0xC1, 0x32, 0x01, 0, 0, 0, 0)
	appendForwardJump()
	stub = append(stub, 0x81, 0x3D, 0xB0, 0xC1, 0x32, 0x01, 0x05, 0x00, 0x00, 0x00)
	appendForwardJump()
	stub = append(stub, 0x81, 0x3D, 0xB4, 0xC1, 0x32, 0x01, 0x1C, 0x02, 0x00, 0x00)
	appendForwardJump()
	stub = append(stub, 0x81, 0xFE, 0, 0, 0, 0) // cmp esi,verified pre-handler VM IP
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(clientBase+0x9E988D))
	appendForwardJump()
	stub = append(stub, 0x83, 0xFD, 0x00) // cmp ebp,0
	appendForwardJump()
	stub = append(stub, 0xFF, 0x05, 0, 0, 0, 0) // inc [counter]
	binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(counterAddress))
	if traceAfterBypass || trapSecondError || bypassSecondError || bypassThirdError {
		stub = append(stub, 0xC7, 0x05, 0, 0, 0, 0, 0x01, 0, 0, 0) // mov [armed],1
		binary.LittleEndian.PutUint32(stub[len(stub)-8:len(stub)-4], uint32(armedAddress))
	}
	stub = append(stub,
		0x9D,                         // restore caller flags
		0xBC, 0xB8, 0xC1, 0x32, 0x01, // mov esp,0132C1B8h
		0x8B, 0x2D, 0xA0, 0xC1, 0x32, 0x01, // mov ebp,[0132C1A0h]
		0x68, 0, 0, 0, 0, // push verified virtual return marker
		0xC3, // ret
	)
	binary.LittleEndian.PutUint32(stub[len(stub)-5:len(stub)-1], uint32(clientBase+0x8D1E74))
	forwardOffset := len(stub)
	if trapSecondError || bypassSecondError {
		var secondForwardJumps []int
		appendSecondForwardJump := func() {
			stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)
			secondForwardJumps = append(secondForwardJumps, len(stub)-4)
		}
		stub = append(stub, 0x83, 0x3D, 0, 0, 0, 0, 0x01) // cmp dword [armed],1
		binary.LittleEndian.PutUint32(stub[len(stub)-5:len(stub)-1], uint32(armedAddress))
		appendSecondForwardJump()
		stub = append(stub, 0x81, 0xFE, 0, 0, 0, 0) // cmp esi,verified generic error VM IP
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(clientBase+0x9E988D))
		appendSecondForwardJump()
		stub = append(stub, 0x83, 0xFD, 0x00) // cmp ebp,0
		appendSecondForwardJump()
		appendStackArgumentCheck := func(offset byte, value uint32) {
			// ESP currently includes the pushfd at hook entry, hence +4.
			stub = append(stub, 0x81, 0x7C, 0x24, offset, 0, 0, 0, 0)
			binary.LittleEndian.PutUint32(stub[len(stub)-4:], value)
			appendSecondForwardJump()
		}
		appendStackArgumentCheck(0x20, 3)
		appendStackArgumentCheck(0x24, 1006)
		appendStackArgumentCheck(0x28, 19008)
		stub = append(stub, 0xFF, 0x05, 0, 0, 0, 0) // inc [trapCounter]
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(trapCounterAddress))
		if bypassSecondError {
			stub = append(stub,
				0x9D,                         // restore exact pre-dispatcher flags
				0xBC, 0x70, 0xC4, 0x32, 0x01, // mov esp,0132C470h
				0xBD, 0xF8, 0xC6, 0x32, 0x01, // mov ebp,0132C6F8h
				0x68, 0, 0, 0, 0, // push verified second-layer virtual return
				0xC3,
			)
			binary.LittleEndian.PutUint32(stub[len(stub)-5:len(stub)-1], uint32(clientBase+0x61096F))
		} else {
			stub = append(stub, 0xEB, 0xFE) // exact pre-dispatcher diagnostic freeze
		}
		secondForward := len(stub)
		for _, displacementOffset := range secondForwardJumps {
			next := displacementOffset + 4
			binary.LittleEndian.PutUint32(stub[displacementOffset:displacementOffset+4], uint32(int32(secondForward-next)))
		}
	}
	if bypassThirdError {
		var thirdForwardJumps []int
		appendThirdForwardJump := func() {
			stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)
			thirdForwardJumps = append(thirdForwardJumps, len(stub)-4)
		}
		stub = append(stub, 0x83, 0x3D, 0, 0, 0, 0, 0x01)
		binary.LittleEndian.PutUint32(stub[len(stub)-5:len(stub)-1], uint32(armedAddress))
		appendThirdForwardJump()
		stub = append(stub, 0x81, 0xFE, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(clientBase+0x9E988D))
		appendThirdForwardJump()
		stub = append(stub, 0x83, 0xFD, 0x00)
		appendThirdForwardJump()
		appendThirdArgumentCheck := func(offset byte, value uint32) {
			stub = append(stub, 0x81, 0x7C, 0x24, offset, 0, 0, 0, 0)
			binary.LittleEndian.PutUint32(stub[len(stub)-4:], value)
			appendThirdForwardJump()
		}
		appendThirdArgumentCheck(0x20, 3)
		appendThirdArgumentCheck(0x24, 1032)
		appendThirdArgumentCheck(0x28, 19008)
		stub = append(stub, 0xFF, 0x05, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(thirdCounterAddress))
		stub = append(stub, 0x9D) // restore exact pre-dispatcher flags
		if thirdReturnEAXSet {
			stub = append(stub, 0xB8, 0, 0, 0, 0) // mov eax,diagnostic return value
			binary.LittleEndian.PutUint32(stub[len(stub)-4:], thirdReturnEAX)
		}
		stub = append(stub,
			0xBC, 0x64, 0xC7, 0x32, 0x01, // mov esp,0132C764h
			0xBD, 0x70, 0xFF, 0x32, 0x01, // mov ebp,0132FF70h
			0x68, 0, 0, 0, 0,
			0xC3,
		)
		binary.LittleEndian.PutUint32(stub[len(stub)-5:len(stub)-1], uint32(clientBase+0x615364))
		thirdForward := len(stub)
		for _, displacementOffset := range thirdForwardJumps {
			next := displacementOffset + 4
			binary.LittleEndian.PutUint32(stub[displacementOffset:displacementOffset+4], uint32(int32(thirdForward-next)))
		}
	}
	stub = append(stub, 0x9D) // restore flags before the original dispatcher
	stub = append(stub, signature[:14]...)
	if traceAfterBypass {
		// Keep a rolling record of the last 128 dispatcher transitions after
		// the first failure has been bypassed. pushfd/pushad make this
		// instrumentation transparent to the protected VM state.
		stub = append(stub, 0x9C, 0x60)                   // pushfd; pushad
		stub = append(stub, 0x83, 0x3D, 0, 0, 0, 0, 0x01) // cmp dword [armed],1
		binary.LittleEndian.PutUint32(stub[len(stub)-5:len(stub)-1], uint32(armedAddress))
		stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0) // jne traceDone
		traceDoneJump := len(stub) - 4
		stub = append(stub, 0x8B, 0x0D, 0, 0, 0, 0) // mov ecx,[traceCounter]
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceCounterAddress))
		stub = append(stub, 0x8B, 0xD1, 0x83, 0xE2, 0x7F)             // edx=ecx&127
		stub = append(stub, 0x8B, 0xEA, 0xC1, 0xE2, 0x04, 0x03, 0xD5) // edx=slot*17
		appendStored := func(load []byte, recordOffset uintptr) {
			stub = append(stub, load...)
			stub = append(stub, 0x89, 0x04, 0x95, 0, 0, 0, 0)
			binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceAddress+recordOffset))
		}
		appendStored([]byte{0x8B, 0x44, 0x24, 0x1C}, 0)
		appendStored([]byte{0x8B, 0x44, 0x24, 0x10}, 4)
		appendStored([]byte{0x8B, 0x44, 0x24, 0x18}, 8)
		appendStored([]byte{0x8B, 0x44, 0x24, 0x14}, 12)
		appendStored([]byte{0x8B, 0x44, 0x24, 0x04}, 16)
		appendStored([]byte{0x8B, 0x04, 0x24}, 20)
		appendStored([]byte{0x8B, 0x44, 0x24, 0x08}, 24)
		stub = append(stub, 0x8B, 0x44, 0x24, 0x0C, 0x83, 0xC0, 0x04)
		appendStored(nil, 28)
		appendStored([]byte{0x8B, 0x44, 0x24, 0x20}, 32)
		stub = append(stub, 0x8B, 0x6C, 0x24, 0x0C, 0x83, 0xC5, 0x04)
		for index := 0; index < 8; index++ {
			appendStored([]byte{0x8B, 0x45, byte(index * 4)}, uintptr(36+index*4))
		}
		stub = append(stub, 0x41, 0x89, 0x0D, 0, 0, 0, 0)
		binary.LittleEndian.PutUint32(stub[len(stub)-4:], uint32(traceCounterAddress))
		traceDone := len(stub)
		binary.LittleEndian.PutUint32(stub[traceDoneJump:traceDoneJump+4], uint32(int32(traceDone-(traceDoneJump+4))))
		stub = append(stub, 0x61, 0x9D, 0xFF, 0xE0) // popad; popfd; jmp eax
	} else {
		stub = append(stub, signature[14:]...)
	}
	for _, displacementOffset := range forwardJumps {
		next := displacementOffset + 4
		binary.LittleEndian.PutUint32(stub[displacementOffset:displacementOffset+4], uint32(int32(forwardOffset-next)))
	}
	if err := writeRemote(process, page, stub); err != nil {
		return ImportStub{}, 0, 0, 0, 0, fmt.Errorf("write TP dispatcher bypass: %w", err)
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0, 0x90}
	displacement := int64(page) - int64(target+5)
	if displacement < -0x80000000 || displacement > 0x7FFFFFFF {
		return ImportStub{}, 0, 0, 0, 0, fmt.Errorf("TP dispatcher bypass displacement out of range")
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(displacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return ImportStub{}, 0, 0, 0, 0, fmt.Errorf("patch TP dispatcher at 0x%X", target)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), target, uintptr(len(targetPatch)))
	behavior := "restore verified outer ESP/EBP and virtual return at earliest stable TP failure state"
	if traceAfterBypass {
		behavior += "; retain a transparent 128-step ring trace of subsequent VM dispatches"
	}
	if trapSecondError {
		behavior += "; freeze exactly before dispatching TP error 3/1006/19008"
	}
	if bypassSecondError {
		behavior += "; restore verified second-layer outer state for TP error 3/1006/19008"
	}
	if bypassThirdError {
		behavior += "; restore verified third-layer outer state for TP error 3/1032/19008"
		if thirdReturnEAXSet {
			behavior += fmt.Sprintf("; force diagnostic third-layer EAX=0x%X", thirdReturnEAX)
		}
	}
	return ImportStub{
		Module: "ClientBase.dll", Library: "ClientBase.dll", Symbol: "TPDispatcherBypass(0,5,540)",
		StubAddress:   fmt.Sprintf("0x%X", page),
		Behavior:      behavior,
		TargetAddress: fmt.Sprintf("0x%X", target), TargetOriginal: fmt.Sprintf("%X", signature[:6]),
		targetValue: target, targetBytes: targetPatch, originalBytes: signature[:6], counterValue: counterAddress,
	}, traceCounterAddress, traceAddress, trapCounterAddress, thirdCounterAddress, nil
}

func waitForExecutablePointer(process syscall.Handle, pointerAddress uintptr, timeout time.Duration) (uintptr, error) {
	deadline := time.Now().Add(timeout)
	var last uintptr
	for time.Now().Before(deadline) {
		pointer, ok := readRemote(process, pointerAddress, 4)
		if ok {
			last = uintptr(binary.LittleEndian.Uint32(pointer))
			if isExecutableAddress(process, last) {
				return last, nil
			}
		}
		time.Sleep(100 * time.Microsecond)
	}
	return 0, fmt.Errorf("IAT 0x%X did not resolve to executable memory within %s; last=0x%X", pointerAddress, timeout, last)
}

func isExecutableAddress(process syscall.Handle, address uintptr) bool {
	if address < 0x10000 {
		return false
	}
	var info memoryBasicInformation
	result, _, _ := procVirtualQueryEx.Call(uintptr(process), address, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if result == 0 || info.State != memCommit {
		return false
	}
	return info.Protect&0xF0 != 0 && info.Protect&0x100 == 0
}

func pinRemotePatches(process syscall.Handle, stub ImportStub, pointerRewrites, targetRewrites *atomic.Uint64, stop <-chan struct{}) {
	expected := make([]byte, 4)
	binary.LittleEndian.PutUint32(expected, uint32(stub.stubValue))
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if stub.iatValue != 0 {
				current, ok := readRemote(process, stub.iatValue, 4)
				if !ok {
					return
				}
				if binary.LittleEndian.Uint32(current) != uint32(stub.stubValue) {
					if ensureRemoteBytes(process, stub.iatValue, expected, pageReadWrite) {
						pointerRewrites.Add(1)
					}
				}
			}
			if stub.targetValue != 0 && len(stub.targetBytes) != 0 {
				currentTarget, ok := readRemote(process, stub.targetValue, len(stub.targetBytes))
				if !ok {
					return
				}
				if string(currentTarget) != string(stub.targetBytes) {
					if stub.strictTargetRestore {
						if len(stub.originalBytes) != len(stub.targetBytes) || string(currentTarget) != string(stub.originalBytes) {
							return
						}
						if err := patchRemoteExecutableBytes(process, stub.targetModuleBase, stub.targetValue, stub.originalBytes, stub.targetBytes); err != nil {
							return
						}
						targetRewrites.Add(1)
					} else if ensureRemoteBytes(process, stub.targetValue, stub.targetBytes, pageExecuteReadWrite) {
						targetRewrites.Add(1)
					}
				}
			}
		}
	}
}

func pinPrivateFunctionCopies(process syscall.Handle, signature, replacement []byte, rewrites *atomic.Uint64, stop <-chan struct{}) {
	if len(signature) == 0 || len(replacement) == 0 {
		return
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if !scanAndPatchPrivateExecutable(process, signature, replacement, rewrites) {
				return
			}
		}
	}
}

func scanAndPatchPrivateExecutable(process syscall.Handle, signature, replacement []byte, rewrites *atomic.Uint64) bool {
	const (
		memPrivate = 0x20000
		pageGuard  = 0x100
		chunkSize  = 1024 * 1024
	)
	for address := uintptr(0x10000); address < uintptr(0x80000000); {
		var info memoryBasicInformation
		queried, _, _ := procVirtualQueryEx.Call(
			uintptr(process), address, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
		)
		if queried == 0 || info.RegionSize == 0 {
			return true
		}
		next := info.BaseAddress + info.RegionSize
		if next <= address {
			return true
		}
		if info.State == memCommit && info.Type == memPrivate && info.Protect&0xF0 != 0 && info.Protect&pageGuard == 0 {
			regionEnd := next
			if regionEnd > uintptr(0x80000000) {
				regionEnd = uintptr(0x80000000)
			}
			var overlap []byte
			for chunkAddress := info.BaseAddress; chunkAddress < regionEnd; {
				size := chunkSize
				if remaining := regionEnd - chunkAddress; remaining < uintptr(size) {
					size = int(remaining)
				}
				data, ok := readRemote(process, chunkAddress, size)
				if !ok {
					break
				}
				combined := make([]byte, 0, len(overlap)+len(data))
				combined = append(combined, overlap...)
				combined = append(combined, data...)
				combinedBase := chunkAddress - uintptr(len(overlap))
				for searchAt := 0; searchAt+len(signature) <= len(combined); {
					relative := bytes.Index(combined[searchAt:], signature)
					if relative < 0 {
						break
					}
					relative += searchAt
					target := combinedBase + uintptr(relative)
					if ensureRemoteBytes(process, target, replacement, pageExecuteReadWrite) {
						rewrites.Add(1)
						copy(combined[relative:relative+len(replacement)], replacement)
					}
					searchAt = relative + len(signature)
				}
				keep := len(signature) - 1
				if len(combined) < keep {
					keep = len(combined)
				}
				overlap = append(overlap[:0], combined[len(combined)-keep:]...)
				chunkAddress += uintptr(len(data))
			}
		}
		address = next
	}
	return true
}

func pinPrivateConditionalFormatCopies(
	process syscall.Handle,
	signature []byte,
	sleepAddress, captureAddress uintptr,
	rewrites *atomic.Uint64,
	stop <-chan struct{},
) {
	if len(signature) == 0 || sleepAddress == 0 || captureAddress == 0 {
		return
	}
	hooks := make(map[uintptr][]byte)
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			for target, patch := range hooks {
				if current, ok := readRemote(process, target, len(patch)); !ok {
					return
				} else if !bytes.Equal(current, patch) {
					ensureRemoteBytes(process, target, patch, pageExecuteReadWrite)
				}
			}
			for _, target := range findPrivateExecutableCopies(process, signature) {
				if _, exists := hooks[target]; exists {
					continue
				}
				patch, ok := installPrivateConditionalFormatHook(process, target, signature, sleepAddress, captureAddress)
				if !ok {
					continue
				}
				hooks[target] = patch
				rewrites.Add(1)
			}
		}
	}
}

func pinPrivateConditionalTPDialogCopies(
	process syscall.Handle,
	signature []byte,
	sleepAddress, captureAddress, expectedCaller, expectedOuterCaller uintptr,
	returnInstead, clearErrorFrame bool,
	returnValue uint32,
	rewrites *atomic.Uint64,
	stop <-chan struct{},
) {
	if len(signature) == 0 || sleepAddress == 0 || captureAddress == 0 || expectedCaller == 0 || expectedOuterCaller == 0 {
		return
	}
	hooks := make(map[uintptr][]byte)
	discoveryComplete := false
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			for target, patch := range hooks {
				if current, ok := readRemote(process, target, len(patch)); !ok {
					return
				} else if !bytes.Equal(current, patch) {
					ensureRemoteBytes(process, target, patch, pageExecuteReadWrite)
				}
			}
			if discoveryComplete {
				continue
			}
			for _, target := range findPrivateExecutableCopiesWithin(process, signature, 2*1024*1024) {
				if _, exists := hooks[target]; exists {
					continue
				}
				patch, ok := installPrivateConditionalTPDialogHook(
					process, target, signature, sleepAddress, captureAddress,
					expectedCaller, expectedOuterCaller, returnInstead, clearErrorFrame, returnValue,
				)
				if !ok {
					continue
				}
				hooks[target] = patch
				rewrites.Add(1)
			}
			// USER32 supplies one ANSI and one Unicode wrapper with this stable
			// prologue. Both are present before the protected warning path runs.
			if len(hooks) >= 2 {
				discoveryComplete = true
			}
		}
	}
}

func pollRemoteCapturePage(process syscall.Handle, base uintptr, shadow *atomic.Value, stop <-chan struct{}) {
	if base == 0 || shadow == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			value, ok := readRemote(process, base, 0x200)
			if !ok {
				return
			}
			if binary.LittleEndian.Uint32(value[:4]) != 0 {
				shadow.Store(append([]byte(nil), value...))
			}
		}
	}
}

func (state *lateTerminateState) setInstalled(stub ImportStub) {
	state.mu.Lock()
	state.stub = stub
	state.installed = true
	state.mu.Unlock()
}

func (state *lateTerminateState) setError(err error) {
	state.mu.Lock()
	state.err = err.Error()
	state.mu.Unlock()
}

func (state *lateTerminateState) snapshot() (bool, ImportStub, string, any) {
	state.mu.Lock()
	installed, stub, errText := state.installed, state.stub, state.err
	state.mu.Unlock()
	return installed, stub, errText, state.shadow.Load()
}

func pollLateTPPostTerminate(
	process syscall.Handle,
	pid uint32,
	dialogCaptureAddress uintptr,
	returnInstead bool,
	state *lateTerminateState,
	stop <-chan struct{},
) {
	if dialogCaptureAddress == 0 || state == nil {
		return
	}
	ticker := time.NewTicker(2 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			value, ok := readRemote(process, dialogCaptureAddress, 4)
			if !ok {
				return
			}
			if binary.LittleEndian.Uint32(value) == 0 {
				continue
			}
			stub, err := installConditionalZeroStatusTerminateStub(process, pid, returnInstead, time.Second)
			if err != nil {
				state.setError(err)
				return
			}
			state.setInstalled(stub)
			pollRemoteCapturePage(process, stub.nativeTerminateCaptureValue, &state.shadow, stop)
			return
		}
	}
}

func readLateTerminateCapture(
	process syscall.Handle,
	state *lateTerminateState,
) (bool, string, *NativeTerminateCapture) {
	if state == nil {
		return false, "", nil
	}
	installed, stub, errText, shadow := state.snapshot()
	if !installed {
		return false, errText, nil
	}
	base := stub.nativeTerminateCaptureValue
	value, ok := readRemote(process, base, 0x200)
	if !ok && shadow != nil {
		if page, pageOK := shadow.([]byte); pageOK && len(page) >= 0x200 {
			value, ok = page, true
		}
	}
	if !ok || len(value) < 56 || binary.LittleEndian.Uint32(value[0:4]) == 0 {
		return true, errText, nil
	}
	caller := binary.LittleEndian.Uint32(value[8:12])
	capture := &NativeTerminateCapture{
		Hits:          binary.LittleEndian.Uint32(value[0:4]),
		ThreadID:      binary.LittleEndian.Uint32(value[4:8]),
		CallerAddress: fmt.Sprintf("0x%08X", caller),
		ProcessHandle: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[12:16])),
		ExitStatus:    fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[16:20])),
		EAX:           binary.LittleEndian.Uint32(value[20:24]),
		EBX:           binary.LittleEndian.Uint32(value[24:28]),
		ECX:           binary.LittleEndian.Uint32(value[28:32]),
		EDX:           binary.LittleEndian.Uint32(value[32:36]),
		ESI:           binary.LittleEndian.Uint32(value[36:40]),
		EDI:           binary.LittleEndian.Uint32(value[40:44]),
		EBP:           binary.LittleEndian.Uint32(value[44:48]),
		EFlags:        binary.LittleEndian.Uint32(value[48:52]),
		EntryESP:      binary.LittleEndian.Uint32(value[52:56]),
	}
	if len(value) >= 0x200 {
		for offset := 0x100; offset < 0x200; offset += 4 {
			capture.Stack = append(capture.Stack,
				fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(value[offset:offset+4])))
		}
	}
	if caller >= 64 {
		codeBase := uintptr(caller) - 64
		if code, codeOK := readRemote(process, codeBase, 128); codeOK {
			capture.CallerCodeBase = fmt.Sprintf("0x%08X", codeBase)
			capture.CallerCodeHex = fmt.Sprintf("%X", code)
		}
	}
	return true, errText, capture
}

func (buffer *tpThreadSampleBuffer) setThreadID(threadID uint32) {
	buffer.mu.Lock()
	buffer.threadID = threadID
	buffer.mu.Unlock()
}

func (buffer *tpThreadSampleBuffer) addFailure() {
	buffer.mu.Lock()
	buffer.failures++
	buffer.mu.Unlock()
}

func (buffer *tpThreadSampleBuffer) add(sample TPThreadSample) {
	const limit = 2048
	buffer.mu.Lock()
	buffer.total++
	sample.Index = buffer.total
	if len(buffer.samples) < limit {
		buffer.samples = append(buffer.samples, sample)
	} else {
		buffer.samples[buffer.next] = sample
		buffer.next = (buffer.next + 1) % limit
	}
	buffer.mu.Unlock()
}

func (buffer *tpThreadSampleBuffer) snapshot() (uint32, uint32, uint32, []TPThreadSample) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	samples := make([]TPThreadSample, 0, len(buffer.samples))
	if len(buffer.samples) == 2048 && buffer.total > uint32(len(buffer.samples)) {
		samples = append(samples, buffer.samples[buffer.next:]...)
		samples = append(samples, buffer.samples[:buffer.next]...)
	} else {
		samples = append(samples, buffer.samples...)
	}
	return buffer.threadID, buffer.total, buffer.failures, samples
}

func pollTPPostDialogThread(
	process syscall.Handle,
	captureAddress uintptr,
	started time.Time,
	buffer *tpThreadSampleBuffer,
	stop <-chan struct{},
) {
	if captureAddress == 0 || buffer == nil {
		return
	}
	var threadID uint32
	detect := time.NewTicker(2 * time.Millisecond)
	defer detect.Stop()
	for threadID == 0 {
		select {
		case <-stop:
			return
		case <-detect.C:
			value, ok := readRemote(process, captureAddress, 8)
			if !ok {
				return
			}
			if binary.LittleEndian.Uint32(value[0:4]) != 0 {
				threadID = binary.LittleEndian.Uint32(value[4:8])
			}
		}
	}
	buffer.setThreadID(threadID)
	modules := snapshotRemoteModules(process)
	const threadAccess = 0x0002 | 0x0008 | 0x0040 // SUSPEND_RESUME | GET_CONTEXT | QUERY_INFORMATION
	var thread syscall.Handle
	for attempts := 0; attempts < 100; attempts++ {
		handle, _, _ := procOpenThread.Call(threadAccess, 0, uintptr(threadID))
		if handle != 0 {
			thread = syscall.Handle(handle)
			break
		}
		select {
		case <-stop:
			return
		case <-time.After(2 * time.Millisecond):
		}
	}
	if thread == 0 {
		buffer.addFailure()
		return
	}
	defer syscall.CloseHandle(thread)

	consecutiveContextFailures := 0
	sample := func() bool {
		previous, _, _ := procSuspendThread.Call(uintptr(thread))
		if previous == ^uintptr(0) {
			buffer.addFailure()
			return false
		}
		context := make([]byte, 716)
		binary.LittleEndian.PutUint32(context[0:4], 0x00010003)
		success, _, _ := procWow64GetThreadContext.Call(uintptr(thread), uintptr(unsafe.Pointer(&context[0])))
		if success == 0 {
			procResumeThread.Call(uintptr(thread))
			buffer.addFailure()
			consecutiveContextFailures++
			return consecutiveContextFailures < 32
		}
		consecutiveContextFailures = 0
		entry := TPThreadSample{
			ElapsedMS: time.Since(started).Milliseconds(),
			ThreadID:  threadID,
			EDI:       binary.LittleEndian.Uint32(context[156:160]),
			ESI:       binary.LittleEndian.Uint32(context[160:164]),
			EBX:       binary.LittleEndian.Uint32(context[164:168]),
			EDX:       binary.LittleEndian.Uint32(context[168:172]),
			ECX:       binary.LittleEndian.Uint32(context[172:176]),
			EAX:       binary.LittleEndian.Uint32(context[176:180]),
			EBP:       binary.LittleEndian.Uint32(context[180:184]),
			EIP:       binary.LittleEndian.Uint32(context[184:188]),
			EFlags:    binary.LittleEndian.Uint32(context[192:196]),
			ESP:       binary.LittleEndian.Uint32(context[196:200]),
		}
		if stack, ok := readRemote(process, uintptr(entry.ESP), 16*4); ok {
			entry.Stack = make([]uint32, 0, 16)
			for offset := 0; offset < len(stack); offset += 4 {
				entry.Stack = append(entry.Stack, binary.LittleEndian.Uint32(stack[offset:offset+4]))
			}
		}
		for _, module := range modules {
			if entry.EIP >= module.base && entry.EIP-module.base < module.size {
				entry.Module = module.name
				entry.RVA = entry.EIP - module.base
				break
			}
		}
		var info memoryBasicInformation
		if queried, _, _ := procVirtualQueryEx.Call(
			uintptr(process), uintptr(entry.EIP), uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
		); queried == unsafe.Sizeof(info) {
			entry.AllocationBase = fmt.Sprintf("0x%08X", info.AllocationBase)
			entry.MemoryProtect = fmt.Sprintf("0x%X", info.Protect)
			entry.MemoryType = fmt.Sprintf("0x%X", info.Type)
		}
		procResumeThread.Call(uintptr(thread))
		buffer.add(entry)
		return true
	}
	if !sample() {
		return
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if !sample() {
				return
			}
		}
	}
}

func pollTPPostDialogProcess(
	process syscall.Handle,
	pid uint32,
	captureAddress uintptr,
	started time.Time,
	buffer *tpThreadSampleBuffer,
	stop <-chan struct{},
) {
	if captureAddress == 0 || buffer == nil {
		return
	}
	detect := time.NewTicker(2 * time.Millisecond)
	defer detect.Stop()
	for {
		select {
		case <-stop:
			return
		case <-detect.C:
			value, ok := readRemote(process, captureAddress, 4)
			if !ok {
				return
			}
			if binary.LittleEndian.Uint32(value) != 0 {
				goto captured
			}
		}
	}

captured:
	modules := snapshotRemoteModules(process)
	ntCreateUserProcess, _ := findRemoteExport(process, pid, "NTDLL.dll", "NtCreateUserProcess", time.Second, 0)
	threads := openRemoteProcessThreads(pid, buffer)
	if len(threads) == 0 {
		buffer.addFailure()
		return
	}
	defer func() {
		for _, thread := range threads {
			if thread.handle != 0 {
				syscall.CloseHandle(thread.handle)
			}
		}
	}()
	sampleAll := func() bool {
		alive := 0
		for index := range threads {
			if threads[index].handle == 0 {
				continue
			}
			sample, captured, threadAlive := captureTPThreadSample(
				process, threads[index].id, threads[index].handle, modules, ntCreateUserProcess, started,
			)
			if !threadAlive {
				syscall.CloseHandle(threads[index].handle)
				threads[index].handle = 0
				continue
			}
			alive++
			if captured {
				buffer.add(sample)
			} else {
				buffer.addFailure()
			}
		}
		return alive != 0
	}
	if !sampleAll() {
		return
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if !sampleAll() {
				return
			}
		}
	}
}

type remoteThreadHandle struct {
	id     uint32
	handle syscall.Handle
}

func openRemoteProcessThreads(pid uint32, buffer *tpThreadSampleBuffer) []remoteThreadHandle {
	const (
		threadSnapshot = 0x00000004
		threadAccess   = 0x0002 | 0x0008 | 0x0040
	)
	snapshot, _, _ := procCreateToolhelp32.Call(threadSnapshot, 0)
	if snapshot == ^uintptr(0) || snapshot == 0 {
		return nil
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	entry := threadEntry32{Size: uint32(unsafe.Sizeof(threadEntry32{}))}
	success, _, _ := procThread32First.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	if success == 0 {
		return nil
	}
	var threads []remoteThreadHandle
	for {
		if entry.OwnerProcessID == pid {
			handle, _, _ := procOpenThread.Call(threadAccess, 0, uintptr(entry.ThreadID))
			if handle != 0 {
				threads = append(threads, remoteThreadHandle{id: entry.ThreadID, handle: syscall.Handle(handle)})
			} else if buffer != nil {
				buffer.addFailure()
			}
		}
		entry.Size = uint32(unsafe.Sizeof(threadEntry32{}))
		success, _, _ = procThread32Next.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
		if success == 0 {
			break
		}
	}
	return threads
}

func captureTPThreadSample(
	process syscall.Handle,
	threadID uint32,
	thread syscall.Handle,
	modules []remoteModuleRange,
	ntCreateUserProcess uintptr,
	started time.Time,
) (TPThreadSample, bool, bool) {
	previous, _, _ := procSuspendThread.Call(uintptr(thread))
	if previous == ^uintptr(0) {
		return TPThreadSample{}, false, false
	}
	context := make([]byte, 716)
	binary.LittleEndian.PutUint32(context[0:4], 0x00010003)
	success, _, _ := procWow64GetThreadContext.Call(uintptr(thread), uintptr(unsafe.Pointer(&context[0])))
	if success == 0 {
		procResumeThread.Call(uintptr(thread))
		return TPThreadSample{}, false, true
	}
	entry := TPThreadSample{
		ElapsedMS: time.Since(started).Milliseconds(),
		ThreadID:  threadID,
		EDI:       binary.LittleEndian.Uint32(context[156:160]),
		ESI:       binary.LittleEndian.Uint32(context[160:164]),
		EBX:       binary.LittleEndian.Uint32(context[164:168]),
		EDX:       binary.LittleEndian.Uint32(context[168:172]),
		ECX:       binary.LittleEndian.Uint32(context[172:176]),
		EAX:       binary.LittleEndian.Uint32(context[176:180]),
		EBP:       binary.LittleEndian.Uint32(context[180:184]),
		EIP:       binary.LittleEndian.Uint32(context[184:188]),
		EFlags:    binary.LittleEndian.Uint32(context[192:196]),
		ESP:       binary.LittleEndian.Uint32(context[196:200]),
	}
	if stack, ok := readRemote(process, uintptr(entry.ESP), 16*4); ok {
		entry.Stack = make([]uint32, 0, 16)
		for offset := 0; offset < len(stack); offset += 4 {
			entry.Stack = append(entry.Stack, binary.LittleEndian.Uint32(stack[offset:offset+4]))
		}
	}
	for _, module := range modules {
		if entry.EIP >= module.base && entry.EIP-module.base < module.size {
			entry.Module = module.name
			entry.RVA = entry.EIP - module.base
			break
		}
	}
	var info memoryBasicInformation
	if queried, _, _ := procVirtualQueryEx.Call(
		uintptr(process), uintptr(entry.EIP), uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
	); queried == unsafe.Sizeof(info) {
		entry.AllocationBase = fmt.Sprintf("0x%08X", info.AllocationBase)
		entry.MemoryProtect = fmt.Sprintf("0x%X", info.Protect)
		entry.MemoryType = fmt.Sprintf("0x%X", info.Type)
	}
	if ntCreateUserProcess != 0 && uintptr(entry.EIP) >= ntCreateUserProcess &&
		uintptr(entry.EIP) <= ntCreateUserProcess+0x10 && len(entry.Stack) >= 10 {
		parameters := uintptr(entry.Stack[9])
		entry.ImagePath = readRemoteUnicodeString32(process, parameters+0x38, 2048)
		entry.CommandLine = readRemoteUnicodeString32(process, parameters+0x40, 4096)
	}
	procResumeThread.Call(uintptr(thread))
	return entry, true, true
}

func captureTPStartupTimeoutThreads(process syscall.Handle, pid uint32, started time.Time) []TPThreadSample {
	modules := snapshotRemoteModules(process)
	threads := openRemoteProcessThreads(pid, nil)
	defer func() {
		for _, thread := range threads {
			syscall.CloseHandle(thread.handle)
		}
	}()
	samples := make([]TPThreadSample, 0, len(threads))
	for _, thread := range threads {
		sample, captured, alive := captureTPThreadSample(
			process, thread.id, thread.handle, modules, 0, started,
		)
		if !alive || !captured {
			continue
		}
		sample.Index = uint32(len(samples))
		samples = append(samples, sample)
	}
	return samples
}

func readRemoteUnicodeString32(process syscall.Handle, descriptor uintptr, maximumBytes uint16) string {
	value, ok := readRemote(process, descriptor, 8)
	if !ok {
		return ""
	}
	length := binary.LittleEndian.Uint16(value[0:2])
	buffer := uintptr(binary.LittleEndian.Uint32(value[4:8]))
	if length == 0 || buffer == 0 {
		return ""
	}
	if length > maximumBytes {
		length = maximumBytes
	}
	length &^= 1
	text, ok := readRemote(process, buffer, int(length))
	if !ok {
		return ""
	}
	words := make([]uint16, 0, len(text)/2)
	for offset := 0; offset+1 < len(text); offset += 2 {
		words = append(words, binary.LittleEndian.Uint16(text[offset:offset+2]))
	}
	return syscall.UTF16ToString(words)
}

func snapshotRemoteModules(process syscall.Handle) []remoteModuleRange {
	modules := make([]uintptr, 1024)
	var needed uint32
	success, _, _ := procEnumProcessModulesEx.Call(
		uintptr(process), uintptr(unsafe.Pointer(&modules[0])), uintptr(len(modules))*unsafe.Sizeof(modules[0]),
		uintptr(unsafe.Pointer(&needed)), 0x03,
	)
	if success == 0 {
		return nil
	}
	count := int(needed / uint32(unsafe.Sizeof(modules[0])))
	if count > len(modules) {
		count = len(modules)
	}
	ranges := make([]remoteModuleRange, 0, count)
	for _, module := range modules[:count] {
		header, ok := readRemote(process, module, 4096)
		if !ok || len(header) < 0x40 {
			continue
		}
		peOffset := int(binary.LittleEndian.Uint32(header[0x3C:0x40]))
		optional := peOffset + 24
		if peOffset < 0 || optional+60 > len(header) || string(header[peOffset:peOffset+4]) != "PE\x00\x00" {
			continue
		}
		size := binary.LittleEndian.Uint32(header[optional+56 : optional+60])
		if size == 0 || module > 0xFFFFFFFF {
			continue
		}
		name := fmt.Sprintf("module-%08X", module)
		pathBuffer := make([]uint16, 32768)
		length, _, _ := procGetModuleFileNameEx.Call(
			uintptr(process), module, uintptr(unsafe.Pointer(&pathBuffer[0])), uintptr(len(pathBuffer)),
		)
		if length != 0 {
			name = filepath.Base(syscall.UTF16ToString(pathBuffer[:length]))
		}
		ranges = append(ranges, remoteModuleRange{base: uint32(module), size: size, name: name})
	}
	return ranges
}

func findPrivateExecutableCopies(process syscall.Handle, signature []byte) []uintptr {
	return findPrivateExecutableCopiesWithin(process, signature, 0)
}

func findPrivateExecutableCopiesWithin(process syscall.Handle, signature []byte, maximumRegionSize uintptr) []uintptr {
	const (
		memPrivate = 0x20000
		pageGuard  = 0x100
		chunkSize  = 1024 * 1024
	)
	var matches []uintptr
	for address := uintptr(0x10000); address < uintptr(0x80000000); {
		var info memoryBasicInformation
		queried, _, _ := procVirtualQueryEx.Call(
			uintptr(process), address, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
		)
		if queried == 0 || info.RegionSize == 0 {
			break
		}
		next := info.BaseAddress + info.RegionSize
		if next <= address {
			break
		}
		if info.State == memCommit && info.Type == memPrivate && info.Protect&0xF0 != 0 && info.Protect&pageGuard == 0 &&
			(maximumRegionSize == 0 || info.RegionSize <= maximumRegionSize) {
			regionEnd := next
			if regionEnd > uintptr(0x80000000) {
				regionEnd = uintptr(0x80000000)
			}
			var overlap []byte
			for chunkAddress := info.BaseAddress; chunkAddress < regionEnd; {
				size := chunkSize
				if remaining := regionEnd - chunkAddress; remaining < uintptr(size) {
					size = int(remaining)
				}
				data, ok := readRemote(process, chunkAddress, size)
				if !ok {
					break
				}
				combined := make([]byte, 0, len(overlap)+len(data))
				combined = append(combined, overlap...)
				combined = append(combined, data...)
				combinedBase := chunkAddress - uintptr(len(overlap))
				for searchAt := 0; searchAt+len(signature) <= len(combined); {
					relative := bytes.Index(combined[searchAt:], signature)
					if relative < 0 {
						break
					}
					relative += searchAt
					matches = append(matches, combinedBase+uintptr(relative))
					searchAt = relative + len(signature)
				}
				keep := len(signature) - 1
				if len(combined) < keep {
					keep = len(combined)
				}
				overlap = append(overlap[:0], combined[len(combined)-keep:]...)
				chunkAddress += uintptr(len(data))
			}
		}
		address = next
	}
	return matches
}

func installPrivateConditionalFormatHook(
	process syscall.Handle,
	target uintptr,
	original []byte,
	sleepAddress, captureAddress uintptr,
) ([]byte, bool) {
	if len(original) < 5 {
		return nil, false
	}
	page, _, _ := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, false
	}
	trampolineAddress := page + 0x200
	trampoline := append([]byte{}, original[:5]...)
	trampoline = append(trampoline, 0xE9, 0, 0, 0, 0)
	trampolineDisplacement := int64(target+5) - int64(trampolineAddress+uintptr(len(trampoline)))
	if trampolineDisplacement < -0x80000000 || trampolineDisplacement > 0x7FFFFFFF {
		return nil, false
	}
	binary.LittleEndian.PutUint32(trampoline[6:], uint32(int32(trampolineDisplacement)))
	stub := buildConditionalFormatTrapStub(captureAddress, sleepAddress, trampolineAddress)
	if writeRemote(process, page, stub) != nil || writeRemote(process, trampolineAddress, trampoline) != nil {
		return nil, false
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0}
	targetDisplacement := int64(page) - int64(target+uintptr(len(targetPatch)))
	if targetDisplacement < -0x80000000 || targetDisplacement > 0x7FFFFFFF {
		return nil, false
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(targetDisplacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return nil, false
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), trampolineAddress, uintptr(len(trampoline)))
	return targetPatch, true
}

func installPrivateConditionalTPDialogHook(
	process syscall.Handle,
	target uintptr,
	original []byte,
	sleepAddress, captureAddress, expectedCaller, expectedOuterCaller uintptr,
	returnInstead, clearErrorFrame bool,
	returnValue uint32,
) ([]byte, bool) {
	if len(original) < 5 {
		return nil, false
	}
	page, _, _ := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, false
	}
	trampolineAddress := page + 0x200
	trampoline := append([]byte{}, original[:5]...)
	trampoline = append(trampoline, 0xE9, 0, 0, 0, 0)
	trampolineDisplacement := int64(target+5) - int64(trampolineAddress+uintptr(len(trampoline)))
	if trampolineDisplacement < -0x80000000 || trampolineDisplacement > 0x7FFFFFFF {
		return nil, false
	}
	binary.LittleEndian.PutUint32(trampoline[6:], uint32(int32(trampolineDisplacement)))
	stub := buildConditionalTPDialogTrapStub(
		captureAddress, sleepAddress, trampolineAddress,
		expectedCaller, expectedOuterCaller, returnInstead, clearErrorFrame, returnValue,
	)
	if writeRemote(process, page, stub) != nil || writeRemote(process, trampolineAddress, trampoline) != nil {
		return nil, false
	}
	targetPatch := []byte{0xE9, 0, 0, 0, 0}
	targetDisplacement := int64(page) - int64(target+uintptr(len(targetPatch)))
	if targetDisplacement < -0x80000000 || targetDisplacement > 0x7FFFFFFF {
		return nil, false
	}
	binary.LittleEndian.PutUint32(targetPatch[1:], uint32(int32(targetDisplacement)))
	if !ensureRemoteBytes(process, target, targetPatch, pageExecuteReadWrite) {
		return nil, false
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	procFlushInstruction.Call(uintptr(process), trampolineAddress, uintptr(len(trampoline)))
	return targetPatch, true
}

func ensureRemoteBytes(process syscall.Handle, address uintptr, data []byte, temporaryProtect uint32) bool {
	var oldProtect uint32
	success, _, _ := procVirtualProtectEx.Call(uintptr(process), address, uintptr(len(data)), uintptr(temporaryProtect), uintptr(unsafe.Pointer(&oldProtect)))
	if success == 0 {
		return false
	}
	err := writeRemote(process, address, data)
	var ignored uint32
	procVirtualProtectEx.Call(uintptr(process), address, uintptr(len(data)), uintptr(oldProtect), uintptr(unsafe.Pointer(&ignored)))
	if err == nil {
		procFlushInstruction.Call(uintptr(process), address, uintptr(len(data)))
	}
	return err == nil
}

func waitForModule(process syscall.Handle, pid uint32, moduleName string, timeout time.Duration) (uintptr, error) {
	return waitForModuleInterval(process, pid, moduleName, timeout, 10*time.Millisecond)
}

func waitForModuleInterval(process syscall.Handle, pid uint32, moduleName string, timeout, interval time.Duration) (uintptr, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		modules := make([]uintptr, 1024)
		var needed uint32
		success, _, _ := procEnumProcessModulesEx.Call(
			uintptr(process), uintptr(unsafe.Pointer(&modules[0])), uintptr(len(modules))*unsafe.Sizeof(modules[0]),
			uintptr(unsafe.Pointer(&needed)), 0x03,
		)
		if success != 0 {
			count := int(needed / uint32(unsafe.Sizeof(modules[0])))
			if count > len(modules) {
				count = len(modules)
			}
			for _, module := range modules[:count] {
				buffer := make([]uint16, 32768)
				length, _, _ := procGetModuleFileNameEx.Call(uintptr(process), module, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
				if length != 0 && strings.EqualFold(filepath.Base(syscall.UTF16ToString(buffer[:length])), moduleName) {
					return module, nil
				}
			}
		}
		time.Sleep(interval)
	}
	return 0, fmt.Errorf("module %s not loaded in pid %d within %s", moduleName, pid, timeout)
}

func findImportAddress(process syscall.Handle, moduleBase uintptr, libraryName, symbolName string) (uintptr, error) {
	header, ok := readRemote(process, moduleBase, 4096)
	if !ok || len(header) < 0x100 || header[0] != 'M' || header[1] != 'Z' {
		return 0, fmt.Errorf("invalid remote PE header")
	}
	peOffset := int(binary.LittleEndian.Uint32(header[0x3C:0x40]))
	optional := peOffset + 24
	if optional+112 > len(header) || binary.LittleEndian.Uint16(header[optional:optional+2]) != 0x10B {
		return 0, fmt.Errorf("remote module is not PE32")
	}
	importRVA := binary.LittleEndian.Uint32(header[optional+104 : optional+108])
	if importRVA == 0 {
		return 0, fmt.Errorf("remote module has no import directory")
	}
	for descriptorIndex := 0; descriptorIndex < 512; descriptorIndex++ {
		descriptorAddress := moduleBase + uintptr(importRVA) + uintptr(descriptorIndex*20)
		descriptor, ok := readRemote(process, descriptorAddress, 20)
		if !ok {
			return 0, fmt.Errorf("read import descriptor %d", descriptorIndex)
		}
		originalThunk := binary.LittleEndian.Uint32(descriptor[0:4])
		nameRVA := binary.LittleEndian.Uint32(descriptor[12:16])
		firstThunk := binary.LittleEndian.Uint32(descriptor[16:20])
		if originalThunk == 0 && nameRVA == 0 && firstThunk == 0 {
			break
		}
		remoteLibrary, ok := readCString(process, moduleBase+uintptr(nameRVA), 512)
		if !ok || !strings.EqualFold(remoteLibrary, libraryName) {
			continue
		}
		lookupThunk := originalThunk
		if lookupThunk == 0 {
			lookupThunk = firstThunk
		}
		for index := 0; index < 16384; index++ {
			entry, ok := readRemote(process, moduleBase+uintptr(lookupThunk)+uintptr(index*4), 4)
			if !ok {
				return 0, fmt.Errorf("read import thunk %d", index)
			}
			value := binary.LittleEndian.Uint32(entry)
			if value == 0 {
				break
			}
			if value&0x80000000 != 0 {
				continue
			}
			name, ok := readCString(process, moduleBase+uintptr(value)+2, 512)
			if ok && name == symbolName {
				return moduleBase + uintptr(firstThunk) + uintptr(index*4), nil
			}
		}
		return 0, fmt.Errorf("symbol %s not found in %s imports", symbolName, libraryName)
	}
	return 0, fmt.Errorf("library %s not found in imports", libraryName)
}

func findImportAddressOrdinal(process syscall.Handle, moduleBase uintptr, libraryName string, ordinal uint16) (uintptr, error) {
	header, ok := readRemote(process, moduleBase, 4096)
	if !ok || len(header) < 0x100 || header[0] != 'M' || header[1] != 'Z' {
		return 0, fmt.Errorf("invalid remote PE header")
	}
	peOffset := int(binary.LittleEndian.Uint32(header[0x3C:0x40]))
	optional := peOffset + 24
	if optional+112 > len(header) || binary.LittleEndian.Uint16(header[optional:optional+2]) != 0x10B {
		return 0, fmt.Errorf("remote module is not PE32")
	}
	importRVA := binary.LittleEndian.Uint32(header[optional+104 : optional+108])
	if importRVA == 0 {
		return 0, fmt.Errorf("remote module has no import directory")
	}
	for descriptorIndex := 0; descriptorIndex < 512; descriptorIndex++ {
		descriptorAddress := moduleBase + uintptr(importRVA) + uintptr(descriptorIndex*20)
		descriptor, ok := readRemote(process, descriptorAddress, 20)
		if !ok {
			return 0, fmt.Errorf("read import descriptor %d", descriptorIndex)
		}
		originalThunk := binary.LittleEndian.Uint32(descriptor[0:4])
		nameRVA := binary.LittleEndian.Uint32(descriptor[12:16])
		firstThunk := binary.LittleEndian.Uint32(descriptor[16:20])
		if originalThunk == 0 && nameRVA == 0 && firstThunk == 0 {
			break
		}
		remoteLibrary, ok := readCString(process, moduleBase+uintptr(nameRVA), 512)
		if !ok || !strings.EqualFold(remoteLibrary, libraryName) {
			continue
		}
		lookupThunk := originalThunk
		if lookupThunk == 0 {
			lookupThunk = firstThunk
		}
		for index := 0; index < 16384; index++ {
			entry, ok := readRemote(process, moduleBase+uintptr(lookupThunk)+uintptr(index*4), 4)
			if !ok {
				return 0, fmt.Errorf("read import thunk %d", index)
			}
			value := binary.LittleEndian.Uint32(entry)
			if value == 0 {
				break
			}
			if value&0x80000000 != 0 && uint16(value&0xFFFF) == ordinal {
				return moduleBase + uintptr(firstThunk) + uintptr(index*4), nil
			}
		}
		return 0, fmt.Errorf("ordinal %d not found in %s imports", ordinal, libraryName)
	}
	return 0, fmt.Errorf("library %s not found in imports", libraryName)
}

func readRemote(process syscall.Handle, address uintptr, size int) ([]byte, bool) {
	buffer := make([]byte, size)
	var read uintptr
	success, _, _ := procReadProcessMemory.Call(uintptr(process), address, uintptr(unsafe.Pointer(&buffer[0])), uintptr(size), uintptr(unsafe.Pointer(&read)))
	if success == 0 || read == 0 {
		return nil, false
	}
	return buffer[:read], true
}

func readCString(process syscall.Handle, address uintptr, limit int) (string, bool) {
	data, ok := readRemote(process, address, limit)
	if !ok {
		return "", false
	}
	for index, value := range data {
		if value == 0 {
			return string(data[:index]), true
		}
	}
	return "", false
}

func writeRemote(process syscall.Handle, address uintptr, data []byte) error {
	var written uintptr
	success, _, callErr := procWriteProcessMemory.Call(
		uintptr(process), address, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(unsafe.Pointer(&written)),
	)
	if success == 0 || written != uintptr(len(data)) {
		return fmt.Errorf("WriteProcessMemory wrote %d/%d: %w", written, len(data), callErr)
	}
	return nil
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

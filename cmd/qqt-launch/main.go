package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"qqtang/internal/tooling/winlaunch"
)

type pageList []uintptr

func (pages *pageList) String() string {
	values := make([]string, 0, len(*pages))
	for _, page := range *pages {
		values = append(values, fmt.Sprintf("0x%X", page))
	}
	return strings.Join(values, ",")
}

func (pages *pageList) Set(value string) error {
	parsed, err := strconv.ParseUint(value, 0, 32)
	if err != nil {
		return err
	}
	*pages = append(*pages, uintptr(parsed))
	return nil
}

func main() {
	executable := flag.String("exe", "runtime/client-patched/Client.exe", "client executable")
	workingDir := flag.String("cwd", "runtime/client-patched", "client working directory")
	wait := flag.Duration("wait", 5*time.Second, "time to wait before reporting the client as still running")
	outputPath := flag.String("out", "", "optional JSON result path (stdout when empty)")
	localCompat := flag.Bool("local-compat", false, "use the verified Windows 10/11 local TP compatibility set without loading the legacy driver")
	tpFree := flag.Bool("tp-free", false, "launch a reconstructed client that has no ClientBase/TerSafe imports")
	fixTPContextRead := flag.Bool("fix-tp-context-read", false, "compatibility: preserve a readable ClientBase startup context, otherwise supply its verified zero default")
	fixTPEarlySystemScan := flag.Bool("fix-tp-early-system-scan", false, "experimental: bypass the obsolete system-header write and emulate its unreadable memory scan")
	fixTPEarlySystemHeader := flag.Bool("fix-tp-early-system-header", false, "compatibility: temporarily expose and then restore the parsed KERNELBASE BaseOfCode scratch field")
	fixLocalShopOrdering := flag.Bool("fix-local-shop-ordering", false, "diagnostic opt-in: rewrite the legacy loopback shop socket callback ordering")
	suppressTPWarning := flag.Bool("suppress-tp-warning", false, "replace ClientBase MessageBoxA import with an in-memory IDOK return stub")
	disableIME := flag.Bool("disable-ime", false, "disable IME loading for the suspended client main thread")
	trapExit := flag.Bool("trap-exit", false, "freeze ClientBase ExitProcess and RtlExitUserProcess without intercepting arbitrary NtTerminateProcess targets")
	bypassNativeTerminate := flag.Bool("bypass-native-terminate", false, "explicit legacy diagnostic: make every ntdll NtTerminateProcess call return success")
	handleTPBreakpoints := flag.Bool("handle-tp-breakpoints", false, "install a narrow x86 VEH for WOW64/INT3 protection sentinels")
	dialogReturn := flag.Int("dialog-return", 1, "DialogBoxParamA return value used by broad suppression or the exact TP dialog bypass")
	trapWarningFormat := flag.Bool("trap-warning-format", false, "redirect ClientBase wsprintfA import to an infinite-wait diagnostic stub")
	trapTPDialogEntry := flag.Bool("trap-tp-dialog-entry", false, "record and freeze only the verified ClientBase+0x7BBEF4 private DialogBoxParamA call")
	traceTPPostDialog := flag.Bool("trace-tp-post-dialog", false, "return IDOK from the verified TP dialog call, then freeze at ClientBase+0x6CFF15 after the real frame unwinds")
	traceTPPostDialogVM := flag.Bool("trace-tp-post-dialog-vm", false, "after the exact TP dialog bypass, retain the last 128 protected dispatcher transitions until exit")
	bypassTPPostReturn := flag.Bool("bypass-tp-post-dialog-return", false, "after the exact TP dialog bypass, recover through the verified outer +0x70708B entry")
	tpPostReturnMode := flag.String("tp-post-dialog-recovery", "full", "outer recovery stack mode: full (object and two markers), drop (two markers), or return (consume first marker)")
	sampleTPPostDialog := flag.Bool("sample-tp-post-dialog-thread", false, "after the exact TP dialog bypass, sample only the captured warning thread until exit")
	sampleTPPostProcess := flag.Bool("sample-tp-post-dialog-process", false, "after the exact TP dialog bypass, sample every current client thread until exit")
	trapTPPostTerminate := flag.Bool("trap-tp-post-dialog-terminate", false, "after the exact TP dialog bypass, record and freeze the first status-0 NtTerminateProcess call")
	bypassTPPostTerminate := flag.Bool("bypass-tp-post-dialog-terminate", false, "after the exact TP dialog bypass, return success from status-0 NtTerminateProcess calls")
	trapTPPostAccessViolation := flag.Bool("trap-tp-post-dialog-access-violation", false, "after the exact TP dialog bypass, record and freeze the first x86 access violation while forwarding every pre-dialog violation")
	bypassTPDialogEntry := flag.Bool("bypass-tp-dialog-entry", false, "return IDOK only for the fully verified TP 1008 DialogBoxParamA frame and continue")
	clearTPDialogFrame := flag.Bool("clear-tp-dialog-error-frame", false, "after recording the exact TP 1008 dialog frame, clear only its verified 31000/1008/3 stack slots")
	trapWarningBuffer := flag.Bool("trap-warning-buffer", false, "suspend the main thread when the fixed GBK warning-title stack buffer is populated")
	bypassTPFailureFrame := flag.Bool("bypass-tp-failure-frame", false, "conditionally unwind the verified ClientBase error (0,5,540) frame instead of terminating")
	bypassTPEntry := flag.Bool("bypass-tp-entry", false, "use a hardware write breakpoint and VEH to skip the verified TP error handler before its first instruction")
	trapTPArgs := flag.Bool("trap-tp-args", false, "late-poll the verified TP (0,5,540) argument frame and suspend the main thread")
	trapTPFirstArg := flag.Bool("trap-tp-first-arg", false, "late-poll the first pushed TP argument (540) and suspend before the handler call")
	spoofWin7 := flag.Bool("spoof-win7", false, "report Windows 7 SP1 to ClientBase GetVersionExA inside the client process")
	bypassTPErrorHandler := flag.Bool("bypass-tp-error-handler", false, "skip the verified ClientBase fatal handler only for TP error (0,5,540)")
	traceTPDispatcher := flag.Bool("trace-tp-dispatcher", false, "trace the verified TP dispatcher; with -bypass-tp-dispatcher, retain the last 128 post-bypass transitions")
	traceTPPreError := flag.Bool("trace-tp-pre-error", false, "retain the main thread's last 128 dispatcher transitions before the first TP warning")
	traceTPWorkerVM := flag.Bool("trace-tp-worker-vm", false, "retain the verified TP worker thread's last 128 protected dispatcher transitions")
	bypassTPDispatcher := flag.Bool("bypass-tp-dispatcher", false, "skip TP (0,5,540) at its earliest verified dispatcher state")
	bypassTPSecondError := flag.Bool("bypass-tp-second-dispatcher", false, "skip post-540 TP (3,1006,19008) using its verified outer VM state")
	bypassTPThirdError := flag.Bool("bypass-tp-third-dispatcher", false, "skip post-1006 TP (3,1032,19008) using its verified outer VM state")
	tpThirdReturnEAX := flag.String("tp-third-return-eax", "", "optional diagnostic EAX value restored by the third TP dispatcher bypass")
	trapTPSecondError := flag.Bool("trap-tp-second-error", false, "suspend when the post-540 TP error (3,1006,19008) frame appears")
	trapAccessViolation := flag.Bool("trap-access-violation", false, "record and freeze the original context of the first x86 access violation")
	traceExceptions := flag.Bool("trace-exceptions", false, "transparently retain the last 32 x86 exceptions without handling them")
	traceStartupDebugger := flag.Bool("trace-startup-debugger", false, "diagnostic: attach a controller-side debugger before releasing the ClientBase startup gate and retain x86 exceptions")
	traceTPThirdVM := flag.Bool("trace-tp-third-vm", false, "retain the last 128 transitions of the VM entered after the third TP recovery")
	traceModuleHandle := flag.Bool("trace-get-module-handle", false, "record ClientBase GetModuleHandleA name pointers and returned handles")
	traceTPSystemAPIs := flag.Bool("trace-tp-system-apis", false, "record TP service/device API return values, arguments, and GetLastError")
	clearAdjustTokenError := flag.Bool("clear-adjust-token-error", false, "diagnostic: clear ERROR_NOT_ALL_ASSIGNED after successful AdjustTokenPrivileges")
	clearAdjustTokenCall := flag.Uint("clear-adjust-token-error-call", 0, "limit -clear-adjust-token-error to this 1-based call (0 means every call)")
	bypassTesSafeDriver := flag.Bool("bypass-tes-safe-driver", false, "diagnostic: replace only the legacy TesSafe device with a user-mode compatibility handle")
	traceTesSafeVM := flag.Bool("trace-tessafe-vm", false, "retain the main thread's last 128 transitions through the verified TesSafe VM call bridge")
	traceTesSafeIOReturn := flag.Bool("trace-tessafe-io-return", false, "transparently retain the last 128 DeviceIoControl return contexts")
	traceTesSafeConsumer := flag.Bool("trace-tessafe-consumer", false, "transparently capture 256 bytes of the protected helper context and the complete 1024-byte decoded object")
	traceTesSafeChecksum := flag.Bool("trace-tessafe-checksum", false, "transparently retain the checksum result and stack before the verified TesSafe cleanup tail")
	traceClientBaseEntry := flag.Bool("trace-clientbase-entry", false, "transparently capture Client.exe calls through its sole ClientBase ordinal-1 import")
	traceClientBaseThreads := flag.Bool("trace-clientbase-threads", false, "transparently retain the last 16 ClientBase CreateThread calls and returned thread IDs")
	bypassClientBaseDLLMain := flag.Bool("bypass-clientbase-dllmain", false, "diagnostic: return TRUE from the in-memory ClientBase PE entry while the early loader gate is held")
	bypassTPWorkerThread := flag.Bool("bypass-tp-worker-thread", false, "diagnostic: return only from the verified ClientBase+0x70708B TP dialog worker thread")
	zeroTPWorkerErrorClass := flag.Bool("zero-tp-worker-error-class", false, "diagnostic: change only the traced final TP VM error-class immediate from 3 to 0")
	bypassTPWorkerFatal := flag.Bool("bypass-tp-worker-fatal-exception", false, "compatibility: end only the armed TP worker on either verified terminal BOUND or read-fault form")
	clearTesSafeStatus100 := flag.Bool("clear-tessafe-status-minus-100", false, "diagnostic: clear only the verified TesSafe context+0x10 status when it is signed -100")
	captureClientBaseRVA := flag.String("capture-clientbase-rva", "", "read-only diagnostic: capture bytes at this ClientBase RVA after the wait interval")
	captureClientBaseSize := flag.Uint("capture-clientbase-size", 128, "number of bytes for -capture-clientbase-rva (maximum 4096)")
	traceTPEarlySystemState := flag.Bool("trace-tp-early-system-state", false, "read-only diagnostic: capture KERNELBASE+0x114 and the page it references after startup")
	bypassTPPostHandshake := flag.Bool("bypass-tp-post-handshake", false, "diagnostic: skip the verified TP-only post-handshake call that consumes unavailable driver state")
	traceKernelETWWrapper := flag.Bool("trace-kernel-etw-wrapper", false, "diagnostic: transparently record the caller and ECX entering the faulting 32-bit kernel32 ETW wrapper")
	traceTPETWCallerPath := flag.Bool("trace-tp-etw-caller-path", false, "diagnostic: include the bounded final TP worker contexts leading to the ClientBase ETW call boundary")
	bypassTPETWRegister := flag.Bool("bypass-tp-etw-registration", false, "compatibility: skip only the TP call whose stack continuation is ClientBase+0x3FBB8, before entering Windows-private code")
	bypassTPFinalError := flag.Bool("bypass-tp-final-error", false, "disabled disproved diagnostic: the former tuple is python23 metadata, not TP state")
	gateClientBase := flag.Bool("gate-clientbase", false, "diagnostic: suspend the main thread after ClientBase imports resolve while installing hooks")
	gateUnpackedEntry := flag.Bool("gate-unpacked-entry", false, "diagnostic: freeze Client.exe at its restored CRT entry before the first instruction executes")
	allowMultiClient := flag.Bool("allow-multi-client", false, "allow another local Client.exe by bypassing only Core.dll's verified QQTangWinClass existing-instance exit branch")
	highResolutionTimer := flag.Bool("high-resolution-timer", false, "request 1 ms Windows timer precision inside Client.exe for its native limitfps loop")
	tpContextValue := flag.String("tp-context-value", "", "optional startup DWORD written to the reserved TP context address 0x210200F0")
	var pages pageList
	flag.Var(&pages, "reserve-page", "page-aligned address to reserve in the suspended child (repeatable)")
	flag.Parse()
	var thirdReturnEAX uint64
	thirdReturnEAXSet := *tpThirdReturnEAX != ""
	if thirdReturnEAXSet {
		var err error
		thirdReturnEAX, err = strconv.ParseUint(*tpThirdReturnEAX, 0, 32)
		if err != nil {
			fmt.Fprintln(os.Stderr, "qqt-launch: invalid -tp-third-return-eax:", err)
			os.Exit(2)
		}
	}
	var contextValue uint64
	contextValueSet := *tpContextValue != ""
	if contextValueSet {
		var err error
		contextValue, err = strconv.ParseUint(*tpContextValue, 0, 32)
		if err != nil {
			fmt.Fprintln(os.Stderr, "qqt-launch: invalid -tp-context-value:", err)
			os.Exit(2)
		}
	}
	var clientBaseRVA uint64
	clientBaseRVASet := *captureClientBaseRVA != ""
	if clientBaseRVASet {
		var err error
		clientBaseRVA, err = strconv.ParseUint(*captureClientBaseRVA, 0, 32)
		if err != nil {
			fmt.Fprintln(os.Stderr, "qqt-launch: invalid -capture-clientbase-rva:", err)
			os.Exit(2)
		}
		if *captureClientBaseSize == 0 || *captureClientBaseSize > 4096 {
			fmt.Fprintln(os.Stderr, "qqt-launch: -capture-clientbase-size must be between 1 and 4096")
			os.Exit(2)
		}
	}
	if *trapTPPostAccessViolation && !*bypassTPDialogEntry {
		fmt.Fprintln(os.Stderr, "qqt-launch: -trap-tp-post-dialog-access-violation requires -bypass-tp-dialog-entry")
		os.Exit(2)
	}
	if *trapTPPostAccessViolation && *trapAccessViolation {
		fmt.Fprintln(os.Stderr, "qqt-launch: -trap-tp-post-dialog-access-violation conflicts with -trap-access-violation")
		os.Exit(2)
	}
	if *bypassTPWorkerFatal && !*zeroTPWorkerErrorClass && !*localCompat {
		fmt.Fprintln(os.Stderr, "qqt-launch: -bypass-tp-worker-fatal-exception requires -zero-tp-worker-error-class")
		os.Exit(2)
	}
	if *traceTPETWCallerPath && !*localCompat {
		fmt.Fprintln(os.Stderr, "qqt-launch: -trace-tp-etw-caller-path requires -local-compat")
		os.Exit(2)
	}
	switch *tpPostReturnMode {
	case "full", "drop", "return":
	default:
		fmt.Fprintln(os.Stderr, "qqt-launch: -tp-post-dialog-recovery must be full, drop, or return")
		os.Exit(2)
	}
	output := os.Stdout
	var outputFile *os.File
	if *outputPath != "" {
		absoluteOutput, err := filepath.Abs(*outputPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "qqt-launch: resolve output path:", err)
			os.Exit(1)
		}
		if err := os.MkdirAll(filepath.Dir(absoluteOutput), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "qqt-launch: create output directory:", err)
			os.Exit(1)
		}
		outputFile, err = os.Create(absoluteOutput)
		if err != nil {
			fmt.Fprintln(os.Stderr, "qqt-launch: create output file:", err)
			os.Exit(1)
		}
		defer outputFile.Close()
		output = outputFile
	}
	_, err := winlaunch.Launch(winlaunch.Options{
		Executable: *executable, WorkingDir: *workingDir, Arguments: flag.Args(),
		TPFree:       *tpFree,
		ReservePages: pages, SuppressTPWarning: *suppressTPWarning, DisableIME: *disableIME,
		TPContextValue: uint32(contextValue), TPContextValueSet: contextValueSet,
		FixTPContextRead:       *fixTPContextRead || *localCompat,
		FixTPEarlySystemScan:   *fixTPEarlySystemScan,
		FixTPEarlySystemHeader: *fixTPEarlySystemHeader || *localCompat,
		TrapExit:               *trapExit, BypassNativeTerminate: *bypassNativeTerminate,
		HandleTPBreakpoints: *handleTPBreakpoints, DialogReturn: int32(*dialogReturn),
		TrapWarningFormat: *trapWarningFormat, TrapTPDialogEntry: *trapTPDialogEntry,
		TraceTPPostDialog:     *traceTPPostDialog,
		TraceTPPostDialogVM:   *traceTPPostDialogVM,
		BypassTPPostReturn:    *bypassTPPostReturn,
		TPPostReturnMode:      *tpPostReturnMode,
		SampleTPPostDialog:    *sampleTPPostDialog,
		SampleTPPostProcess:   *sampleTPPostProcess,
		TrapTPPostTerminate:   *trapTPPostTerminate,
		BypassTPPostTerminate: *bypassTPPostTerminate,
		TrapTPPostAccessAV:    *trapTPPostAccessViolation,
		BypassTPDialogEntry:   *bypassTPDialogEntry,
		ClearTPDialogFrame:    *clearTPDialogFrame,
		TrapWarningBuffer:     *trapWarningBuffer,
		BypassTPFailureFrame:  *bypassTPFailureFrame, BypassTPEntry: *bypassTPEntry,
		TrapTPArgs: *trapTPArgs, TrapTPFirstArg: *trapTPFirstArg,
		SpoofWin7:               *spoofWin7,
		BypassTPErrorHandler:    *bypassTPErrorHandler,
		TraceTPDispatcher:       *traceTPDispatcher,
		TraceTPPreError:         *traceTPPreError,
		TraceTPWorkerVM:         *traceTPWorkerVM,
		BypassTPDispatcher:      *bypassTPDispatcher,
		BypassTPSecondError:     *bypassTPSecondError,
		BypassTPThirdError:      *bypassTPThirdError,
		TPThirdReturnEAX:        uint32(thirdReturnEAX),
		TPThirdReturnEAXSet:     thirdReturnEAXSet,
		TrapTPSecondError:       *trapTPSecondError,
		TrapAccessViolation:     *trapAccessViolation,
		TraceExceptions:         *traceExceptions,
		TraceStartupDebugger:    *traceStartupDebugger,
		TraceTPThirdVM:          *traceTPThirdVM,
		TraceModuleHandle:       *traceModuleHandle,
		TraceTPSystemAPIs:       *traceTPSystemAPIs,
		ClearAdjustTokenError:   *clearAdjustTokenError || *localCompat,
		ClearAdjustTokenCall:    uint32(*clearAdjustTokenCall),
		BypassTesSafeDriver:     *bypassTesSafeDriver || *localCompat,
		TraceTesSafeVM:          *traceTesSafeVM,
		TraceTesSafeIOReturn:    *traceTesSafeIOReturn,
		TraceTesSafeConsumer:    *traceTesSafeConsumer,
		TraceTesSafeChecksum:    *traceTesSafeChecksum,
		TraceClientBaseEntry:    *traceClientBaseEntry,
		TraceClientBaseThreads:  *traceClientBaseThreads,
		BypassClientBaseDLLMain: *bypassClientBaseDLLMain,
		BypassTPWorkerThread:    *bypassTPWorkerThread,
		ZeroTPWorkerErrorClass:  *zeroTPWorkerErrorClass || *localCompat,
		BypassTPWorkerFatal:     *bypassTPWorkerFatal || *localCompat,
		ClearTesSafeStatus100:   *clearTesSafeStatus100,
		CaptureClientBaseRVA:    uint32(clientBaseRVA),
		CaptureClientBaseSet:    clientBaseRVASet,
		CaptureClientBaseSize:   uint32(*captureClientBaseSize),
		TraceTPEarlySystemState: *traceTPEarlySystemState,
		BypassTPPostHandshake:   *bypassTPPostHandshake,
		TraceKernelETWWrapper:   *traceKernelETWWrapper,
		TraceTPETWCallerPath:    *traceTPETWCallerPath,
		BypassTPETWRegister:     *bypassTPETWRegister || *localCompat,
		BypassTPFinalError:      *bypassTPFinalError,
		GateClientBase:          *gateClientBase,
		GateUnpackedEntry:       *gateUnpackedEntry,
		AllowMultiClient:        *allowMultiClient,
		FixLocalShopOrdering:    *fixLocalShopOrdering,
		HighResolutionTimer:     *highResolutionTimer,
		Wait:                    *wait, Output: output,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "qqt-launch:", err)
		os.Exit(1)
	}
}

package winlaunch

import (
	"encoding/binary"
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	tpETWRepairPageSize     = uintptr(0x2000)
	tpETWRepairRecordOffset = uintptr(0x800)
	tpETWRepairObjectOffset = uintptr(0xA00)
	tpETWRepairDescOffset   = uintptr(0xC00)
	tpETWRepairInstallOff   = uintptr(0xE00)
)

type TPETWCallBoundaryEvent struct {
	Index  uint32 `json:"index"`
	EIP    string `json:"eip"`
	Module string `json:"module,omitempty"`
	RVA    string `json:"rva,omitempty"`
	ESP    string `json:"esp"`
	ECX    string `json:"ecx"`
	EFlags string `json:"eflags"`
}

// TPETWCallBoundaryCapture is the diagnostic result shared by the active
// provider-repair path and launcher output.
type TPETWCallBoundaryCapture struct {
	PID               uint32                   `json:"pid"`
	ThreadID          uint32                   `json:"thread_id"`
	EntryAddress      string                   `json:"entry_address"`
	ReturnAddress     string                   `json:"return_address"`
	ElapsedMS         int64                    `json:"elapsed_ms"`
	TotalSteps        uint32                   `json:"total_steps"`
	Completed         bool                     `json:"completed"`
	Bypassed          bool                     `json:"bypassed"`
	StepLimit         bool                     `json:"step_limit"`
	CallTarget        string                   `json:"call_target,omitempty"`
	CallTargetModule  string                   `json:"call_target_module,omitempty"`
	CallTargetRVA     string                   `json:"call_target_rva,omitempty"`
	CallESP           string                   `json:"call_esp,omitempty"`
	CallECX           string                   `json:"call_ecx,omitempty"`
	ArmEIP            string                   `json:"arm_eip,omitempty"`
	ArmEFlags         string                   `json:"arm_eflags,omitempty"`
	ArmDR0            string                   `json:"arm_dr0,omitempty"`
	ArmDR7            string                   `json:"arm_dr7,omitempty"`
	ResumePrevious    uint32                   `json:"resume_previous,omitempty"`
	ProcessExited     bool                     `json:"process_exited,omitempty"`
	ProcessExitCode   string                   `json:"process_exit_code,omitempty"`
	StackMatchIndex   uint32                   `json:"stack_match_index,omitempty"`
	StackMatchOffset  uint32                   `json:"stack_match_offset,omitempty"`
	StackMatchEIP     string                   `json:"stack_match_eip,omitempty"`
	StackMatchESP     string                   `json:"stack_match_esp,omitempty"`
	FaultCode         string                   `json:"fault_code,omitempty"`
	FaultEIP          string                   `json:"fault_eip,omitempty"`
	FaultESP          string                   `json:"fault_esp,omitempty"`
	FaultFirstChance  uint32                   `json:"fault_first_chance,omitempty"`
	FaultAccessType   string                   `json:"fault_access_type,omitempty"`
	FaultAccess       string                   `json:"fault_access_address,omitempty"`
	FaultStack        []string                 `json:"fault_stack,omitempty"`
	PointerRegisters  []string                 `json:"pointer_registers,omitempty"`
	PointerOriginal   string                   `json:"pointer_original,omitempty"`
	PointerReplace    string                   `json:"pointer_replacement,omitempty"`
	ProviderPage      string                   `json:"provider_page,omitempty"`
	SystemReturnEAX   string                   `json:"system_return_eax,omitempty"`
	VEHExceptionCount uint32                   `json:"veh_exception_count,omitempty"`
	VEHStage          uint32                   `json:"veh_stage,omitempty"`
	VEHLastCode       string                   `json:"veh_last_code,omitempty"`
	VEHLastThreadID   uint32                   `json:"veh_last_thread_id,omitempty"`
	VEHLastEIP        string                   `json:"veh_last_eip,omitempty"`
	VEHLastESP        string                   `json:"veh_last_esp,omitempty"`
	VEHLastAccessType string                   `json:"veh_last_access_type,omitempty"`
	VEHLastAccess     string                   `json:"veh_last_access_address,omitempty"`
	VEHRegisters      map[string]string        `json:"veh_registers,omitempty"`
	Events            []TPETWCallBoundaryEvent `json:"last_events,omitempty"`
	Behavior          string                   `json:"behavior"`
}

type x86RelativeFixup struct {
	displacement int
	label        string
}

type x86Program struct {
	code   []byte
	labels map[string]int
	fixups []x86RelativeFixup
}

func newX86Program() *x86Program {
	return &x86Program{labels: make(map[string]int)}
}

func (program *x86Program) emit(values ...byte) {
	program.code = append(program.code, values...)
}

func (program *x86Program) imm32(value uint32) {
	program.code = binary.LittleEndian.AppendUint32(program.code, value)
}

func (program *x86Program) label(name string) {
	program.labels[name] = len(program.code)
}

func (program *x86Program) jcc(condition byte, label string) {
	program.emit(0x0F, condition)
	program.fixups = append(program.fixups, x86RelativeFixup{displacement: len(program.code), label: label})
	program.imm32(0)
}

func (program *x86Program) jmp(label string) {
	program.emit(0xE9)
	program.fixups = append(program.fixups, x86RelativeFixup{displacement: len(program.code), label: label})
	program.imm32(0)
}

func (program *x86Program) resolve() ([]byte, error) {
	for _, fixup := range program.fixups {
		target, ok := program.labels[fixup.label]
		if !ok {
			return nil, fmt.Errorf("unresolved x86 label %q", fixup.label)
		}
		delta := int64(target) - int64(fixup.displacement+4)
		if delta < -0x80000000 || delta > 0x7FFFFFFF {
			return nil, fmt.Errorf("x86 label %q is out of range", fixup.label)
		}
		binary.LittleEndian.PutUint32(program.code[fixup.displacement:fixup.displacement+4], uint32(int32(delta)))
	}
	return program.code, nil
}

type tpETWRepairModule struct {
	base uint32
	end  uint32
}

func buildTPETWProviderRepairVEH(
	threadID uint32,
	returnAddress uint32,
	kernel32Range, kernelBaseRange tpETWRepairModule,
	recordAddress, providerObject uint32,
) ([]byte, error) {
	program := newX86Program()
	program.emit(
		0x55, 0x8B, 0xEC, // push ebp; mov ebp,esp
		0x53, 0x56, 0x57, // save ebx,esi,edi
		0x8B, 0x45, 0x08, // mov eax,[ebp+8] (EXCEPTION_POINTERS)
		0x8B, 0x10, // mov edx,[eax] (EXCEPTION_RECORD)
		0x8B, 0x48, 0x04, // mov ecx,[eax+4] (CONTEXT)
	)
	// Retain the last exception and deepest predicate stage even when the
	// process terminates before a repair is accepted.
	program.emit(0xFF, 0x05)
	program.imm32(recordAddress + 36)
	program.emit(0x8B, 0x02, 0xA3)
	program.imm32(recordAddress + 40)
	program.emit(0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0xA3)
	program.imm32(recordAddress + 44)
	program.emit(0x8B, 0x81)
	program.imm32(wow64ContextEIP)
	program.emit(0xA3)
	program.imm32(recordAddress + 48)
	program.emit(0x8B, 0x81)
	program.imm32(wow64ContextESP)
	program.emit(0xA3)
	program.imm32(recordAddress + 52)
	program.emit(0x8B, 0x42, 0x14, 0xA3)
	program.imm32(recordAddress + 56)
	program.emit(0x8B, 0x42, 0x18, 0xA3)
	program.imm32(recordAddress + 60)
	for index, offset := range []uint32{
		wow64ContextEDI, wow64ContextESI, wow64ContextEBX,
		wow64ContextEDX, wow64ContextECX, wow64ContextEAX,
	} {
		program.emit(0x8B, 0x81)
		program.imm32(offset)
		program.emit(0xA3)
		program.imm32(recordAddress + 68 + uint32(index*4))
	}
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 64)
	program.imm32(1)
	program.emit(0x81, 0x3A, 0x05, 0x00, 0x00, 0xC0) // cmp [edx],STATUS_ACCESS_VIOLATION
	program.jcc(0x85, "forward")                     // jne
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 64)
	program.imm32(2)
	program.emit(0x83, 0x7A, 0x10, 0x02) // cmp NumberParameters,2
	program.jcc(0x85, "forward")
	program.emit(0x83, 0x7A, 0x14, 0x00) // cmp ExceptionInformation[0],read
	program.jcc(0x85, "forward")
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 64)
	program.imm32(3)
	program.emit(0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0x3D) // mov eax,fs:[24h]; cmp eax,threadID
	program.imm32(threadID)
	program.jcc(0x85, "forward")
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 64)
	program.imm32(4)

	program.emit(0x8B, 0x81, 0xB8, 0x00, 0x00, 0x00, 0x3D) // mov eax,[ecx+Eip]; cmp eax,kernel32.base
	program.imm32(kernel32Range.base)
	program.jcc(0x82, "check_kernelbase") // jb
	program.emit(0x3D)
	program.imm32(kernel32Range.end)
	program.jcc(0x82, "module_ok") // jb
	program.label("check_kernelbase")
	program.emit(0x3D)
	program.imm32(kernelBaseRange.base)
	program.jcc(0x82, "forward")
	program.emit(0x3D)
	program.imm32(kernelBaseRange.end)
	program.jcc(0x83, "forward") // jae
	program.label("module_ok")
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 64)
	program.imm32(5)

	// Scan stack offsets +4..+0xFC. Exactly one fixed ClientBase return marker
	// must be present; [ESP] is intentionally excluded.
	program.emit(
		0x8B, 0xB1, 0xC4, 0x00, 0x00, 0x00, // mov esi,[ecx+Esp]
		0x8D, 0x7E, 0x04, // lea edi,[esi+4]
		0x33, 0xDB, // xor ebx,ebx
	)
	program.label("stack_scan")
	program.emit(0x81, 0x3F)
	program.imm32(returnAddress)
	program.jcc(0x85, "stack_next")
	program.emit(0x43, 0x83, 0xFB, 0x01)       // inc ebx; cmp ebx,1
	program.jcc(0x87, "forward")               // ja
	program.emit(0x8B, 0xC7, 0x2B, 0xC6, 0xA3) // mov eax,edi; sub eax,esi; mov [offset],eax
	program.imm32(recordAddress + 20)
	program.label("stack_next")
	program.emit(
		0x83, 0xC7, 0x04, // add edi,4
		0x8B, 0xC6, // mov eax,esi
		0x05, 0x00, 0x01, 0x00, 0x00, // add eax,100h
		0x3B, 0xF8, // cmp edi,eax
	)
	program.jcc(0x82, "stack_scan") // jb
	program.emit(0x83, 0xFB, 0x01)
	program.jcc(0x85, "forward")
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 64)
	program.imm32(6)

	// Find a context register whose value plus a small signed displacement is
	// the reported read address. The helper commonly copies ECX into EBX, so all
	// registers holding the same original value are repaired together.
	program.emit(0x8B, 0x7A, 0x18) // mov edi,[edx+18h] (read address)
	registers := []struct {
		name   string
		offset uint32
		bit    uint32
	}{
		{name: "ebx", offset: wow64ContextEBX, bit: 1 << 0},
		{name: "ecx", offset: wow64ContextECX, bit: 1 << 1},
		{name: "eax", offset: wow64ContextEAX, bit: 1 << 2},
		{name: "edx", offset: wow64ContextEDX, bit: 1 << 3},
		{name: "esi", offset: wow64ContextESI, bit: 1 << 4},
		{name: "edi", offset: wow64ContextEDI, bit: 1 << 5},
	}
	for _, register := range registers {
		next := "candidate_next_" + register.name
		program.emit(0x8B, 0x81)
		program.imm32(register.offset) // mov eax,[ecx+offset]
		program.emit(0x85, 0xC0)       // test eax,eax
		program.jcc(0x84, next)        // jz
		program.emit(
			0x8B, 0xDF, // mov ebx,edi
			0x2B, 0xD8, // sub ebx,eax
			0x81, 0xC3, 0x00, 0x01, 0x00, 0x00, // add ebx,100h
			0x81, 0xFB, 0x00, 0x02, 0x00, 0x00, // cmp ebx,200h
		)
		program.jcc(0x86, "candidate_found") // jbe
		program.label(next)
	}
	program.jmp("forward")

	program.label("candidate_found")
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 64)
	program.imm32(7)
	program.emit(0xA3)
	program.imm32(recordAddress + 24) // original pointer
	program.emit(0x8B, 0x99)
	program.imm32(wow64ContextEIP)
	program.emit(0x89, 0x1D)
	program.imm32(recordAddress + 8)
	program.emit(0x8B, 0x99)
	program.imm32(wow64ContextESP)
	program.emit(0x89, 0x1D)
	program.imm32(recordAddress + 12)
	program.emit(0x8B, 0x5A, 0x18, 0x89, 0x1D)
	program.imm32(recordAddress + 16)
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 28)
	program.imm32(providerObject)
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 32)
	program.imm32(0)

	for _, register := range registers {
		skip := "replace_skip_" + register.name
		program.emit(0x39, 0x81)
		program.imm32(register.offset) // cmp [ecx+offset],eax
		program.jcc(0x85, skip)
		program.emit(0xC7, 0x81)
		program.imm32(register.offset)
		program.imm32(providerObject)
		program.emit(0x81, 0x0D)
		program.imm32(recordAddress + 32)
		program.imm32(register.bit)
		program.label(skip)
	}
	program.emit(0xFF, 0x05)
	program.imm32(recordAddress) // inc hit count
	program.emit(0xC7, 0x05)
	program.imm32(recordAddress + 64)
	program.imm32(8)
	program.emit(
		0x5F, 0x5E, 0x5B, // restore edi,esi,ebx
		0x83, 0xC8, 0xFF, // mov eax,-1 (EXCEPTION_CONTINUE_EXECUTION)
		0x5D, 0xC2, 0x04, 0x00,
	)

	program.label("forward")
	program.emit(
		0x5F, 0x5E, 0x5B,
		0x33, 0xC0, // EXCEPTION_CONTINUE_SEARCH
		0x5D, 0xC2, 0x04, 0x00,
	)
	return program.resolve()
}

func runGatedTPETWProviderRepair(
	process syscall.Handle,
	pid, threadID uint32,
	entry, returnAddress, gateFlag uintptr,
	timeout time.Duration,
) (TPETWCallBoundaryCapture, error) {
	started := time.Now()
	result := TPETWCallBoundaryCapture{
		PID: pid, ThreadID: threadID,
		EntryAddress: fmt.Sprintf("0x%08X", entry), ReturnAddress: fmt.Sprintf("0x%08X", returnAddress),
		Behavior: "process-local first-chance AV repair scoped to the gated TP worker, KERNEL32/KERNELBASE module ranges, and one fixed ClientBase continuation; no external debugger or Windows-private code scan",
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	modules := snapshotRemoteModules(process)
	kernel32Range, ok := tpETWNamedModuleRange(modules, "KERNEL32.dll")
	if !ok {
		return result, fmt.Errorf("KERNEL32 module range unavailable")
	}
	kernelBaseRange, ok := tpETWNamedModuleRange(modules, "KERNELBASE.dll")
	if !ok {
		return result, fmt.Errorf("KERNELBASE module range unavailable")
	}
	addHandler, err := findRemoteExport(process, pid, "NTDLL.dll", "RtlAddVectoredExceptionHandler", timeout, 0)
	if err != nil {
		return result, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, tpETWRepairPageSize, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xFFFFFFFF {
		return result, fmt.Errorf("VirtualAllocEx TP ETW provider repair: 0x%X (%v)", page, allocErr)
	}
	record := uint32(page + tpETWRepairRecordOffset)
	providerObject := uint32(page + tpETWRepairObjectOffset)
	descriptor := uint32(page + tpETWRepairDescOffset)
	result.ProviderPage = fmt.Sprintf("0x%08X", page)
	result.PointerReplace = fmt.Sprintf("0x%08X", providerObject)

	handler, err := buildTPETWProviderRepairVEH(
		threadID, uint32(returnAddress), kernel32Range, kernelBaseRange,
		record, providerObject,
	)
	if err != nil {
		procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
		return result, err
	}
	if len(handler) >= int(tpETWRepairRecordOffset) {
		procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
		return result, fmt.Errorf("TP ETW provider repair handler is too large: %d", len(handler))
	}
	installerAddress := page + tpETWRepairInstallOff
	installer := []byte{0x68, 0, 0, 0, 0, 0x6A, 0x01, 0xB8, 0, 0, 0, 0, 0xFF, 0xD0, 0xC2, 0x04, 0x00}
	binary.LittleEndian.PutUint32(installer[1:5], uint32(page))
	binary.LittleEndian.PutUint32(installer[8:12], uint32(addHandler))
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, descriptor)
	guid := []byte{0x7A, 0x49, 0x51, 0x21, 0x4B, 0xA8, 0x5E, 0x43, 0x92, 0x2C, 0xD3, 0x6B, 0x96, 0x0A, 0x6C, 0x71}
	if err := writeRemote(process, page, handler); err != nil {
		return result, fmt.Errorf("write TP ETW provider repair handler: %w", err)
	}
	if err := writeRemote(process, installerAddress, installer); err != nil {
		return result, fmt.Errorf("write TP ETW provider repair installer: %w", err)
	}
	if err := writeRemote(process, uintptr(providerObject)+4, pointer); err != nil {
		return result, fmt.Errorf("write TP ETW provider descriptor pointer: %w", err)
	}
	if err := writeRemote(process, uintptr(descriptor)-16, guid); err != nil {
		return result, fmt.Errorf("write TP ETW provider GUID: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(handler)))
	procFlushInstruction.Call(uintptr(process), installerAddress, uintptr(len(installer)))
	registered, err := remoteCallOne(process, installerAddress, 0, timeout)
	if err != nil {
		return result, err
	}
	if registered == 0 {
		return result, fmt.Errorf("RtlAddVectoredExceptionHandler for TP ETW provider repair returned NULL")
	}

	ready := make([]byte, 4)
	binary.LittleEndian.PutUint32(ready, 1)
	if err := writeRemote(process, gateFlag, ready); err != nil {
		return result, fmt.Errorf("release TP worker startup gate after provider repair: %w", err)
	}

	deadline := time.Now().Add(timeout)
	var values []byte
	var lastValues []byte
	for time.Now().Before(deadline) {
		if current, readOK := readRemote(process, uintptr(record), 104); readOK && len(current) >= 92 {
			lastValues = current
			if binary.LittleEndian.Uint32(current[0:4]) != 0 {
				values = current
				break
			}
		}
		if waitResult, _, _ := procWaitForSingle.Call(uintptr(process), 0); waitResult == waitObject0 {
			var exitCode uint32
			procGetExitCode.Call(uintptr(process), uintptr(unsafePointer(&exitCode)))
			result.ProcessExited = true
			result.ProcessExitCode = fmt.Sprintf("0x%08X", exitCode)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(values) < 36 {
		applyTPETWRepairVEHDiagnostics(&result, lastValues)
		if len(lastValues) >= 56 {
			faultEIP := binary.LittleEndian.Uint32(lastValues[48:52])
			result.CallTarget = fmt.Sprintf("0x%08X", faultEIP)
			result.CallESP = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(lastValues[52:56]))
			moduleName, faultRVA, _ := tpETWFaultModule(faultEIP, modules)
			result.CallTargetModule = moduleName
			result.CallTargetRVA = fmt.Sprintf("0x%X", faultRVA)
		}
		result.ElapsedMS = time.Since(started).Milliseconds()
		return result, fmt.Errorf("TP ETW provider repair did not observe an accepted AV before process exit/timeout: exited=%t exit=%s", result.ProcessExited, result.ProcessExitCode)
	}

	faultEIP := binary.LittleEndian.Uint32(values[8:12])
	faultESP := binary.LittleEndian.Uint32(values[12:16])
	accessAddress := binary.LittleEndian.Uint32(values[16:20])
	result.FaultCode = fmt.Sprintf("0x%08X", statusAccessViolation)
	result.FaultEIP = fmt.Sprintf("0x%08X", faultEIP)
	result.FaultESP = fmt.Sprintf("0x%08X", faultESP)
	result.FaultFirstChance = 1
	result.FaultAccessType = "read"
	result.FaultAccess = fmt.Sprintf("0x%08X", accessAddress)
	result.StackMatchOffset = binary.LittleEndian.Uint32(values[20:24])
	result.StackMatchEIP = result.FaultEIP
	result.StackMatchESP = result.FaultESP
	result.PointerOriginal = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(values[24:28]))
	mask := binary.LittleEndian.Uint32(values[32:36])
	registerNames := []string{"EBX", "ECX", "EAX", "EDX", "ESI", "EDI"}
	for index, name := range registerNames {
		if mask&(1<<index) != 0 {
			result.PointerRegisters = append(result.PointerRegisters, name)
		}
	}
	result.CallTarget = result.FaultEIP
	result.CallESP = result.FaultESP
	moduleName, moduleRVA, _ := tpETWFaultModule(faultEIP, modules)
	result.CallTargetModule = moduleName
	result.CallTargetRVA = fmt.Sprintf("0x%X", moduleRVA)
	if stack, readOK := readRemote(process, uintptr(faultESP), 64*4); readOK {
		for offset := 0; offset+4 <= len(stack); offset += 4 {
			result.FaultStack = append(result.FaultStack, fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(stack[offset:offset+4])))
		}
	}

	// The handler has resumed the faulting instruction. Give the system helper
	// a bounded interval to finish before declaring the repair usable.
	waitResult, _, _ := procWaitForSingle.Call(uintptr(process), 500)
	if waitResult == waitObject0 {
		var exitCode uint32
		procGetExitCode.Call(uintptr(process), uintptr(unsafePointer(&exitCode)))
		result.ProcessExited = true
		result.ProcessExitCode = fmt.Sprintf("0x%08X", exitCode)
		result.ElapsedMS = time.Since(started).Milliseconds()
		return result, fmt.Errorf("client exited after TP ETW provider repair: %s", result.ProcessExitCode)
	}
	result.Completed = true
	result.Bypassed = true
	applyTPETWRepairVEHDiagnostics(&result, values)
	result.ElapsedMS = time.Since(started).Milliseconds()
	return result, nil
}

func applyTPETWRepairVEHDiagnostics(result *TPETWCallBoundaryCapture, values []byte) {
	if len(values) < 92 {
		return
	}
	result.VEHExceptionCount = binary.LittleEndian.Uint32(values[36:40])
	result.VEHLastCode = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(values[40:44]))
	result.VEHLastThreadID = binary.LittleEndian.Uint32(values[44:48])
	result.VEHLastEIP = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(values[48:52]))
	result.VEHLastESP = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(values[52:56]))
	result.VEHLastAccessType = fmt.Sprintf("0x%X", binary.LittleEndian.Uint32(values[56:60]))
	result.VEHLastAccess = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(values[60:64]))
	result.VEHStage = binary.LittleEndian.Uint32(values[64:68])
	result.VEHRegisters = make(map[string]string, 6)
	for index, name := range []string{"EDI", "ESI", "EBX", "EDX", "ECX", "EAX"} {
		value := binary.LittleEndian.Uint32(values[68+index*4 : 72+index*4])
		result.VEHRegisters[name] = fmt.Sprintf("0x%08X", value)
	}
}

func tpETWNamedModuleRange(modules []remoteModuleRange, name string) (tpETWRepairModule, bool) {
	for _, module := range modules {
		if !strings.EqualFold(module.name, name) || module.size == 0 || module.base > ^uint32(0)-module.size {
			continue
		}
		return tpETWRepairModule{base: module.base, end: module.base + module.size}, true
	}
	return tpETWRepairModule{}, false
}

// unsafePointer keeps the only unsafe conversion in the existing launch file's
// helper vocabulary and avoids exposing pointers to injected code.
func unsafePointer(value *uint32) unsafe.Pointer {
	return unsafe.Pointer(value)
}

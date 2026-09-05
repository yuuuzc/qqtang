package winlaunch

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// ShopServerSelectorResult is a scratch-only invocation of the original
// QQTDir GetRandShopServerID interface method. It does not patch the method,
// object, vtable, or any client module byte.
type ShopServerSelectorResult struct {
	PID      uint32 `json:"pid"`
	Object   string `json:"object"`
	Method   string `json:"method"`
	Status   uint32 `json:"status"`
	ServerID uint32 `json:"server_id"`
	Behavior string `json:"behavior"`
}

// ShopServerSelectorTraceResult captures the original selector's instruction
// path while invoking it on a dedicated suspended remote thread. The target
// method and QQTDir object remain unmodified; only temporary scratch memory and
// x86 debug registers on the dedicated thread are used.
type ShopServerSelectorTraceResult struct {
	Selector ShopServerSelectorResult     `json:"selector"`
	Trace    ClientInstructionPathCapture `json:"trace"`
	Return   string                       `json:"return_address"`
}

type ShopServerSelectorCountProbeResult struct {
	Selector     ShopServerSelectorResult `json:"selector"`
	CountAddress string                   `json:"count_address"`
	Original     uint8                    `json:"original_count"`
	Temporary    uint8                    `json:"temporary_count"`
	Restored     bool                     `json:"restored"`
	Behavior     string                   `json:"behavior"`
}

type ShopServerSelectorCountTraceResult struct {
	Selector     ShopServerSelectorTraceResult `json:"selector_trace"`
	CountAddress string                        `json:"count_address"`
	Original     uint8                         `json:"original_count"`
	Temporary    uint8                         `json:"temporary_count"`
	Restored     bool                          `json:"restored"`
	Behavior     string                        `json:"behavior"`
}

const qqtDirShopCandidateCountFromInterface = uintptr(0x56A77)

func CallOriginalShopServerSelector(pid uint32, timeout time.Duration) (ShopServerSelectorResult, error) {
	result := ShopServerSelectorResult{
		PID:      pid,
		Behavior: "scratch-only call of original QQTDir GetRandShopServerID(object, outID); no client or vtable writes",
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	snapshot, err := QueryClientInterfaceSnapshot(pid, 0x3974E0, 23, timeout)
	if err != nil {
		return result, err
	}
	if !snapshot.Available || len(snapshot.Methods) <= 22 {
		return result, fmt.Errorf("QQTDir interface or slot 22 is unavailable")
	}
	object, err := parseShopSelectorAddress(snapshot.Object)
	if err != nil {
		return result, fmt.Errorf("parse QQTDir object: %w", err)
	}
	method, err := parseShopSelectorAddress(snapshot.Methods[22].Address)
	if err != nil {
		return result, fmt.Errorf("parse QQTDir selector method: %w", err)
	}
	result.Object = fmt.Sprintf("0x%08X", object)
	result.Method = fmt.Sprintf("0x%08X", method)

	process, err := syscall.OpenProcess(attachedProcessAccess|processCreateThread, false, pid)
	if err != nil {
		return result, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return result, fmt.Errorf("VirtualAllocEx shop selector call: 0x%X (%v)", page, allocErr)
	}
	defer procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
	outID := page + 0x800
	status := page + 0x804
	stub := buildOriginalShopServerSelectorCall(object, method, outID, status)
	if err := writeRemote(process, page, stub); err != nil {
		return result, fmt.Errorf("write shop selector call: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	if _, err := remoteCallOne(process, page, 0, timeout); err != nil {
		return result, fmt.Errorf("run original shop selector: %w", err)
	}
	values, ok := readRemote(process, outID, 8)
	if !ok || len(values) != 8 {
		return result, fmt.Errorf("read original shop selector result")
	}
	result.ServerID = binary.LittleEndian.Uint32(values[:4])
	result.Status = binary.LittleEndian.Uint32(values[4:8])
	return result, nil
}

func TraceOriginalShopServerSelector(pid uint32, timeout time.Duration) (ShopServerSelectorTraceResult, error) {
	result := ShopServerSelectorTraceResult{}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	snapshot, err := QueryClientInterfaceSnapshot(pid, 0x3974E0, 23, timeout)
	if err != nil {
		return result, err
	}
	if !snapshot.Available || len(snapshot.Methods) <= 22 {
		return result, fmt.Errorf("QQTDir interface or slot 22 is unavailable")
	}
	object, err := parseShopSelectorAddress(snapshot.Object)
	if err != nil {
		return result, fmt.Errorf("parse QQTDir object: %w", err)
	}
	method, err := parseShopSelectorAddress(snapshot.Methods[22].Address)
	if err != nil {
		return result, fmt.Errorf("parse QQTDir selector method: %w", err)
	}
	result.Selector = ShopServerSelectorResult{
		PID: pid, Object: fmt.Sprintf("0x%08X", object), Method: fmt.Sprintf("0x%08X", method),
		Behavior: "scratch-only call of original QQTDir GetRandShopServerID(object, outID); dedicated-thread hardware trace; no client or vtable writes",
	}
	process, err := syscall.OpenProcess(attachedProcessAccess|processCreateThread, false, pid)
	if err != nil {
		return result, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return result, fmt.Errorf("VirtualAllocEx traced shop selector call: 0x%X (%v)", page, allocErr)
	}
	defer procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
	outID := page + 0x800
	status := page + 0x804
	stub, returnOffset := buildOriginalShopServerSelectorTraceCall(object, method, outID, status)
	returnAddress := page + uintptr(returnOffset)
	result.Return = fmt.Sprintf("0x%08X", returnAddress)
	if err := writeRemote(process, page, stub); err != nil {
		return result, fmt.Errorf("write traced shop selector call: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	var threadID uint32
	threadValue, _, createErr := procCreateRemoteThread.Call(
		uintptr(process), 0, 0, page, 0, createSuspended, uintptr(unsafe.Pointer(&threadID)),
	)
	if threadValue == 0 {
		return result, fmt.Errorf("CreateRemoteThread traced shop selector: %w", createErr)
	}
	thread := syscall.Handle(threadValue)
	defer syscall.CloseHandle(thread)
	tracer, err := InstallClientInstructionPathTracer(pid, threadID, method, returnAddress, timeout)
	if err != nil {
		return result, fmt.Errorf("install shop selector instruction trace: %w", err)
	}
	defer tracer.Close()
	previous, _, resumeErr := procResumeThread.Call(threadValue)
	if previous == ^uintptr(0) {
		return result, fmt.Errorf("ResumeThread traced shop selector: %v", resumeErr)
	}
	result.Trace = tracer.Capture(timeout)
	waitMilliseconds := uint32(timeout / time.Millisecond)
	waitResult, _, waitErr := procWaitForSingle.Call(threadValue, uintptr(waitMilliseconds))
	if waitResult != waitObject0 {
		return result, fmt.Errorf("wait traced shop selector thread %d result 0x%X: %w", threadID, waitResult, waitErr)
	}
	values, ok := readRemote(process, outID, 8)
	if !ok || len(values) != 8 {
		return result, fmt.Errorf("read traced shop selector result")
	}
	result.Selector.ServerID = binary.LittleEndian.Uint32(values[:4])
	result.Selector.Status = binary.LittleEndian.Uint32(values[4:8])
	return result, nil
}

// ProbeOriginalShopServerSelectorWithCount temporarily changes only the
// selector's verified candidate-count byte for the duration of one original
// method call. The exact original byte is restored before returning. This is a
// diagnostic for locating the candidate array; it is not a compatibility fix.
func ProbeOriginalShopServerSelectorWithCount(pid uint32, count uint8, timeout time.Duration) (ShopServerSelectorCountProbeResult, error) {
	result := ShopServerSelectorCountProbeResult{
		Temporary: count,
		Behavior:  "single-call diagnostic: temporarily replace verified QQTDir shop-candidate count, invoke original selector, restore exact byte",
	}
	if count == 0 {
		return result, fmt.Errorf("temporary candidate count must be non-zero")
	}
	snapshot, err := QueryClientInterfaceSnapshot(pid, 0x3974E0, 23, timeout)
	if err != nil {
		return result, err
	}
	object, err := parseShopSelectorAddress(snapshot.Object)
	if err != nil || object <= qqtDirShopCandidateCountFromInterface {
		return result, fmt.Errorf("parse QQTDir object: %w", err)
	}
	countAddress := object - qqtDirShopCandidateCountFromInterface
	result.CountAddress = fmt.Sprintf("0x%08X", countAddress)
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return result, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	original, ok := readRemote(process, countAddress, 1)
	if !ok || len(original) != 1 {
		return result, fmt.Errorf("read QQTDir shop candidate count")
	}
	result.Original = original[0]
	if !ensureRemoteBytes(process, countAddress, []byte{count}, pageReadWrite) {
		return result, fmt.Errorf("write temporary QQTDir shop candidate count")
	}
	selector, callErr := CallOriginalShopServerSelector(pid, timeout)
	result.Selector = selector
	if !ensureRemoteBytes(process, countAddress, original, pageReadWrite) {
		return result, fmt.Errorf("restore QQTDir shop candidate count")
	}
	result.Restored = true
	return result, callErr
}

// TraceOriginalShopServerSelectorWithCount is the traced counterpart of the
// one-call count probe. It restores the exact candidate-count byte before it
// returns, including when instruction capture reports an error.
func TraceOriginalShopServerSelectorWithCount(pid uint32, count uint8, timeout time.Duration) (ShopServerSelectorCountTraceResult, error) {
	result := ShopServerSelectorCountTraceResult{
		Temporary: count,
		Behavior:  "single-call diagnostic: temporarily replace verified QQTDir shop-candidate count, trace the original selector, restore exact byte",
	}
	if count == 0 {
		return result, fmt.Errorf("temporary candidate count must be non-zero")
	}
	snapshot, err := QueryClientInterfaceSnapshot(pid, 0x3974E0, 23, timeout)
	if err != nil {
		return result, err
	}
	object, err := parseShopSelectorAddress(snapshot.Object)
	if err != nil || object <= qqtDirShopCandidateCountFromInterface {
		return result, fmt.Errorf("parse QQTDir object: %w", err)
	}
	countAddress := object - qqtDirShopCandidateCountFromInterface
	result.CountAddress = fmt.Sprintf("0x%08X", countAddress)
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return result, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	original, ok := readRemote(process, countAddress, 1)
	if !ok || len(original) != 1 {
		return result, fmt.Errorf("read QQTDir shop candidate count")
	}
	result.Original = original[0]
	if !ensureRemoteBytes(process, countAddress, []byte{count}, pageReadWrite) {
		return result, fmt.Errorf("write temporary QQTDir shop candidate count")
	}
	trace, traceErr := TraceOriginalShopServerSelector(pid, timeout)
	result.Selector = trace
	if !ensureRemoteBytes(process, countAddress, original, pageReadWrite) {
		return result, fmt.Errorf("restore QQTDir shop candidate count")
	}
	result.Restored = true
	return result, traceErr
}

func buildOriginalShopServerSelectorCall(object, method, outID, status uintptr) []byte {
	stub, _ := buildOriginalShopServerSelectorTraceCall(object, method, outID, status)
	return stub
}

func buildOriginalShopServerSelectorTraceCall(object, method, outID, status uintptr) ([]byte, int) {
	stub := []byte{0xC7, 0x05} // mov dword ptr [outID], 0
	stub = binary.LittleEndian.AppendUint32(stub, uint32(outID))
	stub = binary.LittleEndian.AppendUint32(stub, 0)
	stub = appendPushImmediate(stub, outID)
	stub = appendPushImmediate(stub, object)
	stub = append(stub, 0xB8) // mov eax, method; call eax
	stub = binary.LittleEndian.AppendUint32(stub, uint32(method))
	stub = append(stub, 0xFF, 0xD0)
	returnOffset := len(stub)
	stub = append(stub, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(status))
	stub = append(stub, 0x31, 0xC0, 0xC2, 0x04, 0x00)
	return stub, returnOffset
}

func parseShopSelectorAddress(text string) (uintptr, error) {
	value, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(text), "0x"), 16, 32)
	return uintptr(value), err
}

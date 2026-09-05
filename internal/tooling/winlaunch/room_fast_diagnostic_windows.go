package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

const (
	qqtSectionMainVTableRVA       = uintptr(0x54794)
	qqtSectionOnRecvP2PTCPDataRVA = uintptr(0x24097)
	clientRemotePutBombRVA        = uintptr(0x59530)
	clientRemoteMoveRVA           = uintptr(0x595A0)
	roomBindingHookSize           = 9
)

var roomBindingEntrySignature = []byte{0x55, 0x8B, 0xEC, 0x83, 0xEC, 0x44, 0x53, 0x56, 0x57}

// RoomFastDiagnosticResult reports whether QQTSection's native P2P receive
// callback accepts one decrypted QQT_MSG_DATA batch and reaches the waiting-
// room Python bindings. The diagnostic restores both binding prefixes before
// returning and never modifies persistent client files.
type RoomFastDiagnosticResult struct {
	PID             uint32 `json:"pid"`
	SectionID       uint16 `json:"section_id"`
	SourceUIN       uint32 `json:"source_uin"`
	DataLength      int    `json:"data_length"`
	QQTSectionBase  string `json:"qqt_section_base"`
	SectionObject   string `json:"section_object"`
	ReceiveMethod   string `json:"receive_method"`
	ReceiveResult   uint32 `json:"receive_result"`
	ThreadID        uint32 `json:"thread_id,omitempty"`
	ThreadCompleted bool   `json:"thread_completed"`
	ScratchRetained bool   `json:"scratch_retained"`
	CallError       string `json:"call_error,omitempty"`
	RemoteMoveCalls uint32 `json:"remote_move_calls"`
	RemoteBombCalls uint32 `json:"remote_bomb_calls"`
	Behavior        string `json:"behavior"`
}

// DiagnoseRoomFastReceive invokes the proven CMySection::OnRecvP2pTcpData
// callback with one already-decrypted batch. It is intentionally a diagnostic
// boundary: production relaying must still use the native NetCenter transport.
func DiagnoseRoomFastReceive(pid uint32, sectionID uint16, sourceUIN uint32, data []byte, timeout time.Duration) (RoomFastDiagnosticResult, error) {
	return diagnoseRoomFastReceive(pid, 0, sectionID, sourceUIN, data, timeout)
}

// DiagnoseRoomFastReceiveOnThread invokes the receive callback on an existing
// client thread. QQTang dispatches NetCenter socket callbacks on its original
// main thread; the protected QQTPPP parser can synchronously enter MFC code and
// therefore cannot safely be exercised from an arbitrary CreateRemoteThread.
func DiagnoseRoomFastReceiveOnThread(pid, threadID uint32, sectionID uint16, sourceUIN uint32, data []byte, timeout time.Duration) (RoomFastDiagnosticResult, error) {
	if threadID == 0 {
		return RoomFastDiagnosticResult{PID: pid}, fmt.Errorf("thread ID must be non-zero")
	}
	return diagnoseRoomFastReceive(pid, threadID, sectionID, sourceUIN, data, timeout)
}

func diagnoseRoomFastReceive(pid, threadID uint32, sectionID uint16, sourceUIN uint32, data []byte, timeout time.Duration) (RoomFastDiagnosticResult, error) {
	result := RoomFastDiagnosticResult{
		PID: pid, ThreadID: threadID, SectionID: sectionID, SourceUIN: sourceUIN, DataLength: len(data),
		Behavior: "temporary native receive invocation with transparent waiting-room binding counters; all hooks and scratch memory are restored on return",
	}
	if threadID != 0 {
		result.Behavior = "existing-thread native receive invocation with full register/flags preservation and transparent waiting-room binding counters"
	}
	if pid == 0 || sectionID == 0 || sourceUIN == 0 {
		return result, fmt.Errorf("pid, section ID and source UIN must be non-zero")
	}
	if len(data) == 0 || len(data) > 1792 {
		return result, fmt.Errorf("room-fast data length %d is outside 1..1792", len(data))
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	process, err := syscall.OpenProcess(attachedProcessAccess|processCreateThread, false, pid)
	if err != nil {
		return result, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	clientBase, err := waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return result, err
	}
	sectionBase, err := waitForModule(process, pid, "QQTSection.dll", timeout)
	if err != nil {
		return result, err
	}
	result.QQTSectionBase = fmt.Sprintf("0x%08X", sectionBase)
	sectionObject, err := findQQTSectionMainObject(process, pid, sectionBase)
	if err != nil {
		return result, err
	}
	result.SectionObject = fmt.Sprintf("0x%08X", sectionObject)
	receiveMethod := sectionBase + qqtSectionOnRecvP2PTCPDataRVA
	result.ReceiveMethod = fmt.Sprintf("0x%08X", receiveMethod)

	moveAddress := clientBase + clientRemoteMoveRVA
	bombAddress := clientBase + clientRemotePutBombRVA
	for label, address := range map[string]uintptr{"RemotePlayerMove": moveAddress, "RemotePlayerPutBomb": bombAddress} {
		actual, ok := readRemote(process, address, len(roomBindingEntrySignature))
		if !ok || !equalBytes(actual, roomBindingEntrySignature) {
			return result, fmt.Errorf("%s entry signature mismatch at 0x%08X: got %X", label, address, actual)
		}
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x2000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return result, fmt.Errorf("VirtualAllocEx room-fast diagnostic: 0x%X (%v)", page, allocErr)
	}
	freePage := true
	defer func() {
		if freePage {
			procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
		}
	}()
	moveStubAddress := page
	bombStubAddress := page + 0x100
	callStubAddress := page + 0x200
	dataAddress := page + 0x600
	moveCountAddress := page + 0xE00
	bombCountAddress := page + 0xE04
	callStatusAddress := page + 0xE08
	callDoneAddress := page + 0xE0C

	moveStub := buildRoomBindingCounterStub(moveStubAddress, moveAddress, moveCountAddress)
	bombStub := buildRoomBindingCounterStub(bombStubAddress, bombAddress, bombCountAddress)
	callStub := buildRoomFastReceiveCallStub(callStubAddress, receiveMethod, sectionObject, sectionID, sourceUIN, dataAddress, len(data), callStatusAddress)
	for _, write := range []struct {
		label   string
		address uintptr
		data    []byte
	}{
		{"move counter", moveStubAddress, moveStub},
		{"bomb counter", bombStubAddress, bombStub},
		{"receive call", callStubAddress, callStub},
		{"event data", dataAddress, data},
		{"counters", moveCountAddress, []byte{0, 0, 0, 0, 0, 0, 0, 0, 0xFF, 0xFF, 0xFF, 0xFF, 0, 0, 0, 0}},
	} {
		if err := writeRemote(process, write.address, write.data); err != nil {
			return result, fmt.Errorf("write room-fast %s: %w", write.label, err)
		}
	}
	movePatch := relativeJumpPatch(moveAddress, moveStubAddress, roomBindingHookSize)
	bombPatch := relativeJumpPatch(bombAddress, bombStubAddress, roomBindingHookSize)
	if !ensureRemoteBytes(process, moveAddress, movePatch, pageExecuteReadWrite) {
		return result, fmt.Errorf("install RemotePlayerMove diagnostic hook")
	}
	moveInstalled := true
	defer func() {
		if moveInstalled {
			ensureRemoteBytes(process, moveAddress, roomBindingEntrySignature, pageExecuteReadWrite)
		}
	}()
	if !ensureRemoteBytes(process, bombAddress, bombPatch, pageExecuteReadWrite) {
		return result, fmt.Errorf("install RemotePlayerPutBomb diagnostic hook")
	}
	bombInstalled := true
	defer func() {
		if bombInstalled {
			ensureRemoteBytes(process, bombAddress, roomBindingEntrySignature, pageExecuteReadWrite)
		}
	}()
	procFlushInstruction.Call(uintptr(process), page, 0x1000)
	procFlushInstruction.Call(uintptr(process), moveAddress, roomBindingHookSize)
	procFlushInstruction.Call(uintptr(process), bombAddress, roomBindingHookSize)

	var callErr error
	if threadID == 0 {
		_, callErr = remoteCallOne(process, callStubAddress, 0, timeout)
	} else {
		callErr = hijackRoomFastReceiveThread(process, threadID, callStubAddress, receiveMethod, sectionObject, sectionID, sourceUIN, dataAddress, len(data), callStatusAddress, callDoneAddress, timeout)
	}
	if callErr != nil {
		// A protected legacy parser can keep the worker alive while it marshals
		// work to another client thread. Never free executable/data storage from
		// underneath such a worker. The retained 8 KiB dies with Client.exe.
		freePage = false
		result.ScratchRetained = true
		result.CallError = callErr.Error()
	} else {
		result.ThreadCompleted = true
	}
	time.Sleep(500 * time.Millisecond)
	status, ok := readRemote(process, moveCountAddress, 12)
	if !ok || len(status) != 12 {
		return result, fmt.Errorf("read room-fast diagnostic counters")
	}
	result.RemoteMoveCalls = binary.LittleEndian.Uint32(status[0:4])
	result.RemoteBombCalls = binary.LittleEndian.Uint32(status[4:8])
	result.ReceiveResult = binary.LittleEndian.Uint32(status[8:12])

	// Restore synchronously so the caller can safely run another diagnostic.
	if !ensureRemoteBytes(process, bombAddress, roomBindingEntrySignature, pageExecuteReadWrite) {
		return result, fmt.Errorf("restore RemotePlayerPutBomb entry")
	}
	bombInstalled = false
	if !ensureRemoteBytes(process, moveAddress, roomBindingEntrySignature, pageExecuteReadWrite) {
		return result, fmt.Errorf("restore RemotePlayerMove entry")
	}
	moveInstalled = false
	return result, nil
}

func hijackRoomFastReceiveThread(
	process syscall.Handle,
	threadID uint32,
	stubAddress, methodAddress, objectAddress uintptr,
	sectionID uint16,
	sourceUIN uint32,
	dataAddress uintptr,
	dataLength int,
	statusAddress, doneAddress uintptr,
	timeout time.Duration,
) error {
	const threadAccess = 0x0002 | 0x0008 | 0x0010 | 0x0040 // suspend, get/set context, query
	handle, _, openErr := procOpenThread.Call(threadAccess, 0, uintptr(threadID))
	if handle == 0 {
		return fmt.Errorf("OpenThread %d: %v", threadID, openErr)
	}
	thread := syscall.Handle(handle)
	defer syscall.CloseHandle(thread)

	previous, _, suspendErr := procSuspendThread.Call(uintptr(thread))
	if previous == ^uintptr(0) {
		return fmt.Errorf("SuspendThread %d: %v", threadID, suspendErr)
	}
	resumed := false
	defer func() {
		if !resumed {
			procResumeThread.Call(uintptr(thread))
		}
	}()

	context := make([]byte, 716)
	binary.LittleEndian.PutUint32(context[0:4], 0x00010003)
	got, _, contextErr := procWow64GetThreadContext.Call(uintptr(thread), uintptr(unsafe.Pointer(&context[0])))
	if got == 0 {
		return fmt.Errorf("Wow64GetThreadContext %d: %v", threadID, contextErr)
	}
	originalEIP := uintptr(binary.LittleEndian.Uint32(context[184:188]))
	if originalEIP == 0 {
		return fmt.Errorf("thread %d has zero EIP", threadID)
	}
	stub := buildRoomFastReceiveHijackStub(
		methodAddress, objectAddress, sectionID, sourceUIN, dataAddress, dataLength,
		statusAddress, doneAddress, originalEIP,
	)
	if err := writeRemote(process, stubAddress, stub); err != nil {
		return fmt.Errorf("write existing-thread receive stub: %w", err)
	}
	procFlushInstruction.Call(uintptr(process), stubAddress, uintptr(len(stub)))
	binary.LittleEndian.PutUint32(context[184:188], uint32(stubAddress))
	set, _, setErr := procWow64SetThreadContext.Call(uintptr(thread), uintptr(unsafe.Pointer(&context[0])))
	if set == 0 {
		return fmt.Errorf("Wow64SetThreadContext %d: %v", threadID, setErr)
	}
	if value, _, resumeErr := procResumeThread.Call(uintptr(thread)); value == ^uintptr(0) {
		return fmt.Errorf("ResumeThread %d: %v", threadID, resumeErr)
	}
	resumed = true

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if value, ok := readRemote(process, doneAddress, 4); ok && binary.LittleEndian.Uint32(value) == 1 {
			// The completion flag is written only after all original registers and
			// flags have been restored. Leave a short margin for the final RET.
			time.Sleep(20 * time.Millisecond)
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("existing thread %d did not complete room-fast receive within %s", threadID, timeout)
}

func findQQTSectionMainObject(process syscall.Handle, pid uint32, sectionBase uintptr) (uintptr, error) {
	vtable := sectionBase + qqtSectionMainVTableRVA
	pattern := make([]byte, 4)
	binary.LittleEndian.PutUint32(pattern, uint32(vtable))
	matches, err := SearchProcessMemory(pid, pattern, 32)
	if err != nil {
		return 0, fmt.Errorf("search CMySection vtable pointer: %w", err)
	}
	for _, match := range matches {
		if match >= sectionBase && match < sectionBase+0x100000 {
			continue
		}
		fields, ok := readRemote(process, match, 0x194)
		if !ok || len(fields) != 0x194 {
			continue
		}
		if binary.LittleEndian.Uint32(fields[0:4]) != uint32(vtable) || binary.LittleEndian.Uint32(fields[0x2C:0x30]) == 0 {
			continue
		}
		if binary.LittleEndian.Uint16(fields[0x192:0x194]) == 0 {
			continue
		}
		return match, nil
	}
	return 0, fmt.Errorf("live CMySection object was not found for vtable 0x%08X", vtable)
}

func buildRoomBindingCounterStub(stubAddress, targetAddress, countAddress uintptr) []byte {
	stub := []byte{
		0x9C, 0x60, // pushfd; pushad
		0xFF, 0x05, 0, 0, 0, 0, // inc dword ptr [count]
		0x61, 0x9D, // popad; popfd
	}
	binary.LittleEndian.PutUint32(stub[4:8], uint32(countAddress))
	stub = append(stub, roomBindingEntrySignature...)
	return appendRelativeJump(stub, stubAddress+uintptr(len(stub)), targetAddress+roomBindingHookSize)
}

func buildRoomFastReceiveCallStub(stubAddress, methodAddress, objectAddress uintptr, sectionID uint16, sourceUIN uint32, dataAddress uintptr, dataLength int, statusAddress uintptr) []byte {
	stub := []byte{0x68}
	stub = binary.LittleEndian.AppendUint32(stub, uint32(dataLength))
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(dataAddress))
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, sourceUIN)
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(sectionID))
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(objectAddress))
	stub = append(stub, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(methodAddress))
	stub = append(stub, 0xFF, 0xD0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(statusAddress))
	stub = append(stub, 0x33, 0xC0, 0xC2, 0x04, 0x00)
	return stub
}

func buildRoomFastReceiveHijackStub(methodAddress, objectAddress uintptr, sectionID uint16, sourceUIN uint32, dataAddress uintptr, dataLength int, statusAddress, doneAddress, originalEIP uintptr) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(dataLength))
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(dataAddress))
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, sourceUIN)
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(sectionID))
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(objectAddress))
	stub = append(stub, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(methodAddress))
	stub = append(stub, 0xFF, 0xD0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(statusAddress))
	stub = append(stub, 0x61, 0x9D)       // popad; popfd
	stub = append(stub, 0x9C, 0x50, 0xB8) // preserve flags/eax while marking done
	stub = binary.LittleEndian.AppendUint32(stub, uint32(doneAddress))
	stub = append(stub, 0xC7, 0x00, 0x01, 0x00, 0x00, 0x00, 0x58, 0x9D)
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(originalEIP))
	return append(stub, 0xC3)
}

package winlaunch

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"time"
	"unsafe"
)

const (
	qqtPPPType2SendRVA       = uintptr(0x6704)
	qqtPPPMode1VTableRVA     = uintptr(0xD558)
	qqtPPPSinkVTableRVA      = uintptr(0xD53C)
	qqtPPPMode1SinkOffset    = uintptr(0x10)
	qqtPPPModeOffset         = uintptr(0x42C)
	qqtPPPUINOffset          = uintptr(0x430)
	qqtPPPPlayerIDOffset     = uintptr(0x434)
	qqtPPPType2ProbePageSize = uintptr(0x2000)
)

// QQTPPPType2ProbeResult records one temporary invocation of the original
// QQTPPP Type-2 constructor. The constructor and QQTModules perform the real
// client-side wire transformation; a simultaneous Winsock trace can therefore
// capture an authoritative frame without permanently modifying Client.exe.
type QQTPPPType2ProbeResult struct {
	PID           uint32 `json:"pid"`
	ThreadID      uint32 `json:"thread_id"`
	QQTPPPBase    string `json:"qqtppp_base"`
	Mode1Object   string `json:"mode1_object"`
	Mode1Sink     string `json:"mode1_sink"`
	UIN           uint32 `json:"uin"`
	PlayerID      uint16 `json:"player_id"`
	TargetIPv4    string `json:"target_ipv4"`
	TargetPort    uint16 `json:"target_port"`
	PayloadLength int    `json:"payload_length"`
	CallResult    uint32 `json:"call_result"`
	Completed     bool   `json:"completed"`
	Behavior      string `json:"behavior"`
}

// ProbeQQTPPPType2 invokes QQTPPP!FUN_10006704 once on an existing client
// thread. All registers/flags are restored and the scratch page is freed only
// after the thread has returned to its original instruction.
func ProbeQQTPPPType2(pid, threadID uint32, targetIP net.IP, targetPort uint16, payload []byte, timeout time.Duration) (QQTPPPType2ProbeResult, error) {
	result := QQTPPPType2ProbeResult{
		PID: pid, ThreadID: threadID, TargetIPv4: targetIP.String(), TargetPort: targetPort,
		PayloadLength: len(payload),
		Behavior:      "temporary existing-thread invocation of the original QQTPPP Type-2 constructor; no persistent client bytes are changed",
	}
	if pid == 0 || threadID == 0 {
		return result, fmt.Errorf("pid and thread ID must be non-zero")
	}
	if targetPort == 0 {
		return result, fmt.Errorf("target port must be non-zero")
	}
	ipv4 := targetIP.To4()
	if ipv4 == nil {
		return result, fmt.Errorf("target IP %q is not IPv4", targetIP)
	}
	if len(payload) == 0 || len(payload) > 1024 {
		return result, fmt.Errorf("Type-2 payload length %d is outside 1..1024", len(payload))
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return result, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	base, err := waitForModule(process, pid, "QQTPPP.dll", timeout)
	if err != nil {
		return result, err
	}
	result.QQTPPPBase = fmt.Sprintf("0x%08X", base)
	outer, sink, uin, playerID, err := findActiveQQTPPPMode1Object(process, pid, base)
	if err != nil {
		return result, err
	}
	result.Mode1Object = fmt.Sprintf("0x%08X", outer)
	result.Mode1Sink = fmt.Sprintf("0x%08X", sink)
	result.UIN = uin
	result.PlayerID = playerID

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, qqtPPPType2ProbePageSize, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return result, fmt.Errorf("VirtualAllocEx QQTPPP Type-2 probe: 0x%X (%v)", page, allocErr)
	}
	defer procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
	stubAddress := page
	payloadAddress := page + 0x400
	portAddress := page + 0x900
	ipAddress := page + 0x904
	resultAddress := page + 0x908
	doneAddress := page + 0x90C

	portBytes := make([]byte, 2)
	binary.LittleEndian.PutUint16(portBytes, targetPort)
	// FUN_10006704 accepts the host-order u_long that it later passes through
	// htonl. Building the value from network bytes gives 0x7F000001 for
	// 127.0.0.1, which is the expected host argument on x86.
	ipBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(ipBytes, binary.BigEndian.Uint32(ipv4))
	for _, write := range []struct {
		address uintptr
		data    []byte
	}{
		{payloadAddress, payload},
		{portAddress, portBytes},
		{ipAddress, ipBytes},
		{resultAddress, make([]byte, 8)},
	} {
		if err := writeRemote(process, write.address, write.data); err != nil {
			return result, err
		}
	}

	if err := invokeQQTPPPType2OnThread(
		process, threadID, stubAddress, base+qqtPPPType2SendRVA, sink,
		payloadAddress, len(payload), portAddress, ipAddress, resultAddress, doneAddress, timeout,
	); err != nil {
		return result, err
	}
	status, ok := readRemote(process, resultAddress, 8)
	if !ok || len(status) != 8 {
		return result, fmt.Errorf("read QQTPPP Type-2 probe result")
	}
	result.CallResult = binary.LittleEndian.Uint32(status[0:4])
	result.Completed = binary.LittleEndian.Uint32(status[4:8]) == 1
	return result, nil
}

func findActiveQQTPPPMode1Object(process syscall.Handle, pid uint32, base uintptr) (uintptr, uintptr, uint32, uint16, error) {
	pattern := make([]byte, 4)
	binary.LittleEndian.PutUint32(pattern, uint32(base+qqtPPPMode1VTableRVA))
	matches, err := SearchProcessMemory(pid, pattern, 64)
	if err != nil {
		return 0, 0, 0, 0, fmt.Errorf("search QQTPPP mode-1 object: %w", err)
	}
	for _, outer := range matches {
		sink := outer + qqtPPPMode1SinkOffset
		fields, ok := readRemote(process, sink, int(qqtPPPPlayerIDOffset+4))
		if !ok || len(fields) < int(qqtPPPPlayerIDOffset+4) {
			continue
		}
		if binary.LittleEndian.Uint32(fields[0:4]) != uint32(base+qqtPPPSinkVTableRVA) || binary.LittleEndian.Uint32(fields[qqtPPPModeOffset:qqtPPPModeOffset+4]) != 1 {
			continue
		}
		uin := binary.LittleEndian.Uint32(fields[qqtPPPUINOffset : qqtPPPUINOffset+4])
		playerID := binary.LittleEndian.Uint16(fields[qqtPPPPlayerIDOffset : qqtPPPPlayerIDOffset+2])
		if uin != 0 && playerID != 0 {
			return outer, sink, uin, playerID, nil
		}
	}
	return 0, 0, 0, 0, fmt.Errorf("active QQTPPP mode-1 object was not found")
}

func invokeQQTPPPType2OnThread(
	process syscall.Handle, threadID uint32, stubAddress, methodAddress, sinkAddress,
	payloadAddress uintptr, payloadLength int, portAddress, ipAddress, resultAddress, doneAddress uintptr,
	timeout time.Duration,
) error {
	const threadAccess = 0x0002 | 0x0008 | 0x0010 | 0x0040
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
	stub := buildQQTPPPType2ProbeStub(
		methodAddress, sinkAddress, payloadAddress, payloadLength, portAddress, ipAddress,
		resultAddress, doneAddress, originalEIP,
	)
	if err := writeRemote(process, stubAddress, stub); err != nil {
		return fmt.Errorf("write QQTPPP Type-2 probe stub: %w", err)
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
			time.Sleep(20 * time.Millisecond)
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("existing thread %d did not complete QQTPPP Type-2 probe within %s", threadID, timeout)
}

func buildQQTPPPType2ProbeStub(methodAddress, sinkAddress, payloadAddress uintptr, payloadLength int, portAddress, ipAddress, resultAddress, doneAddress, originalEIP uintptr) []byte {
	stub := []byte{0x9C, 0x60}      // pushfd; pushad
	stub = append(stub, 0x6A, 0x01) // target count
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(ipAddress))
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(portAddress))
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(payloadLength))
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(payloadAddress))
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(sinkAddress))
	stub = append(stub, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(methodAddress))
	stub = append(stub, 0xFF, 0xD0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(resultAddress))
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, 0x9C, 0x50, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(doneAddress))
	stub = append(stub, 0xC7, 0x00, 0x01, 0x00, 0x00, 0x00, 0x58, 0x9D)
	stub = append(stub, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(originalEIP))
	return append(stub, 0xC3)
}

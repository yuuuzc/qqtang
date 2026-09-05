package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

const qqtPPPType3ReceiveRVA = uintptr(0x6C03)

// QQTPPPType3ProbeResult records one temporary invocation of the original
// mode-0 UDP sink. It is a receive-path probe: QQTPPP performs its normal
// checksum, sequence, identity and rendezvous dispatch without changing any
// persistent client bytes.
type QQTPPPType3ProbeResult struct {
	PID           uint32 `json:"pid"`
	ThreadID      uint32 `json:"thread_id"`
	QQTPPPBase    string `json:"qqtppp_base"`
	Sink          string `json:"sink"`
	UIN           uint32 `json:"uin"`
	PlayerID      uint16 `json:"player_id"`
	PayloadLength int    `json:"payload_length"`
	PayloadHex    string `json:"payload_hex"`
	CallResult    uint32 `json:"call_result"`
	Completed     bool   `json:"completed"`
	Behavior      string `json:"behavior"`
}

func ProbeQQTPPPType3(pid, threadID, sourceAddress, sourceEndpoint uint32, payload []byte, timeout time.Duration) (QQTPPPType3ProbeResult, error) {
	return probeQQTPPPReceive(pid, threadID, sourceAddress, sourceEndpoint, payload, 0, timeout)
}

// ProbeQQTPPPType2Receive invokes the same native UDP sink on the active
// mode-1 object. The sink is shared by all QQTPPP control packet types; mode 1
// is required when replaying a captured Type-2 frame through its real decode
// path for protocol diagnostics.
func ProbeQQTPPPType2Receive(pid, threadID, sourceAddress, sourceEndpoint uint32, payload []byte, timeout time.Duration) (QQTPPPType3ProbeResult, error) {
	return probeQQTPPPReceive(pid, threadID, sourceAddress, sourceEndpoint, payload, 1, timeout)
}

func probeQQTPPPReceive(pid, threadID, sourceAddress, sourceEndpoint uint32, payload []byte, mode uint32, timeout time.Duration) (QQTPPPType3ProbeResult, error) {
	result := QQTPPPType3ProbeResult{
		PID: pid, ThreadID: threadID, PayloadLength: len(payload), PayloadHex: hex.EncodeToString(payload),
		Behavior: fmt.Sprintf("temporary existing-thread invocation of the original QQTPPP mode-%d UDP sink; no persistent client bytes are changed", mode),
	}
	if pid == 0 || threadID == 0 {
		return result, fmt.Errorf("pid and thread ID must be non-zero")
	}
	if len(payload) == 0 || len(payload) > 1024 {
		return result, fmt.Errorf("Type-3 payload length %d is outside 1..1024", len(payload))
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
	sink, uin, playerID, err := findActiveQQTPPPSink(process, pid, base, mode)
	if err != nil {
		return result, err
	}
	result.Sink = fmt.Sprintf("0x%08X", sink)
	result.UIN, result.PlayerID = uin, playerID

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, qqtPPPType2ProbePageSize, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return result, fmt.Errorf("VirtualAllocEx QQTPPP Type-3 probe: 0x%X (%v)", page, allocErr)
	}
	defer procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
	payloadAddress, resultAddress, doneAddress := page+0x400, page+0x900, page+0x904
	if err := writeRemote(process, payloadAddress, payload); err != nil {
		return result, err
	}
	if err := writeRemote(process, resultAddress, make([]byte, 8)); err != nil {
		return result, err
	}
	if err := invokeQQTPPPType3OnThread(process, threadID, page, base+qqtPPPType3ReceiveRVA, sink, sourceAddress, sourceEndpoint, payloadAddress, len(payload), resultAddress, doneAddress, timeout); err != nil {
		return result, err
	}
	status, ok := readRemote(process, resultAddress, 8)
	if !ok {
		return result, fmt.Errorf("read QQTPPP Type-3 probe result")
	}
	result.CallResult = binary.LittleEndian.Uint32(status[0:4])
	result.Completed = binary.LittleEndian.Uint32(status[4:8]) == 1
	return result, nil
}

func findActiveQQTPPPSink(process syscall.Handle, pid uint32, base uintptr, mode uint32) (uintptr, uint32, uint16, error) {
	pattern := make([]byte, 4)
	binary.LittleEndian.PutUint32(pattern, uint32(base+qqtPPPSinkVTableRVA))
	matches, err := SearchProcessMemory(pid, pattern, 64)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("search QQTPPP sink: %w", err)
	}
	for _, sink := range matches {
		fields, ok := readRemote(process, sink, int(qqtPPPPlayerIDOffset+4))
		if !ok || binary.LittleEndian.Uint32(fields[qqtPPPModeOffset:qqtPPPModeOffset+4]) != mode {
			continue
		}
		uin := binary.LittleEndian.Uint32(fields[qqtPPPUINOffset : qqtPPPUINOffset+4])
		playerID := binary.LittleEndian.Uint16(fields[qqtPPPPlayerIDOffset : qqtPPPPlayerIDOffset+2])
		if uin != 0 && playerID != 0 {
			return sink, uin, playerID, nil
		}
	}
	return 0, 0, 0, fmt.Errorf("active QQTPPP mode-%d sink was not found", mode)
}

func invokeQQTPPPType3OnThread(process syscall.Handle, threadID uint32, stubAddress, methodAddress, sinkAddress uintptr, sourceAddress, sourceEndpoint uint32, payloadAddress uintptr, payloadLength int, resultAddress, doneAddress uintptr, timeout time.Duration) error {
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
	stub := buildQQTPPPType3ProbeStub(methodAddress, sinkAddress, sourceAddress, sourceEndpoint, payloadAddress, payloadLength, resultAddress, doneAddress, originalEIP)
	if err := writeRemote(process, stubAddress, stub); err != nil {
		return err
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
	return fmt.Errorf("existing thread %d did not complete QQTPPP Type-3 probe within %s", threadID, timeout)
}

func buildQQTPPPType3ProbeStub(methodAddress, sinkAddress uintptr, sourceAddress, sourceEndpoint uint32, payloadAddress uintptr, payloadLength int, resultAddress, doneAddress, originalEIP uintptr) []byte {
	stub := []byte{0x9C, 0x60}
	for _, value := range []uint32{uint32(payloadLength), uint32(payloadAddress), sourceEndpoint, sourceAddress} {
		stub = append(stub, 0x68)
		stub = binary.LittleEndian.AppendUint32(stub, value)
	}
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(sinkAddress))
	stub = append(stub, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(methodAddress))
	stub = append(stub, 0xFF, 0xD0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(resultAddress))
	stub = append(stub, 0x61, 0x9D, 0x9C, 0x50, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(doneAddress))
	stub = append(stub, 0xC7, 0x00, 0x01, 0, 0, 0, 0x58, 0x9D, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(originalEIP))
	return append(stub, 0xC3)
}

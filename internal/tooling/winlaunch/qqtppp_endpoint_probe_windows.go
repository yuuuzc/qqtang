package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

const (
	qqtPPPControllerVTableRVA  = uintptr(0xD214)
	qqtPPPControllerSlot2RVA   = uintptr(0x3BBD)
	qqtPPPControllerAddPeerRVA = uintptr(0x358A)
	qqtPPPControllerPeerBase   = uintptr(0x88)
	qqtPPPPeerStride           = uintptr(0x4B28)
	qqtPPPPeerStateOffset      = uintptr(0x460)
)

// QQTPPPEndpointProbeResult records one call through QQTPPP's native
// controller endpoint-update method. It is a diagnostic of the normal
// rendezvous continuation: the peer object itself chooses and emits the
// subsequent 0x20 handshake frame.
type QQTPPPEndpointProbeResult struct {
	PID          uint32 `json:"pid"`
	ThreadID     uint32 `json:"thread_id"`
	QQTPPPBase   string `json:"qqtppp_base"`
	Controller   string `json:"controller"`
	Peer         string `json:"peer"`
	PeerIndex    int    `json:"peer_index"`
	PeerUIN      uint32 `json:"peer_uin"`
	PeerPlayerID uint16 `json:"peer_player_id"`
	StateBefore  uint32 `json:"state_before"`
	StateAfter   uint32 `json:"state_after"`
	PeerCreated  bool   `json:"peer_created"`
	Completed    bool   `json:"completed"`
	Behavior     string `json:"behavior"`
}

func ProbeQQTPPPEndpoint(pid, threadID, peerUIN uint32, peerPlayerID uint16, firstIP uint32, firstPort uint16, secondIP uint32, secondPort uint16, timeout time.Duration) (QQTPPPEndpointProbeResult, error) {
	result := QQTPPPEndpointProbeResult{
		PID: pid, ThreadID: threadID, PeerUIN: peerUIN,
		Behavior: "temporary existing-thread call to the original QQTPPP controller endpoint-update method; no persistent client bytes are changed",
	}
	if pid == 0 || threadID == 0 || peerUIN == 0 || firstIP == 0 || firstPort == 0 {
		return result, fmt.Errorf("pid, thread ID, peer UIN and first endpoint must be non-zero")
	}
	if secondIP == 0 {
		secondIP, secondPort = firstIP, firstPort
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
	controller, err := findQQTPPPController(process, pid, base)
	if err != nil {
		return result, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return result, fmt.Errorf("VirtualAllocEx QQTPPP endpoint probe: 0x%X (%v)", page, allocErr)
	}
	defer procVirtualFreeEx.Call(uintptr(process), page, 0, memRelease)
	doneAddress := page + 0x300
	peer, peerIndex, playerID, state, found := findQQTPPPPeer(process, controller, peerUIN)
	if !found {
		if peerPlayerID == 0 {
			return result, fmt.Errorf("active QQTPPP peer UIN %d was not found and peer player ID is zero", peerUIN)
		}
		if err := invokeQQTPPPAddPeerOnThread(process, threadID, page, base+qqtPPPControllerAddPeerRVA, controller-0x20, peerPlayerID, peerUIN, doneAddress, timeout); err != nil {
			return result, err
		}
		result.PeerCreated = true
		peer, peerIndex, playerID, state, found = findQQTPPPPeer(process, controller, peerUIN)
		if !found {
			return result, fmt.Errorf("QQTPPP add-peer method completed but UIN %d remains absent", peerUIN)
		}
	}
	result.Controller = fmt.Sprintf("0x%08X", controller)
	result.Peer = fmt.Sprintf("0x%08X", peer)
	result.PeerIndex, result.PeerPlayerID, result.StateBefore = peerIndex, playerID, state

	if err := writeRemote(process, doneAddress, make([]byte, 4)); err != nil {
		return result, err
	}
	if err := invokeQQTPPPEndpointOnThread(
		process, threadID, page, base+qqtPPPControllerSlot2RVA, controller,
		playerID, peerUIN, firstIP, firstPort, secondIP, secondPort, doneAddress, timeout,
	); err != nil {
		return result, err
	}
	result.Completed = true
	if fields, ok := readRemote(process, peer+qqtPPPPeerStateOffset, 4); ok {
		result.StateAfter = binary.LittleEndian.Uint32(fields)
	}
	return result, nil
}

func findQQTPPPController(process syscall.Handle, pid uint32, base uintptr) (uintptr, error) {
	pattern := make([]byte, 4)
	binary.LittleEndian.PutUint32(pattern, uint32(base+qqtPPPControllerVTableRVA))
	matches, err := SearchProcessMemory(pid, pattern, 32)
	if err != nil {
		return 0, fmt.Errorf("search QQTPPP controller: %w", err)
	}
	for _, controller := range matches {
		if controller >= base && controller < base+0x100000 {
			continue
		}
		if _, ok := readRemote(process, controller+qqtPPPControllerPeerBase, 4); ok {
			return controller, nil
		}
	}
	return 0, fmt.Errorf("active QQTPPP controller was not found")
}

func findQQTPPPPeer(process syscall.Handle, controller uintptr, peerUIN uint32) (uintptr, int, uint16, uint32, bool) {
	for index := 0; index < 8; index++ {
		peer := controller + qqtPPPControllerPeerBase + uintptr(index)*qqtPPPPeerStride
		fields, ok := readRemote(process, peer+qqtPPPPeerStateOffset, 16)
		if !ok || len(fields) != 16 || binary.LittleEndian.Uint32(fields[4:8]) != peerUIN {
			continue
		}
		playerID := binary.LittleEndian.Uint16(fields[8:10])
		if playerID != 0 && playerID != 0xffff {
			return peer, index, playerID, binary.LittleEndian.Uint32(fields[0:4]), true
		}
	}
	return 0, -1, 0, 0, false
}

func invokeQQTPPPEndpointOnThread(process syscall.Handle, threadID uint32, stubAddress, methodAddress, controller uintptr, playerID uint16, peerUIN uint32, firstIP uint32, firstPort uint16, secondIP uint32, secondPort uint16, doneAddress uintptr, timeout time.Duration) error {
	return invokeQQTPPPStubOnThread(process, threadID, stubAddress, doneAddress, timeout, func(originalEIP uintptr) []byte {
		return buildQQTPPPEndpointProbeStub(methodAddress, controller, playerID, peerUIN, firstIP, firstPort, secondIP, secondPort, doneAddress, originalEIP)
	})
}

func invokeQQTPPPAddPeerOnThread(process syscall.Handle, threadID uint32, stubAddress, methodAddress, root uintptr, playerID uint16, peerUIN uint32, doneAddress uintptr, timeout time.Duration) error {
	return invokeQQTPPPStubOnThread(process, threadID, stubAddress, doneAddress, timeout, func(originalEIP uintptr) []byte {
		return buildQQTPPPAddPeerProbeStub(methodAddress, root, playerID, peerUIN, doneAddress, originalEIP)
	})
}

func invokeQQTPPPStubOnThread(process syscall.Handle, threadID uint32, stubAddress, doneAddress uintptr, timeout time.Duration, build func(uintptr) []byte) error {
	const threadAccess = 0x0002 | 0x0008 | 0x0010 | 0x0040
	if err := writeRemote(process, doneAddress, make([]byte, 4)); err != nil {
		return err
	}
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
	stub := build(originalEIP)
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
	return fmt.Errorf("existing thread %d did not complete QQTPPP endpoint probe within %s", threadID, timeout)
}

func buildQQTPPPAddPeerProbeStub(methodAddress, root uintptr, playerID uint16, peerUIN uint32, doneAddress, originalEIP uintptr) []byte {
	stub := []byte{0x9C, 0x60}
	for _, value := range []uint32{0, 0, peerUIN, uint32(playerID), uint32(root)} {
		stub = append(stub, 0x68)
		stub = binary.LittleEndian.AppendUint32(stub, value)
	}
	stub = append(stub, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(methodAddress))
	stub = append(stub, 0xFF, 0xD0, 0x61, 0x9D, 0x9C, 0x50, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(doneAddress))
	stub = append(stub, 0xC7, 0x00, 0x01, 0, 0, 0, 0x58, 0x9D, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(originalEIP))
	return append(stub, 0xC3)
}

func buildQQTPPPEndpointProbeStub(methodAddress, controller uintptr, playerID uint16, peerUIN uint32, firstIP uint32, firstPort uint16, secondIP uint32, secondPort uint16, doneAddress, originalEIP uintptr) []byte {
	stub := []byte{0x9C, 0x60}
	for _, value := range []uint32{uint32(secondPort), secondIP, uint32(firstPort), firstIP, peerUIN, uint32(playerID)} {
		stub = append(stub, 0x68)
		stub = binary.LittleEndian.AppendUint32(stub, value)
	}
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(controller))
	stub = append(stub, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(methodAddress))
	stub = append(stub, 0xFF, 0xD0, 0x61, 0x9D, 0x9C, 0x50, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(doneAddress))
	stub = append(stub, 0xC7, 0x00, 0x01, 0, 0, 0, 0x58, 0x9D, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(originalEIP))
	return append(stub, 0xC3)
}

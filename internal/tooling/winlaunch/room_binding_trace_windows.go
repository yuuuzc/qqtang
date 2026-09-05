package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

type RoomBindingTraceResult struct {
	PID             uint32 `json:"pid"`
	ElapsedMS       int64  `json:"elapsed_ms"`
	RemoteMoveCalls uint32 `json:"remote_move_calls"`
	RemoteBombCalls uint32 `json:"remote_bomb_calls"`
	Behavior        string `json:"behavior"`
}

// RoomBindingTracer passively counts the final native room-render bindings.
// It is useful for proving whether a candidate network frame reached gameplay
// without relying on screenshots or changing any game state itself.
type RoomBindingTracer struct {
	pid       uint32
	process   syscall.Handle
	started   time.Time
	page      uintptr
	move      uintptr
	bomb      uintptr
	moveCount uintptr
	bombCount uintptr
	closed    bool
}

func InstallRoomBindingTracer(pid uint32, timeout time.Duration) (*RoomBindingTracer, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &RoomBindingTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()
	clientBase, err := waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return nil, err
	}
	tracer.move = clientBase + clientRemoteMoveRVA
	tracer.bomb = clientBase + clientRemotePutBombRVA
	for label, address := range map[string]uintptr{"RemotePlayerMove": tracer.move, "RemotePlayerPutBomb": tracer.bomb} {
		actual, ok := readRemote(process, address, len(roomBindingEntrySignature))
		if !ok || !equalBytes(actual, roomBindingEntrySignature) {
			return nil, fmt.Errorf("%s entry signature mismatch at 0x%08X: got %X", label, address, actual)
		}
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xffffffff {
		return nil, fmt.Errorf("VirtualAllocEx room binding trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page
	tracer.moveCount = page + 0x800
	tracer.bombCount = page + 0x804
	moveStub := buildRoomBindingCounterStub(page, tracer.move, tracer.moveCount)
	bombStub := buildRoomBindingCounterStub(page+0x100, tracer.bomb, tracer.bombCount)
	for _, write := range []struct {
		address uintptr
		data    []byte
	}{{page, moveStub}, {page + 0x100, bombStub}, {tracer.moveCount, make([]byte, 8)}} {
		if err := writeRemote(process, write.address, write.data); err != nil {
			return nil, err
		}
	}
	if !ensureRemoteBytes(process, tracer.move, relativeJumpPatch(tracer.move, page, roomBindingHookSize), pageExecuteReadWrite) {
		return nil, fmt.Errorf("install RemotePlayerMove trace")
	}
	if !ensureRemoteBytes(process, tracer.bomb, relativeJumpPatch(tracer.bomb, page+0x100, roomBindingHookSize), pageExecuteReadWrite) {
		ensureRemoteBytes(process, tracer.move, roomBindingEntrySignature, pageExecuteReadWrite)
		return nil, fmt.Errorf("install RemotePlayerPutBomb trace")
	}
	procFlushInstruction.Call(uintptr(process), page, 0x1000)
	procFlushInstruction.Call(uintptr(process), tracer.move, roomBindingHookSize)
	procFlushInstruction.Call(uintptr(process), tracer.bomb, roomBindingHookSize)
	failed = false
	return tracer, nil
}

func (tracer *RoomBindingTracer) Capture(duration time.Duration) RoomBindingTraceResult {
	if duration <= 0 {
		duration = 5 * time.Second
	}
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	result := RoomBindingTraceResult{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		Behavior: "passive counters on final remote movement and bomb bindings; original instructions restored on close",
	}
	if counters, ok := readRemote(tracer.process, tracer.moveCount, 8); ok && len(counters) == 8 {
		result.RemoteMoveCalls = binary.LittleEndian.Uint32(counters[0:4])
		result.RemoteBombCalls = binary.LittleEndian.Uint32(counters[4:8])
	}
	return result
}

func (tracer *RoomBindingTracer) Close() {
	if tracer == nil || tracer.closed {
		return
	}
	tracer.closed = true
	if tracer.process != 0 {
		if tracer.bomb != 0 {
			ensureRemoteBytes(tracer.process, tracer.bomb, roomBindingEntrySignature, pageExecuteReadWrite)
			procFlushInstruction.Call(uintptr(tracer.process), tracer.bomb, roomBindingHookSize)
		}
		if tracer.move != 0 {
			ensureRemoteBytes(tracer.process, tracer.move, roomBindingEntrySignature, pageExecuteReadWrite)
			procFlushInstruction.Call(uintptr(tracer.process), tracer.move, roomBindingHookSize)
		}
		if tracer.page != 0 {
			procVirtualFreeEx.Call(uintptr(tracer.process), tracer.page, 0, memRelease)
		}
		syscall.CloseHandle(tracer.process)
		tracer.process = 0
	}
}

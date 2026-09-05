package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	netCenterSocketAOnReceiveSlotRVA = uintptr(0x2F178)
	netCenterSocketBOnReceiveSlotRVA = uintptr(0x2FAA4)
	netCenterSocketAOnReceiveRVA     = uintptr(0x1302)
	netCenterSocketBOnReceiveRVA     = uintptr(0x10C3)
)

type NetCenterOnReceiveCall struct {
	Class          string `json:"class"`
	Calls          uint32 `json:"calls"`
	ThreadID       uint32 `json:"thread_id,omitempty"`
	Object         string `json:"object"`
	VTable         string `json:"vtable"`
	ErrorCode      int32  `json:"error_code"`
	Caller         string `json:"caller"`
	SlotAddress    string `json:"slot_address"`
	OriginalTarget string `json:"original_target"`
}

type NetCenterOnReceiveCapture struct {
	PID       uint32                   `json:"pid"`
	ElapsedMS int64                    `json:"elapsed_ms"`
	Module    string                   `json:"module"`
	Calls     []NetCenterOnReceiveCall `json:"calls"`
	Behavior  string                   `json:"behavior"`
}

type netCenterOnReceiveHook struct {
	class          string
	slotAddress    uintptr
	originalTarget uintptr
	original       []byte
	stubAddress    uintptr
	recordAddress  uintptr
}

type NetCenterOnReceiveTracer struct {
	pid     uint32
	process syscall.Handle
	started time.Time
	page    uintptr
	hooks   []netCenterOnReceiveHook
}

func InstallNetCenterOnReceiveTracer(pid uint32, timeout time.Duration) (*NetCenterOnReceiveTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &NetCenterOnReceiveTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	moduleBase, err := waitForModule(process, pid, "NetCenter.dll", timeout)
	if err != nil {
		return nil, err
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx NetCenter OnReceive trace: 0x%X (%v)", page, allocErr)
	}
	tracer.page = page

	specs := []struct {
		class        string
		slotRVA      uintptr
		targetRVA    uintptr
		stubOffset   uintptr
		recordOffset uintptr
	}{
		{class: "socket-a", slotRVA: netCenterSocketAOnReceiveSlotRVA, targetRVA: netCenterSocketAOnReceiveRVA, stubOffset: 0, recordOffset: 0x300},
		{class: "socket-b", slotRVA: netCenterSocketBOnReceiveSlotRVA, targetRVA: netCenterSocketBOnReceiveRVA, stubOffset: 0x100, recordOffset: 0x340},
	}
	for _, spec := range specs {
		slot := moduleBase + spec.slotRVA
		original, ok := readRemote(process, slot, 4)
		if !ok {
			return nil, fmt.Errorf("read NetCenter %s OnReceive slot 0x%08X", spec.class, slot)
		}
		originalTarget := uintptr(binary.LittleEndian.Uint32(original))
		expectedTarget := moduleBase + spec.targetRVA
		if originalTarget != expectedTarget {
			return nil, fmt.Errorf("NetCenter %s OnReceive target mismatch: got 0x%08X want 0x%08X", spec.class, originalTarget, expectedTarget)
		}
		hook := netCenterOnReceiveHook{
			class: spec.class, slotAddress: slot, originalTarget: originalTarget,
			original: original, stubAddress: page + spec.stubOffset, recordAddress: page + spec.recordOffset,
		}
		stub := buildNetCenterOnReceiveTraceStub(hook.recordAddress)
		finalizeNetCenterOnReceiveTraceStub(stub, hook.stubAddress, hook.originalTarget)
		if err := writeRemote(process, hook.stubAddress, stub); err != nil {
			return nil, fmt.Errorf("write NetCenter %s OnReceive stub: %w", spec.class, err)
		}
		pointer := make([]byte, 4)
		binary.LittleEndian.PutUint32(pointer, uint32(hook.stubAddress))
		if !ensureRemoteBytes(process, hook.slotAddress, pointer, pageReadWrite) {
			return nil, fmt.Errorf("patch NetCenter %s OnReceive slot 0x%08X", spec.class, hook.slotAddress)
		}
		procFlushInstruction.Call(uintptr(process), hook.stubAddress, uintptr(len(stub)))
		tracer.hooks = append(tracer.hooks, hook)
	}

	failed = false
	return tracer, nil
}

func buildNetCenterOnReceiveTraceStub(record uintptr) []byte {
	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record)) // calls
	stub = append(stub, 0x8B, 0x44, 0x24, 0x18, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8)) // original ECX / this
	stub = append(stub, 0x8B, 0x44, 0x24, 0x28, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12)) // nErrorCode
	stub = append(stub, 0x8B, 0x44, 0x24, 0x24, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+16)) // caller return address
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4)) // thread id
	stub = append(stub, 0x61, 0x9D, 0xE9)
	// Position-dependent relative jump placeholder; Install finalizes it after
	// choosing the remote address.
	stub = binary.LittleEndian.AppendUint32(stub, 0)
	return stub
}

func finalizeNetCenterOnReceiveTraceStub(stub []byte, stubAddress, originalTarget uintptr) {
	jumpImmediate := len(stub) - 4
	jumpNext := stubAddress + uintptr(len(stub))
	binary.LittleEndian.PutUint32(stub[jumpImmediate:], uint32(originalTarget-jumpNext))
}

func (tracer *NetCenterOnReceiveTracer) CaptureUntilFirst(duration time.Duration) NetCenterOnReceiveCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		if tracer.anyCalls() {
			time.Sleep(100 * time.Millisecond)
			break
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	return tracer.capture()
}

func (tracer *NetCenterOnReceiveTracer) anyCalls() bool {
	for _, hook := range tracer.hooks {
		record, ok := readRemote(tracer.process, hook.recordAddress, 4)
		if ok && binary.LittleEndian.Uint32(record) != 0 {
			return true
		}
	}
	return false
}

func (tracer *NetCenterOnReceiveTracer) capture() NetCenterOnReceiveCapture {
	result := NetCenterOnReceiveCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(), Module: "NetCenter.dll",
		Behavior: "transparent CAsyncSocket-derived OnReceive vtable hooks; record metadata and tail-jump to each original handler",
	}
	for _, hook := range tracer.hooks {
		record, ok := readRemote(tracer.process, hook.recordAddress, 20)
		if !ok {
			continue
		}
		calls := binary.LittleEndian.Uint32(record[0:4])
		if calls == 0 {
			continue
		}
		object := binary.LittleEndian.Uint32(record[8:12])
		vtable := uint32(0)
		if object != 0 {
			if pointer, pointerOK := readRemote(tracer.process, uintptr(object), 4); pointerOK {
				vtable = binary.LittleEndian.Uint32(pointer)
			}
		}
		result.Calls = append(result.Calls, NetCenterOnReceiveCall{
			Class: hook.class, Calls: calls, ThreadID: binary.LittleEndian.Uint32(record[4:8]),
			Object: fmt.Sprintf("0x%08X", object), VTable: fmt.Sprintf("0x%08X", vtable),
			ErrorCode: int32(binary.LittleEndian.Uint32(record[12:16])), Caller: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[16:20])),
			SlotAddress: fmt.Sprintf("0x%08X", hook.slotAddress), OriginalTarget: fmt.Sprintf("0x%08X", hook.originalTarget),
		})
	}
	return result
}

func (tracer *NetCenterOnReceiveTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	for index := len(tracer.hooks) - 1; index >= 0; index-- {
		hook := tracer.hooks[index]
		ensureRemoteBytes(tracer.process, hook.slotAddress, hook.original, pageReadWrite)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

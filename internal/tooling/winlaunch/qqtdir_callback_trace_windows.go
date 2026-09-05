package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	qqtDirLoginFailedCallRVA   = uintptr(0x211A0)
	qqtDirLoginFailedResumeRVA = uintptr(0x211A6)
)

var qqtDirLoginFailedCallSignature = []byte{0xFF, 0x51, 0x14, 0x8B, 0x45, 0x08}

type QQTDirCallbackCapture struct {
	PID            uint32 `json:"pid"`
	ElapsedMS      int64  `json:"elapsed_ms"`
	Calls          uint32 `json:"calls"`
	CallsCompleted uint32 `json:"calls_completed"`
	ThreadID       uint32 `json:"thread_id,omitempty"`
	Object         string `json:"object"`
	VTable         string `json:"vtable"`
	Target         string `json:"target"`
	PatchAddress   string `json:"patch_address"`
	StubAddress    string `json:"stub_address"`
	OriginalBytes  string `json:"original_bytes"`
	Behavior       string `json:"behavior"`
}

// QQTDirCallbackTracer wraps the exact call [ecx+0x14] immediately associated
// with QQTDir's LoginFailed() diagnostic. It records the callback object and
// target, replays the call, then executes the overwritten mov eax,[ebp+8].
type QQTDirCallbackTracer struct {
	pid          uint32
	process      syscall.Handle
	started      time.Time
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	original     []byte
}

func InstallQQTDirCallbackTracer(pid uint32, timeout time.Duration) (*QQTDirCallbackTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &QQTDirCallbackTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	moduleBase, err := waitForModule(process, pid, "QQTDir.dll", timeout)
	if err != nil {
		return nil, err
	}
	tracer.patchAddress = moduleBase + qqtDirLoginFailedCallRVA
	tracer.original, _ = readRemote(process, tracer.patchAddress, len(qqtDirLoginFailedCallSignature))
	if len(tracer.original) != len(qqtDirLoginFailedCallSignature) ||
		!equalBytes(tracer.original, qqtDirLoginFailedCallSignature) {
		return nil, fmt.Errorf(
			"QQTDir LoginFailed callback signature mismatch at 0x%08X: got %X",
			tracer.patchAddress, tracer.original,
		)
	}

	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx QQTDir callback trace: 0x%X (%v)", page, allocErr)
	}
	tracer.stubAddress = page
	tracer.record = page + 0x200

	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.record))
	stub = append(stub, 0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.record+8))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x18, 0xA3) // saved ECX object
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.record+12))
	stub = append(stub, 0x8B, 0x10, 0x89, 0x15) // vtable
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.record+16))
	stub = append(stub, 0x8B, 0x52, 0x14, 0x89, 0x15) // slot +0x14 target
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.record+20))
	stub = append(stub, 0x61, 0x9D)       // popad; popfd
	stub = append(stub, 0xFF, 0x51, 0x14) // original call [ecx+0x14]
	stub = append(stub, 0x9C, 0x60)
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.record+4))
	stub = append(stub, 0x61, 0x9D)
	stub = append(stub, 0x8B, 0x45, 0x08) // original mov eax,[ebp+8]
	stub = appendRelativeJump(stub, page+uintptr(len(stub)), moduleBase+qqtDirLoginFailedResumeRVA)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write QQTDir callback trace stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0, 0x90}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(tracer.patchAddress+5)))
	if !ensureRemoteBytes(process, tracer.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch QQTDir LoginFailed callback at 0x%08X", tracer.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), tracer.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return tracer, nil
}

func (tracer *QQTDirCallbackTracer) CaptureUntilFirst(duration time.Duration) QQTDirCallbackCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.record, 24)
		if ok && len(record) == 24 && binary.LittleEndian.Uint32(record[0:4]) != 0 {
			return tracer.captureRecord(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(tracer.process, tracer.record, 24)
	return tracer.captureRecord(record)
}

func (tracer *QQTDirCallbackTracer) captureRecord(record []byte) QQTDirCallbackCapture {
	result := QQTDirCallbackCapture{
		PID: tracer.pid, ElapsedMS: time.Since(tracer.started).Milliseconds(),
		PatchAddress:  fmt.Sprintf("0x%08X", tracer.patchAddress),
		StubAddress:   fmt.Sprintf("0x%08X", tracer.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", tracer.original),
		Behavior:      "transparent QQTDir LoginFailed() call [ecx+0x14]: record object/vtable/target and replay exact call",
	}
	if len(record) == 24 {
		result.Calls = binary.LittleEndian.Uint32(record[0:4])
		result.CallsCompleted = binary.LittleEndian.Uint32(record[4:8])
		result.ThreadID = binary.LittleEndian.Uint32(record[8:12])
		result.Object = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[12:16]))
		result.VTable = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[16:20]))
		result.Target = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[20:24]))
	}
	return result
}

func (tracer *QQTDirCallbackTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.patchAddress != 0 && len(tracer.original) != 0 {
		ensureRemoteBytes(tracer.process, tracer.patchAddress, tracer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(tracer.process), tracer.patchAddress, uintptr(len(tracer.original)))
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

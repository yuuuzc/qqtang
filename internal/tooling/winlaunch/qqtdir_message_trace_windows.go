package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const qqtDirMessageRecordSize = 64

// QQTDirMessageCall is one transparent QQTDir.dll SendMessageA call. The
// tracer records only scalar arguments; it never dereferences lParam.
type QQTDirMessageCall struct {
	ElapsedMS      int64  `json:"elapsed_ms"`
	CallsEntered   uint32 `json:"calls_entered"`
	CallsCompleted uint32 `json:"calls_completed"`
	Caller         string `json:"caller"`
	ThreadID       uint32 `json:"thread_id"`
	Window         string `json:"window"`
	Message        uint32 `json:"message"`
	WParam         string `json:"wparam"`
	LParam         string `json:"lparam"`
	Result         int32  `json:"result"`
}

type QQTDirMessageCapture struct {
	PID       uint32              `json:"pid"`
	ElapsedMS int64               `json:"elapsed_ms"`
	Calls     []QQTDirMessageCall `json:"calls"`
}

type QQTDirMessageTracer struct {
	pid           uint32
	process       syscall.Handle
	started       time.Time
	iatAddress    uintptr
	originalIAT   []byte
	recordAddress uintptr
	lastCompleted uint32
}

// ReadActiveQQTDirMessageTrace reads the latest completed record from an
// already installed tracer. It performs no writes to the target process.
func ReadActiveQQTDirMessageTrace(pid uint32, timeout time.Duration) (QQTDirMessageCall, error) {
	process, err := syscall.OpenProcess(0x0400|0x0010, false, pid)
	if err != nil {
		return QQTDirMessageCall{}, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	moduleBase, err := waitForModule(process, pid, "QQTDir.dll", timeout)
	if err != nil {
		return QQTDirMessageCall{}, err
	}
	iatAddress, err := findImportAddress(process, moduleBase, "USER32.dll", "SendMessageA")
	if err != nil {
		return QQTDirMessageCall{}, err
	}
	pointerBytes, ok := readRemote(process, iatAddress, 4)
	if !ok {
		return QQTDirMessageCall{}, fmt.Errorf("read QQTDir SendMessageA IAT")
	}
	stubAddress := uintptr(binary.LittleEndian.Uint32(pointerBytes))
	stubSignature, ok := readRemote(process, stubAddress, 8)
	expectedSignature := []byte{0x53, 0x56, 0x57, 0x55, 0x8D, 0x6C, 0x24, 0x10}
	if !ok || !equalBytes(stubSignature, expectedSignature) {
		return QQTDirMessageCall{}, fmt.Errorf(
			"QQTDir SendMessageA is not wrapped by the expected tracer: 0x%08X", stubAddress,
		)
	}
	record, ok := readRemote(process, stubAddress+0x400, qqtDirMessageRecordSize)
	if !ok {
		return QQTDirMessageCall{}, fmt.Errorf("read active QQTDir message record")
	}
	return decodeQQTDirMessageRecord(record, 0), nil
}

// InstallQQTDirMessageTracer wraps only QQTDir.dll's SendMessageA import. It
// forwards the original hwnd/message/wParam/lParam unchanged and preserves the
// real LRESULT.
func InstallQQTDirMessageTracer(pid uint32, timeout time.Duration) (*QQTDirMessageTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &QQTDirMessageTracer{pid: pid, process: process, started: time.Now()}
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
	iatAddress, targetAddress, err := findAttachedImportAddress(
		process, pid, moduleBase, "USER32.dll", "SendMessageA", timeout,
	)
	if err != nil {
		return nil, fmt.Errorf("resolve QQTDir SendMessageA: %w", err)
	}
	tracer.iatAddress = iatAddress
	tracer.originalIAT, _ = readRemote(process, iatAddress, 4)
	if len(tracer.originalIAT) != 4 {
		return nil, fmt.Errorf("read QQTDir SendMessageA IAT at 0x%08X", iatAddress)
	}

	page, _, allocErr := procVirtualAllocEx.Call(
		uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite,
	)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx QQTDir message trace: 0x%X (%v)", page, allocErr)
	}
	tracer.recordAddress = page + 0x400

	// On entry: [esp]=return, +4=hwnd, +8=message, +12=wParam,
	// +16=lParam. Preserve all nonvolatile registers and forward every call.
	stub := []byte{0x53, 0x56, 0x57, 0x55, 0x8D, 0x6C, 0x24, 0x10}
	stub = append(stub, 0xF0, 0xFF, 0x05) // lock inc entered
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress))
	stub = append(stub, 0x8B, 0x45, 0x00, 0xA3) // caller
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+8))
	stub = append(stub, 0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0xA3) // TEB thread id
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+12))
	for _, field := range []struct {
		stackOffset  byte
		recordOffset uintptr
	}{{4, 16}, {8, 20}, {12, 24}, {16, 28}} {
		stub = append(stub, 0x8B, 0x45, field.stackOffset, 0xA3)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+field.recordOffset))
	}
	stub = append(stub,
		0xFF, 0x75, 0x10, // lParam
		0xFF, 0x75, 0x0C, // wParam
		0xFF, 0x75, 0x08, // message
		0xFF, 0x75, 0x04, // hwnd
		0xB8,
	)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(targetAddress))
	stub = append(stub, 0xFF, 0xD0) // call real SendMessageA
	stub = append(stub, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+32))
	stub = append(stub, 0xF0, 0xFF, 0x05) // lock inc completed
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+4))
	stub = append(stub, 0x5D, 0x5F, 0x5E, 0x5B, 0xC2, 0x10, 0x00)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write QQTDir message trace stub: %w", err)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(page))
	if !ensureRemoteBytes(process, iatAddress, pointer, pageReadWrite) {
		return nil, fmt.Errorf("patch QQTDir SendMessageA IAT at 0x%08X", iatAddress)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return tracer, nil
}

func (tracer *QQTDirMessageTracer) Capture(duration time.Duration) QQTDirMessageCapture {
	deadline := time.Now().Add(duration)
	result := QQTDirMessageCapture{PID: tracer.pid}
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.recordAddress, qqtDirMessageRecordSize)
		if ok {
			completed := binary.LittleEndian.Uint32(record[4:8])
			if completed != 0 && completed != tracer.lastCompleted {
				tracer.lastCompleted = completed
				result.Calls = append(result.Calls, decodeQQTDirMessageRecord(
					record, time.Since(tracer.started).Milliseconds(),
				))
			}
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(tracer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	result.ElapsedMS = time.Since(tracer.started).Milliseconds()
	return result
}

func decodeQQTDirMessageRecord(record []byte, elapsedMS int64) QQTDirMessageCall {
	return QQTDirMessageCall{
		ElapsedMS:      elapsedMS,
		CallsEntered:   binary.LittleEndian.Uint32(record[0:4]),
		CallsCompleted: binary.LittleEndian.Uint32(record[4:8]),
		Caller:         fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12])),
		ThreadID:       binary.LittleEndian.Uint32(record[12:16]),
		Window:         fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[16:20])),
		Message:        binary.LittleEndian.Uint32(record[20:24]),
		WParam:         fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[24:28])),
		LParam:         fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[28:32])),
		Result:         int32(binary.LittleEndian.Uint32(record[32:36])),
	}
}

func (tracer *QQTDirMessageTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.iatAddress != 0 && len(tracer.originalIAT) == 4 {
		ensureRemoteBytes(tracer.process, tracer.iatAddress, tracer.originalIAT, pageReadWrite)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"
)

const ssoMessagePayloadLimit = 2048

type SSOMessageCall struct {
	ElapsedMS      int64  `json:"elapsed_ms"`
	CallsEntered   uint32 `json:"calls_entered"`
	CallsCompleted uint32 `json:"calls_completed"`
	Caller         string `json:"caller"`
	ThreadID       uint32 `json:"thread_id"`
	Window         string `json:"window"`
	Message        uint32 `json:"message"`
	WParam         string `json:"wparam"`
	LParam         string `json:"lparam"`
	CopyDataID     uint32 `json:"copy_data_id"`
	DataLength     uint32 `json:"data_length"`
	DataPointer    string `json:"data_pointer"`
	CapturedLength uint32 `json:"captured_length"`
	PayloadHex     string `json:"payload_hex,omitempty"`
	Result         int32  `json:"result"`
}

type SSOMessageCapture struct {
	PID       uint32           `json:"pid"`
	ElapsedMS int64            `json:"elapsed_ms"`
	Calls     []SSOMessageCall `json:"calls"`
}

type SSOMessageTracer struct {
	pid           uint32
	process       syscall.Handle
	started       time.Time
	iatAddress    uintptr
	originalIAT   []byte
	recordAddress uintptr
	recordSize    int
	lastCompleted uint32
}

// ReadActiveSSOMessageTrace reads the last completed record from an already
// installed SSO message tracer. It does not patch or alter the target process.
func ReadActiveSSOMessageTrace(pid uint32, timeout time.Duration) (SSOMessageCall, error) {
	process, err := syscall.OpenProcess(0x0400|0x0010, false, pid)
	if err != nil {
		return SSOMessageCall{}, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	moduleBase, err := waitForModule(process, pid, "SSOPlatform.dll", timeout)
	if err != nil {
		return SSOMessageCall{}, err
	}
	iatAddress, err := findImportAddress(process, moduleBase, "USER32.dll", "SendMessageW")
	if err != nil {
		return SSOMessageCall{}, err
	}
	pointerBytes, ok := readRemote(process, iatAddress, 4)
	if !ok {
		return SSOMessageCall{}, fmt.Errorf("read SSOPlatform SendMessageW IAT")
	}
	stubAddress := uintptr(binary.LittleEndian.Uint32(pointerBytes))
	stubSignature, ok := readRemote(process, stubAddress, 8)
	expectedSignature := []byte{0x53, 0x56, 0x57, 0x55, 0x8D, 0x6C, 0x24, 0x10}
	if !ok || !equalBytes(stubSignature, expectedSignature) {
		return SSOMessageCall{}, fmt.Errorf("SSOPlatform SendMessageW is not wrapped by the expected tracer: 0x%08X", stubAddress)
	}
	record, ok := readRemote(process, stubAddress+0x400, 64+ssoMessagePayloadLimit)
	if !ok {
		return SSOMessageCall{}, fmt.Errorf("read active SSO message record")
	}
	return decodeSSOMessageRecord(record, 0), nil
}

// InstallSSOMessageTracer wraps only SSOPlatform.dll's SendMessageW import and
// records WM_COPYDATA traffic. It never changes the message or its payload.
func InstallSSOMessageTracer(pid uint32, timeout time.Duration) (*SSOMessageTracer, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	tracer := &SSOMessageTracer{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			tracer.Close()
		}
	}()

	moduleBase, err := waitForModule(process, pid, "SSOPlatform.dll", timeout)
	if err != nil {
		return nil, err
	}
	iatAddress, targetAddress, err := findAttachedImportAddress(process, pid, moduleBase, "USER32.dll", "SendMessageW", timeout)
	if err != nil {
		return nil, fmt.Errorf("resolve SSOPlatform SendMessageW: %w", err)
	}
	tracer.iatAddress = iatAddress
	tracer.originalIAT, _ = readRemote(process, iatAddress, 4)
	if len(tracer.originalIAT) != 4 {
		return nil, fmt.Errorf("read SSOPlatform SendMessageW IAT at 0x%08X", iatAddress)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x2000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx SSO message trace: 0x%X (%v)", page, allocErr)
	}
	tracer.recordAddress = page + 0x400
	tracer.recordSize = 64 + ssoMessagePayloadLimit

	// On entry: [esp]=return, +4=hwnd, +8=message, +12=wParam,
	// +16=lParam. Preserve all nonvolatile registers and forward every call.
	stub := []byte{0x53, 0x56, 0x57, 0x55, 0x8D, 0x6C, 0x24, 0x10}
	stub = append(stub, 0x81, 0x7D, 0x08, 0x4A, 0x00, 0x00, 0x00) // cmp message,WM_COPYDATA
	notCopyDataJump := len(stub)
	stub = append(stub, 0x0F, 0x85, 0, 0, 0, 0)
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress))
	stub = append(stub, 0x8B, 0x45, 0x00, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+8))
	stub = append(stub, 0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+12))
	for stackOffset, recordOffset := range map[byte]uintptr{4: 16, 8: 20, 12: 24, 16: 28} {
		stub = append(stub, 0x8B, 0x45, stackOffset, 0xA3)
		stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+recordOffset))
	}
	stub = append(stub, 0x8B, 0x45, 0x10, 0x85, 0xC0) // COPYDATASTRUCT*
	nullStructJump := len(stub)
	stub = append(stub, 0x0F, 0x84, 0, 0, 0, 0)
	stub = append(stub, 0x8B, 0x08, 0x89, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+32))
	stub = append(stub, 0x8B, 0x48, 0x04, 0x89, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+36))
	stub = append(stub, 0x8B, 0x50, 0x08, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+40))
	stub = append(stub, 0x85, 0xD2)
	nullPayloadJump := len(stub)
	stub = append(stub, 0x74, 0)
	stub = append(stub, 0x81, 0xF9)
	stub = binary.LittleEndian.AppendUint32(stub, ssoMessagePayloadLimit)
	lengthOKJump := len(stub)
	stub = append(stub, 0x76, 0)
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, ssoMessagePayloadLimit)
	lengthOK := len(stub)
	stub[lengthOKJump+1] = byte(lengthOK - (lengthOKJump + 2))
	stub = append(stub, 0x89, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+48))
	stub = append(stub, 0x8B, 0xF2, 0xBF)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+64))
	stub = append(stub, 0xFC, 0xF3, 0xA4) // cld; rep movsb
	copyDone := len(stub)
	stub[nullPayloadJump+1] = byte(copyDone - (nullPayloadJump + 2))
	writeRelative32(stub[nullStructJump+2:nullStructJump+6], copyDone-(nullStructJump+6))

	forward := len(stub)
	writeRelative32(stub[notCopyDataJump+2:notCopyDataJump+6], forward-(notCopyDataJump+6))
	stub = append(stub, 0xFF, 0x75, 0x10, 0xFF, 0x75, 0x0C, 0xFF, 0x75, 0x08, 0xFF, 0x75, 0x04, 0xB8)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(targetAddress))
	stub = append(stub, 0xFF, 0xD0)
	stub = append(stub, 0x81, 0x7D, 0x08, 0x4A, 0x00, 0x00, 0x00)
	notRecordedJump := len(stub)
	stub = append(stub, 0x75, 0)
	stub = append(stub, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+44))
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(tracer.recordAddress+4))
	notRecorded := len(stub)
	stub[notRecordedJump+1] = byte(notRecorded - (notRecordedJump + 2))
	stub = append(stub, 0x5D, 0x5F, 0x5E, 0x5B, 0xC2, 0x10, 0x00)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write SSO message trace stub: %w", err)
	}
	pointer := make([]byte, 4)
	binary.LittleEndian.PutUint32(pointer, uint32(page))
	if !ensureRemoteBytes(process, iatAddress, pointer, pageReadWrite) {
		return nil, fmt.Errorf("patch SSOPlatform SendMessageW IAT at 0x%08X", iatAddress)
	}
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return tracer, nil
}

func (tracer *SSOMessageTracer) Capture(duration time.Duration) SSOMessageCapture {
	deadline := time.Now().Add(duration)
	result := SSOMessageCapture{PID: tracer.pid}
	for time.Now().Before(deadline) {
		record, ok := readRemote(tracer.process, tracer.recordAddress, tracer.recordSize)
		if ok {
			completed := binary.LittleEndian.Uint32(record[4:8])
			if completed != 0 && completed != tracer.lastCompleted {
				tracer.lastCompleted = completed
				call := decodeSSOMessageRecord(record, time.Since(tracer.started).Milliseconds())
				result.Calls = append(result.Calls, call)
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

func decodeSSOMessageRecord(record []byte, elapsedMS int64) SSOMessageCall {
	capturedLength := binary.LittleEndian.Uint32(record[48:52])
	if capturedLength > ssoMessagePayloadLimit {
		capturedLength = ssoMessagePayloadLimit
	}
	call := SSOMessageCall{
		ElapsedMS:    elapsedMS,
		CallsEntered: binary.LittleEndian.Uint32(record[0:4]), CallsCompleted: binary.LittleEndian.Uint32(record[4:8]),
		Caller:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[8:12])),
		ThreadID:    binary.LittleEndian.Uint32(record[12:16]),
		Window:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[16:20])),
		Message:     binary.LittleEndian.Uint32(record[20:24]),
		WParam:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[24:28])),
		LParam:      fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[28:32])),
		CopyDataID:  binary.LittleEndian.Uint32(record[32:36]),
		DataLength:  binary.LittleEndian.Uint32(record[36:40]),
		DataPointer: fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[40:44])),
		Result:      int32(binary.LittleEndian.Uint32(record[44:48])), CapturedLength: capturedLength,
	}
	call.PayloadHex = hex.EncodeToString(record[64 : 64+capturedLength])
	return call
}

func (tracer *SSOMessageTracer) Close() {
	if tracer == nil || tracer.process == 0 {
		return
	}
	if tracer.iatAddress != 0 && len(tracer.originalIAT) == 4 {
		ensureRemoteBytes(tracer.process, tracer.iatAddress, tracer.originalIAT, pageReadWrite)
	}
	syscall.CloseHandle(tracer.process)
	tracer.process = 0
}

func writeRelative32(target []byte, relative int) {
	binary.LittleEndian.PutUint32(target, uint32(int32(relative)))
}

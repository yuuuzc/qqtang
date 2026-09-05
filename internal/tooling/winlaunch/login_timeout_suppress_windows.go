package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	qqtModulesLoginResultRVA       = uintptr(0x8F77)
	qqtModulesLoginResultResumeRVA = uintptr(0x8F7C)
	qqtModulesEHRecordRVA          = uintptr(0x683BC)
	qqtModulesEHPrologRVA          = uintptr(0x539D0)
)

type LoginTimeoutSuppressionCapture struct {
	PID           uint32 `json:"pid"`
	ElapsedMS     int64  `json:"elapsed_ms"`
	Calls         uint32 `json:"calls"`
	ThreadID      uint32 `json:"thread_id,omitempty"`
	PatchAddress  string `json:"patch_address"`
	StubAddress   string `json:"stub_address"`
	OriginalBytes string `json:"original_bytes"`
	Behavior      string `json:"behavior"`
}

// LoginTimeoutSuppressor suppresses only QQTModules' verified status-3
// callback ("验证密码超时"). Other login statuses, including success, replay
// the original entry bytes and continue through the unmodified function.
type LoginTimeoutSuppressor struct {
	pid          uint32
	process      syscall.Handle
	started      time.Time
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	original     []byte
}

func InstallLoginTimeoutSuppressor(pid uint32, timeout time.Duration) (*LoginTimeoutSuppressor, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	suppressor := &LoginTimeoutSuppressor{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			suppressor.Close()
		}
	}()

	moduleBase, err := waitForModule(process, pid, "QQTModules.dll", timeout)
	if err != nil {
		return nil, err
	}
	suppressor.patchAddress = moduleBase + qqtModulesLoginResultRVA
	signature, ok := readRemote(process, suppressor.patchAddress, 10)
	if !ok || len(signature) != 10 {
		return nil, fmt.Errorf("read QQTModules login-result signature at 0x%08X", suppressor.patchAddress)
	}
	expected := make([]byte, 10)
	expected[0] = 0xB8 // mov eax, QQTModules exception record
	binary.LittleEndian.PutUint32(expected[1:5], uint32(moduleBase+qqtModulesEHRecordRVA))
	expected[5] = 0xE8 // call QQTModules EH prolog
	binary.LittleEndian.PutUint32(expected[6:10], uint32((moduleBase+qqtModulesEHPrologRVA)-(suppressor.patchAddress+10)))
	if !equalBytes(signature, expected) {
		return nil, fmt.Errorf("QQTModules login-result signature mismatch at 0x%08X: got %X want %X", suppressor.patchAddress, signature, expected)
	}
	suppressor.original = append([]byte(nil), signature[:5]...)

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx login-timeout suppressor: 0x%X (%v)", page, allocErr)
	}
	suppressor.stubAddress = page
	suppressor.record = page + 0x200

	stub := []byte{0x83, 0x7C, 0x24, 0x08, 0x03} // cmp dword [esp+8],3
	notTimeoutJump := len(stub)
	stub = append(stub, 0x75, 0) // jne replayOriginal
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(suppressor.record)) // lock inc calls
	stub = append(stub, 0x64, 0xA1, 0x24, 0, 0, 0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(suppressor.record+4)) // thread id
	stub = append(stub, 0x33, 0xC0, 0xC2, 0x08, 0x00)                          // xor eax,eax; ret 8
	replayOffset := len(stub)
	stub[notTimeoutJump+1] = byte(replayOffset - (notTimeoutJump + 2))
	stub = append(stub, suppressor.original...)
	stub = appendRelativeJump(stub, page+uintptr(len(stub)), moduleBase+qqtModulesLoginResultResumeRVA)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write login-timeout suppression stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(suppressor.patchAddress+5)))
	if !ensureRemoteBytes(process, suppressor.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch QQTModules login result at 0x%08X", suppressor.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), suppressor.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return suppressor, nil
}

func (suppressor *LoginTimeoutSuppressor) Capture(duration time.Duration) LoginTimeoutSuppressionCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		waitResult, _, _ := procWaitForSingle.Call(uintptr(suppressor.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(suppressor.process, suppressor.record, 8)
	result := LoginTimeoutSuppressionCapture{
		PID: suppressor.pid, ElapsedMS: time.Since(suppressor.started).Milliseconds(),
		PatchAddress:  fmt.Sprintf("0x%08X", suppressor.patchAddress),
		StubAddress:   fmt.Sprintf("0x%08X", suppressor.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", suppressor.original),
		Behavior:      "QQTModules login result status 3 only: return success without creating the stale password-timeout dialog",
	}
	if len(record) == 8 {
		result.Calls = binary.LittleEndian.Uint32(record[:4])
		result.ThreadID = binary.LittleEndian.Uint32(record[4:8])
	}
	return result
}

func (suppressor *LoginTimeoutSuppressor) Close() {
	if suppressor == nil || suppressor.process == 0 {
		return
	}
	if suppressor.patchAddress != 0 && len(suppressor.original) != 0 {
		ensureRemoteBytes(suppressor.process, suppressor.patchAddress, suppressor.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(suppressor.process), suppressor.patchAddress, uintptr(len(suppressor.original)))
	}
	syscall.CloseHandle(suppressor.process)
	suppressor.process = 0
}

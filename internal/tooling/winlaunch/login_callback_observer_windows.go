package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

type LoginCallbackObservation struct {
	PID             uint32 `json:"pid"`
	ElapsedMS       int64  `json:"elapsed_ms"`
	Calls           uint32 `json:"calls"`
	ThreadID        uint32 `json:"thread_id,omitempty"`
	OriginalStatus  uint32 `json:"original_status"`
	DeliveredStatus uint32 `json:"delivered_status"`
	OriginalLength  uint32 `json:"original_length"`
	DispatchObject  string `json:"dispatch_object,omitempty"`
	DispatchVTable  string `json:"dispatch_vtable,omitempty"`
	DispatchTarget  string `json:"dispatch_target,omitempty"`
	PatchAddress    string `json:"patch_address"`
	StubAddress     string `json:"stub_address"`
	OriginalBytes   string `json:"original_bytes"`
	Behavior        string `json:"behavior"`
}

// LoginCallbackObserver records the native Client login result. Its password
// rejection gate is disarmed by default. While armed, only legacy SSO status 4
// is translated to native Client status 2, whose original UI path displays
// "验证密码失败,请输入正确的密码." and calls LoginFailed() to end the wait state.
// No payload bytes are changed.
type LoginCallbackObserver struct {
	pid          uint32
	process      syscall.Handle
	started      time.Time
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	gate         uintptr
	original     []byte
}

func InstallLoginCallbackObserver(pid uint32, timeout time.Duration) (*LoginCallbackObserver, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	observer := &LoginCallbackObserver{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			observer.Close()
		}
	}()

	moduleBase, err := waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return nil, err
	}
	observer.patchAddress = moduleBase + clientLoginCallbackRVA
	observer.original, _ = readRemote(process, observer.patchAddress, len(clientLoginCallbackSignature))
	if len(observer.original) != len(clientLoginCallbackSignature) || !equalBytes(observer.original, clientLoginCallbackSignature) {
		return nil, fmt.Errorf("Client login callback signature mismatch at 0x%08X: got %X", observer.patchAddress, observer.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx login callback observer: 0x%X (%v)", page, allocErr)
	}
	observer.stubAddress = page
	observer.record = page + 0x200
	observer.gate = observer.record + 28

	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(observer.record)) // lock inc calls
	stub = append(stub, 0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(observer.record+4)) // GUI thread id
	stub = append(stub, 0x8B, 0x45, 0xB0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(observer.record+8)) // [ebp-0x50] status
	stub = append(stub, 0x8B, 0x85, 0xAC, 0xF7, 0xFF, 0xFF, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(observer.record+12)) // [ebp-0x854] length
	stub = append(stub, 0x8B, 0x85, 0x6C, 0xF7, 0xFF, 0xFF, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(observer.record+16)) // [ebp-0x894] dispatch object
	stub = append(stub, 0x85, 0xC0)
	nullObjectJump := len(stub)
	stub = append(stub, 0x74, 0)
	stub = append(stub, 0x8B, 0x08, 0x89, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(observer.record+20)) // vtable
	stub = append(stub, 0x8B, 0x51, 0x20, 0x89, 0x15)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(observer.record+24)) // slot +0x20 target
	stub[nullObjectJump+1] = byte(len(stub) - (nullObjectJump + 2))
	stub = append(stub, 0x83, 0x3D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(observer.gate))
	stub = append(stub, 0x00) // cmp dword [gate],0
	disarmedJump := len(stub)
	stub = append(stub, 0x74, 0)
	stub = append(stub, 0x83, 0x7D, 0xB0, 0x04) // cmp dword [ebp-0x50],4
	notLegacyFailureJump := len(stub)
	stub = append(stub, 0x75, 0)
	stub = append(stub, 0xC7, 0x45, 0xB0, 0x02, 0x00, 0x00, 0x00) // status 2
	statusRecordedOffset := len(stub)
	stub[disarmedJump+1] = byte(statusRecordedOffset - (disarmedJump + 2))
	stub[notLegacyFailureJump+1] = byte(statusRecordedOffset - (notLegacyFailureJump + 2))
	stub = append(stub, 0x8B, 0x45, 0xB0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(observer.record+32)) // delivered status
	stub = append(stub, 0x61, 0x9D)                                           // popad; popfd
	stub = append(stub, clientLoginCallbackSignature...)
	stub = appendRelativeJump(stub, page+uintptr(len(stub)), moduleBase+clientLoginCallbackResumeRVA)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write login callback observer stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0, 0x90}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(observer.patchAddress+5)))
	if !ensureRemoteBytes(process, observer.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch Client login callback observer at 0x%08X", observer.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), observer.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return observer, nil
}

// ArmPasswordRejection enables the single narrow status translation used
// after the Go account server has rejected the captured password.
func (observer *LoginCallbackObserver) ArmPasswordRejection() error {
	if observer == nil || observer.process == 0 || observer.gate == 0 {
		return fmt.Errorf("login callback observer is not active")
	}
	value := make([]byte, 4)
	binary.LittleEndian.PutUint32(value, 1)
	if err := writeRemote(observer.process, observer.gate, value); err != nil {
		return fmt.Errorf("arm native password rejection: %w", err)
	}
	return nil
}

func (observer *LoginCallbackObserver) DisarmPasswordRejection() {
	if observer == nil || observer.process == 0 || observer.gate == 0 {
		return
	}
	_ = writeRemote(observer.process, observer.gate, []byte{0, 0, 0, 0})
}

func (observer *LoginCallbackObserver) Calls() uint32 {
	if observer == nil || observer.process == 0 {
		return 0
	}
	record, ok := readRemote(observer.process, observer.record, 4)
	if !ok || len(record) != 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(record)
}

func (observer *LoginCallbackObserver) CaptureAfter(previousCalls uint32, duration time.Duration) LoginCallbackObservation {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(observer.process, observer.record, 36)
		if ok && len(record) == 36 && binary.LittleEndian.Uint32(record[0:4]) > previousCalls {
			return observer.captureRecord(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(observer.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	record, _ := readRemote(observer.process, observer.record, 36)
	return observer.captureRecord(record)
}

func (observer *LoginCallbackObserver) captureRecord(record []byte) LoginCallbackObservation {
	result := LoginCallbackObservation{
		PID: observer.pid, ElapsedMS: time.Since(observer.started).Milliseconds(),
		PatchAddress: fmt.Sprintf("0x%08X", observer.patchAddress), StubAddress: fmt.Sprintf("0x%08X", observer.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", observer.original),
		Behavior:      "gate-controlled local password rejection: translate only legacy SSO status 4 to native password-error status 2",
	}
	if len(record) == 36 {
		result.Calls = binary.LittleEndian.Uint32(record[0:4])
		result.ThreadID = binary.LittleEndian.Uint32(record[4:8])
		result.OriginalStatus = binary.LittleEndian.Uint32(record[8:12])
		result.OriginalLength = binary.LittleEndian.Uint32(record[12:16])
		result.DeliveredStatus = binary.LittleEndian.Uint32(record[32:36])
		if value := binary.LittleEndian.Uint32(record[16:20]); value != 0 {
			result.DispatchObject = fmt.Sprintf("0x%08X", value)
		}
		if value := binary.LittleEndian.Uint32(record[20:24]); value != 0 {
			result.DispatchVTable = fmt.Sprintf("0x%08X", value)
		}
		if value := binary.LittleEndian.Uint32(record[24:28]); value != 0 {
			result.DispatchTarget = fmt.Sprintf("0x%08X", value)
		}
	}
	return result
}

func (observer *LoginCallbackObserver) Close() {
	if observer == nil || observer.process == 0 {
		return
	}
	if observer.patchAddress != 0 && len(observer.original) != 0 {
		observer.DisarmPasswordRejection()
		ensureRemoteBytes(observer.process, observer.patchAddress, observer.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(observer.process), observer.patchAddress, uintptr(len(observer.original)))
	}
	syscall.CloseHandle(observer.process)
	observer.process = 0
}

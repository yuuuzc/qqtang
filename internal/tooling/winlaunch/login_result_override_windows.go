package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
	"unicode/utf16"
)

const (
	ssoResultDispatchRVA = uintptr(0x1312B)
	ssoResultResumeRVA   = uintptr(0x13134)
)

var ssoResultDispatchSignature = []byte{
	0x8B, 0x4D, 0xAC, // mov ecx,[ebp-0x54]
	0x53,                         // push ebx (ITXData*)
	0x68, 0x80, 0x19, 0x00, 0x00, // push 0x1980
}

type LoginResultOverrideCapture struct {
	PID           uint32 `json:"pid"`
	ElapsedMS     int64  `json:"elapsed_ms"`
	Calls         uint32 `json:"calls"`
	DataPointer   string `json:"data_pointer,omitempty"`
	SetterVTable  string `json:"setter_vtable,omitempty"`
	SetterResult  int32  `json:"setter_result"`
	ThreadID      uint32 `json:"thread_id,omitempty"`
	PatchAddress  string `json:"patch_address"`
	StubAddress   string `json:"stub_address"`
	SuccessField  string `json:"success_field"`
	SuccessValue  uint32 `json:"success_value"`
	OriginalBytes string `json:"original_bytes"`
}

// LoginResultOverride is a narrowly scoped compatibility patch for the retired
// TXSSO result boundary. It changes only the bSSO_Result_bSucceed field on the
// ITXData object immediately before SSOPlatform serializes and dispatches the
// normal 0x1980 result. The original instruction bytes are restored by Close.
type LoginResultOverride struct {
	pid          uint32
	process      syscall.Handle
	started      time.Time
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	original     []byte
}

func InstallLoginResultOverride(pid uint32, timeout time.Duration) (*LoginResultOverride, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	override := &LoginResultOverride{pid: pid, process: process, started: time.Now()}
	failed := true
	defer func() {
		if failed {
			override.Close()
		}
	}()

	moduleBase, err := waitForModule(process, pid, "SSOPlatform.dll", timeout)
	if err != nil {
		return nil, err
	}
	override.patchAddress = moduleBase + ssoResultDispatchRVA
	override.original, _ = readRemote(process, override.patchAddress, len(ssoResultDispatchSignature))
	if len(override.original) != len(ssoResultDispatchSignature) || !equalBytes(override.original, ssoResultDispatchSignature) {
		return nil, fmt.Errorf("SSO result callsite signature mismatch at 0x%08X: got %X", override.patchAddress, override.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx login result override: 0x%X (%v)", page, allocErr)
	}
	override.stubAddress = page
	override.record = page + 0x300

	// Store a real BSTR-compatible field name: byte length immediately before
	// the UTF-16 characters. ITXData's setter may use SysStringLen.
	fieldName := "bSSO_Result_bSucceed"
	fieldWords := utf16.Encode([]rune(fieldName))
	fieldStorage := make([]byte, 4+(len(fieldWords)+1)*2)
	binary.LittleEndian.PutUint32(fieldStorage[0:4], uint32(len(fieldWords)*2))
	for index, word := range fieldWords {
		binary.LittleEndian.PutUint16(fieldStorage[4+index*2:], word)
	}
	fieldAddress := page + 0x104
	if err := writeRemote(process, page+0x100, fieldStorage); err != nil {
		return nil, fmt.Errorf("write login result field BSTR: %w", err)
	}

	stub := []byte{0x9C, 0x60} // pushfd; pushad
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(override.record))
	stub = append(stub, 0x89, 0x1D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(override.record+4)) // ITXData* in EBX
	stub = append(stub, 0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(override.record+16)) // thread id
	stub = append(stub, 0x85, 0xDB)                                           // test ebx,ebx
	nullDataJump := len(stub)
	stub = append(stub, 0x74, 0x00)
	stub = append(stub, 0x8B, 0x03, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(override.record+8)) // vtable
	stub = append(stub, 0x85, 0xC0)                                          // test eax,eax
	nullVTableJump := len(stub)
	stub = append(stub, 0x74, 0x00)
	stub = append(stub, 0x6A, 0x01, 0x68)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(fieldAddress))
	stub = append(stub, 0x53, 0xFF, 0x90, 0xCC, 0x00, 0x00, 0x00, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(override.record+12)) // HRESULT
	setterDone := len(stub)
	stub[nullDataJump+1] = byte(setterDone - (nullDataJump + 2))
	stub[nullVTableJump+1] = byte(setterDone - (nullVTableJump + 2))
	stub = append(stub, 0x61, 0x9D) // popad; popfd
	stub = append(stub, ssoResultDispatchSignature...)
	stub = appendRelativeJump(stub, page+uintptr(len(stub)), moduleBase+ssoResultResumeRVA)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write login result override stub: %w", err)
	}

	patch := make([]byte, len(ssoResultDispatchSignature))
	patch[0] = 0xE9
	relative := uint32(page - (override.patchAddress + 5))
	binary.LittleEndian.PutUint32(patch[1:5], relative)
	for index := 5; index < len(patch); index++ {
		patch[index] = 0x90
	}
	if !ensureRemoteBytes(process, override.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch SSO result callsite at 0x%08X", override.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), override.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return override, nil
}

func (override *LoginResultOverride) Capture(duration time.Duration) LoginResultOverrideCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		waitResult, _, _ := procWaitForSingle.Call(uintptr(override.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(override.process, override.record, 20)
	result := LoginResultOverrideCapture{
		PID: override.pid, ElapsedMS: time.Since(override.started).Milliseconds(),
		PatchAddress: fmt.Sprintf("0x%08X", override.patchAddress),
		StubAddress:  fmt.Sprintf("0x%08X", override.stubAddress),
		SuccessField: "bSSO_Result_bSucceed", SuccessValue: 1,
		OriginalBytes: fmt.Sprintf("%X", override.original),
	}
	if len(record) == 20 {
		result.Calls = binary.LittleEndian.Uint32(record[0:4])
		if pointer := binary.LittleEndian.Uint32(record[4:8]); pointer != 0 {
			result.DataPointer = fmt.Sprintf("0x%08X", pointer)
		}
		if pointer := binary.LittleEndian.Uint32(record[8:12]); pointer != 0 {
			result.SetterVTable = fmt.Sprintf("0x%08X", pointer)
		}
		result.SetterResult = int32(binary.LittleEndian.Uint32(record[12:16]))
		result.ThreadID = binary.LittleEndian.Uint32(record[16:20])
	}
	return result
}

func (override *LoginResultOverride) CaptureUntilFirst(duration time.Duration) LoginResultOverrideCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(override.process, override.record, 20)
		if ok && len(record) == 20 && binary.LittleEndian.Uint32(record[0:4]) != 0 {
			return override.captureRecord(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(override.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(override.process, override.record, 20)
	return override.captureRecord(record)
}

func (override *LoginResultOverride) captureRecord(record []byte) LoginResultOverrideCapture {
	result := LoginResultOverrideCapture{
		PID: override.pid, ElapsedMS: time.Since(override.started).Milliseconds(),
		PatchAddress: fmt.Sprintf("0x%08X", override.patchAddress),
		StubAddress:  fmt.Sprintf("0x%08X", override.stubAddress),
		SuccessField: "bSSO_Result_bSucceed", SuccessValue: 1,
		OriginalBytes: fmt.Sprintf("%X", override.original),
	}
	if len(record) == 20 {
		result.Calls = binary.LittleEndian.Uint32(record[0:4])
		if pointer := binary.LittleEndian.Uint32(record[4:8]); pointer != 0 {
			result.DataPointer = fmt.Sprintf("0x%08X", pointer)
		}
		if pointer := binary.LittleEndian.Uint32(record[8:12]); pointer != 0 {
			result.SetterVTable = fmt.Sprintf("0x%08X", pointer)
		}
		result.SetterResult = int32(binary.LittleEndian.Uint32(record[12:16]))
		result.ThreadID = binary.LittleEndian.Uint32(record[16:20])
	}
	return result
}

func (override *LoginResultOverride) Close() {
	if override == nil || override.process == 0 {
		return
	}
	if override.patchAddress != 0 && len(override.original) != 0 {
		ensureRemoteBytes(override.process, override.patchAddress, override.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(override.process), override.patchAddress, uintptr(len(override.original)))
	}
	syscall.CloseHandle(override.process)
	override.process = 0
}

func appendRelativeJump(code []byte, instructionAddress, target uintptr) []byte {
	code = append(code, 0xE9)
	return binary.LittleEndian.AppendUint32(code, uint32(target-(instructionAddress+5)))
}

func equalBytes(left, right []byte) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

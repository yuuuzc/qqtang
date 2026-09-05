package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	clientLoginCallbackRVA       = uintptr(0x2E0C8)
	clientLoginCallbackResumeRVA = uintptr(0x2E0CE)
)

var clientLoginCallbackSignature = []byte{0x8B, 0x8D, 0xAC, 0xF7, 0xFF, 0xFF}

type LoginCallbackOverrideCapture struct {
	PID            uint32 `json:"pid"`
	ElapsedMS      int64  `json:"elapsed_ms"`
	Calls          uint32 `json:"calls"`
	ThreadID       uint32 `json:"thread_id,omitempty"`
	OriginalStatus uint32 `json:"original_status"`
	OriginalLength uint32 `json:"original_length"`
	LocalLength    uint32 `json:"local_length"`
	PatchAddress   string `json:"patch_address"`
	StubAddress    string `json:"stub_address"`
	OriginalBytes  string `json:"original_bytes"`
	Behavior       string `json:"behavior"`
}

// LoginCallbackOverride replaces only a failed 0x1980 callback's final local
// result buffer. It leaves SSOPlatform and its ITXData object untouched. The
// client receives the same opaque layout its normal success branch constructs:
// 37-byte profile, 16-byte GT key, length-prefixed ST, and length-prefixed HTTP
// ST. Close restores the exact six original Client.exe bytes.
type LoginCallbackOverride struct {
	pid          uint32
	process      syscall.Handle
	started      time.Time
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	original     []byte
	localLength  uint32
}

func BuildLocalLoginCallbackPayload(nickname string) ([]byte, error) {
	return BuildLocalLoginCallbackPayloadWithGender(nickname, 0)
}

func BuildLocalLoginCallbackPayloadWithGender(nickname string, gender byte) ([]byte, error) {
	if nickname == "" {
		nickname = "LocalPlayer"
	}
	if gender > 1 {
		return nil, fmt.Errorf("gender %d is outside the observed 0..1 range", gender)
	}
	encodedNickname, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(nickname))
	if err != nil {
		return nil, fmt.Errorf("encode nickname as GBK: %w", err)
	}
	if len(encodedNickname) > 31 {
		return nil, fmt.Errorf("nickname must be at most 31 GBK bytes")
	}
	profile := make([]byte, 37)
	binary.LittleEndian.PutUint16(profile[0:2], 0) // face index
	profile[2] = 18                                // age
	profile[3] = gender
	copy(profile[5:37], encodedNickname)

	gtKey := []byte{
		0x10, 0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17,
		0x18, 0x19, 0x1A, 0x1B, 0x1C, 0x1D, 0x1E, 0x1F,
	}
	serviceTicket := []byte{
		0x20, 0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27,
		0x28, 0x29, 0x2A, 0x2B, 0x2C, 0x2D, 0x2E, 0x2F,
		0x30, 0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37,
		0x38, 0x39, 0x3A, 0x3B, 0x3C, 0x3D, 0x3E, 0x3F,
	}
	httpTicket := []byte{
		0x40, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47,
		0x48, 0x49, 0x4A, 0x4B, 0x4C, 0x4D, 0x4E, 0x4F,
		0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x56, 0x57,
		0x58, 0x59, 0x5A, 0x5B, 0x5C, 0x5D, 0x5E, 0x5F,
	}
	payload := make([]byte, 0, len(profile)+len(gtKey)+2+len(serviceTicket)+len(httpTicket))
	payload = append(payload, profile...)
	payload = append(payload, gtKey...)
	payload = append(payload, byte(len(serviceTicket)))
	payload = append(payload, serviceTicket...)
	payload = append(payload, byte(len(httpTicket)))
	payload = append(payload, httpTicket...)
	return payload, nil
}

func InstallLoginCallbackOverride(pid uint32, timeout time.Duration, payload []byte) (*LoginCallbackOverride, error) {
	if len(payload) == 0 || len(payload) > 0x7FF {
		return nil, fmt.Errorf("local callback payload must be 1..2047 bytes")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	override := &LoginCallbackOverride{pid: pid, process: process, started: time.Now(), localLength: uint32(len(payload))}
	failed := true
	defer func() {
		if failed {
			override.Close()
		}
	}()

	moduleBase, err := waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return nil, err
	}
	override.patchAddress = moduleBase + clientLoginCallbackRVA
	signatureDeadline := time.Now().Add(timeout)
	for time.Now().Before(signatureDeadline) {
		override.original, _ = readRemote(process, override.patchAddress, len(clientLoginCallbackSignature))
		if len(override.original) == len(clientLoginCallbackSignature) && equalBytes(override.original, clientLoginCallbackSignature) {
			break
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(override.original) != len(clientLoginCallbackSignature) || !equalBytes(override.original, clientLoginCallbackSignature) {
		return nil, fmt.Errorf("Client login callback signature mismatch at 0x%08X: got %X", override.patchAddress, override.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx local login callback: 0x%X (%v)", page, allocErr)
	}
	override.stubAddress = page
	override.record = page + 0x200
	payloadAddress := page + 0x300
	if err := writeRemote(process, payloadAddress, payload); err != nil {
		return nil, fmt.Errorf("write local login callback payload: %w", err)
	}

	stub := []byte{0x9C, 0x60}                  // pushfd; pushad
	stub = append(stub, 0x83, 0x7D, 0xB0, 0x00) // cmp dword [ebp-0x50],0
	alreadySuccessJump := len(stub)
	stub = append(stub, 0x74, 0)
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(override.record))
	stub = append(stub, 0x8B, 0x45, 0xB0, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(override.record+8))
	stub = append(stub, 0x8B, 0x85, 0xAC, 0xF7, 0xFF, 0xFF, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(override.record+12))
	stub = append(stub, 0x64, 0xA1, 0x24, 0x00, 0x00, 0x00, 0xA3)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(override.record+4))
	stub = append(stub, 0x8D, 0xBD, 0xB0, 0xF7, 0xFF, 0xFF, 0xBE)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(payloadAddress))
	stub = append(stub, 0xB9)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(len(payload)))
	stub = append(stub, 0xFC, 0xF3, 0xA4)
	stub = append(stub, 0xC7, 0x85, 0xAC, 0xF7, 0xFF, 0xFF)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(len(payload)))
	stub = append(stub, 0xC7, 0x45, 0xB0, 0, 0, 0, 0)
	overrideDone := len(stub)
	stub[alreadySuccessJump+1] = byte(overrideDone - (alreadySuccessJump + 2))
	stub = append(stub, 0x61, 0x9D) // popad; popfd
	stub = append(stub, clientLoginCallbackSignature...)
	stub = appendRelativeJump(stub, page+uintptr(len(stub)), moduleBase+clientLoginCallbackResumeRVA)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write local login callback stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0, 0x90}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(override.patchAddress+5)))
	if !ensureRemoteBytes(process, override.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch Client login callback at 0x%08X", override.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), override.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return override, nil
}

func (override *LoginCallbackOverride) CaptureUntilFirst(duration time.Duration) LoginCallbackOverrideCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(override.process, override.record, 16)
		if ok && len(record) == 16 && binary.LittleEndian.Uint32(record[0:4]) != 0 {
			return override.captureRecord(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(override.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	record, _ := readRemote(override.process, override.record, 16)
	return override.captureRecord(record)
}

// Capture keeps the override installed for the full observation interval or
// until the client exits. This is the local compatibility mode: old SSO can
// deliver more than one terminal callback after its first timeout, so
// restoring the patch after the first call leaves later callbacks unhandled.
func (override *LoginCallbackOverride) Capture(duration time.Duration) LoginCallbackOverrideCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		waitResult, _, _ := procWaitForSingle.Call(uintptr(override.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(override.process, override.record, 16)
	return override.captureRecord(record)
}

// CaptureUntilClientExit keeps the compatibility callback owned by the
// Client.exe lifetime instead of an arbitrary launcher timeout.  The helper
// remains a sibling process for fault isolation, but it cannot outlive its
// target and a client that stays open longer than 24 hours cannot silently
// lose the patch.
func (override *LoginCallbackOverride) CaptureUntilClientExit() LoginCallbackOverrideCapture {
	result := override.captureRecord(nil)
	for {
		if record, ok := readRemote(override.process, override.record, 16); ok {
			result = override.captureRecord(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(override.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return result
}

func (override *LoginCallbackOverride) captureRecord(record []byte) LoginCallbackOverrideCapture {
	result := LoginCallbackOverrideCapture{
		PID: override.pid, ElapsedMS: time.Since(override.started).Milliseconds(), LocalLength: override.localLength,
		PatchAddress: fmt.Sprintf("0x%08X", override.patchAddress), StubAddress: fmt.Sprintf("0x%08X", override.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", override.original),
		Behavior:      "failed 0x1980 final callback only: replace status/profile/key/ST/HTTP-ST with deterministic local data",
	}
	if len(record) == 16 {
		result.Calls = binary.LittleEndian.Uint32(record[0:4])
		result.ThreadID = binary.LittleEndian.Uint32(record[4:8])
		result.OriginalStatus = binary.LittleEndian.Uint32(record[8:12])
		result.OriginalLength = binary.LittleEndian.Uint32(record[12:16])
	}
	return result
}

func (override *LoginCallbackOverride) Close() {
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

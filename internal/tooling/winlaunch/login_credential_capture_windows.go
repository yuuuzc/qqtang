package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	clientNativeLoginRVA       = uintptr(0x8C3A0)
	clientNativeLoginResumeRVA = clientNativeLoginRVA + 9
	legacyLoginPasswordBytes   = 16
)

var clientNativeLoginSignature = []byte{0x55, 0x8B, 0xEC, 0x81, 0xEC, 0x90, 0x02, 0x00, 0x00}

type LocalLoginCredential struct {
	UIN      uint32
	Password []byte
}

type LoginCredentialCapture struct {
	pid          uint32
	process      syscall.Handle
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	original     []byte
}

// InstallLoginCredentialCapture observes only the verified native Python
// Login(uin, subUin, password, verifyCode) binding. It does not alter arguments
// or the SSO result and keeps at most the legacy 16-byte password until Take is
// called. Close restores the exact Client.exe prologue.
func InstallLoginCredentialCapture(pid uint32, timeout time.Duration) (*LoginCredentialCapture, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	capture := &LoginCredentialCapture{pid: pid, process: process}
	failed := true
	defer func() {
		if failed {
			capture.Close()
		}
	}()
	moduleBase, err := waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return nil, err
	}
	capture.patchAddress = moduleBase + clientNativeLoginRVA
	deadline := time.Now().Add(timeout)
	for {
		capture.original, _ = readRemote(process, capture.patchAddress, len(clientNativeLoginSignature))
		if equalBytes(capture.original, clientNativeLoginSignature) {
			break
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(process), 0)
		if waitResult == waitObject0 {
			return nil, fmt.Errorf("Client.exe exited before native Login code became ready")
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("Client native Login signature not ready at 0x%08X before %s timeout: got %X", capture.patchAddress, timeout, capture.original)
		}
		time.Sleep(25 * time.Millisecond)
	}
	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx login credential capture: 0x%X (%v)", page, allocErr)
	}
	capture.stubAddress = page
	capture.record = page + 0x200 // calls:u32, uin:u32, length:u32, password[17]
	stub := buildLoginCredentialCaptureStub(page, capture.record, moduleBase)
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write login credential capture stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0, 0x90, 0x90, 0x90, 0x90}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(capture.patchAddress+5)))
	if !ensureRemoteBytes(process, capture.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch Client native Login at 0x%08X", capture.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), capture.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return capture, nil
}

func buildLoginCredentialCaptureStub(page, record, moduleBase uintptr) []byte {
	stub := []byte{0x9C, 0x60}                        // pushfd; pushad
	stub = append(stub, 0x8B, 0x44, 0x24, 0x28, 0xA3) // mov eax,[esp+40]; mov [uin],eax
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+4))
	stub = append(stub, 0x8B, 0x74, 0x24, 0x30, 0xBF) // mov esi,[esp+48]; mov edi,password
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+12))
	stub = append(stub, 0x31, 0xC9, 0x85, 0xF6) // xor ecx,ecx; test esi,esi
	nullJump := len(stub)
	stub = append(stub, 0x74, 0)
	loop := len(stub)
	stub = append(stub, 0x83, 0xF9, legacyLoginPasswordBytes) // cmp ecx,16
	limitJump := len(stub)
	stub = append(stub, 0x73, 0)
	stub = append(stub, 0x8A, 0x04, 0x0E, 0x88, 0x04, 0x0F, 0x84, 0xC0) // copy byte; test al
	zeroJump := len(stub)
	stub = append(stub, 0x74, 0, 0x41, 0xEB, 0) // jz done; inc ecx; jmp loop
	stub[len(stub)-1] = byte(int8(loop - len(stub)))
	done := len(stub)
	stub[nullJump+1] = byte(done - (nullJump + 2))
	stub[limitJump+1] = byte(done - (limitJump + 2))
	stub[zeroJump+1] = byte(done - (zeroJump + 2))
	stub = append(stub, 0x89, 0x0D)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record+8))
	stub = append(stub, 0xC6, 0x47, legacyLoginPasswordBytes, 0x00) // terminate
	// Publish the record only after all credential bytes are complete. LOCK INC
	// is the release point observed by Take, so retries cannot expose a partly
	// written UIN/password record.
	stub = append(stub, 0xF0, 0xFF, 0x05)
	stub = binary.LittleEndian.AppendUint32(stub, uint32(record))
	stub = append(stub, 0x61, 0x9D) // popad; popfd
	stub = append(stub, clientNativeLoginSignature...)
	return appendRelativeJump(stub, page+uintptr(len(stub)), moduleBase+clientNativeLoginResumeRVA)
}

func (capture *LoginCredentialCapture) TakeUntilFirst(timeout time.Duration) (LocalLoginCredential, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if credential, ok := capture.take(); ok {
			return credential, nil
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(capture.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	return LocalLoginCredential{}, fmt.Errorf("Client native Login credential was not observed")
}

func (capture *LoginCredentialCapture) take() (LocalLoginCredential, bool) {
	record, ok := readRemote(capture.process, capture.record, 12+legacyLoginPasswordBytes+1)
	if !ok || len(record) < 12 || binary.LittleEndian.Uint32(record[:4]) == 0 {
		return LocalLoginCredential{}, false
	}
	length := int(binary.LittleEndian.Uint32(record[8:12]))
	if length < 0 || length > legacyLoginPasswordBytes {
		return LocalLoginCredential{}, false
	}
	credential := LocalLoginCredential{
		UIN:      binary.LittleEndian.Uint32(record[4:8]),
		Password: append([]byte(nil), record[12:12+length]...),
	}
	_ = writeRemote(capture.process, capture.record, make([]byte, 12+legacyLoginPasswordBytes+1))
	return credential, true
}

func (capture *LoginCredentialCapture) Close() error {
	if capture == nil {
		return nil
	}
	var first error
	if capture.process != 0 && capture.patchAddress != 0 && len(capture.original) != 0 {
		if !ensureRemoteBytes(capture.process, capture.patchAddress, capture.original, pageExecuteReadWrite) {
			first = fmt.Errorf("restore Client native Login at 0x%08X", capture.patchAddress)
		} else {
			procFlushInstruction.Call(uintptr(capture.process), capture.patchAddress, uintptr(len(capture.original)))
		}
	}
	if capture.process != 0 {
		if capture.stubAddress != 0 {
			procVirtualFreeEx.Call(uintptr(capture.process), capture.stubAddress, 0, memRelease)
		}
		syscall.CloseHandle(capture.process)
		capture.process = 0
	}
	return first
}

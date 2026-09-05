package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const qqtDirRandomShopServerMethodRVA = uintptr(0x2030B)

var qqtDirRandomShopServerMethodSignature = []byte{0x9C, 0x89, 0x2C, 0x24, 0x8B, 0xEC}

// ShopServerProbeCapture describes one diagnostic override of
// QQTDir::GetRandShopServerID. The override only substitutes a caller-selected
// server ID; it does not alter any directory object or client file.
type ShopServerProbeCapture struct {
	PID           uint32 `json:"pid"`
	Calls         uint32 `json:"calls"`
	Directory     string `json:"directory_object,omitempty"`
	ServerID      uint32 `json:"server_id"`
	PatchAddress  string `json:"patch_address"`
	StubAddress   string `json:"stub_address"`
	OriginalBytes string `json:"original_bytes"`
	Behavior      string `json:"behavior"`
}

type ShopServerProbe struct {
	pid          uint32
	serverID     uint32
	process      syscall.Handle
	patchAddress uintptr
	stubAddress  uintptr
	record       uintptr
	original     []byte
}

// InstallShopServerProbe temporarily replaces the packed QQTDir method used by
// the Python go2shop() gate. It is intentionally a live diagnostic: Close
// restores the exact original bytes.
func InstallShopServerProbe(pid, serverID uint32, timeout time.Duration) (*ShopServerProbe, error) {
	if pid == 0 || serverID == 0 {
		return nil, fmt.Errorf("pid and server ID must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	probe := &ShopServerProbe{pid: pid, serverID: serverID, process: process}
	failed := true
	defer func() {
		if failed {
			probe.Close()
		}
	}()

	moduleBase, err := waitForModule(process, pid, "QQTDir.dll", timeout)
	if err != nil {
		return nil, err
	}
	probe.patchAddress = moduleBase + qqtDirRandomShopServerMethodRVA
	probe.original, _ = readRemote(process, probe.patchAddress, len(qqtDirRandomShopServerMethodSignature))
	if len(probe.original) != len(qqtDirRandomShopServerMethodSignature) ||
		!equalBytes(probe.original, qqtDirRandomShopServerMethodSignature) {
		return nil, fmt.Errorf("QQTDir random-shop signature mismatch at 0x%08X: got %X", probe.patchAddress, probe.original)
	}

	page, _, allocErr := procVirtualAllocEx.Call(uintptr(process), 0, 0x1000, memReserve|memCommit, pageExecuteReadWrite)
	if page == 0 || page > 0xFFFFFFFF {
		return nil, fmt.Errorf("VirtualAllocEx shop-server probe: 0x%X (%v)", page, allocErr)
	}
	probe.stubAddress = page
	probe.record = page + 0x100
	stub := []byte{0xF0, 0xFF, 0x05} // lock inc dword ptr [calls]
	stub = binary.LittleEndian.AppendUint32(stub, uint32(probe.record))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x04, 0xA3) // mov eax,[esp+4]; mov [directory object],eax
	stub = binary.LittleEndian.AppendUint32(stub, uint32(probe.record+4))
	stub = append(stub, 0x8B, 0x44, 0x24, 0x08, 0xC7, 0x00) // mov eax,[esp+8]; mov dword ptr [eax],serverID
	stub = binary.LittleEndian.AppendUint32(stub, serverID)
	stub = append(stub, 0x33, 0xC0, 0xC2, 0x08, 0x00) // success; ret 8
	if err := writeRemote(process, page, stub); err != nil {
		return nil, fmt.Errorf("write shop-server probe stub: %w", err)
	}
	patch := []byte{0xE9, 0, 0, 0, 0, 0x90}
	binary.LittleEndian.PutUint32(patch[1:5], uint32(page-(probe.patchAddress+5)))
	if !ensureRemoteBytes(process, probe.patchAddress, patch, pageExecuteReadWrite) {
		return nil, fmt.Errorf("patch QQTDir random-shop method at 0x%08X", probe.patchAddress)
	}
	procFlushInstruction.Call(uintptr(process), probe.patchAddress, uintptr(len(patch)))
	procFlushInstruction.Call(uintptr(process), page, uintptr(len(stub)))
	failed = false
	return probe, nil
}

func (probe *ShopServerProbe) CaptureUntilFirst(duration time.Duration) ShopServerProbeCapture {
	deadline := time.Now().Add(duration)
	for time.Now().Before(deadline) {
		record, ok := readRemote(probe.process, probe.record, 8)
		if ok && binary.LittleEndian.Uint32(record[:4]) != 0 {
			return probe.capture(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(probe.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	record, _ := readRemote(probe.process, probe.record, 8)
	return probe.capture(record)
}

// CaptureUntilClientExit keeps the narrowly verified selector override alive
// for the complete target-client lifetime. This is the managed compatibility
// mode used by the local launcher: repeated shop entries all pass through the
// same selector, and Close restores the exact bytes when the client exits.
func (probe *ShopServerProbe) CaptureUntilClientExit(duration time.Duration) ShopServerProbeCapture {
	deadline := time.Time{}
	if duration > 0 {
		deadline = time.Now().Add(duration)
	}
	result := probe.capture(nil)
	for deadline.IsZero() || time.Now().Before(deadline) {
		if record, ok := readRemote(probe.process, probe.record, 8); ok {
			result = probe.capture(record)
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(probe.process), 0)
		if waitResult == waitObject0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return result
}

func (probe *ShopServerProbe) capture(record []byte) ShopServerProbeCapture {
	result := ShopServerProbeCapture{
		PID: probe.pid, ServerID: probe.serverID,
		PatchAddress:  fmt.Sprintf("0x%08X", probe.patchAddress),
		StubAddress:   fmt.Sprintf("0x%08X", probe.stubAddress),
		OriginalBytes: fmt.Sprintf("%X", probe.original),
		Behavior:      "one-process diagnostic: substitute local server ID for GetRandShopServerID; restore original code on close",
	}
	if len(record) == 8 {
		result.Calls = binary.LittleEndian.Uint32(record[:4])
		result.Directory = fmt.Sprintf("0x%08X", binary.LittleEndian.Uint32(record[4:8]))
	}
	return result
}

func (probe *ShopServerProbe) Close() {
	if probe == nil || probe.process == 0 {
		return
	}
	if probe.patchAddress != 0 && len(probe.original) != 0 {
		ensureRemoteBytes(probe.process, probe.patchAddress, probe.original, pageExecuteReadWrite)
		procFlushInstruction.Call(uintptr(probe.process), probe.patchAddress, uintptr(len(probe.original)))
	}
	syscall.CloseHandle(probe.process)
	probe.process = 0
}

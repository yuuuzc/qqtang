package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

// ReadActiveLoginCallbackOverride reads the record page of an already
// installed LoginCallbackOverride. It does not modify the target process.
func ReadActiveLoginCallbackOverride(pid uint32, timeout time.Duration) (LoginCallbackOverrideCapture, error) {
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return LoginCallbackOverrideCapture{}, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)

	moduleBase, err := waitForModule(process, pid, "Client.exe", timeout)
	if err != nil {
		return LoginCallbackOverrideCapture{}, err
	}
	patchAddress := moduleBase + clientLoginCallbackRVA
	patch, ok := readRemote(process, patchAddress, 6)
	if !ok || len(patch) != 6 {
		return LoginCallbackOverrideCapture{}, fmt.Errorf("read active login callback patch at 0x%08X", patchAddress)
	}
	stubAddress, err := resolveRelativeJump32(patchAddress, patch)
	if err != nil {
		return LoginCallbackOverrideCapture{}, err
	}
	record, ok := readRemote(process, stubAddress+0x200, 16)
	if !ok || len(record) != 16 {
		return LoginCallbackOverrideCapture{}, fmt.Errorf("read active login callback record at 0x%08X", stubAddress+0x200)
	}
	return LoginCallbackOverrideCapture{
		PID:            pid,
		Calls:          binary.LittleEndian.Uint32(record[0:4]),
		ThreadID:       binary.LittleEndian.Uint32(record[4:8]),
		OriginalStatus: binary.LittleEndian.Uint32(record[8:12]),
		OriginalLength: binary.LittleEndian.Uint32(record[12:16]),
		PatchAddress:   fmt.Sprintf("0x%08X", patchAddress),
		StubAddress:    fmt.Sprintf("0x%08X", stubAddress),
		OriginalBytes:  fmt.Sprintf("%X", patch),
		Behavior:       "read-only snapshot of the active local login callback override",
	}, nil
}

func resolveRelativeJump32(address uintptr, instruction []byte) (uintptr, error) {
	if len(instruction) < 5 || instruction[0] != 0xE9 {
		return 0, fmt.Errorf("active login callback is not an E9 rel32 jump at 0x%08X: got %X", address, instruction)
	}
	displacement := int64(int32(binary.LittleEndian.Uint32(instruction[1:5])))
	target := int64(address) + 5 + displacement
	if target <= 0 || target > 0xFFFFFFFF {
		return 0, fmt.Errorf("active login callback jump target is outside x86 address space: 0x%X", target)
	}
	return uintptr(target), nil
}

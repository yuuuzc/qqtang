package winlaunch

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

const (
	wmCopyData      = 0x004A
	smtoBlock       = 0x0001
	smtoAbortIfHung = 0x0002
)

var (
	replayUser32                     = syscall.NewLazyDLL("user32.dll")
	procReplayEnumWindows            = replayUser32.NewProc("EnumWindows")
	procReplayGetClassName           = replayUser32.NewProc("GetClassNameW")
	procReplayIsWindowVisible        = replayUser32.NewProc("IsWindowVisible")
	procReplayGetWindowThreadProcess = replayUser32.NewProc("GetWindowThreadProcessId")
	procReplaySendMessageTimeout     = replayUser32.NewProc("SendMessageTimeoutW")
)

type replayCopyDataStruct struct {
	Data   uintptr
	Length uint32
	Buffer uintptr
}

type SSOMessageReplayResult struct {
	PID           uint32 `json:"pid"`
	Window        string `json:"window"`
	CopyDataID    uint32 `json:"copy_data_id"`
	PayloadBytes  int    `json:"payload_bytes"`
	MessageResult string `json:"message_result"`
}

// ReplaySSOFailureMessage asks Windows to marshal a previously captured 0x1980
// payload back to the target's visible top-level window. Cross-process
// WM_COPYDATA delivery executes the normal Client window procedure on its GUI
// thread; no remote thread or replacement window procedure is created.
func ReplaySSOFailureMessage(pid uint32, copyDataID uint32, payload []byte, timeout time.Duration) (SSOMessageReplayResult, error) {
	if pid == 0 {
		return SSOMessageReplayResult{}, fmt.Errorf("pid must be non-zero")
	}
	if copyDataID != 0x1980 {
		return SSOMessageReplayResult{}, fmt.Errorf("only the verified 0x1980 login-result message may be replayed")
	}
	if len(payload) == 0 || len(payload) > ssoMessagePayloadLimit {
		return SSOMessageReplayResult{}, fmt.Errorf("payload must be 1..%d bytes", ssoMessagePayloadLimit)
	}
	hwnd := replayVisibleWindow(pid)
	if hwnd == 0 {
		return SSOMessageReplayResult{}, fmt.Errorf("no visible top-level window for pid %d", pid)
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	copyData := replayCopyDataStruct{Data: uintptr(copyDataID), Length: uint32(len(payload)), Buffer: uintptr(unsafe.Pointer(&payload[0]))}
	var messageResult uintptr
	success, _, callErr := procReplaySendMessageTimeout.Call(
		hwnd,
		wmCopyData,
		0,
		uintptr(unsafe.Pointer(&copyData)),
		smtoBlock|smtoAbortIfHung,
		uintptr(timeout.Milliseconds()),
		uintptr(unsafe.Pointer(&messageResult)),
	)
	runtime.KeepAlive(payload)
	runtime.KeepAlive(copyData)
	result := SSOMessageReplayResult{
		PID: pid, Window: fmt.Sprintf("0x%08X", hwnd), CopyDataID: copyDataID,
		PayloadBytes: len(payload), MessageResult: fmt.Sprintf("0x%08X", messageResult),
	}
	if success == 0 {
		return result, fmt.Errorf("SendMessageTimeoutW WM_COPYDATA to %s: %w", result.Window, callErr)
	}
	return result, nil
}

func replayVisibleWindow(pid uint32) uintptr {
	var found uintptr
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var windowPID uint32
		procReplayGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&windowPID)))
		visible, _, _ := procReplayIsWindowVisible.Call(hwnd)
		if windowPID == pid && visible != 0 && replayWindowClass(hwnd) == "QQTangWinClass" {
			found = hwnd
			return 0
		}
		return 1
	})
	procReplayEnumWindows.Call(callback, 0)
	return found
}

func replayWindowClass(hwnd uintptr) string {
	buffer := make([]uint16, 256)
	length, _, _ := procReplayGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:length])
}

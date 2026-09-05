package winlaunch

import (
	"context"
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
)

const (
	qqtSectionLobbyCallbackPointerRVA = uintptr(0x672EC)
	qqtSectionLobbyCallbackOffset     = uintptr(0xBF30)
	qqtSectionLobbyStageOffset        = uintptr(0x1A0)
	qqtSectionLobbyUINOffset          = uintptr(0x1A8)
)

type LobbyStageCapture struct {
	PID                    uint32 `json:"pid"`
	ModuleBase             string `json:"module_base"`
	CallbackPointerAddress string `json:"callback_pointer_address"`
	CallbackObject         string `json:"callback_object"`
	LobbyObject            string `json:"lobby_object"`
	StageAddress           string `json:"stage_address"`
	Stage                  uint32 `json:"stage"`
	UIN                    uint32 `json:"uin"`
	Behavior               string `json:"behavior"`
}

type LobbyStageWriteCapture struct {
	LobbyStageCapture
	OriginalStage uint32 `json:"original_stage"`
	WrittenStage  uint32 `json:"written_stage"`
}

// LobbyStageMaintenanceCapture records the narrow stage repair performed when
// the legacy client destroys and recreates its lobby object.  The client can
// do this after returning from another screen; a one-shot stage 1 -> 2 update
// therefore does not survive for the lifetime of the authenticated session.
type LobbyStageMaintenanceCapture struct {
	PID              uint32            `json:"pid"`
	AuthenticatedUIN uint32            `json:"authenticated_uin"`
	Checks           uint64            `json:"checks"`
	Writes           uint64            `json:"writes"`
	LobbyChanges     uint64            `json:"lobby_object_changes"`
	Last             LobbyStageCapture `json:"last"`
	LastReadError    string            `json:"last_read_error,omitempty"`
	StoppedByClient  bool              `json:"stopped_by_client"`
	Behavior         string            `json:"behavior"`
}

type lobbyStageLocation struct {
	moduleBase     uintptr
	pointerAddress uintptr
	callbackObject uintptr
	lobbyObject    uintptr
	stageAddress   uintptr
}

// ReadLobbyStage reads the QQTSection login-stage counter without modifying
// the target process. Static analysis proves that the lobby object starts
// 0xBF30 bytes before its embedded login callback and that Create Room is
// gated on lobby+0x1A0 reaching two.
func ReadLobbyStage(pid uint32, timeout time.Duration) (LobbyStageCapture, error) {
	process, err := syscall.OpenProcess(0x0400|0x0010, false, pid)
	if err != nil {
		return LobbyStageCapture{}, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)

	location, err := locateLobbyStage(process, pid, timeout)
	if err != nil {
		return LobbyStageCapture{}, err
	}
	return readLobbyStageAt(process, pid, location)
}

// WriteLobbyStage performs the narrowly scoped stage-1 to stage-2 diagnostic
// used to prove the Create Room gate. It verifies the current value immediately
// before writing and reads it back; no code bytes or source client files change.
func WriteLobbyStage(pid uint32, expected, value uint32, timeout time.Duration) (LobbyStageWriteCapture, error) {
	if expected != 1 || value != 2 {
		return LobbyStageWriteCapture{}, fmt.Errorf("lobby stage diagnostic only permits expected=1 value=2")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return LobbyStageWriteCapture{}, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	location, err := locateLobbyStage(process, pid, timeout)
	if err != nil {
		return LobbyStageWriteCapture{}, err
	}
	before, err := readLobbyStageAt(process, pid, location)
	if err != nil {
		return LobbyStageWriteCapture{}, err
	}
	if before.Stage != expected {
		return LobbyStageWriteCapture{}, fmt.Errorf("QQTSection lobby stage is %d, want expected %d", before.Stage, expected)
	}
	encoded := make([]byte, 4)
	binary.LittleEndian.PutUint32(encoded, value)
	if err := writeRemote(process, location.stageAddress, encoded); err != nil {
		return LobbyStageWriteCapture{}, fmt.Errorf("write QQTSection lobby stage: %w", err)
	}
	after, err := readLobbyStageAt(process, pid, location)
	if err != nil {
		return LobbyStageWriteCapture{}, err
	}
	if after.Stage != value {
		return LobbyStageWriteCapture{}, fmt.Errorf("QQTSection lobby stage readback is %d, want %d", after.Stage, value)
	}
	after.Behavior = "verified live-only QQTSection lobby stage diagnostic; no client file modified"
	return LobbyStageWriteCapture{LobbyStageCapture: after, OriginalStage: before.Stage, WrittenStage: value}, nil
}

// WaitAndAdvanceTutorialCompletedLobbyStage completes the narrowly verified
// stage-1 gate left behind when any local tutorial-completed profile skips the
// legacy two-pass tutorial entry. The active client's actual non-zero UIN is
// read from QQTSection and must remain unchanged across the 1 -> 2 write.
func WaitAndAdvanceTutorialCompletedLobbyStage(pid uint32, timeout time.Duration) (LobbyStageWriteCapture, error) {
	if pid == 0 {
		return LobbyStageWriteCapture{}, fmt.Errorf("pid must be non-zero")
	}
	if timeout <= 0 {
		return LobbyStageWriteCapture{}, fmt.Errorf("timeout must be positive")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return LobbyStageWriteCapture{}, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		waitResult, _, _ := procWaitForSingle.Call(uintptr(process), 0)
		if waitResult == waitObject0 {
			return LobbyStageWriteCapture{}, fmt.Errorf("client pid %d exited before the lobby stage became ready", pid)
		}
		probeTimeout := 100 * time.Millisecond
		if remaining := time.Until(deadline); remaining < probeTimeout {
			probeTimeout = remaining
		}
		if probeTimeout <= 0 {
			break
		}
		location, locateErr := locateLobbyStage(process, pid, probeTimeout)
		if locateErr != nil {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		before, readErr := readLobbyStageAt(process, pid, location)
		if readErr != nil || before.UIN == 0 || before.Stage == 0 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		actualUIN := before.UIN
		if before.Stage == 2 {
			before.Behavior = "tutorial-completed local profile gate already satisfied for active UIN; no write performed"
			return LobbyStageWriteCapture{LobbyStageCapture: before, OriginalStage: 2, WrittenStage: 2}, nil
		}
		if before.Stage != 1 {
			return LobbyStageWriteCapture{}, fmt.Errorf("QQTSection lobby stage is %d, expected 1 or 2", before.Stage)
		}
		encoded := make([]byte, 4)
		binary.LittleEndian.PutUint32(encoded, 2)
		if err := writeRemote(process, location.stageAddress, encoded); err != nil {
			return LobbyStageWriteCapture{}, fmt.Errorf("write QQTSection lobby stage: %w", err)
		}
		after, err := readLobbyStageAt(process, pid, location)
		if err != nil {
			return LobbyStageWriteCapture{}, err
		}
		if after.UIN != actualUIN || after.Stage != 2 {
			return LobbyStageWriteCapture{}, fmt.Errorf("QQTSection lobby gate changed active UIN %d -> %d or read back stage=%d", actualUIN, after.UIN, after.Stage)
		}
		after.Behavior = "tutorial-completed local profile: verified active non-zero UIN and lobby stage 1 -> 2; no client file modified"
		return LobbyStageWriteCapture{LobbyStageCapture: after, OriginalStage: 1, WrittenStage: 2}, nil
	}
	return LobbyStageWriteCapture{}, fmt.Errorf("QQTSection lobby stage did not become ready within %s", timeout)
}

// MaintainTutorialCompletedLobbyStage keeps the already-proven tutorial gate
// satisfied when the client recreates its lobby object.  It never changes an
// unknown stage, a zero-UIN object, or an object owned by another account.
// Cancellation is expected when the login helper finishes; client exit is
// detected independently so this routine cannot keep the helper alive.
func MaintainTutorialCompletedLobbyStage(ctx context.Context, pid, authenticatedUIN uint32) (LobbyStageMaintenanceCapture, error) {
	result := LobbyStageMaintenanceCapture{
		PID:              pid,
		AuthenticatedUIN: authenticatedUIN,
		Behavior:         "maintains only the proven tutorial-completed lobby stage 1 -> 2 transition for the same authenticated process",
	}
	if ctx == nil {
		return result, fmt.Errorf("context must not be nil")
	}
	if pid == 0 || authenticatedUIN == 0 {
		return result, fmt.Errorf("pid and authenticated UIN must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return result, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var lastLobbyObject uintptr
	for {
		select {
		case <-ctx.Done():
			return result, nil
		case <-ticker.C:
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(process), 0)
		if waitResult == waitObject0 {
			result.StoppedByClient = true
			return result, nil
		}
		location, locateErr := locateLobbyStage(process, pid, 50*time.Millisecond)
		if locateErr != nil {
			result.LastReadError = locateErr.Error()
			continue
		}
		if lastLobbyObject != 0 && lastLobbyObject != location.lobbyObject {
			result.LobbyChanges++
		}
		lastLobbyObject = location.lobbyObject
		capture, readErr := readLobbyStageAt(process, pid, location)
		if readErr != nil {
			result.LastReadError = readErr.Error()
			continue
		}
		result.Checks++
		result.Last = capture
		result.LastReadError = ""
		if !shouldAdvanceTutorialLobbyStage(capture.Stage, capture.UIN, authenticatedUIN) {
			continue
		}
		encoded := make([]byte, 4)
		binary.LittleEndian.PutUint32(encoded, 2)
		if writeErr := writeRemote(process, location.stageAddress, encoded); writeErr != nil {
			result.LastReadError = writeErr.Error()
			continue
		}
		after, readErr := readLobbyStageAt(process, pid, location)
		if readErr != nil {
			result.LastReadError = readErr.Error()
			continue
		}
		if after.UIN != authenticatedUIN || after.Stage != 2 {
			result.LastReadError = fmt.Sprintf("lobby gate readback changed UIN=%d or stage=%d", after.UIN, after.Stage)
			continue
		}
		result.Writes++
		result.Last = after
	}
}

func shouldAdvanceTutorialLobbyStage(stage, uin, authenticatedUIN uint32) bool {
	return stage == 1 && uin != 0 && uin == authenticatedUIN
}

func locateLobbyStage(process syscall.Handle, pid uint32, timeout time.Duration) (lobbyStageLocation, error) {
	moduleBase, err := waitForModule(process, pid, "QQTSection.dll", timeout)
	if err != nil {
		return lobbyStageLocation{}, err
	}
	pointerAddress := moduleBase + qqtSectionLobbyCallbackPointerRVA
	pointerBytes, ok := readRemote(process, pointerAddress, 4)
	if !ok || len(pointerBytes) != 4 {
		return lobbyStageLocation{}, fmt.Errorf("read QQTSection lobby callback pointer at 0x%08X", pointerAddress)
	}
	callbackObject := uintptr(binary.LittleEndian.Uint32(pointerBytes))
	if callbackObject < qqtSectionLobbyCallbackOffset {
		return lobbyStageLocation{}, fmt.Errorf("QQTSection lobby callback pointer is invalid: 0x%08X", callbackObject)
	}
	lobbyObject := callbackObject - qqtSectionLobbyCallbackOffset
	return lobbyStageLocation{
		moduleBase:     moduleBase,
		pointerAddress: pointerAddress,
		callbackObject: callbackObject,
		lobbyObject:    lobbyObject,
		stageAddress:   lobbyObject + qqtSectionLobbyStageOffset,
	}, nil
}

func readLobbyStageAt(process syscall.Handle, pid uint32, location lobbyStageLocation) (LobbyStageCapture, error) {
	stageBytes, ok := readRemote(process, location.stageAddress, 4)
	if !ok || len(stageBytes) != 4 {
		return LobbyStageCapture{}, fmt.Errorf("read QQTSection lobby stage at 0x%08X", location.stageAddress)
	}
	uinBytes, ok := readRemote(process, location.lobbyObject+qqtSectionLobbyUINOffset, 4)
	if !ok || len(uinBytes) != 4 {
		return LobbyStageCapture{}, fmt.Errorf("read QQTSection lobby UIN at 0x%08X", location.lobbyObject+qqtSectionLobbyUINOffset)
	}
	return LobbyStageCapture{
		PID:                    pid,
		ModuleBase:             fmt.Sprintf("0x%08X", location.moduleBase),
		CallbackPointerAddress: fmt.Sprintf("0x%08X", location.pointerAddress),
		CallbackObject:         fmt.Sprintf("0x%08X", location.callbackObject),
		LobbyObject:            fmt.Sprintf("0x%08X", location.lobbyObject),
		StageAddress:           fmt.Sprintf("0x%08X", location.stageAddress),
		Stage:                  binary.LittleEndian.Uint32(stageBytes),
		UIN:                    binary.LittleEndian.Uint32(uinBytes),
		Behavior:               "read-only QQTSection lobby stage snapshot",
	}, nil
}

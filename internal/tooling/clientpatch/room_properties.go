package clientpatch

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
)

const (
	roomPropertiesHookRVA    = uint32(0x00029644)
	roomPropertiesResumeRVA  = uint32(0x0002964A)
	roomPropertiesCaveRVA    = uint32(0x00053900)
	roomPropertiesTextEndRVA = uint32(0x00053920)
	roomPropertiesFlagOffset = byte(0x9C) // [ebp-0x64], REQUEST_MODIFY_ROOM.RoomFlag
)

var roomPropertiesHookOriginal = []byte{
	0x8A, 0x43, 0x16, // mov al,[ebx+16h] -- uiRoom.py property flag
	0x83, 0xC4, 0x28, // add esp,28h
}

// RoomPropertiesPatchResult records the narrow static QQTSection repair which
// restores REQUEST_MODIFY_ROOM.RoomFlag. Client.exe passes QQTSection only the
// standard/free bit and the password string, while the shipped producer clears
// the 54-byte request and never reconstructs the protocol's two-bit property.
// Its native change detector can also suppress a rule-only confirmation after
// password state has changed, even though uiRoom.py unconditionally submits it.
type RoomPropertiesPatchResult struct {
	Path                    string `json:"path"`
	BeforeSHA256            string `json:"before_sha256"`
	AfterSHA256             string `json:"after_sha256"`
	AlreadyPatched          bool   `json:"already_patched"`
	HookRVA                 string `json:"hook_rva"`
	CaveRVA                 string `json:"cave_rva"`
	ResumeRVA               string `json:"resume_rva"`
	RoomFlagRequestOffset   uint32 `json:"room_flag_request_offset"`
	RequiredTextVirtualSize uint32 `json:"required_text_virtual_size"`
	OriginalBytes           string `json:"original_bytes"`
	PatchedBytes            string `json:"patched_bytes"`
	Behavior                string `json:"behavior"`
}

// PatchStaticRoomProperties reconstructs REQUEST_MODIFY_ROOM.RoomFlag before
// NetCenter serializes the request. This is a build-time PE edit; it installs
// no runtime writer, hook or injected DLL.
func PatchStaticRoomProperties(path string) (RoomPropertiesPatchResult, error) {
	result, data, image, state, err := inspectStaticRoomProperties(path)
	if err != nil {
		return result, err
	}
	defer image.Close()
	if state.fullyPatched {
		return result, nil
	}
	if !state.hookPatched && !state.caveEmpty {
		return result, fmt.Errorf("QQTSection.dll has room-property cave bytes without its hook")
	}
	if state.hookPatched && !state.cavePatched && !state.legacyCavePatched {
		return result, fmt.Errorf("QQTSection.dll room-property hook targets an unknown cave")
	}

	cave := buildRoomPropertiesCave()
	copy(data[state.caveOffset:], cave)
	copy(data[state.hookOffset:], relativeJump(roomPropertiesHookRVA, roomPropertiesCaveRVA, len(roomPropertiesHookOriginal)))
	if state.textVirtualSize < state.requiredTextVirtualSize {
		binary.LittleEndian.PutUint32(data[state.textHeaderOffset+8:state.textHeaderOffset+12], state.requiredTextVirtualSize)
	}
	if err := os.WriteFile(path, data, 0o755); err != nil {
		return result, fmt.Errorf("write QQTSection room-property transport patch: %w", err)
	}
	result.AfterSHA256 = sha256Hex(data)
	return result, nil
}

func CheckStaticRoomProperties(path string) (RoomPropertiesPatchResult, error) {
	result, _, image, state, err := inspectStaticRoomProperties(path)
	if err != nil {
		return result, err
	}
	defer image.Close()
	if !state.fullyPatched {
		return result, fmt.Errorf("QQTSection.dll room-property transport patch is not fully installed")
	}
	return result, nil
}

type roomPropertiesPatchState struct {
	hookOffset              uint32
	caveOffset              uint32
	textHeaderOffset        uint32
	textVirtualSize         uint32
	requiredTextVirtualSize uint32
	hookPatched             bool
	cavePatched             bool
	legacyCavePatched       bool
	caveEmpty               bool
	fullyPatched            bool
}

func inspectStaticRoomProperties(path string) (RoomPropertiesPatchResult, []byte, *pe.File, roomPropertiesPatchState, error) {
	patchedHook := relativeJump(roomPropertiesHookRVA, roomPropertiesCaveRVA, len(roomPropertiesHookOriginal))
	result := RoomPropertiesPatchResult{
		Path:                    path,
		HookRVA:                 fmt.Sprintf("0x%X", roomPropertiesHookRVA),
		CaveRVA:                 fmt.Sprintf("0x%X", roomPropertiesCaveRVA),
		ResumeRVA:               fmt.Sprintf("0x%X", roomPropertiesResumeRVA),
		RoomFlagRequestOffset:   32,
		RequiredTextVirtualSize: roomPropertiesTextEndRVA - 0x1000,
		OriginalBytes:           hex.EncodeToString(roomPropertiesHookOriginal),
		PatchedBytes:            hex.EncodeToString(patchedHook),
		Behavior:                "statically reconstruct REQUEST_MODIFY_ROOM.RoomFlag as (QQTSection standard/free bit << 1) | password-string-present and preserve each explicit uiRoom.py confirmation past QQTSection's stale native change detector; preserve ModifyFlag as the native name/password change mask and leave authoritative topology validation to the server",
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return result, nil, nil, roomPropertiesPatchState{}, fmt.Errorf("read QQTSection.dll: %w", err)
	}
	result.BeforeSHA256 = sha256Hex(data)
	result.AfterSHA256 = result.BeforeSHA256
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return result, nil, nil, roomPropertiesPatchState{}, fmt.Errorf("parse QQTSection.dll: %w", err)
	}
	if image.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		image.Close()
		return result, nil, nil, roomPropertiesPatchState{}, fmt.Errorf("QQTSection.dll is not x86")
	}
	hookOffset, err := peRVAFileOffset(image, "QQTSection.dll", roomPropertiesHookRVA, uint32(len(roomPropertiesHookOriginal)))
	if err != nil {
		image.Close()
		return result, nil, nil, roomPropertiesPatchState{}, err
	}
	cave := buildRoomPropertiesCave()
	caveOffset, err := peRVAFileOffset(image, "QQTSection.dll", roomPropertiesCaveRVA, uint32(len(cave)))
	if err != nil {
		image.Close()
		return result, nil, nil, roomPropertiesPatchState{}, err
	}
	textHeader, textVirtualSize, textVA, err := peSectionHeader(data, ".text")
	if err != nil {
		image.Close()
		return result, nil, nil, roomPropertiesPatchState{}, err
	}
	requiredTextVirtualSize := roomPropertiesTextEndRVA - textVA
	result.RequiredTextVirtualSize = requiredTextVirtualSize
	hook := data[hookOffset : hookOffset+uint32(len(roomPropertiesHookOriginal))]
	hookPatched := bytes.Equal(hook, patchedHook)
	if !hookPatched && !bytes.Equal(hook, roomPropertiesHookOriginal) {
		image.Close()
		return result, nil, nil, roomPropertiesPatchState{}, fmt.Errorf("QQTSection sc_modifyRoom signature mismatch at RVA 0x%X: %s", roomPropertiesHookRVA, hex.EncodeToString(hook))
	}
	caveBytes := data[caveOffset : caveOffset+uint32(len(cave))]
	cavePatched := bytes.Equal(caveBytes, cave)
	legacyCavePatched := matchesLegacyRoomPropertiesCave(caveBytes)
	caveEmpty := allZero(caveBytes)
	if !cavePatched && !legacyCavePatched && !caveEmpty {
		image.Close()
		return result, nil, nil, roomPropertiesPatchState{}, fmt.Errorf("QQTSection room-property cave at RVA 0x%X is neither empty, legacy nor current", roomPropertiesCaveRVA)
	}
	fullyPatched := hookPatched && cavePatched && textVirtualSize >= requiredTextVirtualSize
	result.AlreadyPatched = fullyPatched
	return result, data, image, roomPropertiesPatchState{
		hookOffset: hookOffset, caveOffset: caveOffset,
		textHeaderOffset: textHeader, textVirtualSize: textVirtualSize,
		requiredTextVirtualSize: requiredTextVirtualSize,
		hookPatched:             hookPatched, cavePatched: cavePatched,
		legacyCavePatched: legacyCavePatched, caveEmpty: caveEmpty,
		fullyPatched: fullyPatched,
	}, nil
}

func buildRoomPropertiesCave() []byte {
	b := newX86Builder(roomPropertiesCaveRVA)
	b.emit(0x8A, 0x43, 0x16)       // mov al,[ebx+16h] -- standard/free bit
	b.emit(0x83, 0xC4, 0x28)       // add esp,28h
	b.emit(0x24, 0x01)             // and al,1
	b.emit(0xD0, 0xE0)             // shl al,1 -- wire free-rule bit
	b.emit(0x80, 0x7D, 0x9D, 0x00) // cmp byte ptr [ebp-63h],0 -- copied password
	b.short(0x74, "store")         // je store
	b.emit(0x0C, 0x01)             // or al,1 -- wire password bit
	b.mark("store")
	b.emit(0x88, 0x45, roomPropertiesFlagOffset) // mov [ebp-64h],al
	b.emit(0x6A, 0x01)                           // push 1
	b.emit(0x59)                                 // pop ecx
	b.emit(0x89, 0x4D, 0x08)                     // mov [ebp+8h],ecx -- submit this UI confirmation
	b.near(0xE9, roomPropertiesResumeRVA)
	return b.finish()
}

func buildLegacyRoomPropertiesCave() []byte {
	b := newX86Builder(roomPropertiesCaveRVA)
	b.emit(0x8A, 0x43, 0x16)
	b.emit(0x83, 0xC4, 0x28)
	b.emit(0x88, 0x45, roomPropertiesFlagOffset)
	b.near(0xE9, roomPropertiesResumeRVA)
	return b.finish()
}

func matchesLegacyRoomPropertiesCave(candidate []byte) bool {
	for _, legacy := range [][]byte{buildLegacyRoomPropertiesCave(), buildRoomPropertiesCaveV2()} {
		if len(candidate) >= len(legacy) && bytes.Equal(candidate[:len(legacy)], legacy) && allZero(candidate[len(legacy):]) {
			return true
		}
	}
	return false
}

func buildRoomPropertiesCaveV2() []byte {
	b := newX86Builder(roomPropertiesCaveRVA)
	b.emit(0x8A, 0x43, 0x16)
	b.emit(0x83, 0xC4, 0x28)
	b.emit(0x24, 0x01)
	b.emit(0xD0, 0xE0)
	b.emit(0x80, 0x7D, 0x9D, 0x00)
	b.short(0x74, "store")
	b.emit(0x0C, 0x01)
	b.mark("store")
	b.emit(0x88, 0x45, roomPropertiesFlagOffset)
	b.near(0xE9, roomPropertiesResumeRVA)
	return b.finish()
}

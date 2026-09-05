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
	startRejectionPromptHookRVA     = uint32(0x0001FC25)
	startRejectionPromptResumeRVA   = uint32(0x0001FC2E)
	startRejectionPromptEpilogueRVA = uint32(0x0001FE88)
	startRejectionPromptCaveRVA     = uint32(0x00053A00)
	startRejectionPromptTextEndRVA  = uint32(0x00053A40)
)

var startRejectionPromptHookOriginal = []byte{
	0x66, 0x8B, 0x03, // mov ax,[ebx] -- NOTIFY_ROOM_MSG.SourcePlayerID
	0x8B, 0x8F, 0x58, 0xC5, 0x00, 0x00, // mov ecx,[edi+C558h]
}

type StartRejectionPromptPatchResult struct {
	Path                    string `json:"path"`
	BeforeSHA256            string `json:"before_sha256"`
	AfterSHA256             string `json:"after_sha256"`
	AlreadyPatched          bool   `json:"already_patched"`
	HookRVA                 string `json:"hook_rva"`
	CaveRVA                 string `json:"cave_rva"`
	ResumeRVA               string `json:"resume_rva"`
	SystemSourcePlayerID    uint16 `json:"system_source_player_id"`
	RequiredTextVirtualSize uint32 `json:"required_text_virtual_size"`
	OriginalBytes           string `json:"original_bytes"`
	PatchedBytes            string `json:"patched_bytes"`
	Behavior                string `json:"behavior"`
}

// PatchStaticStartRejectionPrompt teaches the existing room-chat consumer one
// server-system source value.  The hook runs after QQTSection has performed its
// native 1024-byte length check and copied the message into its zeroed local
// buffer.  Ordinary player messages execute the exact two overwritten native
// instructions and resume unchanged.
func PatchStaticStartRejectionPrompt(path string) (StartRejectionPromptPatchResult, error) {
	result, data, image, state, err := inspectStaticStartRejectionPrompt(path)
	if err != nil {
		return result, err
	}
	defer image.Close()
	if state.fullyPatched {
		return result, nil
	}
	if !state.hookPatched && !state.caveEmpty {
		return result, fmt.Errorf("QQTSection.dll has start-rejection prompt cave bytes without its hook")
	}
	if state.hookPatched && !state.cavePatched {
		return result, fmt.Errorf("QQTSection.dll start-rejection prompt hook targets an unknown cave")
	}
	cave := buildStartRejectionPromptCave()
	copy(data[state.caveOffset:], cave)
	copy(data[state.hookOffset:], relativeJump(startRejectionPromptHookRVA, startRejectionPromptCaveRVA, len(startRejectionPromptHookOriginal)))
	if state.textVirtualSize < state.requiredTextVirtualSize {
		binary.LittleEndian.PutUint32(data[state.textHeaderOffset+8:state.textHeaderOffset+12], state.requiredTextVirtualSize)
	}
	if err := os.WriteFile(path, data, 0o755); err != nil {
		return result, fmt.Errorf("write QQTSection start-rejection prompt patch: %w", err)
	}
	result.AfterSHA256 = sha256Hex(data)
	return result, nil
}

func CheckStaticStartRejectionPrompt(path string) (StartRejectionPromptPatchResult, error) {
	result, _, image, state, err := inspectStaticStartRejectionPrompt(path)
	if err != nil {
		return result, err
	}
	defer image.Close()
	if !state.fullyPatched {
		return result, fmt.Errorf("QQTSection.dll start-rejection prompt patch is not fully installed")
	}
	return result, nil
}

type startRejectionPromptPatchState struct {
	hookOffset, caveOffset, textHeaderOffset          uint32
	textVirtualSize, requiredTextVirtualSize          uint32
	hookPatched, cavePatched, caveEmpty, fullyPatched bool
}

func inspectStaticStartRejectionPrompt(path string) (StartRejectionPromptPatchResult, []byte, *pe.File, startRejectionPromptPatchState, error) {
	patchedHook := relativeJump(startRejectionPromptHookRVA, startRejectionPromptCaveRVA, len(startRejectionPromptHookOriginal))
	result := StartRejectionPromptPatchResult{
		Path: path, HookRVA: fmt.Sprintf("0x%X", startRejectionPromptHookRVA), CaveRVA: fmt.Sprintf("0x%X", startRejectionPromptCaveRVA),
		ResumeRVA: fmt.Sprintf("0x%X", startRejectionPromptResumeRVA), SystemSourcePlayerID: 0xffff,
		RequiredTextVirtualSize: startRejectionPromptTextEndRVA - 0x1000,
		OriginalBytes:           hex.EncodeToString(startRejectionPromptHookOriginal), PatchedBytes: hex.EncodeToString(patchedHook),
		Behavior: "render NOTIFY_ROOM_MSG SourcePlayerID 0xFFFF through QQTSection's existing room system-message UI after native length validation and copying; preserve all ordinary player chat handling",
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return result, nil, nil, startRejectionPromptPatchState{}, fmt.Errorf("read QQTSection.dll: %w", err)
	}
	result.BeforeSHA256, result.AfterSHA256 = sha256Hex(data), sha256Hex(data)
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return result, nil, nil, startRejectionPromptPatchState{}, fmt.Errorf("parse QQTSection.dll: %w", err)
	}
	if image.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		image.Close()
		return result, nil, nil, startRejectionPromptPatchState{}, fmt.Errorf("QQTSection.dll is not x86")
	}
	hookOffset, err := peRVAFileOffset(image, "QQTSection.dll", startRejectionPromptHookRVA, uint32(len(startRejectionPromptHookOriginal)))
	if err != nil {
		image.Close()
		return result, nil, nil, startRejectionPromptPatchState{}, err
	}
	cave := buildStartRejectionPromptCave()
	caveOffset, err := peRVAFileOffset(image, "QQTSection.dll", startRejectionPromptCaveRVA, uint32(len(cave)))
	if err != nil {
		image.Close()
		return result, nil, nil, startRejectionPromptPatchState{}, err
	}
	textHeader, textVirtualSize, textVA, err := peSectionHeader(data, ".text")
	if err != nil {
		image.Close()
		return result, nil, nil, startRejectionPromptPatchState{}, err
	}
	requiredTextVirtualSize := startRejectionPromptTextEndRVA - textVA
	result.RequiredTextVirtualSize = requiredTextVirtualSize
	hook := data[hookOffset : hookOffset+uint32(len(startRejectionPromptHookOriginal))]
	hookPatched := bytes.Equal(hook, patchedHook)
	if !hookPatched && !bytes.Equal(hook, startRejectionPromptHookOriginal) {
		image.Close()
		return result, nil, nil, startRejectionPromptPatchState{}, fmt.Errorf("QQTSection room-chat signature mismatch at RVA 0x%X: %s", startRejectionPromptHookRVA, hex.EncodeToString(hook))
	}
	caveBytes := data[caveOffset : caveOffset+uint32(len(cave))]
	cavePatched := bytes.Equal(caveBytes, cave)
	caveEmpty := allZero(caveBytes)
	if !cavePatched && !caveEmpty {
		image.Close()
		return result, nil, nil, startRejectionPromptPatchState{}, fmt.Errorf("QQTSection start-rejection prompt cave at RVA 0x%X is not empty or current", startRejectionPromptCaveRVA)
	}
	fullyPatched := hookPatched && cavePatched && textVirtualSize >= requiredTextVirtualSize
	result.AlreadyPatched = fullyPatched
	return result, data, image, startRejectionPromptPatchState{
		hookOffset: hookOffset, caveOffset: caveOffset, textHeaderOffset: textHeader,
		textVirtualSize: textVirtualSize, requiredTextVirtualSize: requiredTextVirtualSize,
		hookPatched: hookPatched, cavePatched: cavePatched, caveEmpty: caveEmpty, fullyPatched: fullyPatched,
	}, nil
}

func buildStartRejectionPromptCave() []byte {
	b := newX86Builder(startRejectionPromptCaveRVA)
	b.emit(0x66, 0x83, 0x3B, 0xFF)             // cmp word ptr [ebx],0FFFFh
	b.short(0x75, "ordinary")                  // jne ordinary
	b.emit(0x83, 0xC4, 0x18)                   // add esp,18h -- native memset/memcpy arguments
	b.emit(0x8B, 0x45, 0x08)                   // mov eax,[ebp+8] -- room UI interface
	b.emit(0x8B, 0x08)                         // mov ecx,[eax]
	b.emit(0x68, 0x79, 0xFF, 0xB8, 0x00)       // push 00B8FF79h -- native system color
	b.emit(0x8D, 0x95, 0x8C, 0xFB, 0xFF, 0xFF) // lea edx,[ebp-474h] -- copied message
	b.emit(0x52, 0x6A, 0x00, 0x6A, 0x02, 0x50) // push edx,0,2,eax
	b.emit(0xFF, 0x51, 0x18)                   // call dword ptr [ecx+18h]
	b.emit(0x33, 0xC0)                         // xor eax,eax
	b.near(0xE9, startRejectionPromptEpilogueRVA)
	b.mark("ordinary")
	b.emit(startRejectionPromptHookOriginal...)
	b.near(0xE9, startRejectionPromptResumeRVA)
	return b.finish()
}

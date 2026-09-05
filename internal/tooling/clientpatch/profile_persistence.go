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
	profileImageBase = uint32(0x10000000)

	profileCaveRVA  = uint32(0x0006B600)
	profileCaveSize = uint32(0x00000500)

	profileSerializerRVA       = uint32(0x0000F37D)
	profileSerializerEntryHook = uint32(0x0000F387)
	profileSerializerResumeRVA = uint32(0x0000F38C)
	profileDestructorCallHook  = uint32(0x00010921)
	profileDestructorResumeRVA = uint32(0x00010928)

	profileIntegerSetterExit = uint32(0x0000F656)
	profileStringSetterExit  = uint32(0x0000F712)
	profileBufferSetterExit  = uint32(0x0000F7DF)
	profileBuffer2SetterExit = uint32(0x0000F8B9)

	profileDelete1Call      = uint32(0x0000F446)
	profileStringFreeCall   = uint32(0x0000F46C)
	profileDelete2Call      = uint32(0x0000F472)
	profileDelete3Call      = uint32(0x0000F496)
	profileDelete4Call      = uint32(0x0000F4BA)
	profileClearIntegerCall = uint32(0x0000F4C6)
	profileClearStringCall  = uint32(0x0000F4CE)
	profileClearBufferCall  = uint32(0x0000F4D6)
	profileClearBuffer2Call = uint32(0x0000F4E0)
	profileFinalizeCall     = uint32(0x0000F4F8)

	profileOperatorDeleteRVA     = uint32(0x000534E6)
	profileCStringDestructorRVA  = uint32(0x00053480)
	profileClearIntegerTargetRVA = uint32(0x00001D0C)
	profileClearStringTargetRVA  = uint32(0x000019F6)
	profileClearBufferTargetRVA  = uint32(0x00001A91)
	profileClearBuffer2TargetRVA = uint32(0x000011DB)

	profileEntryStubOffset         = uint32(0x040)
	profileDestructorStubOffset    = uint32(0x080)
	profileDeleteStubOffset        = uint32(0x0C0)
	profileStringFreeStubOffset    = uint32(0x100)
	profileClearIntegerStubOffset  = uint32(0x140)
	profileClearStringStubOffset   = uint32(0x180)
	profileClearBufferStubOffset   = uint32(0x1C0)
	profileClearBuffer2StubOffset  = uint32(0x200)
	profileFinalizeStubOffset      = uint32(0x240)
	profileIntegerSetterStubOffset = uint32(0x2C0)
	profileStringSetterStubOffset  = uint32(0x300)
	profileBufferSetterStubOffset  = uint32(0x350)
	profileBuffer2SetterStubOffset = uint32(0x3A0)

	profileFlushMarker = uint32(0x51504631) // "1FPQ" in little-endian memory
)

var (
	profileMagic = []byte("QQTPRF01")

	profileSerializerEntryOriginal   = []byte{0x83, 0xEC, 0x3C, 0x53, 0x56}
	profileDestructorCallOriginal    = []byte{0x8B, 0xCB, 0xE8, 0x20, 0x14, 0xFF, 0xFF}
	profileIntegerSetterExitOriginal = []byte{0x5F, 0x5E, 0x5B, 0x5D, 0xC2, 0x08, 0x00}
	profileStringSetterExitOriginal  = []byte{0x8B, 0x4D, 0xF4, 0x5F, 0x5E, 0x5B, 0x64, 0x89, 0x0D, 0x00, 0x00, 0x00, 0x00, 0xC9, 0xC2, 0x08, 0x00}
	profileBufferSetterExitOriginal  = []byte{0x5F, 0x5E, 0x5B, 0xC9, 0xC2, 0x0C, 0x00}

	profileCallOriginals = map[uint32][]byte{
		profileDelete1Call:      {0xE8, 0x9B, 0x40, 0x04, 0x00},
		profileStringFreeCall:   {0xE8, 0x0F, 0x40, 0x04, 0x00},
		profileDelete2Call:      {0xE8, 0x6F, 0x40, 0x04, 0x00},
		profileDelete3Call:      {0xE8, 0x4B, 0x40, 0x04, 0x00},
		profileDelete4Call:      {0xE8, 0x27, 0x40, 0x04, 0x00},
		profileClearIntegerCall: {0xE8, 0x41, 0x28, 0xFF, 0xFF},
		profileClearStringCall:  {0xE8, 0x23, 0x25, 0xFF, 0xFF},
		profileClearBufferCall:  {0xE8, 0xB6, 0x25, 0xFF, 0xFF},
		profileClearBuffer2Call: {0xE8, 0xF6, 0x1C, 0xFF, 0xFF},
		profileFinalizeCall:     {0xE8, 0x83, 0x3F, 0x04, 0x00},
	}
)

type ProfilePersistenceSiteResult struct {
	Name           string `json:"name"`
	RVA            string `json:"rva"`
	OriginalBytes  string `json:"original_bytes"`
	PatchedBytes   string `json:"patched_bytes"`
	AlreadyPatched bool   `json:"already_patched"`
}

type ProfilePersistencePatchResult struct {
	Path           string                         `json:"path"`
	BeforeSHA256   string                         `json:"before_sha256"`
	AfterSHA256    string                         `json:"after_sha256"`
	AlreadyPatched bool                           `json:"already_patched"`
	Behavior       string                         `json:"behavior"`
	CaveRVA        string                         `json:"cave_rva"`
	CaveSize       uint32                         `json:"cave_size"`
	Sites          []ProfilePersistenceSiteResult `json:"sites"`
}

type profilePatchSite struct {
	name     string
	rva      uint32
	original []byte
	patched  []byte
	offset   uint32
	current  bool
}

type profilePatchState struct {
	sites               []profilePatchSite
	caveOffset          uint32
	textHeaderOffset    uint32
	textVirtualSize     uint32
	requiredVirtualSize uint32
	caveCurrent         bool
}

// PatchStaticProfilePersistence turns the native destructor-only TPF writer
// into a conditional serializer: normal destruction keeps the original
// release path, while each Profile setter return requests a serialization
// pass that skips record/container destruction. The mode bit lives in the
// serializer's own stack frame, so concurrent callers cannot share state.
func PatchStaticProfilePersistence(path string) (ProfilePersistencePatchResult, error) {
	result, data, image, state, err := inspectProfilePersistence(path)
	if err != nil {
		return result, err
	}
	defer image.Close()
	if result.AlreadyPatched {
		return result, nil
	}
	for _, site := range state.sites {
		if site.current {
			continue
		}
		copy(data[site.offset:site.offset+uint32(len(site.patched))], site.patched)
	}
	payload := buildProfilePersistencePayload()
	clear(data[state.caveOffset : state.caveOffset+profileCaveSize])
	copy(data[state.caveOffset:], payload)
	if state.textVirtualSize < state.requiredVirtualSize {
		binary.LittleEndian.PutUint32(data[state.textHeaderOffset+8:state.textHeaderOffset+12], state.requiredVirtualSize)
	}
	if err := os.WriteFile(path, data, 0o755); err != nil {
		return result, fmt.Errorf("write QQTModules profile-persistence patch: %w", err)
	}
	result.AfterSHA256 = sha256Hex(data)
	return result, nil
}

func CheckStaticProfilePersistence(path string) (ProfilePersistencePatchResult, error) {
	result, _, image, _, err := inspectProfilePersistence(path)
	if image != nil {
		defer image.Close()
	}
	if err != nil {
		return result, err
	}
	if !result.AlreadyPatched {
		return result, fmt.Errorf("QQTModules.dll profile-persistence patch is incomplete")
	}
	return result, nil
}

func inspectProfilePersistence(path string) (ProfilePersistencePatchResult, []byte, *pe.File, profilePatchState, error) {
	result := ProfilePersistencePatchResult{
		Path:     path,
		Behavior: "serialize profile/<UIN>.tpf immediately after each native Profile setter returns while preserving the original destructive cleanup only for CQQTProfileMgr destruction",
		CaveRVA:  fmt.Sprintf("0x%X", profileCaveRVA), CaveSize: profileCaveSize,
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return result, nil, nil, profilePatchState{}, fmt.Errorf("read QQTModules.dll: %w", err)
	}
	result.BeforeSHA256 = sha256Hex(data)
	result.AfterSHA256 = result.BeforeSHA256
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return result, nil, nil, profilePatchState{}, fmt.Errorf("parse QQTModules.dll: %w", err)
	}
	optionalHeader, ok := image.OptionalHeader.(*pe.OptionalHeader32)
	if image.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 || !ok || optionalHeader.ImageBase != profileImageBase {
		image.Close()
		return result, nil, nil, profilePatchState{}, fmt.Errorf("QQTModules.dll is not the expected x86 image at base 0x%X", profileImageBase)
	}

	sites := buildProfilePatchSites()
	anyPatched := false
	allPatched := true
	for index := range sites {
		offset, err := peRVAFileOffset(image, "QQTModules.dll", sites[index].rva, uint32(len(sites[index].original)))
		if err != nil {
			image.Close()
			return result, nil, nil, profilePatchState{}, err
		}
		sites[index].offset = offset
		current := data[offset : offset+uint32(len(sites[index].original))]
		sites[index].current = bytes.Equal(current, sites[index].patched)
		if !sites[index].current && !bytes.Equal(current, sites[index].original) {
			image.Close()
			return result, nil, nil, profilePatchState{}, fmt.Errorf("QQTModules.dll %s signature mismatch at RVA 0x%X: got %X", sites[index].name, sites[index].rva, current)
		}
		anyPatched = anyPatched || sites[index].current
		allPatched = allPatched && sites[index].current
		result.Sites = append(result.Sites, ProfilePersistenceSiteResult{
			Name: sites[index].name, RVA: fmt.Sprintf("0x%X", sites[index].rva),
			OriginalBytes: hex.EncodeToString(sites[index].original), PatchedBytes: hex.EncodeToString(sites[index].patched),
			AlreadyPatched: sites[index].current,
		})
	}
	caveOffset, err := peRVAFileOffset(image, "QQTModules.dll", profileCaveRVA, profileCaveSize)
	if err != nil {
		image.Close()
		return result, nil, nil, profilePatchState{}, err
	}
	payload := buildProfilePersistencePayload()
	cave := data[caveOffset : caveOffset+profileCaveSize]
	caveCurrent := bytes.Equal(cave[:uint32(len(payload))], payload) && allZero(cave[uint32(len(payload)):])
	caveEmpty := allZero(cave)
	if !caveCurrent && !caveEmpty {
		image.Close()
		return result, nil, nil, profilePatchState{}, fmt.Errorf("QQTModules.dll profile-persistence cave is neither empty nor current")
	}
	if anyPatched != caveCurrent || (anyPatched && !allPatched) {
		image.Close()
		return result, nil, nil, profilePatchState{}, fmt.Errorf("QQTModules.dll has a partial profile-persistence patch")
	}
	textHeader, textVirtualSize, textVA, err := peSectionHeader(data, ".text")
	if err != nil {
		image.Close()
		return result, nil, nil, profilePatchState{}, err
	}
	requiredVirtualSize := profileCaveRVA + profileCaveSize - textVA
	result.AlreadyPatched = allPatched && caveCurrent && textVirtualSize >= requiredVirtualSize
	return result, data, image, profilePatchState{
		sites: sites, caveOffset: caveOffset, textHeaderOffset: textHeader,
		textVirtualSize: textVirtualSize, requiredVirtualSize: requiredVirtualSize, caveCurrent: caveCurrent,
	}, nil
}

func buildProfilePatchSites() []profilePatchSite {
	call := func(fromRVA, targetRVA uint32, original []byte) []byte {
		patched := append([]byte(nil), original...)
		patched[0] = 0xE8
		binary.LittleEndian.PutUint32(patched[1:5], uint32(int32(int64(targetRVA)-int64(fromRVA+5))))
		return patched
	}
	jump := func(fromRVA, targetRVA uint32, original []byte) []byte {
		return relativeJump(fromRVA, targetRVA, len(original))
	}
	sites := []profilePatchSite{
		{"serializer-mode-entry", profileSerializerEntryHook, profileSerializerEntryOriginal, jump(profileSerializerEntryHook, profileCaveRVA+profileEntryStubOffset, profileSerializerEntryOriginal), 0, false},
		{"destructor-serializer-call", profileDestructorCallHook, profileDestructorCallOriginal, jump(profileDestructorCallHook, profileCaveRVA+profileDestructorStubOffset, profileDestructorCallOriginal), 0, false},
		{"integer-setter-exit", profileIntegerSetterExit, profileIntegerSetterExitOriginal, jump(profileIntegerSetterExit, profileCaveRVA+profileIntegerSetterStubOffset, profileIntegerSetterExitOriginal), 0, false},
		{"string-setter-exit", profileStringSetterExit, profileStringSetterExitOriginal, jump(profileStringSetterExit, profileCaveRVA+profileStringSetterStubOffset, profileStringSetterExitOriginal), 0, false},
		{"buffer-setter-exit", profileBufferSetterExit, profileBufferSetterExitOriginal, jump(profileBufferSetterExit, profileCaveRVA+profileBufferSetterStubOffset, profileBufferSetterExitOriginal), 0, false},
		{"buffer2-setter-exit", profileBuffer2SetterExit, profileBufferSetterExitOriginal, jump(profileBuffer2SetterExit, profileCaveRVA+profileBuffer2SetterStubOffset, profileBufferSetterExitOriginal), 0, false},
	}
	callTargets := map[uint32]uint32{
		profileDelete1Call:      profileCaveRVA + profileDeleteStubOffset,
		profileStringFreeCall:   profileCaveRVA + profileStringFreeStubOffset,
		profileDelete2Call:      profileCaveRVA + profileDeleteStubOffset,
		profileDelete3Call:      profileCaveRVA + profileDeleteStubOffset,
		profileDelete4Call:      profileCaveRVA + profileDeleteStubOffset,
		profileClearIntegerCall: profileCaveRVA + profileClearIntegerStubOffset,
		profileClearStringCall:  profileCaveRVA + profileClearStringStubOffset,
		profileClearBufferCall:  profileCaveRVA + profileClearBufferStubOffset,
		profileClearBuffer2Call: profileCaveRVA + profileClearBuffer2StubOffset,
		profileFinalizeCall:     profileCaveRVA + profileFinalizeStubOffset,
	}
	names := map[uint32]string{
		profileDelete1Call: "integer-record-delete", profileStringFreeCall: "string-record-cstring-destroy",
		profileDelete2Call: "string-record-delete", profileDelete3Call: "buffer-record-delete", profileDelete4Call: "buffer2-record-delete",
		profileClearIntegerCall: "integer-container-clear", profileClearStringCall: "string-container-clear",
		profileClearBufferCall: "buffer-container-clear", profileClearBuffer2Call: "buffer2-container-clear",
		profileFinalizeCall: "serializer-finalize",
	}
	order := []uint32{
		profileDelete1Call, profileStringFreeCall, profileDelete2Call, profileDelete3Call, profileDelete4Call,
		profileClearIntegerCall, profileClearStringCall, profileClearBufferCall, profileClearBuffer2Call, profileFinalizeCall,
	}
	for _, rva := range order {
		original := profileCallOriginals[rva]
		sites = append(sites, profilePatchSite{names[rva], rva, original, call(rva, callTargets[rva], original), 0, false})
	}
	return sites
}

func buildProfilePersistencePayload() []byte {
	payload := make([]byte, profileCaveSize)
	copy(payload, profileMagic)
	put := func(offset uint32, code []byte) {
		if int(offset)+len(code) > len(payload) {
			panic("profile persistence payload exceeds cave")
		}
		copy(payload[offset:], code)
	}

	b := newX86Builder(profileCaveRVA + profileEntryStubOffset)
	b.emit(0x83, 0xEC, 0x3C) // sub esp,3Ch
	b.emit(0x81, 0x7D, 0x08, byte(profileFlushMarker&0xFF), byte((profileFlushMarker>>8)&0xFF), byte((profileFlushMarker>>16)&0xFF), byte((profileFlushMarker>>24)&0xFF))
	b.emit(0x0F, 0x94, 0xC0)             // sete al
	b.emit(0x88, 0x45, 0xD8, 0x53, 0x56) // mov [ebp-28h],al; push ebx; push esi
	b.near(0xE9, profileSerializerResumeRVA)
	put(profileEntryStubOffset, b.finish())

	b = newX86Builder(profileCaveRVA + profileDestructorStubOffset)
	b.emit(0x8B, 0xCB, 0x6A, 0x00) // mov ecx,ebx; push 0
	b.near(0xE8, profileSerializerRVA)
	b.emit(0x83, 0xC4, 0x04)
	b.near(0xE9, profileDestructorResumeRVA)
	put(profileDestructorStubOffset, b.finish())

	put(profileDeleteStubOffset, buildProfileConditionalTail(profileCaveRVA+profileDeleteStubOffset, profileOperatorDeleteRVA))
	put(profileStringFreeStubOffset, buildProfileConditionalTail(profileCaveRVA+profileStringFreeStubOffset, profileCStringDestructorRVA))
	put(profileClearIntegerStubOffset, buildProfileConditionalTail(profileCaveRVA+profileClearIntegerStubOffset, profileClearIntegerTargetRVA))
	put(profileClearStringStubOffset, buildProfileConditionalTail(profileCaveRVA+profileClearStringStubOffset, profileClearStringTargetRVA))
	put(profileClearBufferStubOffset, buildProfileConditionalTail(profileCaveRVA+profileClearBufferStubOffset, profileClearBufferTargetRVA))
	put(profileClearBuffer2StubOffset, buildProfileConditionalTail(profileCaveRVA+profileClearBuffer2StubOffset, profileClearBuffer2TargetRVA))

	b = newX86Builder(profileCaveRVA + profileFinalizeStubOffset)
	b.emit(0x80, 0x7D, 0xD8, 0x00)
	b.short(0x74, "destructive")
	b.emit(0x58)                              // discard return address to destructive member cleanup
	b.near(0xE8, profileCStringDestructorRVA) // finish local path CString
	b.emit(0x8B, 0x4D, 0xF4, 0x5F, 0x5E)      // restore SEH and preserved registers
	b.emit(0x64, 0x89, 0x0D, 0x00, 0x00, 0x00, 0x00)
	b.emit(0x5B, 0xC9, 0xC3)
	b.mark("destructive")
	b.near(0xE9, profileCStringDestructorRVA)
	put(profileFinalizeStubOffset, b.finish())

	put(profileIntegerSetterStubOffset, buildProfileSetterStub(profileCaveRVA+profileIntegerSetterStubOffset, []byte{0x8B, 0xCB}, []byte{0x5F, 0x5E, 0x5B, 0x5D, 0xC2, 0x08, 0x00}))
	put(profileStringSetterStubOffset, buildProfileSetterStub(profileCaveRVA+profileStringSetterStubOffset, []byte{0x8B, 0xCB}, []byte{0x8B, 0x4D, 0xF4, 0x5F, 0x5E, 0x5B, 0x64, 0x89, 0x0D, 0x00, 0x00, 0x00, 0x00, 0xC9, 0xC2, 0x08, 0x00}))
	put(profileBufferSetterStubOffset, buildProfileSetterStub(profileCaveRVA+profileBufferSetterStubOffset, []byte{0x8B, 0x4D, 0xFC}, []byte{0x5F, 0x5E, 0x5B, 0xC9, 0xC2, 0x0C, 0x00}))
	put(profileBuffer2SetterStubOffset, buildProfileSetterStub(profileCaveRVA+profileBuffer2SetterStubOffset, []byte{0x8B, 0x4D, 0xFC}, []byte{0x5F, 0x5E, 0x5B, 0xC9, 0xC2, 0x0C, 0x00}))
	return payload
}

func buildProfileConditionalTail(base, target uint32) []byte {
	b := newX86Builder(base)
	b.emit(0x80, 0x7D, 0xD8, 0x00)
	b.short(0x74, "destructive")
	b.emit(0xC3)
	b.mark("destructive")
	b.near(0xE9, target)
	return b.finish()
}

func buildProfileSetterStub(base uint32, loadThis, epilogue []byte) []byte {
	b := newX86Builder(base)
	b.emit(0x68, byte(profileFlushMarker&0xFF), byte((profileFlushMarker>>8)&0xFF), byte((profileFlushMarker>>16)&0xFF), byte((profileFlushMarker>>24)&0xFF))
	b.emit(loadThis...)
	b.near(0xE8, profileSerializerRVA)
	b.emit(0x83, 0xC4, 0x04)
	b.emit(epilogue...)
	return b.finish()
}

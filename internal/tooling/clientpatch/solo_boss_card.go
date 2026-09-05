package clientpatch

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"qqtang/internal/protocol/game"
)

const (
	soloBossGateFirstHookRVA   = uint32(0x0000CBBB)
	soloBossGateSecondHookRVA  = uint32(0x0000CC3C)
	soloBossGateFirstCaveRVA   = uint32(0x00053600)
	soloBossGateSecondCaveRVA  = uint32(0x00053700)
	soloBossDescriptionHookRVA = uint32(0x0002C39C)
	soloBossDescriptionCaveRVA = uint32(0x00053800)
	soloBossGateResumeRVA      = uint32(0x0000CBEF)
	soloBossGateNormalRVA      = uint32(0x0000CC45)
	soloBossGateSuccessRVA     = uint32(0x0000CDC3)
	soloBossDescriptionReadRVA = uint32(0x0002C3AC)
	soloBossDescriptionDoneRVA = uint32(0x0002C3DA)
	soloBossGateTextEndRVA     = uint32(0x00053900)
)

var (
	soloBossFirstHookOriginal       = []byte{0x80, 0xFA, 0x02, 0x75, 0x2C}
	soloBossSecondHookOriginal      = []byte{0x80, 0xFA, 0x02, 0x0F, 0x84, 0x7E, 0x01, 0x00, 0x00}
	soloBossDescriptionHookOriginal = []byte{0x81, 0xFB, 0x30, 0x75, 0x00, 0x00}
)

type SoloBossCardPatchResult struct {
	ClientRoot         string `json:"client_root"`
	DLLPath            string `json:"dll_path"`
	BeforeSHA256       string `json:"before_sha256"`
	AfterSHA256        string `json:"after_sha256"`
	ItemID             uint16 `json:"item_id"`
	AIItemID           uint16 `json:"ai_item_id"`
	ResourceItemID     uint16 `json:"resource_item_id"`
	AlreadyPatched     bool   `json:"already_patched"`
	Behavior           string `json:"behavior"`
	FirstHookRVA       string `json:"first_hook_rva"`
	SecondHookRVA      string `json:"second_hook_rva"`
	FirstCaveRVA       string `json:"first_cave_rva"`
	SecondCaveRVA      string `json:"second_cave_rva"`
	DescriptionHookRVA string `json:"description_hook_rva"`
	DescriptionCaveRVA string `json:"description_cave_rva"`
}

// PatchStaticSoloBossCard adds local control-item shop definitions that reuse
// item 99's artwork, then extends QQTSection's proven Start-button gate.
// Adventure still accepts only native item 99. An active local Boss/AI card
// bypasses the native topology gate only for the final client's ordinary map
// families plus the two registered football-Boss maps; the authoritative
// server then applies Boss-first, AI-second and normal-topology fallback rules.
func PatchStaticSoloBossCard(clientRoot string) (SoloBossCardPatchResult, error) {
	result, data, image, state, err := inspectSoloBossCard(clientRoot)
	if err != nil {
		return result, err
	}
	defer image.Close()
	if state.fullyPatched {
		if err := ensureLocalControlCardRegistryEntries(clientRoot, false); err != nil {
			return result, err
		}
		if err := ensureNativeControlCardDescriptions(clientRoot, false); err != nil {
			return result, err
		}
		return result, nil
	}
	if (state.hookPatched || state.cavesPatched) && !state.legacyPatched {
		return result, fmt.Errorf("QQTSection.dll has a partial single-player Boss-card patch")
	}

	firstCave := buildSoloBossFirstCave()
	secondCave := buildSoloBossSecondCave()
	descriptionCave := buildSoloBossDescriptionCave()
	clear(data[state.firstCaveOffset:state.secondCaveOffset])
	clear(data[state.secondCaveOffset:state.descriptionCaveOffset])
	clear(data[state.descriptionCaveOffset : state.descriptionCaveOffset+(soloBossGateTextEndRVA-soloBossDescriptionCaveRVA)])
	copy(data[state.firstCaveOffset:], firstCave)
	copy(data[state.secondCaveOffset:], secondCave)
	copy(data[state.descriptionCaveOffset:], descriptionCave)
	copy(data[state.firstHookOffset:], relativeJump(soloBossGateFirstHookRVA, soloBossGateFirstCaveRVA, len(soloBossFirstHookOriginal)))
	copy(data[state.secondHookOffset:], relativeJump(soloBossGateSecondHookRVA, soloBossGateSecondCaveRVA, len(soloBossSecondHookOriginal)))
	copy(data[state.descriptionHookOffset:], relativeJump(soloBossDescriptionHookRVA, soloBossDescriptionCaveRVA, len(soloBossDescriptionHookOriginal)))
	if state.textVirtualSize < state.requiredTextVirtualSize {
		binary.LittleEndian.PutUint32(data[state.textHeaderOffset+8:state.textHeaderOffset+12], state.requiredTextVirtualSize)
	}
	if err := os.WriteFile(result.DLLPath, data, 0o755); err != nil {
		return result, fmt.Errorf("write QQTSection single-player Boss-card patch: %w", err)
	}
	if err := ensureLocalControlCardRegistryEntries(clientRoot, false); err != nil {
		return result, err
	}
	if err := ensureNativeControlCardDescriptions(clientRoot, false); err != nil {
		return result, err
	}
	result.AfterSHA256 = sha256Hex(data)
	return result, nil
}

func CheckStaticSoloBossCard(clientRoot string) (SoloBossCardPatchResult, error) {
	result, _, image, state, err := inspectSoloBossCard(clientRoot)
	if err != nil {
		return result, err
	}
	defer image.Close()
	if !state.fullyPatched {
		return result, fmt.Errorf("QQTSection.dll single-player Boss-card gate is not fully patched")
	}
	if err := ensureLocalControlCardRegistryEntries(clientRoot, true); err != nil {
		return result, err
	}
	if err := ensureNativeControlCardDescriptions(clientRoot, true); err != nil {
		return result, err
	}
	return result, nil
}

type soloBossPatchState struct {
	firstHookOffset         uint32
	secondHookOffset        uint32
	descriptionHookOffset   uint32
	firstCaveOffset         uint32
	secondCaveOffset        uint32
	descriptionCaveOffset   uint32
	textHeaderOffset        uint32
	textVirtualSize         uint32
	requiredTextVirtualSize uint32
	hookPatched             bool
	cavesPatched            bool
	fullyPatched            bool
	legacyPatched           bool
}

func inspectSoloBossCard(clientRoot string) (SoloBossCardPatchResult, []byte, *pe.File, soloBossPatchState, error) {
	dllPath := filepath.Join(clientRoot, "QQTSection.dll")
	result := SoloBossCardPatchResult{
		ClientRoot: clientRoot, DLLPath: dllPath,
		ItemID: game.SinglePlayerBossCardItemID, AIItemID: game.CompetitiveAICardItemID, ResourceItemID: game.SinglePlayerAdventureCardItemID,
		Behavior:     "register local control cards in Python and native propdescrip catalogs with item 99 artwork; allow only IDs 30098/30099 to carry descriptions through the native material-range GetStorage projection; preserve item-99 adventure admission; an active Boss/AI card defers topology only on ordinary maps or registered football-Boss maps, then the authoritative server applies Boss-first, AI-second and normal-topology fallback rules",
		FirstHookRVA: fmt.Sprintf("0x%X", soloBossGateFirstHookRVA), SecondHookRVA: fmt.Sprintf("0x%X", soloBossGateSecondHookRVA),
		FirstCaveRVA: fmt.Sprintf("0x%X", soloBossGateFirstCaveRVA), SecondCaveRVA: fmt.Sprintf("0x%X", soloBossGateSecondCaveRVA),
		DescriptionHookRVA: fmt.Sprintf("0x%X", soloBossDescriptionHookRVA), DescriptionCaveRVA: fmt.Sprintf("0x%X", soloBossDescriptionCaveRVA),
	}
	data, err := os.ReadFile(dllPath)
	if err != nil {
		return result, nil, nil, soloBossPatchState{}, fmt.Errorf("read QQTSection.dll: %w", err)
	}
	result.BeforeSHA256 = sha256Hex(data)
	result.AfterSHA256 = result.BeforeSHA256
	image, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		return result, nil, nil, soloBossPatchState{}, fmt.Errorf("parse QQTSection.dll: %w", err)
	}
	if image.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, fmt.Errorf("QQTSection.dll is not x86")
	}
	firstHookOffset, err := peRVAFileOffset(image, "QQTSection.dll", soloBossGateFirstHookRVA, uint32(len(soloBossFirstHookOriginal)))
	if err != nil {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, err
	}
	secondHookOffset, err := peRVAFileOffset(image, "QQTSection.dll", soloBossGateSecondHookRVA, uint32(len(soloBossSecondHookOriginal)))
	if err != nil {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, err
	}
	descriptionHookOffset, err := peRVAFileOffset(image, "QQTSection.dll", soloBossDescriptionHookRVA, uint32(len(soloBossDescriptionHookOriginal)))
	if err != nil {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, err
	}
	firstCave := buildSoloBossFirstCave()
	secondCave := buildSoloBossSecondCave()
	descriptionCave := buildSoloBossDescriptionCave()
	legacyFirstCave := buildSoloBossFirstCaveV1()
	legacySecondCave := buildSoloBossSecondCaveV1()
	previousFirstCave := buildSoloBossFirstCaveV3()
	previousSecondCave := buildSoloBossSecondCaveV3()
	previousSecondCaveV2 := buildSoloBossSecondCaveV2()
	previousBroadFirstCave := buildSoloBossFirstCaveV4BroadAdmission()
	previousBroadSecondCave := buildSoloBossSecondCaveV4BroadAdmission()
	firstCaveOffset, err := peRVAFileOffset(image, "QQTSection.dll", soloBossGateFirstCaveRVA, uint32(len(firstCave)))
	if err != nil {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, err
	}
	secondCaveOffset, err := peRVAFileOffset(image, "QQTSection.dll", soloBossGateSecondCaveRVA, uint32(len(secondCave)))
	if err != nil {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, err
	}
	descriptionCaveOffset, err := peRVAFileOffset(image, "QQTSection.dll", soloBossDescriptionCaveRVA, uint32(len(descriptionCave)))
	if err != nil {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, err
	}
	textHeader, textVirtualSize, textVA, err := peSectionHeader(data, ".text")
	if err != nil {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, err
	}
	requiredTextVirtualSize := soloBossGateTextEndRVA - textVA
	firstPatched := bytes.Equal(data[firstHookOffset:firstHookOffset+uint32(len(soloBossFirstHookOriginal))], relativeJump(soloBossGateFirstHookRVA, soloBossGateFirstCaveRVA, len(soloBossFirstHookOriginal)))
	secondPatched := bytes.Equal(data[secondHookOffset:secondHookOffset+uint32(len(soloBossSecondHookOriginal))], relativeJump(soloBossGateSecondHookRVA, soloBossGateSecondCaveRVA, len(soloBossSecondHookOriginal)))
	descriptionPatched := bytes.Equal(data[descriptionHookOffset:descriptionHookOffset+uint32(len(soloBossDescriptionHookOriginal))], relativeJump(soloBossDescriptionHookRVA, soloBossDescriptionCaveRVA, len(soloBossDescriptionHookOriginal)))
	hookPatched := firstPatched || secondPatched || descriptionPatched
	if !firstPatched && !bytes.Equal(data[firstHookOffset:firstHookOffset+uint32(len(soloBossFirstHookOriginal))], soloBossFirstHookOriginal) {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, fmt.Errorf("QQTSection first Start-gate signature mismatch: %s", hex.EncodeToString(data[firstHookOffset:firstHookOffset+uint32(len(soloBossFirstHookOriginal))]))
	}
	if !secondPatched && !bytes.Equal(data[secondHookOffset:secondHookOffset+uint32(len(soloBossSecondHookOriginal))], soloBossSecondHookOriginal) {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, fmt.Errorf("QQTSection second Start-gate signature mismatch: %s", hex.EncodeToString(data[secondHookOffset:secondHookOffset+uint32(len(soloBossSecondHookOriginal))]))
	}
	if !descriptionPatched && !bytes.Equal(data[descriptionHookOffset:descriptionHookOffset+uint32(len(soloBossDescriptionHookOriginal))], soloBossDescriptionHookOriginal) {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, fmt.Errorf("QQTSection storage-description signature mismatch: %s", hex.EncodeToString(data[descriptionHookOffset:descriptionHookOffset+uint32(len(soloBossDescriptionHookOriginal))]))
	}
	firstCavePatched := bytes.Equal(data[firstCaveOffset:firstCaveOffset+uint32(len(firstCave))], firstCave)
	secondCavePatched := bytes.Equal(data[secondCaveOffset:secondCaveOffset+uint32(len(secondCave))], secondCave)
	descriptionCavePatched := bytes.Equal(data[descriptionCaveOffset:descriptionCaveOffset+uint32(len(descriptionCave))], descriptionCave)
	legacyFirstPatched := bytes.Equal(data[firstCaveOffset:firstCaveOffset+uint32(len(legacyFirstCave))], legacyFirstCave)
	legacySecondPatched := bytes.Equal(data[secondCaveOffset:secondCaveOffset+uint32(len(legacySecondCave))], legacySecondCave)
	previousFirstPatched := bytes.Equal(data[firstCaveOffset:firstCaveOffset+uint32(len(previousFirstCave))], previousFirstCave)
	previousSecondPatched := bytes.Equal(data[secondCaveOffset:secondCaveOffset+uint32(len(previousSecondCave))], previousSecondCave)
	previousSecondV2Patched := bytes.Equal(data[secondCaveOffset:secondCaveOffset+uint32(len(previousSecondCaveV2))], previousSecondCaveV2)
	previousBroadFirstPatched := bytes.Equal(data[firstCaveOffset:firstCaveOffset+uint32(len(previousBroadFirstCave))], previousBroadFirstCave)
	previousBroadSecondPatched := bytes.Equal(data[secondCaveOffset:secondCaveOffset+uint32(len(previousBroadSecondCave))], previousBroadSecondCave)
	knownDescriptionState := (descriptionPatched && descriptionCavePatched) ||
		(!descriptionPatched && allZero(data[descriptionCaveOffset:descriptionCaveOffset+uint32(len(descriptionCave))]))
	legacyPatched := firstPatched && secondPatched &&
		((legacyFirstPatched && legacySecondPatched) ||
			(previousFirstPatched && previousSecondPatched) ||
			(previousFirstPatched && previousSecondV2Patched) ||
			(previousBroadFirstPatched && previousBroadSecondPatched) ||
			(firstCavePatched && secondCavePatched)) &&
		knownDescriptionState
	cavesPatched := firstCavePatched || secondCavePatched || descriptionCavePatched
	if !firstCavePatched && !legacyFirstPatched && !previousFirstPatched && !previousBroadFirstPatched && !allZero(data[firstCaveOffset:firstCaveOffset+uint32(len(firstCave))]) {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, fmt.Errorf("QQTSection first Start-gate cave is not empty")
	}
	if !secondCavePatched && !legacySecondPatched && !previousSecondPatched && !previousSecondV2Patched && !previousBroadSecondPatched && !allZero(data[secondCaveOffset:secondCaveOffset+uint32(len(secondCave))]) {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, fmt.Errorf("QQTSection second Start-gate cave is not empty")
	}
	if !descriptionCavePatched && !allZero(data[descriptionCaveOffset:descriptionCaveOffset+uint32(len(descriptionCave))]) {
		image.Close()
		return result, nil, nil, soloBossPatchState{}, fmt.Errorf("QQTSection storage-description cave is not empty")
	}
	fullyPatched := firstPatched && secondPatched && descriptionPatched && firstCavePatched && secondCavePatched && descriptionCavePatched && textVirtualSize >= requiredTextVirtualSize
	result.AlreadyPatched = fullyPatched
	return result, data, image, soloBossPatchState{
		firstHookOffset: firstHookOffset, secondHookOffset: secondHookOffset, descriptionHookOffset: descriptionHookOffset,
		firstCaveOffset: firstCaveOffset, secondCaveOffset: secondCaveOffset, descriptionCaveOffset: descriptionCaveOffset,
		textHeaderOffset: textHeader, textVirtualSize: textVirtualSize,
		requiredTextVirtualSize: requiredTextVirtualSize,
		hookPatched:             hookPatched, cavesPatched: cavesPatched, fullyPatched: fullyPatched, legacyPatched: legacyPatched,
	}, nil
}

func peSectionHeader(data []byte, wanted string) (uint32, uint32, uint32, error) {
	if len(data) < 0x40 || string(data[:2]) != "MZ" {
		return 0, 0, 0, fmt.Errorf("PE image has no DOS header")
	}
	peOffset := binary.LittleEndian.Uint32(data[0x3C:0x40])
	if peOffset+24 > uint32(len(data)) || string(data[peOffset:peOffset+4]) != "PE\x00\x00" {
		return 0, 0, 0, fmt.Errorf("PE image has no valid signature")
	}
	count := binary.LittleEndian.Uint16(data[peOffset+6 : peOffset+8])
	optionalSize := binary.LittleEndian.Uint16(data[peOffset+20 : peOffset+22])
	table := peOffset + 24 + uint32(optionalSize)
	for index := uint16(0); index < count; index++ {
		header := table + uint32(index)*40
		if header+40 > uint32(len(data)) {
			return 0, 0, 0, fmt.Errorf("PE section table is truncated")
		}
		name := string(bytes.TrimRight(data[header:header+8], "\x00"))
		if name == wanted {
			return header, binary.LittleEndian.Uint32(data[header+8 : header+12]), binary.LittleEndian.Uint32(data[header+12 : header+16]), nil
		}
	}
	return 0, 0, 0, fmt.Errorf("PE section %s is missing", wanted)
}

func relativeJump(fromRVA, targetRVA uint32, size int) []byte {
	result := make([]byte, size)
	result[0] = 0xE9
	binary.LittleEndian.PutUint32(result[1:5], uint32(int32(int64(targetRVA)-int64(fromRVA+5))))
	for index := 5; index < size; index++ {
		result[index] = 0x90
	}
	return result
}

type x86Fixup struct {
	offset int
	size   int
	label  string
}

type x86Builder struct {
	base   uint32
	code   []byte
	labels map[string]int
	fixups []x86Fixup
}

func newX86Builder(base uint32) *x86Builder {
	return &x86Builder{base: base, labels: make(map[string]int)}
}

func (builder *x86Builder) emit(values ...byte) { builder.code = append(builder.code, values...) }
func (builder *x86Builder) mark(label string)   { builder.labels[label] = len(builder.code) }
func (builder *x86Builder) short(op byte, label string) {
	builder.emit(op, 0)
	builder.fixups = append(builder.fixups, x86Fixup{offset: len(builder.code) - 1, size: 1, label: label})
}
func (builder *x86Builder) near(op byte, targetRVA uint32) {
	builder.emit(op, 0, 0, 0, 0)
	displacement := int64(targetRVA) - int64(builder.base+uint32(len(builder.code)))
	binary.LittleEndian.PutUint32(builder.code[len(builder.code)-4:], uint32(int32(displacement)))
}
func (builder *x86Builder) finish() []byte {
	for _, fixup := range builder.fixups {
		target, ok := builder.labels[fixup.label]
		if !ok {
			panic("missing x86 label " + fixup.label)
		}
		displacement := target - (fixup.offset + fixup.size)
		if displacement < -128 || displacement > 127 {
			panic("x86 short branch outside range")
		}
		builder.code[fixup.offset] = byte(int8(displacement))
	}
	return builder.code
}

func buildSoloBossFirstCave() []byte {
	b := newX86Builder(soloBossGateFirstCaveRVA)
	b.emit(0x8B, 0x75, 0xF0) // mov esi,[ebp-10h]
	b.emit(0x80, 0xFA, 0x02)
	b.short(0x74, "scan") // adventure retains native item-99 admission
	b.emit(0x80, 0xFA, 0x00)
	b.short(0x74, "scan")
	b.emit(0x80, 0xFA, 0x01)
	b.short(0x74, "scan")
	b.emit(0x80, 0xFA, 0x03)
	b.short(0x75, "resume")
	b.mark("scan")
	b.emit(0x33, 0xFF, 0x0F, 0xB7, 0x4E, 0x54, 0x85, 0xC9)
	b.short(0x7E, "resume")
	b.emit(0x8D, 0x46, 0x56)
	b.mark("loop")
	b.emit(0x80, 0xFA, 0x02)
	b.short(0x75, "competitive_item")
	b.emit(0x66, 0x83, 0x38, byte(game.SinglePlayerAdventureCardItemID))
	b.short(0x74, "found")
	b.short(0xEB, "next")
	b.mark("competitive_item")
	b.emit(0x66, 0x81, 0x38, byte(game.CompetitiveAICardItemID&0xFF), byte(game.CompetitiveAICardItemID>>8))
	b.short(0x74, "found")
	b.emit(0x66, 0x81, 0x38, byte(game.SinglePlayerBossCardItemID&0xFF), byte(game.SinglePlayerBossCardItemID>>8))
	b.short(0x75, "next")
	b.mark("found")
	b.emit(0x83, 0x78, 0x02, 0x00)
	b.short(0x7F, "active")
	b.mark("next")
	b.emit(0x47, 0x83, 0xC0, 0x12, 0x3B, 0xF9)
	b.short(0x7C, "loop")
	b.short(0xEB, "resume")
	b.mark("active")
	b.emit(0xC6, 0x45, 0xFF, 0x01)
	b.mark("resume")
	b.near(0xE9, soloBossGateResumeRVA)
	return b.finish()
}

var competitiveControlCardMapRanges = [...]struct{ first, last uint16 }{
	{1, 28}, {101, 128}, {201, 231}, {301, 329},
	{401, 426}, {501, 525}, {901, 923},
}

var competitiveControlCardExtraMapIDs = [...]uint16{701, 702}

func competitiveControlCardAllowsMapID(mapID uint16) bool {
	// Zero is the native random-map selection. The server resolves it only from
	// the installed ordinary-map pool before making the authoritative decision.
	if mapID == 0 {
		return true
	}
	for _, bounds := range competitiveControlCardMapRanges {
		if mapID >= bounds.first && mapID <= bounds.last {
			return true
		}
	}
	for _, candidate := range competitiveControlCardExtraMapIDs {
		if mapID == candidate {
			return true
		}
	}
	return false
}

func buildSoloBossSecondCave() []byte {
	b := newX86Builder(soloBossGateSecondCaveRVA)
	b.emit(0x80, 0xFA, 0x02)
	b.short(0x74, "success")
	b.emit(0x80, 0x7D, 0xFF, 0x00)
	b.short(0x74, "normal")
	b.emit(0x0F, 0xB7, 0x83, 0x88, 0x00, 0x00, 0x00) // movzx eax,word ptr [ebx+88h]
	b.emit(0x66, 0x85, 0xC0)                         // test ax,ax (random map)
	b.short(0x74, "success")
	for index, bounds := range competitiveControlCardMapRanges {
		nextRange := fmt.Sprintf("next_range_%d", index)
		b.emit(0x66, 0x3D, byte(bounds.first&0xFF), byte(bounds.first>>8))
		b.short(0x72, nextRange) // jb
		b.emit(0x66, 0x3D, byte(bounds.last&0xFF), byte(bounds.last>>8))
		b.short(0x76, "success") // jbe
		b.mark(nextRange)
	}
	for _, mapID := range competitiveControlCardExtraMapIDs {
		b.emit(0x66, 0x3D, byte(mapID&0xFF), byte(mapID>>8))
		b.short(0x74, "success")
	}
	b.mark("normal")
	b.near(0xE9, soloBossGateNormalRVA)
	b.mark("success")
	b.near(0xE9, soloBossGateSuccessRVA)
	return b.finish()
}

// The native GetStorage projection resolves names and descriptions from
// propdescrip.xml. Its material range (30001..31999) deliberately skips every
// description, so the two local control cards need a narrow exception while
// ordinary materials retain their original name-only behavior.
func buildSoloBossDescriptionCave() []byte {
	b := newX86Builder(soloBossDescriptionCaveRVA)
	b.emit(0x81, 0xFB, byte(game.SinglePlayerBossCardItemID&0xFF), byte(game.SinglePlayerBossCardItemID>>8), 0x00, 0x00)
	b.short(0x74, "read")
	b.emit(0x81, 0xFB, byte(game.CompetitiveAICardItemID&0xFF), byte(game.CompetitiveAICardItemID>>8), 0x00, 0x00)
	b.short(0x74, "read")
	b.emit(0x81, 0xFB, 0x30, 0x75, 0x00, 0x00) // cmp ebx,30000
	b.short(0x7E, "read")
	b.emit(0x81, 0xFB, 0x00, 0x7D, 0x00, 0x00) // cmp ebx,32000
	b.short(0x7C, "done")
	b.mark("read")
	b.near(0xE9, soloBossDescriptionReadRVA)
	b.mark("done")
	b.near(0xE9, soloBossDescriptionDoneRVA)
	return b.finish()
}

// Version 4 sent every competitive Start request to the server regardless of
// whether a control card was active or the selected map belonged to the
// supported ordinary/Boss union. Retain its exact bodies only so an already
// prepared client can be migrated to the scoped gate above.
func buildSoloBossFirstCaveV4BroadAdmission() []byte {
	b := newX86Builder(soloBossGateFirstCaveRVA)
	b.emit(0x8B, 0x75, 0xF0)
	b.emit(0x80, 0xFA, 0x02)
	b.short(0x74, "scan")
	b.emit(0xC6, 0x45, 0xFF, 0x02)
	b.short(0xEB, "resume")
	b.mark("scan")
	b.emit(0x33, 0xFF, 0x0F, 0xB7, 0x4E, 0x54, 0x85, 0xC9)
	b.short(0x7E, "resume")
	b.emit(0x8D, 0x46, 0x56)
	b.mark("loop")
	b.emit(0x66, 0x83, 0x38, byte(game.SinglePlayerAdventureCardItemID))
	b.short(0x75, "next")
	b.emit(0x83, 0x78, 0x02, 0x00)
	b.short(0x7F, "found")
	b.mark("next")
	b.emit(0x47, 0x83, 0xC0, 0x12, 0x3B, 0xF9)
	b.short(0x7C, "loop")
	b.short(0xEB, "resume")
	b.mark("found")
	b.emit(0xC6, 0x45, 0xFF, 0x01)
	b.mark("resume")
	b.near(0xE9, soloBossGateResumeRVA)
	return b.finish()
}

func buildSoloBossSecondCaveV4BroadAdmission() []byte {
	b := newX86Builder(soloBossGateSecondCaveRVA)
	b.emit(0x80, 0xFA, 0x02)
	b.short(0x74, "success")
	b.emit(0x80, 0x7D, 0xFF, 0x02)
	b.short(0x74, "success")
	b.emit(0x83, 0xF8, 0x02)
	b.short(0x7D, "normal")
	b.emit(0x80, 0x7D, 0xFF, 0x00)
	b.short(0x75, "success")
	b.mark("normal")
	b.near(0xE9, soloBossGateNormalRVA)
	b.mark("success")
	b.near(0xE9, soloBossGateSuccessRVA)
	return b.finish()
}

// Version 3 scanned the custom cards from the client's native inventory.
// Keep it recognizable so prepared clients can migrate to the server-owned
// admission rule without retaining client-side ownership checks.
func buildSoloBossFirstCaveV3() []byte {
	const legacyBossCardID = uint16(30098)
	const legacyAICardID = uint16(30099)
	b := newX86Builder(soloBossGateFirstCaveRVA)
	b.emit(0x8B, 0x75, 0xF0)
	b.emit(0x80, 0xFA, 0x02)
	b.short(0x74, "scan")
	b.emit(0x80, 0xFA, 0x00)
	b.short(0x74, "scan")
	b.emit(0x80, 0xFA, 0x01)
	b.short(0x74, "scan")
	b.emit(0x80, 0xFA, 0x03)
	b.short(0x75, "resume")
	b.mark("scan")
	b.emit(0x33, 0xFF, 0x0F, 0xB7, 0x4E, 0x54, 0x85, 0xC9)
	b.short(0x7E, "resume")
	b.emit(0x8D, 0x46, 0x56)
	b.mark("loop")
	b.emit(0x80, 0xFA, 0x02)
	b.short(0x75, "competitive_item")
	b.emit(0x66, 0x83, 0x38, byte(game.SinglePlayerAdventureCardItemID))
	b.short(0x74, "ordinary_found_check")
	b.short(0xEB, "next")
	b.mark("competitive_item")
	b.emit(0x66, 0x81, 0x38, byte(legacyAICardID&0xFF), byte(legacyAICardID>>8))
	b.short(0x74, "ai_found_check")
	b.emit(0x66, 0x81, 0x38, byte(legacyBossCardID&0xFF), byte(legacyBossCardID>>8))
	b.short(0x75, "next")
	b.mark("ordinary_found_check")
	b.emit(0x83, 0x78, 0x02, 0x00)
	b.short(0x7F, "ordinary_found")
	b.short(0xEB, "next")
	b.mark("ai_found_check")
	b.emit(0x83, 0x78, 0x02, 0x00)
	b.short(0x7F, "ai_found")
	b.mark("next")
	b.emit(0x47, 0x83, 0xC0, 0x12, 0x3B, 0xF9)
	b.short(0x7C, "loop")
	b.short(0xEB, "resume")
	b.mark("ordinary_found")
	b.emit(0xC6, 0x45, 0xFF, 0x01)
	b.short(0xEB, "resume")
	b.mark("ai_found")
	b.emit(0xC6, 0x45, 0xFF, 0x02)
	b.mark("resume")
	b.near(0xE9, soloBossGateResumeRVA)
	return b.finish()
}

func buildSoloBossSecondCaveV3() []byte {
	const legacyAICardID = uint16(30099)
	b := newX86Builder(soloBossGateSecondCaveRVA)
	b.emit(0x80, 0xFA, 0x02)
	b.short(0x74, "success")
	b.emit(0x50, 0x51, 0x57)
	b.emit(0x0F, 0xB7, 0x4E, 0x54, 0x85, 0xC9)
	b.short(0x7E, "not_ai")
	b.emit(0x8D, 0x7E, 0x56)
	b.mark("ai_loop")
	b.emit(0x66, 0x81, 0x3F, byte(legacyAICardID&0xFF), byte(legacyAICardID>>8))
	b.short(0x75, "ai_next")
	b.emit(0x83, 0x7F, 0x02, 0x00)
	b.short(0x7F, "ai_found")
	b.mark("ai_next")
	b.emit(0x83, 0xC7, 0x12)
	b.emit(0x49)
	b.short(0x75, "ai_loop")
	b.mark("not_ai")
	b.emit(0x5F, 0x59, 0x58)
	b.emit(0x80, 0x7D, 0xFF, 0x02)
	b.short(0x74, "success")
	b.emit(0x83, 0xF8, 0x02)
	b.short(0x7D, "normal")
	b.emit(0x80, 0x7D, 0xFF, 0x00)
	b.short(0x75, "success")
	b.mark("normal")
	b.near(0xE9, soloBossGateNormalRVA)
	b.mark("ai_found")
	b.emit(0x83, 0xC4, 0x0C)
	b.mark("success")
	b.near(0xE9, soloBossGateSuccessRVA)
	return b.finish()
}

// Version 2 trusted the first hook's [ebp-1] marker at the topology hook. It
// remains recognizable only so already prepared development clients can be
// upgraded in place to the direct inventory re-check above.
func buildSoloBossSecondCaveV2() []byte {
	b := newX86Builder(soloBossGateSecondCaveRVA)
	b.emit(0x80, 0xFA, 0x02)
	b.short(0x74, "success")
	b.emit(0x80, 0x7D, 0xFF, 0x02)
	b.short(0x74, "success")
	b.emit(0x83, 0xF8, 0x02)
	b.short(0x7D, "normal")
	b.emit(0x80, 0x7D, 0xFF, 0x00)
	b.short(0x75, "success")
	b.mark("normal")
	b.near(0xE9, soloBossGateNormalRVA)
	b.mark("success")
	b.near(0xE9, soloBossGateSuccessRVA)
	return b.finish()
}

// Version 1 is retained only so an already prepared development/release copy
// can be upgraded in place. New packages never install this body.
func buildSoloBossFirstCaveV1() []byte {
	const legacyBossCardID = uint16(30098)
	b := newX86Builder(soloBossGateFirstCaveRVA)
	b.emit(0x8B, 0x75, 0xF0)
	b.emit(0x80, 0xFA, 0x02)
	b.short(0x74, "scan")
	b.emit(0x80, 0xFA, 0x00)
	b.short(0x74, "scan")
	b.emit(0x80, 0xFA, 0x01)
	b.short(0x74, "scan")
	b.emit(0x80, 0xFA, 0x03)
	b.short(0x75, "resume")
	b.mark("scan")
	b.emit(0x33, 0xFF, 0x0F, 0xB7, 0x4E, 0x54, 0x85, 0xC9)
	b.short(0x7E, "resume")
	b.emit(0x8D, 0x46, 0x56)
	b.mark("loop")
	b.emit(0x80, 0xFA, 0x02)
	b.short(0x75, "boss_item")
	b.emit(0x66, 0x83, 0x38, byte(game.SinglePlayerAdventureCardItemID))
	b.short(0xEB, "id_checked")
	b.mark("boss_item")
	b.emit(0x66, 0x81, 0x38, byte(legacyBossCardID&0xFF), byte(legacyBossCardID>>8))
	b.mark("id_checked")
	b.short(0x75, "next")
	b.emit(0x83, 0x78, 0x02, 0x00)
	b.short(0x7F, "found")
	b.mark("next")
	b.emit(0x47, 0x83, 0xC0, 0x12, 0x3B, 0xF9)
	b.short(0x7C, "loop")
	b.short(0xEB, "resume")
	b.mark("found")
	b.emit(0xC6, 0x45, 0xFF, 0x01)
	b.mark("resume")
	b.near(0xE9, soloBossGateResumeRVA)
	return b.finish()
}

func buildSoloBossSecondCaveV1() []byte {
	b := newX86Builder(soloBossGateSecondCaveRVA)
	b.emit(0x80, 0xFA, 0x02)
	b.short(0x74, "success")
	b.emit(0x83, 0xF8, 0x02)
	b.short(0x7D, "normal")
	b.emit(0x80, 0x7D, 0xFF, 0x00)
	b.short(0x75, "success")
	b.mark("normal")
	b.near(0xE9, soloBossGateNormalRVA)
	b.mark("success")
	b.near(0xE9, soloBossGateSuccessRVA)
	return b.finish()
}

const (
	soloBossItemEntry             = "\t\t30098:('item', 99, '单人BOSS卡', '持有且未收藏时，满足BOSS挑战条件即可单人开始竞技BOSS战。', '', 0),"
	soloBossCommodityEntry        = "\t30098:('item', 99, '单人BOSS卡', '持有且未收藏时，满足BOSS挑战条件即可单人开始竞技BOSS战。'),"
	soloBossItemEntryV1           = "\t\t30098:('item', 99, '单人BOSS卡', '持有此卡且满足BOSS挑战条件时，可以单人开始竞技BOSS战。', '', 0),"
	soloBossCommodityEntryV1      = "\t30098:('item', 99, '单人BOSS卡', '持有此卡且满足BOSS挑战条件时，可以单人开始竞技BOSS战。'),"
	competitiveAIItemEntry        = "\t\t30099:('item', 99, 'AI对战卡', '持有且未收藏时，普通竞技开局自动补充AI对手。自由场补满可用位置；组队房只有一队且剩余位置足够时补充等人数对手，否则不能开始。AI只在本局出现。', '', 0),"
	competitiveAICommodityEntry   = "\t30099:('item', 99, 'AI对战卡', '持有且未收藏时，普通竞技开局自动补充AI对手。自由场补满可用位置；组队房只有一队且剩余位置足够时补充等人数对手，否则不能开始。AI只在本局出现。'),"
	competitiveAIItemEntryV1      = "\t\t30099:('item', 99, 'AI对战卡', '放在背包时，符合条件的普通竞技房会自动补充AI对手；放入收藏柜即可关闭。', '', 0),"
	competitiveAICommodityEntryV1 = "\t30099:('item', 99, 'AI对战卡', '放在背包时，符合条件的普通竞技房会自动补充AI对手；放入收藏柜即可关闭。'),"
	competitiveAIItemEntryV2      = "\t\t30099:('item', 99, 'AI对战卡', '持有且未收藏时，普通竞技开局自动补充AI对手。自由场补满可用位置；组队房只有一队时补充等人数对手。AI只在本局出现。', '', 0),"
	competitiveAICommodityEntryV2 = "\t30099:('item', 99, 'AI对战卡', '持有且未收藏时，普通竞技开局自动补充AI对手。自由场补满可用位置；组队房只有一队时补充等人数对手。AI只在本局出现。'),"
	nativeBossDescriptionEntry    = "\t\t<localcard98 id=\"30098\" name=\"单人BOSS卡\" description=\"持有且未收藏时，满足BOSS挑战条件即可单人开始竞技BOSS战。\"></localcard98>"
	nativeAIDescriptionEntry      = "\t\t<localcard99 id=\"30099\" name=\"AI对战卡\" description=\"持有且未收藏时，普通竞技开局会按房间规则自动补充AI对手。\"></localcard99>"
)

func ensureNativeControlCardDescriptions(clientRoot string, check bool) error {
	path := filepath.Join(clientRoot, "config", "propdescrip.xml")
	encoded, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read propdescrip.xml: %w", err)
	}
	decoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), encoded)
	if err != nil {
		return fmt.Errorf("decode propdescrip.xml: %w", err)
	}
	text := string(decoded)
	marker := "\t</材料>"
	if !strings.Contains(text, marker) {
		return fmt.Errorf("propdescrip.xml has no material-category closing tag")
	}
	changed := false
	for _, entry := range []struct {
		id   string
		text string
		name string
	}{
		{"30098", nativeBossDescriptionEntry, "single-player Boss card"},
		{"30099", nativeAIDescriptionEntry, "competitive AI card"},
	} {
		if strings.Contains(text, entry.text) {
			continue
		}
		if strings.Contains(text, "id=\""+entry.id+"\"") {
			return fmt.Errorf("propdescrip.xml contains a conflicting item %s entry", entry.id)
		}
		if check {
			return fmt.Errorf("propdescrip.xml is missing the %s entry", entry.name)
		}
		text = strings.Replace(text, marker, entry.text+"\r\n"+marker, 1)
		changed = true
	}
	if !changed {
		return nil
	}
	updated, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(text))
	if err != nil {
		return fmt.Errorf("encode propdescrip.xml: %w", err)
	}
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("write propdescrip.xml: %w", err)
	}
	return nil
}

func ensureLocalControlCardRegistryEntries(clientRoot string, check bool) error {
	files := []struct {
		path    string
		table   string
		entries []struct {
			id    string
			entry string
			name  string
		}
	}{
		{filepath.Join(clientRoot, "object", "itemCFG.py"), "itemList = {", []struct {
			id    string
			entry string
			name  string
		}{{"30098", soloBossItemEntry, "single-player Boss card"}, {"30099", competitiveAIItemEntry, "competitive AI card"}}},
		{filepath.Join(clientRoot, "object", "commodityCFG.py"), "commodityList = {", []struct {
			id    string
			entry string
			name  string
		}{{"30098", soloBossCommodityEntry, "single-player Boss card"}, {"30099", competitiveAICommodityEntry, "competitive AI card"}}},
	}
	for _, file := range files {
		encoded, err := os.ReadFile(file.path)
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(file.path), err)
		}
		decoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), encoded)
		if err != nil {
			return fmt.Errorf("decode %s: %w", filepath.Base(file.path), err)
		}
		text := string(decoded)
		marker := file.table + "\r\n"
		if !strings.Contains(text, marker) {
			marker = file.table + "\n"
		}
		if !strings.Contains(text, marker) {
			return fmt.Errorf("%s has no %q dictionary marker", filepath.Base(file.path), file.table)
		}
		changed := false
		for _, entry := range file.entries {
			if strings.Contains(text, entry.entry) {
				continue
			}
			var legacyEntries []string
			switch entry.id {
			case "30098":
				legacyEntries = []string{soloBossItemEntryV1, soloBossCommodityEntryV1}
			case "30099":
				legacyEntries = []string{
					competitiveAIItemEntryV1, competitiveAICommodityEntryV1,
					competitiveAIItemEntryV2, competitiveAICommodityEntryV2,
				}
			}
			replacedLegacy := false
			for _, legacy := range legacyEntries {
				if strings.Contains(text, legacy) {
					text = strings.Replace(text, legacy, entry.entry, 1)
					changed = true
					replacedLegacy = true
					break
				}
			}
			if replacedLegacy {
				continue
			}
			if strings.Contains(text, entry.id+":(") || strings.Contains(text, entry.id+": (") {
				return fmt.Errorf("%s contains a conflicting item %s entry", filepath.Base(file.path), entry.id)
			}
			if check {
				return fmt.Errorf("%s is missing the %s entry", filepath.Base(file.path), entry.name)
			}
			text = strings.Replace(text, marker, marker+entry.entry+"\r\n", 1)
			changed = true
		}
		if !changed {
			continue
		}
		updated, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(text))
		if err != nil {
			return fmt.Errorf("encode %s: %w", filepath.Base(file.path), err)
		}
		if err := os.WriteFile(file.path, updated, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", filepath.Base(file.path), err)
		}
	}
	return nil
}

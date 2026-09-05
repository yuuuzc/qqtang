package clientpatch

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"

	"qqtang/internal/protocol/game"
)

func TestLocalControlCardsUseClientCatalogExtensionRange(t *testing.T) {
	if game.SinglePlayerBossCardItemID != 30098 || game.CompetitiveAICardItemID != 30099 {
		t.Fatalf("local control card IDs = %d/%d, want 30098/30099", game.SinglePlayerBossCardItemID, game.CompetitiveAICardItemID)
	}
}

func TestSoloBossGateRequiresLocalControlCardAndDefersEligibleMaps(t *testing.T) {
	first := buildSoloBossFirstCave()
	if !bytes.Contains(first, []byte{0x66, 0x83, 0x38, byte(game.SinglePlayerAdventureCardItemID)}) {
		t.Fatalf("first cave does not compare original adventure item: %X", first)
	}
	bossID := []byte{0x66, 0x81, 0x38, byte(game.SinglePlayerBossCardItemID & 0xFF), byte(game.SinglePlayerBossCardItemID >> 8)}
	aiID := []byte{0x66, 0x81, 0x38, byte(game.CompetitiveAICardItemID & 0xFF), byte(game.CompetitiveAICardItemID >> 8)}
	if !bytes.Contains(first, bossID) || !bytes.Contains(first, aiID) {
		t.Fatalf("current gate does not require both local control-card identities: %X", first)
	}
	if !bytes.Contains(first, []byte{0xC6, 0x45, 0xFF, 0x01}) {
		t.Fatalf("eligible control-card request is not marked for server validation: %X", first)
	}
	if len(first) >= int(soloBossGateSecondCaveRVA-soloBossGateFirstCaveRVA) {
		t.Fatalf("first cave length %d overlaps second cave", len(first))
	}
	second := buildSoloBossSecondCave()
	if !bytes.Contains(second, []byte{0x80, 0x7D, 0xFF, 0x00}) || !bytes.Contains(second, []byte{0x0F, 0xB7, 0x83, 0x88, 0x00, 0x00, 0x00}) {
		t.Fatalf("second cave does not gate topology deferral on card marker and selected map ID: %X", second)
	}
	if len(second) >= int(soloBossGateTextEndRVA-soloBossGateSecondCaveRVA) {
		t.Fatalf("second cave length %d exceeds reserved text tail", len(second))
	}
}

func TestCompetitiveControlCardMapBoundaryMatchesOrdinaryAndFootballBossUnion(t *testing.T) {
	for _, mapID := range []uint16{0, 1, 28, 101, 128, 201, 231, 301, 329, 401, 426, 501, 525, 901, 923, 701, 702} {
		if !competitiveControlCardAllowsMapID(mapID) {
			t.Fatalf("eligible ordinary/Boss map %d was rejected", mapID)
		}
	}
	for _, mapID := range []uint16{29, 703, 704, 801, 1001, 1101, 1201, 1301, 1401, 1701, 4001, 4002, 4003, 4004} {
		if competitiveControlCardAllowsMapID(mapID) {
			t.Fatalf("non-Boss special map %d bypassed the native topology gate", mapID)
		}
	}
}

func TestRelativeJumpTargetsRequestedRVA(t *testing.T) {
	jump := relativeJump(soloBossGateFirstHookRVA, soloBossGateFirstCaveRVA, len(soloBossFirstHookOriginal))
	displacement := int32(binary.LittleEndian.Uint32(jump[1:5]))
	got := uint32(int64(soloBossGateFirstHookRVA+5) + int64(displacement))
	if got != soloBossGateFirstCaveRVA {
		t.Fatalf("relative jump target = 0x%X, want 0x%X", got, soloBossGateFirstCaveRVA)
	}
}

func TestSoloBossDescriptionCaveOnlyExemptsLocalCards(t *testing.T) {
	cave := buildSoloBossDescriptionCave()
	for _, itemID := range []uint16{game.SinglePlayerBossCardItemID, game.CompetitiveAICardItemID} {
		comparison := []byte{0x81, 0xFB, byte(itemID & 0xFF), byte(itemID >> 8), 0x00, 0x00}
		if !bytes.Contains(cave, comparison) {
			t.Fatalf("description cave does not compare item %d: %X", itemID, cave)
		}
	}
	if len(cave) >= int(soloBossGateTextEndRVA-soloBossDescriptionCaveRVA) {
		t.Fatalf("description cave length %d exceeds reserved text tail", len(cave))
	}
}

func TestEnsureNativeControlCardDescriptions(t *testing.T) {
	root := t.TempDir()
	config := filepath.Join(root, "config")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatal(err)
	}
	source := "<?xml version=\"1.0\" encoding=\"gb2312\"?>\r\n<propdescription>\r\n\t<材料>\r\n\t\t<mm1 id=\"30001\" name=\"透明的瓶子\"></mm1>\r\n\t</材料>\r\n</propdescription>\r\n"
	encoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(source))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(config, "propdescrip.xml")
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureNativeControlCardDescriptions(root, false); err != nil {
		t.Fatal(err)
	}
	if err := ensureNativeControlCardDescriptions(root, true); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), updated)
	if err != nil {
		t.Fatal(err)
	}
	text := string(decoded)
	for _, want := range []string{nativeBossDescriptionEntry, nativeAIDescriptionEntry} {
		if strings.Count(text, want) != 1 {
			t.Fatalf("native entry count = %d, want 1: %s", strings.Count(text, want), text)
		}
	}
	for _, description := range []string{
		"持有且未收藏时，满足BOSS挑战条件即可单人开始竞技BOSS战。",
		"持有且未收藏时，普通竞技开局会按房间规则自动补充AI对手。",
	} {
		gbk, _, err := transform.Bytes(simplifiedchinese.GBK.NewEncoder(), []byte(description))
		if err != nil {
			t.Fatal(err)
		}
		if len(gbk) > 99 {
			t.Fatalf("native description is %d bytes, exceeds GetStorage's 99-byte payload: %s", len(gbk), description)
		}
	}
}

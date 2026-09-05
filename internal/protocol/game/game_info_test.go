package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func TestGameInfoNetworkVector(t *testing.T) {
	info := GameInfo{
		WinNum: 1, LossNum: 2, EqualNum: 3, OrgID: 4, Point: 5, Money: 6,
		Degree: 7, RoleID: 8, PetID: 9,
		ExtWinNum: 10, ExtLossNum: 11, ExtEqualNum: 12, ExtPoint: 13,
	}
	want, _ := hex.DecodeString(
		"00000001000000020000000300000004000000050000000600070800000009" +
			"0000000a0000000b0000000c0000000d",
	)
	got := info.AppendNetworkBinary(nil)
	if len(got) != GameInfoNetworkBinarySize || !bytes.Equal(got, want) {
		t.Fatalf("GAME_INFO = %x, want %x", got, want)
	}
	decoded, err := ParseGameInfoNetwork(got)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != info {
		t.Fatalf("GAME_INFO round trip = %+v, want %+v", decoded, info)
	}
}

func TestParseGameInfoMemory(t *testing.T) {
	data := make([]byte, GameInfoNetworkBinarySize)
	binary.LittleEndian.PutUint32(data[GameInfoOffsetWinNum:GameInfoOffsetLossNum], 5200)
	binary.LittleEndian.PutUint32(data[GameInfoOffsetLossNum:GameInfoOffsetEqualNum], 521)
	binary.LittleEndian.PutUint32(data[GameInfoOffsetPoint:GameInfoOffsetMoney], 1_621_150_000)
	binary.LittleEndian.PutUint16(data[GameInfoOffsetDegree:GameInfoOffsetRoleID], 179)
	data[GameInfoOffsetRoleID] = 7
	binary.LittleEndian.PutUint32(data[GameInfoOffsetExtLossNum:GameInfoOffsetExtEqualNum], 72)

	info, err := ParseGameInfoMemory(data)
	if err != nil {
		t.Fatal(err)
	}
	if info.WinNum != 5200 || info.LossNum != 521 || info.Point != 1_621_150_000 || info.Degree != 179 || info.RoleID != 7 || info.ExtLossNum != 72 {
		t.Fatalf("unexpected memory GAME_INFO: %+v", info)
	}
}

func TestParseGameInfoRejectsShortData(t *testing.T) {
	if _, err := ParseGameInfoNetwork(make([]byte, GameInfoNetworkBinarySize-1)); err == nil {
		t.Fatal("expected short GAME_INFO data to fail")
	}
}

func TestGameInfoLocalProfileRules(t *testing.T) {
	info := GameInfo{Point: MaxPlayerExperience, Degree: MaxPlayerLevel}
	if err := info.ValidateLocalClientBounds(); err != nil {
		t.Fatal(err)
	}
	if info.HasCompletedGame() {
		t.Fatal("empty statistics unexpectedly mark a completed game")
	}
	marked := info.WithTutorialCompletionMarker()
	if marked.EqualNum != 1 || info.EqualNum != 0 {
		t.Fatalf("tutorial marker mutated input or used wrong value: input=%+v marked=%+v", info, marked)
	}
	existing := GameInfo{WinNum: 2, LossNum: 3, EqualNum: 4}.WithTutorialCompletionMarker()
	if existing.WinNum != 2 || existing.LossNum != 3 || existing.EqualNum != 4 {
		t.Fatalf("existing record changed: %+v", existing)
	}
	if err := (GameInfo{Degree: MaxPlayerLevel + 1}).ValidateLocalClientBounds(); err == nil {
		t.Fatal("expected above-maximum degree to fail")
	}
	if err := (GameInfo{Point: MaxPlayerExperience + 1}).ValidateLocalClientBounds(); err == nil {
		t.Fatal("expected above-maximum points to fail")
	}
	if err := (GameInfo{Money: MaxGameMoney + 1}).ValidateLocalClientBounds(); err == nil {
		t.Fatal("expected above-maximum money to fail")
	}
}

func TestGameMoneyRewardSaturatesAtClientMaximum(t *testing.T) {
	info, change := ApplyGameMoneyReward(GameInfo{Money: MaxGameMoney - 5}, 20)
	if info.Money != MaxGameMoney || change.Applied != 5 || !change.Capped {
		t.Fatalf("money reward = %+v/%+v", info, change)
	}
}

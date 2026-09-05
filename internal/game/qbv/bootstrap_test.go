package qbv

import (
	"testing"

	"qqtang/internal/protocol/game"
)

func TestParseBootstrapGameBeginAndGameOver(t *testing.T) {
	begin := game.GameBeginData{
		GameID: 5, MapID: 202, SpawnSeed: 0x11223344, ItemSeed: 0x55667788,
		ArbitratorPlayerID: 1,
		Players: []game.PlayerGameInfo{
			{PlayerID: 1, RoleID: 9, TeamID: 1},
			{PlayerID: 2, RoleID: 22, TeamID: 4},
		},
		ContinueID: -1, GameTimeMS: 600_000,
	}
	beginPayload, err := begin.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	over := game.GameOverData{
		Time: 30_000,
		Results: []game.GameResultData{
			{PlayerID: 1, Result: game.GameResultWin},
			{PlayerID: 2, Result: game.GameResultLoss},
		},
		GameMode: game.SettlementGameModeCompetitive,
	}
	overPayload, err := over.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	recording := Recording{
		Header: HeaderWords{PlayerCount: 2},
		Bootstrap: []BootstrapRecord{
			{Schema: game.NotifyGameBeginID, Data: beginPayload},
			{Schema: game.NotifyGameOverEvent, Data: overPayload},
		},
	}
	decodedBegin, err := ParseBootstrapGameBegin(recording)
	if err != nil {
		t.Fatal(err)
	}
	if decodedBegin.MapID != begin.MapID || decodedBegin.ItemSeed != begin.ItemSeed || len(decodedBegin.Players) != 2 {
		t.Fatalf("decoded GAME_BEGIN = %+v", decodedBegin)
	}
	decodedOver, err := ParseBootstrapGameOver(recording)
	if err != nil {
		t.Fatal(err)
	}
	if decodedOver.Time != over.Time || len(decodedOver.Results) != 2 || decodedOver.Results[0].Result != game.GameResultWin {
		t.Fatalf("decoded GAME_OVER = %+v", decodedOver)
	}
}

func TestParseBootstrapRejectsContainerPlayerCountMismatch(t *testing.T) {
	begin := game.GameBeginData{
		GameID: 1, MapID: 1, SpawnSeed: 1, ItemSeed: 2, ArbitratorPlayerID: 1,
		Players: []game.PlayerGameInfo{{PlayerID: 1, RoleID: 1, TeamID: 1}}, GameTimeMS: 240_000,
	}
	payload, err := begin.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	_, err = ParseBootstrapGameBegin(Recording{
		Header:    HeaderWords{PlayerCount: 2},
		Bootstrap: []BootstrapRecord{{Schema: game.NotifyGameBeginID, Data: payload}},
	})
	if err == nil {
		t.Fatal("player-count mismatch was accepted")
	}
}

func TestParseBootstrapAcceptsArchivedZeroGameTime(t *testing.T) {
	begin := game.GameBeginData{
		GameID: 1, MapID: 808, SpawnSeed: 1, ItemSeed: 2, ArbitratorPlayerID: 1,
		Players: []game.PlayerGameInfo{{PlayerID: 1, RoleID: 1, TeamID: 1}}, GameTimeMS: 240_000,
	}
	payload, err := begin.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	for index := len(payload) - 4; index < len(payload); index++ {
		payload[index] = 0
	}
	decoded, err := ParseBootstrapGameBegin(Recording{
		Header:    HeaderWords{PlayerCount: 1},
		Bootstrap: []BootstrapRecord{{Schema: game.NotifyGameBeginID, Data: payload}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if decoded.MapID != begin.MapID || decoded.GameTimeMS != 0 {
		t.Fatalf("decoded archived GAME_BEGIN = %+v", decoded)
	}
}

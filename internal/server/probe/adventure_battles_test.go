package probe

import (
	"testing"

	"qqtang/internal/game/match"
)

func TestAdventureBattleRegistrySharesStateByGameID(t *testing.T) {
	server := &Server{battles: make(map[uint32]*match.AdventureBattle)}
	participants := []match.AdventureParticipant{
		{PlayerID: 1, TeamID: 1},
		{PlayerID: 2, TeamID: 1},
	}
	if err := server.replaceAdventureBattle(9, 1601, 1, participants); err != nil {
		t.Fatal(err)
	}
	firstConnectionView, err := server.adventureBattle(9)
	if err != nil {
		t.Fatal(err)
	}
	secondConnectionView, err := server.adventureBattle(9)
	if err != nil {
		t.Fatal(err)
	}
	if firstConnectionView != secondConnectionView {
		t.Fatal("connections with the same GameID did not receive shared battle state")
	}
	first, err := firstConnectionView.RecordDeath(1)
	if err != nil {
		t.Fatal(err)
	}
	if first.Concluded {
		t.Fatal("first multiplayer death concluded the shared battle")
	}
	second, err := secondConnectionView.RecordDeath(2)
	if err != nil {
		t.Fatal(err)
	}
	if !second.NewlyConcluded {
		t.Fatal("last multiplayer death did not conclude the shared battle")
	}
}

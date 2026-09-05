package probe

import (
	"testing"

	"qqtang/internal/game/match"
)

func TestAdventureNextMapTargetAllowsDoorwayUnspecifiedValue(t *testing.T) {
	if !adventureNextMapTargetMatches(0, 1650, true) {
		t.Fatal("doorway target zero was rejected even though the active route has one next stage")
	}
	if !adventureNextMapTargetMatches(1650, 1650, true) {
		t.Fatal("explicit next map ID was rejected")
	}
	if adventureNextMapTargetMatches(1602, 1650, true) {
		t.Fatal("unrelated explicit next map ID was accepted")
	}
	if !adventureNextMapTargetMatches(0, 0, false) {
		t.Fatal("final doorway target zero was rejected")
	}
}

func TestAdventureFinalStageParticipantsIncludeDeadTeammates(t *testing.T) {
	transition := match.AdventureStageTransition{
		Survivors: []match.AdventureParticipant{{PlayerID: 2, TeamID: 1}},
		SettledOut: []match.AdventureSettlement{{
			Participant: match.AdventureParticipant{PlayerID: 1, TeamID: 1},
			Reason:      match.AdventureSettlementDiedBeforeNextStage,
		}},
	}
	participants := adventureFinalStageParticipants(transition)
	if len(participants) != 2 || participants[0].PlayerID != 1 || participants[1].PlayerID != 2 {
		t.Fatalf("final-stage participants = %+v, want both teammates ordered by player ID", participants)
	}
}

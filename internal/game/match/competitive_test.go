package match

import "testing"

func TestCompetitiveVirtualParticipantsNeverArbitrate(t *testing.T) {
	participants := []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 1, TeamID: 2, Source: CompetitiveParticipantVirtualAI},
		{PlayerID: 3, RoleID: 3, TeamID: 1},
		{PlayerID: 2, RoleID: 2, TeamID: 2, Source: CompetitiveParticipantVirtualAI},
		{PlayerID: 4, RoleID: 4, TeamID: 1},
	}
	selected, err := SelectCompetitiveArbitrator(1, participants)
	if err != nil || selected != 3 {
		t.Fatalf("selected arbitrator = %d, err=%v, want human 3", selected, err)
	}
	if _, err = NewCompetitiveBattle(1, 1, 1, participants); err == nil {
		t.Fatal("battle accepted a virtual arbitrator")
	}
	battle, err := NewCompetitiveBattle(1, 1, 3, participants)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := battle.RecordDeparture(3)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.NewlyConcluded || resolution.ArbitratorPlayerID != 4 || battle.ArbitratorPlayerID() != 4 {
		t.Fatalf("departure resolution = %+v, arbitrator=%d, want human 4", resolution, battle.ArbitratorPlayerID())
	}
}

func TestCompetitiveArbitratorSelectionRejectsAllVirtualParticipants(t *testing.T) {
	_, err := SelectCompetitiveArbitrator(1, []CompetitiveParticipant{
		{PlayerID: 1, Source: CompetitiveParticipantVirtualAI},
		{PlayerID: 2, Source: CompetitiveParticipantVirtualAI},
	})
	if err == nil {
		t.Fatal("selected an arbitrator from all-virtual participants")
	}
}

func TestCompetitiveLastHumanDepartureNeverPromotesVirtualAI(t *testing.T) {
	battle, err := NewCompetitiveBattle(2, 1, 3, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 1, TeamID: 1, Source: CompetitiveParticipantVirtualAI},
		{PlayerID: 2, RoleID: 2, TeamID: 2, Source: CompetitiveParticipantVirtualAI},
		{PlayerID: 3, RoleID: 3, TeamID: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := battle.RecordDeparture(3)
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.NewlyConcluded || !resolution.Results[0].Draw || battle.ArbitratorPlayerID() != 0 {
		t.Fatalf("last-human departure = %+v, arbitrator=%d", resolution, battle.ArbitratorPlayerID())
	}
}

func TestCompetitiveBattleConcludesWhenOneTeamRemains(t *testing.T) {
	battle, err := NewCompetitiveBattle(9, 1, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
		{PlayerID: 3, RoleID: 9, TeamID: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := battle.RecordKill(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.NewlyConcluded || resolution.WinnerTeamID != 2 || len(resolution.Results) != 3 {
		t.Fatalf("resolution = %+v", resolution)
	}
	if resolution.Results[1].KillCount != 1 || !resolution.Results[1].Won || !resolution.Results[2].Won || resolution.Results[0].Won {
		t.Fatalf("results = %+v", resolution.Results)
	}
	duplicate, err := battle.RecordDeath(1)
	if err != nil || duplicate.NewlyConcluded {
		t.Fatalf("duplicate death = %+v, %v", duplicate, err)
	}
}

func TestCompetitiveBattleWaitsWhileTwoTeamsRemain(t *testing.T) {
	battle, err := NewCompetitiveBattle(10, 1, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 1},
		{PlayerID: 3, RoleID: 9, TeamID: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := battle.RecordDeath(1)
	if err != nil || resolution.NewlyConcluded {
		t.Fatalf("first death = %+v, %v", resolution, err)
	}
	resolution, err = battle.RecordDeath(3)
	if err != nil || !resolution.NewlyConcluded || resolution.WinnerTeamID != 1 {
		t.Fatalf("second death = %+v, %v", resolution, err)
	}
	if !resolution.Results[0].Won || !resolution.Results[1].Won || resolution.Results[2].Won {
		t.Fatalf("team outcomes = %+v, want both team-1 players to win even though player 1 died", resolution.Results)
	}
}

func TestCompetitiveBattleTimeoutConcludesOpposedTeamsAsDraw(t *testing.T) {
	battle, err := NewCompetitiveBattle(19, 1, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 1, TeamID: 1},
		{PlayerID: 2, RoleID: 2, TeamID: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := battle.ConcludeDraw()
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.NewlyConcluded || resolution.WinnerTeamID != 0 || len(resolution.Results) != 2 {
		t.Fatalf("timeout resolution = %+v", resolution)
	}
	for _, player := range resolution.Results {
		if !player.Draw || player.Won {
			t.Fatalf("timeout player = %+v", player)
		}
	}
	again, err := battle.ConcludeDraw()
	if err != nil || again.NewlyConcluded {
		t.Fatalf("repeated timeout = %+v, %v", again, err)
	}
	for _, player := range again.Results {
		if !player.Draw || player.Won {
			t.Fatalf("repeated timeout player = %+v", player)
		}
	}
}

func TestCompetitiveBattleTracksModeObjectiveWithoutGuessingSettlementField(t *testing.T) {
	battle, err := NewCompetitiveBattle(4, 801, 1, []CompetitiveParticipant{{PlayerID: 1, RoleID: 1, TeamID: 1}, {PlayerID: 2, RoleID: 2, TeamID: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if err = battle.RecordObjective(1); err != nil {
		t.Fatal(err)
	}
	resolution, err := battle.RecordDeath(2)
	if err != nil || len(resolution.Results) != 2 || resolution.Results[0].ObjectiveCount != 1 {
		t.Fatalf("objective resolution = %+v/%v", resolution, err)
	}
}

func TestCompetitiveBunConservesColourAndConcludesOnCompleteCapture(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(1, 32, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerTimedRespawn,
		Objective:        CompetitiveObjectiveBun,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolution, fresh, pickupErr := battle.RecordBunAction(true, 1, 100, 40, 80, 0, 2); pickupErr != nil || !fresh || resolution.NewlyConcluded {
		t.Fatalf("enemy base pickup = %+v fresh=%t err=%v", resolution, fresh, pickupErr)
	}
	resolution, fresh, err := battle.RecordBunAction(false, 1, 110, 80, 40, 0, 2)
	if err != nil || !fresh || !resolution.NewlyConcluded || resolution.WinnerTeamID != 1 {
		t.Fatalf("complete bun capture = %+v fresh=%t err=%v", resolution, fresh, err)
	}
	if len(resolution.Results) != 2 || resolution.Results[0].ObjectiveCount != 1 || !resolution.Results[0].Won || resolution.Results[1].Won {
		t.Fatalf("complete bun results = %+v", resolution.Results)
	}
	duplicate, fresh, err := battle.RecordBunAction(false, 1, 110, 80, 40, 0, 2)
	if err != nil || fresh || duplicate.NewlyConcluded {
		t.Fatalf("duplicate bun notify = %+v fresh=%t err=%v", duplicate, fresh, err)
	}
}

func TestCompetitiveBunDeathDropsAndEitherTeamCanRecover(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(302, 1301, 1,
		[]CompetitiveParticipant{
			{PlayerID: 1, RoleID: 7, TeamID: 1},
			{PlayerID: 2, RoleID: 8, TeamID: 2},
		},
		CompetitiveRuleConfig{
			ConclusionPolicy: CompetitiveConclusionClientRule,
			PlayerLifecycle:  CompetitivePlayerTimedRespawn,
			Objective:        CompetitiveObjectiveBun,
		})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = battle.RecordBunAction(true, 1, 100, 40, 80, 0, 2); err != nil {
		t.Fatalf("steal team-2 bun: %v", err)
	}
	if _, err = battle.RecordDeath(1); err != nil {
		t.Fatalf("drop carried bun on death: %v", err)
	}
	if err = battle.RecordRespawn(1); err != nil {
		t.Fatalf("respawn carrier: %v", err)
	}
	if _, _, err = battle.RecordBunAction(true, 2, 110, 60, 80, 1, 2); err != nil {
		t.Fatalf("owner recovers loose bun: %v", err)
	}
	resolution, fresh, err := battle.RecordBunAction(false, 2, 120, 80, 40, 0, 2)
	if err != nil || !fresh || resolution.NewlyConcluded {
		t.Fatalf("owner restores bun = %+v fresh=%t err=%v", resolution, fresh, err)
	}
	if got := battle.resolution(false).Results[1].ObjectiveCount; got != 0 {
		t.Fatalf("returning own bun objective count = %d, want 0", got)
	}
	if _, _, err = battle.RecordBunAction(true, 1, 130, 40, 80, 0, 2); err != nil {
		t.Fatalf("steal restored bun again: %v", err)
	}
	if _, err = battle.RecordDeath(1); err != nil {
		t.Fatalf("drop carried bun again: %v", err)
	}
	if _, _, err = battle.RecordBunAction(true, 2, 140, 60, 80, 1, 2); err != nil {
		t.Fatalf("recover loose bun a second time: %v", err)
	}
	if _, fresh, pickupErr := battle.RecordBunAction(true, 1, 141, 60, 80, 1, 2); pickupErr != nil || !fresh {
		t.Fatalf("authoritative loose-bun pickup did not reconcile stale mirror: fresh=%t err=%v", fresh, pickupErr)
	}
}

func TestCompetitiveBunArbitratorPickupReconcilesManualDrop(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(303, 1301, 1,
		[]CompetitiveParticipant{
			{PlayerID: 1, RoleID: 7, TeamID: 1},
			{PlayerID: 2, RoleID: 8, TeamID: 2},
		},
		CompetitiveRuleConfig{
			ConclusionPolicy: CompetitiveConclusionClientRule,
			PlayerLifecycle:  CompetitivePlayerTimedRespawn,
			Objective:        CompetitiveObjectiveBun,
		})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = battle.RecordBunAction(true, 1, 100, 40, 80, 0, 2); err != nil {
		t.Fatalf("steal initial team-2 bun: %v", err)
	}
	// There is no independent network event for the local manual drop. A
	// subsequent arbitrator-approved loose pickup proves the previous carry
	// was returned to the scene and must reconcile the service mirror.
	if _, fresh, pickupErr := battle.RecordBunAction(true, 1, 110, 60, 80, 1, 2); pickupErr != nil || !fresh {
		t.Fatalf("re-pick manually dropped bun fresh=%t err=%v", fresh, pickupErr)
	}
	resolution, fresh, err := battle.RecordBunAction(false, 1, 120, 80, 40, 0, 2)
	if err != nil || !fresh || !resolution.NewlyConcluded || resolution.WinnerTeamID != 1 {
		t.Fatalf("deposit re-picked bun = %+v fresh=%t err=%v", resolution, fresh, err)
	}
}

func TestCompetitiveBunTimeoutUsesHouseTotalsAndDepartureIsPermanent(t *testing.T) {
	participants := []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1}, {PlayerID: 2, RoleID: 8, TeamID: 1},
		{PlayerID: 3, RoleID: 9, TeamID: 2}, {PlayerID: 4, RoleID: 10, TeamID: 2},
	}
	newBattle := func() *CompetitiveBattle {
		battle, newErr := NewCompetitiveBattleWithRuleConfig(1, 32, 1, participants, CompetitiveRuleConfig{
			ConclusionPolicy: CompetitiveConclusionClientRule,
			PlayerLifecycle:  CompetitivePlayerTimedRespawn,
			Objective:        CompetitiveObjectiveBun,
		})
		if newErr != nil {
			t.Fatal(newErr)
		}
		return battle
	}
	battle := newBattle()
	if _, _, err := battle.RecordBunAction(true, 1, 100, 40, 80, 0, 2); err != nil {
		t.Fatal(err)
	}
	if _, _, err := battle.RecordBunAction(false, 1, 110, 80, 40, 0, 2); err != nil {
		t.Fatal(err)
	}
	resolution, err := battle.ConcludeTimeout()
	if err != nil || !resolution.NewlyConcluded || resolution.WinnerTeamID != 1 {
		t.Fatalf("bun timeout after partial capture = %+v err=%v", resolution, err)
	}

	draw, err := newBattle().ConcludeTimeout()
	if err != nil || !draw.NewlyConcluded || draw.WinnerTeamID != 0 || !draw.Results[0].Draw {
		t.Fatalf("equal bun timeout = %+v err=%v", draw, err)
	}

	departureBattle := newBattle()
	if resolution, err = departureBattle.RecordDeparture(3); err != nil || resolution.NewlyConcluded {
		t.Fatalf("first teammate departure = %+v err=%v", resolution, err)
	}
	resolution, err = departureBattle.RecordDeparture(4)
	if err != nil || !resolution.NewlyConcluded || resolution.WinnerTeamID != 1 {
		t.Fatalf("last opposing departure = %+v err=%v", resolution, err)
	}
}

func TestCompetitiveDepartureTransfersArbitratorWhileRoundContinues(t *testing.T) {
	battle, err := NewCompetitiveBattle(9, 1, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 1},
		{PlayerID: 3, RoleID: 9, TeamID: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := battle.RecordDeparture(1)
	if err != nil || resolution.NewlyConcluded || resolution.ArbitratorPlayerID != 2 || battle.ArbitratorPlayerID() != 2 {
		t.Fatalf("continued departure = %+v arbitrator=%d err=%v", resolution, battle.ArbitratorPlayerID(), err)
	}
	resolution, err = battle.RecordDeparture(3)
	if err != nil || !resolution.NewlyConcluded || resolution.WinnerTeamID != 1 || resolution.ArbitratorPlayerID != 0 || battle.ArbitratorPlayerID() != 0 {
		t.Fatalf("concluding departure = %+v arbitrator=%d err=%v", resolution, battle.ArbitratorPlayerID(), err)
	}
}

func TestCompetitiveBossDepartureConcludesWhenOnlyDeadTeammateRemains(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(10, 12, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 1},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerPermanentElimination,
		TeamTopology:     CompetitiveTeamsCooperative,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := battle.RecordDeath(2)
	if err != nil || resolution.NewlyConcluded {
		t.Fatalf("first cooperative death = %+v err=%v", resolution, err)
	}
	resolution, err = battle.RecordDeparture(1)
	if err != nil || !resolution.NewlyConcluded || !resolution.AllPlayersLost || resolution.WinnerTeamID != 0 {
		t.Fatalf("last living cooperative departure = %+v err=%v", resolution, err)
	}
	for _, result := range resolution.Results {
		if result.Won || result.Draw {
			t.Fatalf("cooperative loss result = %+v", result)
		}
	}
}

func TestCompetitiveSculptureRequiresFourValidatedDeposits(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(2, 1201, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerTimedRespawn,
		Objective:        CompetitiveObjectiveSculpture,
	})
	if err != nil {
		t.Fatal(err)
	}
	for piece, material := range []byte{12, 13, 14, 15} {
		clientTime := uint32(200 + piece*2)
		if resolution, fresh, pickupErr := battle.RecordSculptureAction(true, 1, clientTime, 40, 80, 2, material); pickupErr != nil || !fresh || resolution.NewlyConcluded {
			t.Fatalf("piece %d pickup = %+v fresh=%t err=%v", piece+1, resolution, fresh, pickupErr)
		}
		resolution, fresh, depositErr := battle.RecordSculptureAction(false, 1, clientTime+1, 80, 40, 0, material)
		if depositErr != nil || !fresh {
			t.Fatalf("piece %d deposit = %+v fresh=%t err=%v", piece+1, resolution, fresh, depositErr)
		}
		if (piece < 3) == resolution.NewlyConcluded {
			t.Fatalf("piece %d conclusion = %+v", piece+1, resolution)
		}
		if piece == 3 {
			if resolution.WinnerTeamID != 1 || resolution.Results[0].ObjectiveCount != 4 || !resolution.Results[0].Won {
				t.Fatalf("completed sculpture resolution = %+v", resolution)
			}
			duplicate, duplicateFresh, duplicateErr := battle.RecordSculptureAction(false, 1, clientTime+1, 80, 40, 0, material)
			if duplicateErr != nil || duplicateFresh || duplicate.NewlyConcluded {
				t.Fatalf("duplicate final deposit = %+v fresh=%t err=%v", duplicate, duplicateFresh, duplicateErr)
			}
		}
	}
}

func TestCompetitiveSculptureTimeoutUsesProgressAndDeathDropsCarry(t *testing.T) {
	newBattle := func() *CompetitiveBattle {
		battle, err := NewCompetitiveBattleWithRuleConfig(3, 1201, 1, []CompetitiveParticipant{
			{PlayerID: 1, RoleID: 7, TeamID: 1}, {PlayerID: 2, RoleID: 8, TeamID: 2},
		}, CompetitiveRuleConfig{
			ConclusionPolicy: CompetitiveConclusionClientRule,
			PlayerLifecycle:  CompetitivePlayerTimedRespawn,
			Objective:        CompetitiveObjectiveSculpture,
		})
		if err != nil {
			t.Fatal(err)
		}
		return battle
	}
	battle := newBattle()
	if _, _, err := battle.RecordSculptureAction(true, 1, 300, 1, 2, 2, 12); err != nil {
		t.Fatal(err)
	}
	if _, err := battle.RecordDeath(1); err != nil {
		t.Fatal(err)
	}
	if _, fresh, depositErr := battle.RecordSculptureAction(false, 1, 301, 2, 1, 0, 12); depositErr != nil || !fresh {
		t.Fatalf("authoritative sculpture deposit did not reconcile missing fast pickup: fresh=%t err=%v", fresh, depositErr)
	}
	if err := battle.RecordRespawn(1); err != nil {
		t.Fatal(err)
	}
	if _, _, err := battle.RecordSculptureAction(true, 1, 302, 1, 2, 2, 12); err != nil {
		t.Fatal(err)
	}
	if _, _, err := battle.RecordSculptureAction(false, 1, 303, 2, 1, 0, 12); err != nil {
		t.Fatal(err)
	}
	resolution, err := battle.ConcludeTimeout()
	if err != nil || !resolution.NewlyConcluded || resolution.WinnerTeamID != 1 {
		t.Fatalf("sculpture progress timeout = %+v err=%v", resolution, err)
	}
	draw, err := newBattle().ConcludeTimeout()
	if err != nil || !draw.NewlyConcluded || draw.WinnerTeamID != 0 || !draw.Results[0].Draw {
		t.Fatalf("equal sculpture timeout = %+v err=%v", draw, err)
	}
}

func TestCompetitiveSculptureArbitratorNotificationReconcilesStaleCarry(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(3, 1201, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1}, {PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerTimedRespawn,
		Objective:        CompetitiveObjectiveSculpture,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, fresh, pickupErr := battle.RecordSculptureAction(true, 1, 100, 40, 80, 2, 15); pickupErr != nil || !fresh {
		t.Fatalf("initial sculpture pickup fresh=%t err=%v", fresh, pickupErr)
	}
	// Native rule 6 permits manually dropping a fragment and later picking
	// another one. Its arbitrator notification is the authoritative single
	// slot observation even if the server did not observe that local boundary.
	if _, fresh, pickupErr := battle.RecordSculptureAction(true, 1, 101, 80, 80, 2, 13); pickupErr != nil || !fresh {
		t.Fatalf("reconciled sculpture pickup fresh=%t err=%v", fresh, pickupErr)
	}
	resolution, fresh, depositErr := battle.RecordSculptureAction(false, 1, 102, 80, 40, 0, 13)
	if depositErr != nil || !fresh || resolution.Results[0].ObjectiveCount != 1 {
		t.Fatalf("reconciled sculpture deposit = %+v fresh=%t err=%v", resolution, fresh, depositErr)
	}

	if _, fresh, pickupErr := battle.RecordSculptureAction(true, 1, 103, 40, 80, 2, 15); pickupErr != nil || !fresh {
		t.Fatalf("second sculpture pickup fresh=%t err=%v", fresh, pickupErr)
	}
	resolution, fresh, depositErr = battle.RecordSculptureAction(false, 1, 104, 80, 40, 0, 14)
	if depositErr != nil || !fresh || resolution.Results[0].ObjectiveCount != 2 {
		t.Fatalf("arbitrator material correction = %+v fresh=%t err=%v", resolution, fresh, depositErr)
	}
}

func TestCompetitiveBattleIdentifiesOnlyElectedArbitrator(t *testing.T) {
	battle, err := NewCompetitiveBattleWithPolicy(7, 11, 2, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveConclusionClientRule)
	if err != nil {
		t.Fatal(err)
	}
	if battle.IsArbitrator(1) || !battle.IsArbitrator(2) || battle.IsArbitrator(3) {
		t.Fatal("competitive battle did not preserve its elected arbitrator")
	}
	if !battle.AuthorizesParticipantObservation(1, 1) ||
		!battle.AuthorizesParticipantObservation(2, 1) ||
		battle.AuthorizesParticipantObservation(1, 2) ||
		battle.AuthorizesParticipantObservation(2, 3) {
		t.Fatal("competitive participant-observation capability is too narrow or too broad")
	}
}

func TestCompetitiveBattleRequiresOpposingTeams(t *testing.T) {
	_, err := NewCompetitiveBattle(10, 1, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 1},
	})
	if err == nil {
		t.Fatal("expected one-team battle to be rejected")
	}
}

func TestCompetitiveBattleAllowsOnePlayerOnlyForCooperativeBoss(t *testing.T) {
	participant := []CompetitiveParticipant{{PlayerID: 1, RoleID: 7, TeamID: 1}}
	if _, err := NewCompetitiveBattle(20, 11, 1, participant); err == nil {
		t.Fatal("ordinary one-player competitive battle was accepted")
	}
	if _, err := NewCompetitiveBattleWithRuleConfig(20, 11, 1, participant, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerPermanentElimination,
		Objective:        CompetitiveObjectiveBoss,
		TeamTopology:     CompetitiveTeamsCooperative,
		BossEntityIDs:    []uint16{20001},
		BossID:           "sailor",
	}); err != nil {
		t.Fatalf("cooperative one-player Boss battle: %v", err)
	}
}

func TestCompetitiveBattleEnforcesTeamInteractionRules(t *testing.T) {
	battle, err := NewCompetitiveBattle(11, 1, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 1},
		{PlayerID: 3, RoleID: 9, TeamID: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = battle.RecordTrapped(2); err != nil {
		t.Fatal(err)
	}
	if _, err = battle.RecordKill(1, 2); err == nil {
		t.Fatal("same-colour player was allowed to kill teammate")
	}
	if err = battle.RecordRescue(3, 2); err == nil {
		t.Fatal("different-colour player was allowed to rescue opponent")
	}
	if err = battle.RecordRescue(1, 2); err != nil {
		t.Fatalf("same-colour rescue: %v", err)
	}
	if err = battle.RecordTrapped(2); err != nil {
		t.Fatal(err)
	}
	resolution, err := battle.RecordKill(3, 2)
	if err != nil {
		t.Fatalf("different-colour kill: %v", err)
	}
	// Repeated collision messages must not inflate the kill count.
	if _, err = battle.RecordKill(3, 2); err != nil {
		t.Fatalf("duplicate different-colour kill: %v", err)
	}
	if err != nil || resolution.Results[2].KillCount != 1 || resolution.Results[0].RescueCount != 1 {
		t.Fatalf("interaction resolution = %+v, %v", resolution, err)
	}
}

func TestSpecialCompetitiveBattleWaitsForClientRuleGameOver(t *testing.T) {
	battle, err := NewCompetitiveBattleWithPolicy(12, 701, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveConclusionClientRule)
	if err != nil {
		t.Fatal(err)
	}
	resolution, err := battle.RecordKill(1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.NewlyConcluded {
		t.Fatalf("special rule was incorrectly concluded by ordinary elimination: %+v", resolution)
	}
	if len(resolution.Results) != 2 || resolution.Results[0].KillCount != 1 {
		t.Fatalf("special rule counters were not retained: %+v", resolution)
	}
}

func TestTimedRespawnBattleKeepsPlayerActiveAndDeduplicatesOneDefeat(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRules(13, 1101, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveConclusionClientRule, CompetitivePlayerTimedRespawn)
	if err != nil {
		t.Fatal(err)
	}
	if err = battle.RecordTrapped(2); err != nil {
		t.Fatal(err)
	}
	first, err := battle.RecordKill(1, 2)
	if err != nil || first.NewlyConcluded || first.Results[0].KillCount != 1 {
		t.Fatalf("first timed-respawn defeat = %+v, %v", first, err)
	}
	duplicate, err := battle.RecordKill(1, 2)
	if err != nil || duplicate.Results[0].KillCount != 1 {
		t.Fatalf("duplicate timed-respawn defeat = %+v, %v", duplicate, err)
	}
	if err = battle.RecordRespawn(2); err != nil {
		t.Fatal(err)
	}
	// The native client's next trapping event opens a new defeat after the
	// explicit respawn boundary.
	if err = battle.RecordTrapped(2); err != nil {
		t.Fatal(err)
	}
	second, err := battle.RecordKill(1, 2)
	if err != nil || second.Results[0].KillCount != 2 {
		t.Fatalf("second timed-respawn defeat = %+v, %v", second, err)
	}
	death, err := battle.RecordDeath(2)
	if err != nil || death.NewlyConcluded {
		t.Fatalf("timed-respawn death concluded match = %+v, %v", death, err)
	}
}

func TestPermanentEliminationBattleRejectsRespawn(t *testing.T) {
	battle, err := NewCompetitiveBattle(14, 1, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1}, {PlayerID: 2, RoleID: 8, TeamID: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = battle.RecordRespawn(2); err == nil {
		t.Fatal("ordinary permanent-elimination battle accepted respawn")
	}
}

func TestNativeDurabilityBattleRejectsTimedRespawnBoundary(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRules(14, 1101, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveConclusionClientRule, CompetitivePlayerNativeDurability)
	if err != nil {
		t.Fatal(err)
	}
	if err = battle.RecordRespawn(2); err == nil {
		t.Fatal("native-durability battle accepted timed-respawn notification")
	}
}

func TestNativeDurabilityCountsDeathOnlyProducerUntilFourthLayer(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(15, 1301, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerNativeDurability,
		NativeHitLimit:   4,
	})
	if err != nil {
		t.Fatal(err)
	}
	for hit := 1; hit <= 4; hit++ {
		resolution, authoritative, deathErr := battle.RecordNativeDurabilityDeath(2, uint32(hit*100), 40, 80)
		if deathErr != nil {
			t.Fatal(deathErr)
		}
		if hit < 4 && (authoritative || resolution.NewlyConcluded) {
			t.Fatalf("early native death %d = %+v authoritative:%t", hit, resolution, authoritative)
		}
		if hit == 4 && (!authoritative || !resolution.NewlyConcluded || resolution.WinnerTeamID != 1) {
			t.Fatalf("final native death = %+v authoritative:%t", resolution, authoritative)
		}
	}
	if resolution, authoritative, duplicateErr := battle.RecordNativeDurabilityDeath(2, 400, 40, 80); duplicateErr != nil || authoritative || resolution.NewlyConcluded {
		t.Fatalf("duplicate native death = %+v authoritative:%t err:%v", resolution, authoritative, duplicateErr)
	}
}

func TestNativeDurabilityRepeatedHitsFromSameSourceConsumeEveryLayer(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(17, 1402, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerNativeDurability,
		NativeHitLimit:   4,
	})
	if err != nil {
		t.Fatal(err)
	}
	for hit := 1; hit <= 4; hit++ {
		clientTime := uint32(hit * 100)
		remaining, fresh, harmErr := battle.RecordNativeHarm(2, clientTime, 40, 80, false, 0)
		if harmErr != nil || !fresh || remaining != byte(4-hit) {
			t.Fatalf("same-source harm %d = remaining:%d fresh:%t err:%v", hit, remaining, fresh, harmErr)
		}
		resolution, authoritative, deathErr := battle.RecordNativeDurabilityDeath(2, clientTime+1, 40, 80)
		if deathErr != nil {
			t.Fatal(deathErr)
		}
		if hit < 4 && (authoritative || resolution.NewlyConcluded) {
			t.Fatalf("same-source hit %d concluded early = %+v authoritative:%t", hit, resolution, authoritative)
		}
		if hit == 4 && (!authoritative || !resolution.NewlyConcluded || resolution.WinnerTeamID != 1) {
			t.Fatalf("same-source final hit = %+v authoritative:%t", resolution, authoritative)
		}
	}
}

func TestNativeDurabilityAvatarHitDoesNotConsumeUnderlyingLife(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRules(16, 1301, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1}, {PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveConclusionClientRule, CompetitivePlayerNativeDurability)
	if err != nil {
		t.Fatal(err)
	}
	remaining, fresh, err := battle.RecordNativeHarm(2, 100, 40, 80, true, 0)
	if err != nil || !fresh || remaining != 4 {
		t.Fatalf("avatar native harm = remaining:%d fresh:%t err:%v", remaining, fresh, err)
	}
	if resolution, authoritative, deathErr := battle.RecordNativeDurabilityDeath(2, 101, 40, 80); deathErr != nil || authoritative || resolution.NewlyConcluded {
		t.Fatalf("avatar hit opened death = %+v authoritative:%t err:%v", resolution, authoritative, deathErr)
	}
}

func TestNativeDurabilityDeathReporterAllowsSelfAndArbitratorOnly(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRules(18, 1301, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
		{PlayerID: 3, RoleID: 9, TeamID: 1},
	}, CompetitiveConclusionClientRule, CompetitivePlayerNativeDurability)
	if err != nil {
		t.Fatal(err)
	}
	if !battle.AuthorizesNativeDurabilityDeathReporter(1, 2) {
		t.Fatal("arbitrator could not report participant death")
	}
	if !battle.AuthorizesNativeDurabilityDeathReporter(2, 2) {
		t.Fatal("participant could not report its own death")
	}
	if battle.AuthorizesNativeDurabilityDeathReporter(3, 2) {
		t.Fatal("unrelated participant could report another participant death")
	}
	if battle.AuthorizesNativeDurabilityDeathReporter(2, 99) {
		t.Fatal("participant could report an unknown target")
	}
}

func TestNativeRuleNPCRegistryIsSeparateFromParticipantsAndBossObjective(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(18, 1401, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerNativeDurability,
		NativeHitLimit:   4,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = battle.RegisterNativeNPCEntities([]uint16{20_001}); err != nil {
		t.Fatal(err)
	}
	if !battle.HasNativeNPCEntity(20_001) || battle.HasParticipant(20_001) || battle.HasBossEntity(20_001) {
		t.Fatal("dynamic rule NPC crossed participant/Boss objective identity boundaries")
	}
	if !battle.AuthorizesNativeNPCProxy(1, 20_001) || battle.AuthorizesNativeNPCProxy(2, 20_001) {
		t.Fatal("dynamic rule NPC proxy was not restricted to the elected arbitrator")
	}
	fresh, err := battle.RecordNativeNPCDeath(20_001)
	if err != nil || !fresh || battle.IsConcluded() {
		t.Fatalf("native rule NPC death fresh=%t concluded=%t err=%v", fresh, battle.IsConcluded(), err)
	}
	if fresh, err = battle.RecordNativeNPCDeath(20_001); err != nil || fresh {
		t.Fatalf("duplicate native rule NPC death fresh=%t err=%v", fresh, err)
	}
	if battle.AuthorizesNativeNPCProxy(1, 20_001) {
		t.Fatal("dead dynamic rule NPC remained authorized as a live proxy")
	}
}

func TestCooperativeClientRuleConcludesFailureWhenEveryPlayerDies(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(17, 11, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 1},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerPermanentElimination,
		TeamTopology:     CompetitiveTeamsCooperative,
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := battle.RecordDeath(1)
	if err != nil || first.NewlyConcluded {
		t.Fatalf("first cooperative death = %+v err:%v", first, err)
	}
	last, err := battle.RecordDeath(2)
	if err != nil {
		t.Fatal(err)
	}
	if !last.NewlyConcluded || !last.AllPlayersLost || last.WinnerTeamID != 0 {
		t.Fatalf("last cooperative death = %+v", last)
	}
	for _, player := range last.Results {
		if player.Won || player.Draw {
			t.Fatalf("cooperative failure marked player as win/draw: %+v", player)
		}
	}
}

func TestCooperativeBossObjectiveConcludesSuccessOnRegisteredBossDeath(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(18, 11, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 1},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerPermanentElimination,
		Objective:        CompetitiveObjectiveBoss,
		TeamTopology:     CompetitiveTeamsCooperative,
		BossEntityIDs:    []uint16{30001},
		BossID:           "sailor",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !battle.HasBossEntity(30001) || battle.HasBossEntity(30002) {
		t.Fatal("competitive Boss entity registry is incorrect")
	}
	if _, err = battle.RecordBossDeath(30002); err == nil {
		t.Fatal("unregistered competitive Boss death was accepted")
	}
	resolution, err := battle.RecordBossDeath(30001)
	if err != nil {
		t.Fatal(err)
	}
	if !resolution.NewlyConcluded || resolution.WinnerTeamID != 1 || resolution.AllPlayersLost {
		t.Fatalf("Boss success resolution = %+v", resolution)
	}
	for _, player := range resolution.Results {
		if !player.Won || player.Draw {
			t.Fatalf("Boss success player result = %+v", player)
		}
	}
	if duplicate, duplicateErr := battle.RecordBossDeath(30001); duplicateErr != nil || duplicate.NewlyConcluded {
		t.Fatalf("duplicate Boss death = %+v err:%v", duplicate, duplicateErr)
	}
	wake, active := battle.BossVictoryGraceSignal()
	if !active {
		t.Fatal("Boss victory did not open its conclusion grace")
	}
	if handled, allDead, deathErr := battle.RecordBossVictoryGraceDeath(2); deathErr != nil || !handled || allDead {
		t.Fatalf("first grace death = handled:%t allDead:%t err:%v", handled, allDead, deathErr)
	}
	select {
	case <-wake:
		t.Fatal("Boss victory grace ended before every player died")
	default:
	}
	if handled, allDead, deathErr := battle.RecordBossVictoryGraceDeath(1); deathErr != nil || !handled || !allDead {
		t.Fatalf("last grace death = handled:%t allDead:%t err:%v", handled, allDead, deathErr)
	}
	select {
	case <-wake:
	default:
		t.Fatal("all players dead did not end Boss victory grace")
	}
}

func TestCooperativeBossKillsConcludeFailureWhenEveryPlayerIsEliminated(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(19, 702, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 1},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerPermanentElimination,
		Objective:        CompetitiveObjectiveBoss,
		TeamTopology:     CompetitiveTeamsCooperative,
		BossEntityIDs:    []uint16{30001},
		BossID:           "cristiano",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = battle.RecordTrapped(2); err != nil {
		t.Fatal(err)
	}
	first, err := battle.RecordBossKill(30001, 2)
	if err != nil || first.NewlyConcluded {
		t.Fatalf("first Boss kill = %+v err:%v", first, err)
	}
	if err = battle.RecordTrapped(1); err != nil {
		t.Fatal(err)
	}
	last, err := battle.RecordBossKill(30001, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !last.NewlyConcluded || !last.AllPlayersLost || last.WinnerTeamID != 0 {
		t.Fatalf("last Boss kill = %+v", last)
	}
	for _, player := range last.Results {
		if player.Won || player.Draw || player.KillCount != 0 {
			t.Fatalf("Boss failure player result = %+v", player)
		}
	}
}

func TestCompetitiveTimeoutDrawsOpposedRoundAndLosesCooperativeBossRound(t *testing.T) {
	opposed, err := NewCompetitiveBattle(20, 1, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 20_001, RoleID: 8, TeamID: 2, Source: CompetitiveParticipantVirtualAI},
	})
	if err != nil {
		t.Fatal(err)
	}
	draw, err := opposed.ConcludeTimeout()
	if err != nil || !draw.NewlyConcluded || draw.AllPlayersLost {
		t.Fatalf("opposed timeout = %+v err:%v", draw, err)
	}
	if len(draw.Results) != 2 || draw.Results[1].Participant.Source != CompetitiveParticipantVirtualAI {
		t.Fatalf("opposed human/virtual timeout results = %+v", draw.Results)
	}
	for _, player := range draw.Results {
		if !player.Draw || player.Won {
			t.Fatalf("opposed timeout player = %+v", player)
		}
	}

	cooperative, err := NewCompetitiveBattleWithRuleConfig(21, 702, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 1},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerPermanentElimination,
		Objective:        CompetitiveObjectiveBoss,
		TeamTopology:     CompetitiveTeamsCooperative,
		BossEntityIDs:    []uint16{30001},
		BossID:           "cristiano",
	})
	if err != nil {
		t.Fatal(err)
	}
	loss, err := cooperative.ConcludeTimeout()
	if err != nil || !loss.NewlyConcluded || !loss.AllPlayersLost || loss.WinnerTeamID != 0 {
		t.Fatalf("cooperative timeout = %+v err:%v", loss, err)
	}
	for _, player := range loss.Results {
		if player.Won || player.Draw {
			t.Fatalf("cooperative timeout player = %+v", player)
		}
	}
}

func TestCompetitiveBossDeathBeforeDeadlineWakesVictoryGraceWithoutChangingOutcome(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(22, 702, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 1},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerPermanentElimination,
		Objective:        CompetitiveObjectiveBoss,
		TeamTopology:     CompetitiveTeamsCooperative,
		BossEntityIDs:    []uint16{30001},
		BossID:           "cristiano",
	})
	if err != nil {
		t.Fatal(err)
	}
	victory, err := battle.RecordBossDeath(30001)
	if err != nil || !victory.NewlyConcluded || victory.AllPlayersLost || victory.WinnerTeamID != 1 {
		t.Fatalf("Boss death = %+v err:%v", victory, err)
	}
	wake, active := battle.BossVictoryGraceSignal()
	if !active {
		t.Fatal("Boss victory grace was not active after death")
	}
	select {
	case <-wake:
		t.Fatal("Boss victory grace woke before the absolute deadline")
	default:
	}

	atDeadline, err := battle.ConcludeTimeout()
	if err != nil || atDeadline.NewlyConcluded || atDeadline.AllPlayersLost || atDeadline.WinnerTeamID != 1 {
		t.Fatalf("Boss victory at absolute deadline = %+v err:%v", atDeadline, err)
	}
	for _, player := range atDeadline.Results {
		if !player.Won || player.Draw {
			t.Fatalf("Boss victory deadline player = %+v", player)
		}
	}
	select {
	case <-wake:
	default:
		t.Fatal("absolute deadline did not release pending Boss victory")
	}
}

func TestCompetitiveBossVictorySurvivesAllDeathsAndDeparturesDuringGrace(t *testing.T) {
	newBossBattle := func(gameID uint32) *CompetitiveBattle {
		battle, err := NewCompetitiveBattleWithRuleConfig(gameID, 702, 1, []CompetitiveParticipant{
			{PlayerID: 1, RoleID: 7, TeamID: 1},
			{PlayerID: 2, RoleID: 8, TeamID: 1},
		}, CompetitiveRuleConfig{
			ConclusionPolicy: CompetitiveConclusionClientRule,
			PlayerLifecycle:  CompetitivePlayerPermanentElimination,
			Objective:        CompetitiveObjectiveBoss,
			TeamTopology:     CompetitiveTeamsCooperative,
			BossEntityIDs:    []uint16{30001},
			BossID:           "cristiano",
		})
		if err != nil {
			t.Fatal(err)
		}
		if victory, deathErr := battle.RecordBossDeath(30001); deathErr != nil || !victory.NewlyConcluded {
			t.Fatalf("Boss death = %+v err:%v", victory, deathErr)
		}
		return battle
	}
	assertVictory := func(label string, battle *CompetitiveBattle) {
		t.Helper()
		resolution, concluded := battle.ConcludedResolution()
		if !concluded || resolution.WinnerTeamID != 1 || resolution.AllPlayersLost {
			t.Fatalf("%s resolution = %+v concluded:%t", label, resolution, concluded)
		}
		for _, player := range resolution.Results {
			if !player.Won || player.Draw {
				t.Fatalf("%s player = %+v", label, player)
			}
		}
	}

	deaths := newBossBattle(23)
	if handled, allDead, err := deaths.RecordBossVictoryGraceDeath(1); err != nil || !handled || allDead {
		t.Fatalf("first grace death = handled:%t all:%t err:%v", handled, allDead, err)
	}
	if handled, allDead, err := deaths.RecordBossVictoryGraceDeath(2); err != nil || !handled || !allDead {
		t.Fatalf("last grace death = handled:%t all:%t err:%v", handled, allDead, err)
	}
	assertVictory("all deaths", deaths)

	departures := newBossBattle(24)
	if handled, allGone, err := departures.RecordBossVictoryGraceDeparture(1); err != nil || !handled || allGone {
		t.Fatalf("first grace departure = handled:%t all:%t err:%v", handled, allGone, err)
	}
	if handled, allGone, err := departures.RecordBossVictoryGraceDeparture(2); err != nil || !handled || !allGone {
		t.Fatalf("last grace departure = handled:%t all:%t err:%v", handled, allGone, err)
	}
	assertVictory("all departures", departures)
}

func TestTreasureBattleTracksWeightedCarriedGemsAndReconcilesNativeScatter(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(15, 1101, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1}, {PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerTimedRespawn,
		Objective:        CompetitiveObjectiveTreasure,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index, itemID := range []uint32{TreasureGemValueOne, TreasureGemValueTwo, TreasureGemValueThree} {
		if _, _, err = battle.RecordTreasurePickup(2, uint32(index+1), itemID, uint16(index), uint16(index)); err != nil {
			t.Fatal(err)
		}
	}
	if _, recorded, duplicateErr := battle.RecordTreasurePickup(2, 1, TreasureGemValueOne, 0, 0); duplicateErr != nil || recorded {
		t.Fatalf("duplicate treasure pickup recorded=%v err=%v", recorded, duplicateErr)
	}
	if got := battle.resolution(false).Results[1].ObjectiveCount; got != 6 {
		t.Fatalf("weighted treasure value = %d, want 6", got)
	}
	// The native death notification is authoritative even when the server did
	// not observe every preceding fast-channel pickup.
	if err = battle.RecordTreasureScatter(2, []uint32{TreasureGemValueOne}); err != nil {
		t.Fatal(err)
	}
	if got := battle.resolution(false).Results[1].ObjectiveCount; got != 0 {
		t.Fatalf("treasure value after scatter = %d, want 0", got)
	}
}

func TestTimedScoreObjectivesUseTeamTotalsAndTreasureTarget(t *testing.T) {
	participants := []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1}, {PlayerID: 2, RoleID: 8, TeamID: 1},
		{PlayerID: 3, RoleID: 9, TeamID: 2}, {PlayerID: 4, RoleID: 10, TeamID: 2},
	}
	wrestle, err := NewCompetitiveBattleWithRuleConfig(21, 1001, 1, participants, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule, PlayerLifecycle: CompetitivePlayerTimedRespawn,
		Objective: CompetitiveObjectiveWrestle,
	})
	if err != nil {
		t.Fatal(err)
	}
	wrestle.kills[1], wrestle.kills[2], wrestle.kills[3] = 1, 2, 2
	resolution, err := wrestle.ConcludeTimeout()
	if err != nil || resolution.WinnerTeamID != 1 || !resolution.NewlyConcluded {
		t.Fatalf("wrestle timeout = %+v err=%v", resolution, err)
	}

	treasure, err := NewCompetitiveBattleWithRuleConfig(22, 1101, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1}, {PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule, PlayerLifecycle: CompetitivePlayerTimedRespawn,
		Objective: CompetitiveObjectiveTreasure,
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := uint32(1); index <= TreasureTargetPerTeamMember; index++ {
		resolution, recorded, pickupErr := treasure.RecordTreasurePickup(1, index, TreasureGemValueOne, uint16(index), 1)
		if pickupErr != nil || !recorded || resolution.NewlyConcluded != (index == TreasureTargetPerTeamMember) {
			t.Fatalf("treasure pickup %d = resolution:%+v recorded:%t err:%v", index, resolution, recorded, pickupErr)
		}
	}
}

func TestTankBattleRequiresDestroyedBaseAndTeamElimination(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(16, 1801, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1}, {PlayerID: 2, RoleID: 8, TeamID: 2},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerTimedRespawn,
		Objective:        CompetitiveObjectiveTankBase,
	})
	if err != nil {
		t.Fatal(err)
	}
	change, err := battle.RecordTankBaseHPChange(1, 2, -10)
	if err != nil || change.BaseTeamID != 2 || change.HP != 90 || change.Destroyed || !change.Applied || change.Resolution.NewlyConcluded {
		t.Fatalf("tank base damage = %+v err:%v", change, err)
	}
	repair, err := battle.RecordTankBaseHPChange(2, 2, 30)
	if err != nil || repair.HP != 100 || repair.Destroyed || !repair.Applied {
		t.Fatalf("tank base repair = %+v err:%v", repair, err)
	}
	for hit := uint32(0); hit < 10; hit++ {
		change, err = battle.RecordTankBaseHPChange(1, 2, -10)
		if err != nil {
			t.Fatal(err)
		}
	}
	if change.HP != 0 || !change.Destroyed || change.Resolution.NewlyConcluded {
		t.Fatalf("tank base destruction settled before player elimination: %+v", change)
	}
	lateRepair, err := battle.RecordTankBaseHPChange(2, 2, 30)
	if err != nil || lateRepair.HP != 0 || !lateRepair.Destroyed || lateRepair.Applied {
		t.Fatalf("destroyed tank base was repaired: %+v err:%v", lateRepair, err)
	}
	if err = battle.RecordRespawn(2); err == nil {
		t.Fatal("tank player respawned after its base was destroyed")
	}
	resolution, err := battle.RecordDeath(2)
	if err != nil || !resolution.NewlyConcluded || resolution.WinnerTeamID != 1 {
		t.Fatalf("tank final elimination = %+v err:%v", resolution, err)
	}
}

func TestTankBattleSettlesWhenBaseFallsAfterPlayersAreDeadAndIgnoresLateReports(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(17, 1701, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
		{PlayerID: 3, RoleID: 9, TeamID: 2},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerTimedRespawn,
		Objective:        CompetitiveObjectiveTankBase,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, playerID := range []uint16{2, 3} {
		resolution, deathErr := battle.RecordDeath(playerID)
		if deathErr != nil || resolution.NewlyConcluded {
			t.Fatalf("temporary tank death %d = %+v err:%v", playerID, resolution, deathErr)
		}
	}
	var final CompetitiveTankBaseHPResult
	for hit := uint32(0); hit < 10; hit++ {
		final, err = battle.RecordTankBaseHPChange(1, 2, -10)
		if err != nil {
			t.Fatal(err)
		}
	}
	if !final.Resolution.NewlyConcluded || final.Resolution.WinnerTeamID != 1 {
		t.Fatalf("tank base-last conclusion = %+v", final)
	}
	retry, err := battle.RecordTankBaseHPChange(1, 2, -10)
	if err != nil || retry.Applied || retry.Resolution.NewlyConcluded || retry.HP != 0 {
		t.Fatalf("late tank HP report changed concluded base: %+v err:%v", retry, err)
	}
}

func TestTankBattleDepartureDoesNotBlockDestroyedTeamElimination(t *testing.T) {
	battle, err := NewCompetitiveBattleWithRuleConfig(18, 1702, 1, []CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1}, {PlayerID: 2, RoleID: 8, TeamID: 1},
		{PlayerID: 3, RoleID: 9, TeamID: 2}, {PlayerID: 4, RoleID: 10, TeamID: 2},
	}, CompetitiveRuleConfig{
		ConclusionPolicy: CompetitiveConclusionClientRule,
		PlayerLifecycle:  CompetitivePlayerTimedRespawn,
		Objective:        CompetitiveObjectiveTankBase,
	})
	if err != nil {
		t.Fatal(err)
	}
	for hit := 0; hit < 10; hit++ {
		if _, err = battle.RecordTankBaseHPChange(1, 2, -10); err != nil {
			t.Fatal(err)
		}
	}
	if resolution, deathErr := battle.RecordDeath(3); deathErr != nil || resolution.NewlyConcluded {
		t.Fatalf("one living teammate should keep destroyed team active: %+v err:%v", resolution, deathErr)
	}
	resolution, err := battle.RecordDeparture(4)
	if err != nil || !resolution.NewlyConcluded || resolution.WinnerTeamID != 1 {
		t.Fatalf("departed final teammate blocked tank settlement: %+v err:%v", resolution, err)
	}
}

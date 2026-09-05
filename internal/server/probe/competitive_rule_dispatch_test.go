package probe

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"

	"qqtang/internal/game/mapdata"
	"qqtang/internal/game/match"
	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
)

func TestRequireCompetitiveBombDispatchIsNativeRule2Only(t *testing.T) {
	catalog, err := mapdata.LoadCatalog(filepath.Join("..", "..", "..", "runtime", "client-patched"))
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	participants := []match.CompetitiveParticipant{{PlayerID: 1, RoleID: 7, TeamID: 1}, {PlayerID: 2, RoleID: 8, TeamID: 2}}
	newBattle := func(gameID, mapID uint32) *match.CompetitiveBattle {
		battle, battleErr := match.NewCompetitiveBattleWithRuleConfig(gameID, mapID, 1, participants, match.CompetitiveRuleConfig{
			ConclusionPolicy: match.CompetitiveConclusionClientRule, PlayerLifecycle: match.CompetitivePlayerTimedRespawn,
		})
		if battleErr != nil {
			t.Fatal(battleErr)
		}
		return battle
	}
	server, session, peer := newRoomPeerUDPTestServer(t)
	server.mapCatalog = catalog
	server.competitiveBattles = map[uint32]*match.CompetitiveBattle{1: newBattle(1, 701)}
	state, stateErr := server.sessionRoom(session)
	if stateErr != nil {
		t.Fatal(stateErr)
	}
	if _, stateErr = state.UpdateMatchSettings(session.Profile.PlayerID, roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(701), GameType: roomstate.GameTypeCompetitiveNoItem,
	}); stateErr != nil {
		t.Fatal(stateErr)
	}
	if _, stateErr = state.SetReady(peer.Profile.PlayerID, true); stateErr != nil {
		t.Fatal(stateErr)
	}
	if _, stateErr = server.worldState().StartMatchWithID(session.UIN, 1); stateErr != nil {
		t.Fatal(stateErr)
	}
	session.CurrentGameID, session.CurrentMapID = 1, 701
	if _, requireErr := server.requireCompetitiveBombDispatch(session, session.Profile.PlayerID); requireErr != nil {
		t.Fatalf("kick-bomb dispatch rejected: %v", requireErr)
	}
	session.CurrentMapID = 411
	if _, requireErr := server.requireCompetitiveBombDispatch(session, 1); requireErr == nil {
		t.Fatal("ordinary-rule Boss map accepted rule-2 dispatch uplink")
	}
}

func TestReliableNativeTransportSeparatesMirrorsFromServerPeerConversion(t *testing.T) {
	for _, schema := range []uint16{
		game.NotifyPlayerExploded, game.NotifyPlayerGetItem, game.NotifyPlayerKilled, game.NotifyPlayerRelive,
		game.NotifyPlayerPutBun, game.NotifyNPCDropItem, game.NotifyFire,
		game.NotifyGenerateBox, game.PlayerBeHarmed, game.CreateNPCBossEvent,
	} {
		if !nativePeerMulticastMirrorSchema(schema) {
			t.Fatalf("native schema 0x%04X is not classified as a reliable mirror", schema)
		}
	}
	for _, schema := range []uint16{
		game.PlayerUseBomb, game.RequestEatBomb, game.RequestUseItem,
		game.RequestPreparedUseProp, game.RequestGameNextMap,
	} {
		if nativePeerMulticastMirrorSchema(schema) {
			t.Fatalf("server-converted schema 0x%04X was classified as a native mirror", schema)
		}
	}

	server, owner, _, _ := newReliableBossRuleTest(t, 11, []uint16{1, 2, 4})
	body := make([]byte, game.PlayerUseBombBodySize)
	binary.BigEndian.PutUint16(body[0:2], 30_001)
	binary.BigEndian.PutUint32(body[2:6], 1_000)
	binary.BigEndian.PutUint32(body[6:10], 77)
	body[10], body[11] = 4, 5
	binary.BigEndian.PutUint16(body[12:14], 8)
	result := sendReliableBossRuleEvent(t, server, owner, 1, game.PlayerUseBomb, body)
	if !result.handled || len(result.response) == 0 || len(result.followUp) == 0 || result.afterFollowUp == nil {
		t.Fatalf("native PLAYER_USE_BOMB did not produce its peer-direction notification: %+v", result)
	}
	inspection, err := game.InspectLocalPacket(result.followUp)
	if err != nil {
		t.Fatal(err)
	}
	notify, err := game.ParseNotifyGameEventPayload(inspection.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if notify.Schema != game.NotifyPlayerUseBomb || !bytes.Equal(notify.Body, body) {
		t.Fatalf("native PLAYER_USE_BOMB peer projection = %+v", notify)
	}

	unclassified := sendReliableBossRuleEvent(t, server, owner, 2, game.NotifyPlayerUseItem, []byte{1, 2, 3})
	if !unclassified.handled || len(unclassified.response) == 0 || len(unclassified.followUp) != 0 || unclassified.afterResponse != nil {
		t.Fatalf("unclassified reliable native event escaped ACK-only boundary: %+v", unclassified)
	}
}

func TestBunReliableMirrorsOnlyAccountWhileNativeMulticastSettles(t *testing.T) {
	catalog, err := mapdata.LoadCatalog(filepath.Join("..", "..", "..", "runtime", "client-patched"))
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	server, owner, peer := newRoomPeerUDPTestServer(t)
	server.mapCatalog = catalog
	state, err := server.sessionRoom(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.UpdateMatchSettings(owner.Profile.PlayerID, roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(801), GameType: roomstate.GameTypeCompetitiveNoItem,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = state.SetReady(peer.Profile.PlayerID, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 1
	if _, err = server.worldState().StartMatchWithID(owner.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	owner.CurrentGameID, owner.CurrentMapID = gameID, 801
	peer.CurrentGameID, peer.CurrentMapID = gameID, 801
	battle, err := match.NewCompetitiveBattleWithRuleConfig(gameID, 801, owner.Profile.PlayerID, []match.CompetitiveParticipant{
		{PlayerID: owner.Profile.PlayerID, RoleID: 7, TeamID: 1},
		{PlayerID: peer.Profile.PlayerID, RoleID: 8, TeamID: 2},
	}, match.CompetitiveRuleConfig{
		ConclusionPolicy: match.CompetitiveConclusionClientRule,
		PlayerLifecycle:  match.CompetitivePlayerTimedRespawn,
		Objective:        match.CompetitiveObjectiveBun,
	})
	if err != nil {
		t.Fatal(err)
	}
	server.competitiveBattles = map[uint32]*match.CompetitiveBattle{gameID: battle}

	pickup := game.BunActionEvent{PlayerID: peer.Profile.PlayerID, ClientTime: 100, PosX: 40, PosY: 80, BunID: 0, BunTeamID: 1}
	pickupBody, err := pickup.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	request := sendReliableBossRuleEvent(t, server, peer, 1, game.RequestGetBun, pickupBody)
	if !request.handled || len(request.response) == 0 || len(request.followUp) != 0 || request.afterResponse != nil {
		t.Fatalf("bun request reliable mirror escaped ACK-only path: %+v", request)
	}
	forged := sendReliableBossRuleEvent(t, server, peer, 2, game.NotifyPlayerGetBun, pickupBody)
	if !forged.handled || len(forged.response) != 0 || forged.afterResponse != nil {
		t.Fatalf("non-arbitrator bun notification was accepted: %+v", forged)
	}
	notify := sendReliableBossRuleEvent(t, server, owner, 3, game.NotifyPlayerGetBun, pickupBody)
	if !notify.handled || len(notify.response) == 0 || len(notify.followUp) != 0 || notify.afterResponse != nil {
		t.Fatalf("arbitrator bun notification reliable mirror escaped ACK-only path: %+v", notify)
	}

	deposit := pickup
	deposit.ClientTime = 110
	deposit.PosX, deposit.PosY = 80, 40
	depositBody, err := deposit.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	settled := sendReliableBossRuleEvent(t, server, owner, 4, game.NotifyPlayerPutBun, depositBody)
	if !settled.handled || len(settled.response) == 0 || len(settled.followUp) != 0 || settled.afterResponse != nil ||
		len(settled.startFollowUp) == 0 || !settled.completeCompetitiveAfterSend || settled.competitiveRoomSettlement == nil {
		t.Fatalf("complete bun capture reliable mirror did not settle without duplicate scene delivery: %+v", settled)
	}
}

func TestSculptureNotificationsSettleAfterFourthNativeDeposit(t *testing.T) {
	catalog, err := mapdata.LoadCatalog(filepath.Join("..", "..", "..", "runtime", "client-patched"))
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	server, owner, peer := newRoomPeerUDPTestServer(t)
	server.mapCatalog = catalog
	state, err := server.sessionRoom(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.UpdateMatchSettings(owner.Profile.PlayerID, roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(1201), GameType: roomstate.GameTypeCompetitiveNoItem,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = state.SetReady(peer.Profile.PlayerID, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 1
	if _, err = server.worldState().StartMatchWithID(owner.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	owner.CurrentGameID, owner.CurrentMapID = gameID, 1201
	peer.CurrentGameID, peer.CurrentMapID = gameID, 1201
	battle, err := match.NewCompetitiveBattleWithRuleConfig(gameID, 1201, owner.Profile.PlayerID, []match.CompetitiveParticipant{
		{PlayerID: owner.Profile.PlayerID, RoleID: 7, TeamID: 1},
		{PlayerID: peer.Profile.PlayerID, RoleID: 8, TeamID: 2},
	}, match.CompetitiveRuleConfig{
		ConclusionPolicy: match.CompetitiveConclusionClientRule,
		PlayerLifecycle:  match.CompetitivePlayerTimedRespawn,
		Objective:        match.CompetitiveObjectiveSculpture,
	})
	if err != nil {
		t.Fatal(err)
	}
	server.competitiveBattles = map[uint32]*match.CompetitiveBattle{gameID: battle}

	sequence := uint32(1)
	for piece, material := range []byte{12, 13, 14, 15} {
		pickup := game.BunActionEvent{
			PlayerID: owner.Profile.PlayerID, ClientTime: 100 + uint32(piece*2),
			PosX: 40, PosY: 80, BunID: 2, BunTeamID: material,
		}
		pickupBody, marshalErr := pickup.MarshalNetworkBinary()
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		picked := sendReliableBossRuleEvent(t, server, owner, sequence, game.NotifyPlayerGetBun, pickupBody)
		sequence++
		if !picked.handled || len(picked.response) == 0 || picked.afterResponse != nil || len(picked.startFollowUp) != 0 {
			t.Fatalf("sculpture pickup %d transport = %+v", piece+1, picked)
		}

		deposit := pickup
		deposit.ClientTime++
		deposit.PosX, deposit.PosY, deposit.BunID = 80, 40, 0
		depositBody, marshalErr := deposit.MarshalNetworkBinary()
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		placed := sendReliableBossRuleEvent(t, server, owner, sequence, game.NotifyPlayerPutBun, depositBody)
		sequence++
		if !placed.handled || len(placed.response) == 0 || placed.afterResponse != nil {
			t.Fatalf("sculpture deposit %d transport = %+v", piece+1, placed)
		}
		if piece < 3 && (len(placed.startFollowUp) != 0 || placed.completeCompetitiveAfterSend) {
			t.Fatalf("sculpture deposit %d settled early: %+v", piece+1, placed)
		}
		if piece == 3 && (len(placed.startFollowUp) == 0 || !placed.completeCompetitiveAfterSend || placed.competitiveRoomSettlement == nil) {
			t.Fatalf("fourth sculpture deposit did not settle: %+v", placed)
		}
		if piece == 3 && placed.startFollowUpDelay != competitiveNativeConclusionDelay {
			t.Fatalf("fourth sculpture deposit conclusion delay = %s, want %s", placed.startFollowUpDelay, competitiveNativeConclusionDelay)
		}
	}
}

func TestMachineDurabilityFiltersThreeEarlyDeathReportsAndSettlesFourth(t *testing.T) {
	catalog, err := mapdata.LoadCatalog(filepath.Join("..", "..", "..", "runtime", "client-patched"))
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	server, owner, peer := newRoomPeerUDPTestServer(t)
	server.mapCatalog = catalog
	state, err := server.sessionRoom(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.UpdateMatchSettings(owner.Profile.PlayerID, roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(1301), GameType: roomstate.GameTypeCompetitiveNoItem,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = state.SetReady(peer.Profile.PlayerID, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 1
	if _, err = server.worldState().StartMatchWithID(owner.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	owner.CurrentGameID, owner.CurrentMapID = gameID, 1301
	peer.CurrentGameID, peer.CurrentMapID = gameID, 1301
	battle, err := match.NewCompetitiveBattleWithRuleConfig(gameID, 1301, owner.Profile.PlayerID, []match.CompetitiveParticipant{
		{PlayerID: owner.Profile.PlayerID, RoleID: 7, TeamID: 1},
		{PlayerID: peer.Profile.PlayerID, RoleID: 8, TeamID: 2},
	}, match.CompetitiveRuleConfig{
		ConclusionPolicy: match.CompetitiveConclusionClientRule,
		PlayerLifecycle:  match.CompetitivePlayerNativeDurability,
		NativeHitLimit:   4,
	})
	if err != nil {
		t.Fatal(err)
	}
	server.competitiveBattles = map[uint32]*match.CompetitiveBattle{gameID: battle}

	for hit := 1; hit <= 4; hit++ {
		harmBody := make([]byte, 13)
		binary.BigEndian.PutUint16(harmBody[0:2], peer.Profile.PlayerID)
		binary.BigEndian.PutUint32(harmBody[2:6], uint32(hit*100))
		binary.BigEndian.PutUint16(harmBody[6:8], 40)
		binary.BigEndian.PutUint16(harmBody[8:10], 80)
		binary.BigEndian.PutUint16(harmBody[11:13], 25)
		harm := sendReliableBossRuleEvent(t, server, owner, uint32(hit*2-1), game.PlayerBeHarmed, harmBody)
		if !harm.handled || len(harm.response) == 0 || len(harm.followUp) != 0 || harm.afterResponse != nil {
			t.Fatalf("machine harm %d reliable mirror escaped ACK-only path: %+v", hit, harm)
		}

		deathBody := make([]byte, 11)
		binary.BigEndian.PutUint16(deathBody[0:2], peer.Profile.PlayerID)
		binary.BigEndian.PutUint32(deathBody[2:6], uint32(hit*100+1))
		// The damaged client's rule object reports its own death. The original
		// implementation accepted only the arbitrator here, which left 2P at
		// zero visible HP without committing the elimination.
		death := sendReliableBossRuleEvent(t, server, peer, uint32(hit*2), game.NotifyPlayerDieEvent, deathBody)
		if !death.handled || len(death.response) == 0 || len(death.followUp) != 0 {
			t.Fatalf("machine death report %d transport = %+v", hit, death)
		}
		if hit < 4 && (death.afterResponse != nil || len(death.startFollowUp) != 0 || death.completeCompetitiveAfterSend) {
			t.Fatalf("machine early death report %d escaped filter: %+v", hit, death)
		}
		if hit == 4 && (death.afterResponse != nil || len(death.startFollowUp) == 0 || !death.completeCompetitiveAfterSend || death.competitiveRoomSettlement == nil) {
			t.Fatalf("machine fourth death did not settle: %+v", death)
		}
	}
}

func TestTankBaseReportSettlesAfterDestroyedTeamIsAlreadyDead(t *testing.T) {
	catalog, err := mapdata.LoadCatalog(filepath.Join("..", "..", "..", "runtime", "client-patched"))
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	server, owner, peer := newRoomPeerUDPTestServer(t)
	server.mapCatalog = catalog
	var tankLogs bytes.Buffer
	server.logWriter = &tankLogs
	if selected, ok := catalog.CompetitiveMap(1701); !ok || selected.Rule != mapdata.CompetitiveRuleTank {
		t.Fatalf("map 1701 rule = %+v found:%t", selected, ok)
	}
	state, err := server.sessionRoom(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.UpdateMatchSettings(owner.Profile.PlayerID, roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(1701), GameType: roomstate.GameTypeCompetitiveNoItem,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = state.SetReady(peer.Profile.PlayerID, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 1
	if _, err = server.worldState().StartMatchWithID(owner.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	owner.CurrentGameID, owner.CurrentMapID = gameID, 1701
	peer.CurrentGameID, peer.CurrentMapID = gameID, 1701
	battle, err := match.NewCompetitiveBattleWithRuleConfig(gameID, 1701, owner.Profile.PlayerID, []match.CompetitiveParticipant{
		{PlayerID: owner.Profile.PlayerID, RoleID: 7, TeamID: 1},
		{PlayerID: peer.Profile.PlayerID, RoleID: 8, TeamID: 2},
	}, match.CompetitiveRuleConfig{
		ConclusionPolicy: match.CompetitiveConclusionClientRule,
		PlayerLifecycle:  match.CompetitivePlayerTimedRespawn,
		Objective:        match.CompetitiveObjectiveTankBase,
	})
	if err != nil {
		t.Fatal(err)
	}
	server.competitiveBattles = map[uint32]*match.CompetitiveBattle{gameID: battle}
	if _, requireErr := server.requireCompetitiveRule(owner, mapdata.CompetitiveRuleTank, owner.Profile.PlayerID); requireErr != nil {
		t.Fatalf("tank rule setup: %v", requireErr)
	}

	deathBody := make([]byte, 11)
	binary.BigEndian.PutUint16(deathBody[0:2], peer.Profile.PlayerID)
	binary.BigEndian.PutUint32(deathBody[2:6], 1_000)
	death := sendReliableBossRuleEvent(t, server, peer, 1, game.NotifyPlayerDieEvent, deathBody)
	if !death.handled || len(death.response) == 0 || len(death.startFollowUp) != 0 || death.completeCompetitiveAfterSend {
		t.Fatalf("tank death with intact base settled early: %+v", death)
	}

	for hit := 0; hit < 10; hit++ {
		baseBody := make([]byte, 10)
		binary.BigEndian.PutUint16(baseBody[0:2], owner.Profile.PlayerID)
		binary.BigEndian.PutUint32(baseBody[2:6], uint32(1_100+hit))
		// Rule 13 reports the target base's team. The explosion producer reads
		// this byte directly from the base object at the impacted cell.
		binary.BigEndian.PutUint16(baseBody[6:8], 2)
		binary.BigEndian.PutUint16(baseBody[8:10], 0xfff6)
		result := sendReliableBossRuleEvent(t, server, owner, uint32(hit+2), game.RequestTankBaseHP, baseBody)
		if !result.handled || len(result.response) == 0 || len(result.followUp) != 0 {
			t.Fatalf("tank base hit %d transport = %+v logs=%s", hit+1, result, tankLogs.String())
		}
		if hit < 9 && (len(result.startFollowUp) != 0 || result.completeCompetitiveAfterSend) {
			t.Fatalf("tank base hit %d settled early: %+v", hit+1, result)
		}
		if hit == 9 {
			if len(result.startFollowUp) == 0 || !result.completeCompetitiveAfterSend || result.competitiveRoomSettlement == nil {
				t.Fatalf("destroyed tank base plus dead team did not settle: %+v", result)
			}
			if result.startFollowUpDelay != competitiveNativeConclusionDelay {
				t.Fatalf("tank conclusion delay = %s, want %s", result.startFollowUpDelay, competitiveNativeConclusionDelay)
			}
		}
	}
}

func TestTankDepartureSettlesSurvivingTeamAndReturnsRoomToPreparing(t *testing.T) {
	server, owner, peer := newRoomPeerUDPTestServer(t)
	state, err := server.sessionRoom(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.UpdateMatchSettings(owner.Profile.PlayerID, roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(1701), GameType: roomstate.GameTypeCompetitiveNoItem,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = state.SetReady(peer.Profile.PlayerID, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 1
	if _, err = server.worldState().StartMatchWithID(owner.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	owner.CurrentGameID, owner.CurrentMapID = gameID, 1701
	peer.CurrentGameID, peer.CurrentMapID = gameID, 1701
	battle, err := match.NewCompetitiveBattleWithRuleConfig(gameID, 1701, owner.Profile.PlayerID, []match.CompetitiveParticipant{
		{PlayerID: owner.Profile.PlayerID, RoleID: 7, TeamID: 1},
		{PlayerID: peer.Profile.PlayerID, RoleID: 8, TeamID: 2},
	}, match.CompetitiveRuleConfig{
		ConclusionPolicy: match.CompetitiveConclusionClientRule,
		PlayerLifecycle:  match.CompetitivePlayerTimedRespawn,
		Objective:        match.CompetitiveObjectiveTankBase,
	})
	if err != nil {
		t.Fatal(err)
	}
	server.competitiveBattles = map[uint32]*match.CompetitiveBattle{gameID: battle}

	// Exercise the same locked-session path used by an explicit active-room
	// departure. The room leave is projected before the departure GAME_OVER.
	owner.mu.Lock()
	err = server.handleActiveAdventureExitToLobby(owner, "tank-owner-departure")
	owner.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	snapshot := state.Snapshot()
	if snapshot.Phase != roomstate.PhasePreparing || snapshot.ActiveGameID != 0 {
		t.Fatalf("room after tank departure = phase %d game %d, want preparing/0", snapshot.Phase, snapshot.ActiveGameID)
	}
	if owner.CurrentGameID != 0 || peer.CurrentGameID != 0 {
		t.Fatalf("session games after tank departure = owner:%d peer:%d", owner.CurrentGameID, peer.CurrentGameID)
	}
	if _, err = server.competitiveBattle(gameID); err == nil {
		t.Fatal("concluded tank battle remained registered")
	}
}

func TestBoxFireNotificationIsArbitratorOnlyReliableMirror(t *testing.T) {
	catalog, err := mapdata.LoadCatalog(filepath.Join("..", "..", "..", "runtime", "client-patched"))
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	server, owner, peer := newRoomPeerUDPTestServer(t)
	server.mapCatalog = catalog
	state, err := server.sessionRoom(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.UpdateMatchSettings(owner.Profile.PlayerID, roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(1401), GameType: roomstate.GameTypeCompetitiveNoItem,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = state.SetReady(peer.Profile.PlayerID, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 1
	if _, err = server.worldState().StartMatchWithID(owner.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	owner.CurrentGameID, owner.CurrentMapID = gameID, 1401
	peer.CurrentGameID, peer.CurrentMapID = gameID, 1401
	battle, err := match.NewCompetitiveBattleWithRuleConfig(gameID, 1401, owner.Profile.PlayerID, []match.CompetitiveParticipant{
		{PlayerID: owner.Profile.PlayerID, RoleID: 7, TeamID: 1},
		{PlayerID: peer.Profile.PlayerID, RoleID: 8, TeamID: 2},
	}, match.CompetitiveRuleConfig{
		ConclusionPolicy: match.CompetitiveConclusionClientRule,
		PlayerLifecycle:  match.CompetitivePlayerNativeDurability,
		NativeHitLimit:   4,
	})
	if err != nil {
		t.Fatal(err)
	}
	server.competitiveBattles = map[uint32]*match.CompetitiveBattle{gameID: battle}

	fireBody := func(playerID uint16, mask byte) []byte {
		body := make([]byte, 7)
		binary.BigEndian.PutUint16(body[0:2], playerID)
		binary.BigEndian.PutUint32(body[2:6], 3_000)
		body[6] = mask
		return body
	}
	valid := sendReliableBossRuleEvent(t, server, owner, 1, game.NotifyFire, fireBody(owner.Profile.PlayerID, 0x05))
	if !valid.handled || len(valid.response) == 0 || len(valid.followUp) != 0 || valid.afterResponse != nil {
		t.Fatalf("valid box fire reliable mirror escaped ACK-only path: %+v", valid)
	}
	forgedSource := sendReliableBossRuleEvent(t, server, peer, 2, game.NotifyFire, fireBody(peer.Profile.PlayerID, 0x01))
	if !forgedSource.handled || len(forgedSource.response) != 0 || forgedSource.afterResponse != nil {
		t.Fatalf("non-arbitrator box fire was accepted: %+v", forgedSource)
	}
	unknownPlayer := sendReliableBossRuleEvent(t, server, owner, 3, game.NotifyFire, fireBody(999, 0x01))
	if !unknownPlayer.handled || len(unknownPlayer.response) != 0 || unknownPlayer.afterResponse != nil {
		t.Fatalf("box fire for an unknown player was accepted: %+v", unknownPlayer)
	}
	invalidMask := sendReliableBossRuleEvent(t, server, owner, 4, game.NotifyFire, fireBody(owner.Profile.PlayerID, 0x10))
	if !invalidMask.handled || len(invalidMask.response) != 0 || invalidMask.afterResponse != nil {
		t.Fatalf("box fire with an invalid launcher mask was accepted: %+v", invalidMask)
	}

	// Rule 8 uses the same native kick action as rule 2 for bubbles created by
	// its corner launchers and carried bomb props. It must not be rejected by a
	// rule-2-only transport guard.
	kickBody, err := (game.KickBombAction{
		BombRowAndCol: 0x34, BombDestRowAndCol: 0x35, Direction: 2, Power: 3,
		Time: 3_100, PlayerID: peer.Profile.PlayerID, BombPower: 3,
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	kick := sendReliableBossRuleEvent(t, server, peer, 5, game.PlayerThrowBomb, kickBody)
	if !kick.handled || len(kick.response) == 0 || len(kick.followUp) != 0 || kick.afterResponse != nil {
		t.Fatalf("rule-8 kickable bubble action was rejected: %+v", kick)
	}

	// The native rule-8 producer replenishes boxes through 0x1168. The source
	// has already applied it, so the reliable route remains ACK + peer-only.
	generated := []byte{1, 0x11, 0, 0, 0, 0, 0, 0, 0}
	restore := sendReliableBossRuleEvent(t, server, owner, 6, game.NotifyGenerateBox, generated)
	if !restore.handled || len(restore.response) == 0 || len(restore.followUp) != 0 || restore.afterResponse != nil {
		t.Fatalf("rule-8 box replenishment reliable mirror escaped ACK-only path: %+v", restore)
	}

	owner.CurrentMapID = 411
	if _, _, requireErr := server.requireCompetitiveKickableBombRule(owner, owner.Profile.PlayerID); requireErr == nil {
		t.Fatal("ordinary rule accepted the rule-2/rule-8 kickable-bomb action")
	}
}

func TestReliableKickBombAcceptsOnlyAuthenticatedPlayerOrArbitratorBoss(t *testing.T) {
	server, owner, peer, _ := newReliableBossRuleTest(t, 701, []uint16{10, 11, 12})
	send := func(session *connectionSession, sequence uint32, actorID uint16) gameEventMessageResult {
		t.Helper()
		body, err := (game.KickBombAction{
			BombRowAndCol: 0x34, BombDestRowAndCol: 0x35, Direction: 2, Power: 3,
			Time: 1_000 + sequence, PlayerID: actorID, BombPower: 3,
		}).MarshalNetworkBinary()
		if err != nil {
			t.Fatal(err)
		}
		return sendReliableBossRuleEvent(t, server, session, sequence, game.PlayerThrowBomb, body)
	}

	if result := send(owner, 1, 30_001); !result.handled || len(result.response) == 0 || len(result.followUp) != 0 || result.afterResponse != nil {
		t.Fatalf("arbitrator Boss kick was rejected: %+v", result)
	}
	if result := send(owner, 2, owner.Profile.PlayerID); !result.handled || len(result.response) == 0 || len(result.followUp) != 0 || result.afterResponse != nil {
		t.Fatalf("authenticated player kick was rejected: %+v", result)
	}
	if result := send(peer, 3, 30_001); !result.handled || len(result.response) != 0 || len(result.followUp) != 0 {
		t.Fatalf("non-arbitrator Boss kick proxy was accepted: %+v", result)
	}
	if result := send(owner, 4, 30_002); !result.handled || len(result.response) != 0 || len(result.followUp) != 0 {
		t.Fatalf("unregistered Boss kick proxy was accepted: %+v", result)
	}
}

func TestReliableTransformationEventsKeepRegisteredPlayerAndBossIdentity(t *testing.T) {
	server, owner, peer, battle := newReliableBossRuleTest(t, 11, []uint16{1, 2, 4})
	itemBody := func(actorID uint16) []byte {
		body := make([]byte, 14)
		binary.BigEndian.PutUint16(body[0:2], actorID)
		binary.BigEndian.PutUint32(body[2:6], 1_000)
		binary.BigEndian.PutUint32(body[6:10], 110)
		binary.BigEndian.PutUint16(body[10:12], 80)
		binary.BigEndian.PutUint16(body[12:14], 160)
		return body
	}

	if result := sendReliableBossRuleEvent(t, server, owner, 1, game.RequestGetItem, itemBody(owner.Profile.PlayerID)); !result.handled || len(result.response) == 0 || len(result.followUp) != 0 || result.afterResponse != nil {
		t.Fatalf("player transformation pickup was rejected: %+v", result)
	}
	if result := sendReliableBossRuleEvent(t, server, owner, 2, game.RequestGetItem, itemBody(30_001)); !result.handled || len(result.response) == 0 || len(result.followUp) != 0 || result.afterResponse != nil {
		t.Fatalf("arbitrator Boss transformation pickup was rejected: %+v", result)
	}
	if result := sendReliableBossRuleEvent(t, server, peer, 3, game.RequestGetItem, itemBody(30_001)); !result.handled || len(result.response) != 0 || len(result.followUp) != 0 {
		t.Fatalf("non-arbitrator Boss transformation pickup was accepted: %+v", result)
	}
	if result := sendReliableBossRuleEvent(t, server, peer, 4, game.RequestGetItem, itemBody(peer.Profile.PlayerID)); !result.handled || len(result.response) == 0 || len(result.followUp) != 0 || result.afterResponse != nil {
		t.Fatalf("peer pickup request reliable mirror escaped ACK-only path: %+v", result)
	}
	if result := sendReliableBossRuleEvent(t, server, owner, 5, game.NotifyPlayerGetItem, itemBody(peer.Profile.PlayerID)); !result.handled || len(result.response) == 0 || len(result.followUp) != 0 || result.afterResponse != nil {
		t.Fatalf("arbitrator pickup notification reliable mirror escaped ACK-only path: %+v", result)
	}
	if result := sendReliableBossRuleEvent(t, server, peer, 6, game.NotifyPlayerGetItem, itemBody(peer.Profile.PlayerID)); !result.handled || len(result.response) != 0 || len(result.followUp) != 0 || result.afterResponse != nil {
		t.Fatalf("non-arbitrator pickup notification was accepted: %+v", result)
	}

	participantHit, err := (game.PlayerExplodedEvent{
		PlayerID: peer.Profile.PlayerID, ClientTime: 1_100, PosX: 80, PosY: 160, IsAvatar: true,
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if result := sendReliableBossRuleEvent(t, server, owner, 7, game.PlayerBeExploded, participantHit); !result.handled || len(result.response) == 0 || len(result.followUp) != 0 || result.afterResponse != nil {
		t.Fatalf("transformed participant hit was rejected: %+v", result)
	}
	if _, err = battle.RecordBossKill(30_001, peer.Profile.PlayerID); err == nil {
		t.Fatal("transformed participant hit incorrectly entered the trapped-player state")
	}

	bossHit, err := (game.PlayerExplodedEvent{
		PlayerID: 30_001, ClientTime: 1_200, PosX: 80, PosY: 160, IsAvatar: true,
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if result := sendReliableBossRuleEvent(t, server, owner, 8, game.PlayerBeExploded, bossHit); !result.handled || len(result.response) == 0 || len(result.followUp) != 0 || result.afterResponse != nil {
		t.Fatalf("transformed Boss hit was rejected: %+v", result)
	}
	if result := sendReliableBossRuleEvent(t, server, peer, 9, game.PlayerBeExploded, bossHit); !result.handled || len(result.response) != 0 || len(result.followUp) != 0 {
		t.Fatalf("non-arbitrator transformed Boss hit was accepted: %+v", result)
	}
	normalBossHit := append([]byte(nil), bossHit...)
	normalBossHit[len(normalBossHit)-1] = 0
	if result := sendReliableBossRuleEvent(t, server, owner, 7, game.PlayerBeExploded, normalBossHit); !result.handled || len(result.response) != 0 || len(result.followUp) != 0 {
		t.Fatalf("normal-form Boss bypassed the Boss-harmed route: %+v", result)
	}

	recoveryBody, err := (game.AvatarRecoveryEvent{
		PlayerID: peer.Profile.PlayerID, Time: 31_000, PosX: 80, PosY: 160,
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if result := sendReliableBossRuleEvent(t, server, owner, 8, game.NotifyRecoverAvatar, recoveryBody); !result.handled || len(result.response) == 0 || len(result.followUp) != 0 || result.afterResponse != nil {
		t.Fatalf("arbitrator participant avatar recovery was not peer-only: %+v", result)
	}
	if result := sendReliableBossRuleEvent(t, server, peer, 9, game.NotifyRecoverAvatar, recoveryBody); !result.handled || len(result.response) != 0 || result.afterResponse != nil {
		t.Fatalf("non-arbitrator avatar recovery was accepted: %+v", result)
	}
	bossRecovery, err := (game.AvatarRecoveryEvent{PlayerID: 30_001, Time: 31_100, PosX: 84, PosY: 164}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if result := sendReliableBossRuleEvent(t, server, owner, 10, game.NotifyRecoverAvatar, bossRecovery); !result.handled || len(result.response) == 0 || len(result.followUp) != 0 || result.afterResponse != nil {
		t.Fatalf("arbitrator Boss avatar recovery was not peer-only: %+v", result)
	}
	unknownRecovery, err := (game.AvatarRecoveryEvent{PlayerID: 30_002, Time: 31_200, PosX: 88, PosY: 168}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if result := sendReliableBossRuleEvent(t, server, owner, 11, game.NotifyRecoverAvatar, unknownRecovery); !result.handled || len(result.response) != 0 || result.afterResponse != nil {
		t.Fatalf("unregistered avatar-recovery target was accepted: %+v", result)
	}
}

func newReliableBossRuleTest(t *testing.T, mapID uint32, skills []uint16) (*Server, *connectionSession, *connectionSession, *match.CompetitiveBattle) {
	t.Helper()
	catalog, err := mapdata.LoadCatalog(filepath.Join("..", "..", "..", "runtime", "client-patched"))
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	server, owner, peer := newRoomPeerUDPTestServer(t)
	server.mapCatalog = catalog
	state, err := server.sessionRoom(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.UpdateMatchSettings(owner.Profile.PlayerID, roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(mapID), GameType: roomstate.GameTypeCompetitiveNoItem,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = state.SetReady(peer.Profile.PlayerID, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 1
	if _, err = server.worldState().StartMatchWithID(owner.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	owner.CurrentGameID, owner.CurrentMapID = gameID, mapID
	peer.CurrentGameID, peer.CurrentMapID = gameID, mapID
	battle, err := match.NewCompetitiveBattleWithRuleConfig(gameID, mapID, owner.Profile.PlayerID, []match.CompetitiveParticipant{
		{PlayerID: owner.Profile.PlayerID, RoleID: 7, TeamID: 1},
		{PlayerID: peer.Profile.PlayerID, RoleID: 8, TeamID: 1},
	}, match.CompetitiveRuleConfig{
		ConclusionPolicy: match.CompetitiveConclusionClientRule,
		PlayerLifecycle:  match.CompetitivePlayerPermanentElimination,
		Objective:        match.CompetitiveObjectiveBoss,
		TeamTopology:     match.CompetitiveTeamsCooperative,
		BossEntityIDs:    []uint16{30_001},
		BossID:           "test-boss",
		BossSkills:       map[uint16][]uint16{30_001: append([]uint16(nil), skills...)},
	})
	if err != nil {
		t.Fatal(err)
	}
	server.competitiveBattles = map[uint32]*match.CompetitiveBattle{gameID: battle}
	return server, owner, peer, battle
}

func sendReliableBossRuleEvent(t *testing.T, server *Server, session *connectionSession, sequence uint32, schema uint16, body []byte) gameEventMessageResult {
	t.Helper()
	payload, err := game.MarshalGameEventPayload(session.UIN, sequence, schema, body)
	if err != nil {
		t.Fatal(err)
	}
	packet := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, session.Profile.PlayerID, session.UIN, payload)
	return server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, session, "test", "local", "remote", packet)
}

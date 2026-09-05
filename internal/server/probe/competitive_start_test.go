package probe

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"qqtang/internal/clientdata/sceneelement"
	"qqtang/internal/game/mapdata"
	"qqtang/internal/game/match"
	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

func TestCompetitiveRoomStartUsesCompetitiveBattleAndMap(t *testing.T) {
	clientRoot := filepath.Join("..", "..", "..", "runtime", "client-patched")
	catalog, err := mapdata.LoadCatalog(clientRoot)
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	server := &Server{
		mapCatalog: catalog, logWriter: io.Discard,
		battles:            make(map[uint32]*match.AdventureBattle),
		competitiveBattles: make(map[uint32]*match.CompetitiveBattle),
	}
	ownerProfile := game.DefaultPlayerProfile()
	ownerProfile.GameInfo.RoleID = 7
	ownerProfile.GameInfo.Point = ^uint32(0)
	ownerProfile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(17, 5)}
	owner := &connectionSession{UIN: 1_000_001, Profile: ownerProfile}
	if err = server.createSessionRoom(owner, 1, byte(roomstate.GameTypeCompetitiveNoItem)); err != nil {
		t.Fatal(err)
	}
	state, err := server.sessionRoom(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().AttachMember(owner.RoomID, roomstate.Member{PlayerID: 2, RoleID: 8, TeamID: 2, Ready: true}); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[:4], owner.UIN)
	binary.BigEndian.PutUint32(payload[4:], 1)
	request := testLocalRoutedPacketWithPayload(t, game.StartGameCommand, 3, 0xffff, 1, owner.UIN, payload)
	result := server.handleMatchStartMessage(ListenerConfig{Response: ResponseConfig{
		QQTStartGameSuccess: true, QQTGameBegin: true,
	}}, owner, "test", "local", "remote", request)
	if !result.handled || len(result.response) == 0 || len(result.followUp) == 0 {
		t.Fatalf("competitive start = handled:%t response:%d follow-up:%d result:%s", result.handled, len(result.response), len(result.followUp), result.result)
	}
	inspection, err := game.InspectLocalPacket(result.followUp)
	if err != nil {
		t.Fatal(err)
	}
	begin, err := game.ParseLengthPrefixedGameBeginDataNetwork(inspection.Payload)
	if err != nil {
		t.Fatal(err)
	}
	selected, ok := catalog.CompetitiveMap(begin.MapID)
	if !ok || !selected.UsesOrdinaryElimination() || selected.RequiredItemField != 0 || len(begin.Players) != 2 {
		t.Fatalf("competitive GAME_BEGIN = %+v, map %+v, %v", begin, selected, ok)
	}
	if _, err = server.competitiveBattle(begin.GameID); err != nil {
		t.Fatal(err)
	}
	if _, err = server.adventureBattle(begin.GameID); err == nil {
		t.Fatal("competitive start created an adventure battle")
	}
	if snapshot := state.Snapshot(); snapshot.Phase != roomstate.PhaseInMatch || snapshot.ActiveGameID != begin.GameID {
		t.Fatalf("room after competitive start = %+v", snapshot)
	}

	worldUseBody, err := (game.WorldUseItemEvent{
		PlayerID: owner.Profile.PlayerID, ClientTime: 1, ItemID: 17, PosX: 10, PosY: 20,
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	worldUsePayload, err := game.MarshalGameEventPayload(owner.UIN, 1, game.RequestUseItem, worldUseBody)
	if err != nil {
		t.Fatal(err)
	}
	worldUsePacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, worldUsePayload)
	worldUseResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", worldUsePacket)
	if !worldUseResult.handled || len(worldUseResult.response) == 0 || len(worldUseResult.followUp) == 0 {
		t.Fatalf("competitive world item dispatch = %+v", worldUseResult)
	}
	worldUseNotifyPacket, err := game.InspectLocalPacket(worldUseResult.followUp)
	if err != nil {
		t.Fatal(err)
	}
	worldUseNotify, err := game.ParseNotifyGameEventPayload(worldUseNotifyPacket.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if worldUseNotify.Schema != game.NotifyPlayerUseItem || owner.Profile.Inventory[0].NumOfItem != 5 {
		t.Fatalf("world item notify/inventory = 0x%04X/%+v", worldUseNotify.Schema, owner.Profile.Inventory)
	}

	// Mirrors the shipped rule-8 producer: the containing message and each
	// BOSS_INFO are zero-initialized, so Label, HP and trailing Time stay zero.
	bosses := game.CreateNPCBoss{Bosses: []game.CompetitiveBossInfo{{
		BossID: 20_001, RoleID: 25, TeamID: 7, AIType: 2, Row: 4, Col: 5,
		OutfitItems: []game.BossItemInfo{{ItemID: 500, ItemCount: 3}, {ItemID: 501, ItemCount: 2}},
	}}}
	bossBody, err := bosses.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	bossPayload, err := game.MarshalGameEventPayload(owner.UIN, 2, game.CreateNPCBossEvent, bossBody)
	if err != nil {
		t.Fatal(err)
	}
	bossPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, bossPayload)
	bossResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", bossPacket)
	if !bossResult.handled || len(bossResult.response) != 0 || len(bossResult.followUp) != 0 {
		t.Fatalf("ordinary competitive map accepted rule-8 CREATE_NPC_BOSS: %+v", bossResult)
	}

	killedBody, err := (game.PlayerKilledEvent{PlayerInteractionEvent: game.PlayerInteractionEvent{
		PlayerID: owner.Profile.PlayerID, ClientTime: 2, DestinationPlayerID: 2, PosX: 10, PosY: 20,
	}}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	killedPayload, err := game.MarshalGameEventPayload(owner.UIN, 1, game.NotifyPlayerKilled, killedBody)
	if err != nil {
		t.Fatal(err)
	}
	killedPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, killedPayload)
	killedResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", killedPacket)
	if !killedResult.handled || !killedResult.completeCompetitiveAfterSend || killedResult.completeAdventureAfterSend || killedResult.competitiveRoomSettlement == nil || len(killedResult.startFollowUp) == 0 {
		t.Fatalf("competitive killed dispatch = %+v", killedResult)
	}
	overPacket, err := game.InspectLocalPacket(killedResult.startFollowUp)
	if err != nil {
		t.Fatal(err)
	}
	overEvent, err := game.ParseNotifyGameEventPayload(overPacket.Payload)
	if err != nil {
		t.Fatal(err)
	}
	over, err := game.ParseGameOverData(overEvent.Body)
	if err != nil {
		t.Fatal(err)
	}
	if over.GameMode != game.SettlementGameModeCompetitive || len(over.Results) != 2 ||
		over.Results[0].Result != game.GameResultWin || over.Results[0].Point != 60 ||
		over.Results[1].Result != game.GameResultLoss || over.Results[1].Point != 10 {
		t.Fatalf("competitive GAME_OVER = %+v", over)
	}
	ownerStats, ok := over.Results[0].AdventureStatistics()
	if !ok || ownerStats.KillCount != 1 || ownerStats.KillScore != 10 || ownerStats.RescueCount != 0 || ownerStats.RescueScore != 0 {
		t.Fatalf("competitive GAME_OVER action statistics = %+v/%t", ownerStats, ok)
	}
	beforeExtPoint := owner.Profile.GameInfo.ExtPoint
	if err = server.completeCompetitiveRoomMatch(owner, "test", *killedResult.competitiveRoomSettlement); err != nil {
		t.Fatal(err)
	}
	if owner.Profile.GameInfo.WinNum != ownerProfile.GameInfo.WinNum+1 || owner.Profile.GameInfo.ExtPoint != beforeExtPoint || owner.Profile.GameInfo.Point != game.MaxPlayerExperience {
		t.Fatalf("competitive profile after completion = %+v", owner.Profile.GameInfo)
	}
	if snapshot := state.Snapshot(); snapshot.Phase != roomstate.PhasePreparing || snapshot.ActiveGameID != 0 || len(snapshot.Members) != 2 {
		t.Fatalf("room after competitive completion = %+v", snapshot)
	}
}

func TestCompetitivePreparedInventoryRequiresItemField(t *testing.T) {
	profile := game.DefaultPlayerProfile()
	profile.Inventory = []game.ItemInfo{
		{ItemID: 20003, NumOfItem: 3, ItemStatus: 1}, // competitive only
		{ItemID: 20020, NumOfItem: 4, ItemStatus: 1}, // competitive + adventure
		{ItemID: 20043, NumOfItem: 5, ItemStatus: 1}, // adventure only
	}
	session := &connectionSession{UIN: 1_000_001, Profile: profile, CurrentGameID: 7}
	participants := []match.CompetitiveParticipant{{
		PlayerID: profile.PlayerID, RoleID: profile.GameInfo.RoleID, TeamID: 1,
	}}
	server := &Server{}
	selected := mapdata.CompetitiveMap{ID: 101, NativeRule: 1}

	withoutItems, err := server.newCompetitiveGameData(session, selected, roomstate.CompetitiveFieldNoItem, profile.PlayerID, participants, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := withoutItems.Players[0].NewItems; len(got) != 0 {
		t.Fatalf("no-item competitive room carried prepared inventory: %+v", got)
	}

	withItems, err := server.newCompetitiveGameData(session, selected, roomstate.CompetitiveFieldItem, profile.PlayerID, participants, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []game.GameItemType{{ItemID: 20003, Quantity: 3}, {ItemID: 20020, Quantity: 4}}
	if !slices.Equal(withItems.Players[0].NewItems, want) {
		t.Fatalf("item competitive room prepared inventory = %+v, want %+v", withItems.Players[0].NewItems, want)
	}
}

func TestCompetitiveVirtualParticipantHasNoConnectionOwnedPreparedInventory(t *testing.T) {
	profile := game.DefaultPlayerProfile()
	profile.GameInfo.RoleID = 7
	session := &connectionSession{UIN: 1_000_001, Profile: profile, CurrentGameID: 8}
	server := &Server{}
	data, err := server.newCompetitiveGameData(session, mapdata.CompetitiveMap{ID: 101, NativeRule: 1}, roomstate.CompetitiveFieldItem, profile.PlayerID, []match.CompetitiveParticipant{
		{PlayerID: profile.PlayerID, RoleID: 7, TeamID: 1},
		{PlayerID: 30_001, RoleID: 8, TeamID: 2, Source: match.CompetitiveParticipantVirtualAI},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.Players) != 2 || len(data.Players[1].NewItems) != 0 {
		t.Fatalf("virtual GAME_BEGIN projection = %+v", data.Players)
	}
}

func TestCompetitiveProtocolGameTimeIsSeparateFromNativeRuleDeadline(t *testing.T) {
	clientRoot := filepath.Join("..", "..", "..", "runtime", "client-patched")
	catalog, err := mapdata.LoadCatalog(clientRoot)
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	wrestle, ok := catalog.CompetitiveMap(1001)
	if !ok || wrestle.Rule != mapdata.CompetitiveRuleWrestle {
		t.Fatalf("map 1001 = %+v/%t, want wrestle", wrestle, ok)
	}
	rule, ok := mapdata.LookupCompetitiveRule(wrestle.Rule)
	if !ok || rule.RoundDurationMS != 180_000 {
		t.Fatalf("wrestle native deadline = %+v/%t, want 180000ms", rule, ok)
	}
	server := &Server{mapCatalog: catalog}
	session := &connectionSession{CurrentGameID: 1}
	data, err := server.newCompetitiveGameData(session, wrestle, roomstate.CompetitiveFieldNoItem, 1, []match.CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if game.DefaultCompetitiveGameTimeMS != 240_000 {
		t.Fatalf("legacy competitive GameTime constant = %d, want 240000", game.DefaultCompetitiveGameTimeMS)
	}
	if data.GameTimeMS != game.DefaultCompetitiveGameTimeMS {
		t.Fatalf("competitive GAME_BEGIN GameTime = %d, want legacy 240000", data.GameTimeMS)
	}
}

func TestCompetitiveTimeoutUsesAbsoluteNativeSceneDeadline(t *testing.T) {
	if competitiveBossVictoryConclusionDelay != 10*time.Second {
		t.Fatalf("competitive Boss victory conclusion delay = %s, want 10s", competitiveBossVictoryConclusionDelay)
	}
	duration, resultTimeMS := competitiveTimeoutTiming(240_000)
	if duration != 240*time.Second || resultTimeMS != 240_000 {
		t.Fatalf("rule-1 timeout = %s/%d, want 240s/240000", duration, resultTimeMS)
	}
	duration, resultTimeMS = competitiveTimeoutTiming(180_000)
	if duration != 180*time.Second || resultTimeMS != 180_000 {
		t.Fatalf("short-rule timeout = %s/%d, want 180s/180000", duration, resultTimeMS)
	}
}

func TestCompetitiveBossVictoryGraceDepartureKeepsWinningSettlement(t *testing.T) {
	battle, err := match.NewCompetitiveBattleWithRuleConfig(88, 702, 1, []match.CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 1},
	}, match.CompetitiveRuleConfig{
		ConclusionPolicy: match.CompetitiveConclusionClientRule,
		PlayerLifecycle:  match.CompetitivePlayerPermanentElimination,
		Objective:        match.CompetitiveObjectiveBoss,
		TeamTopology:     match.CompetitiveTeamsCooperative,
		BossEntityIDs:    []uint16{30001},
		BossID:           "cristiano",
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolution, deathErr := battle.RecordBossDeath(30001); deathErr != nil || !resolution.NewlyConcluded {
		t.Fatalf("Boss death = %+v err:%v", resolution, deathErr)
	}
	server := &Server{competitiveBattles: map[uint32]*match.CompetitiveBattle{88: battle}}
	for _, playerID := range []uint16{1, 2} {
		profile := game.DefaultPlayerProfile()
		profile.PlayerID = playerID
		session := &connectionSession{CurrentGameID: 88, Profile: profile}
		transition := server.handleCompetitiveDeparture(session, "test")
		if transition.DepartingSettlement == nil || transition.DepartingSettlement.Result != game.GameResultWin {
			t.Fatalf("grace departure player %d settlement = %+v", playerID, transition.DepartingSettlement)
		}
	}
	resolution, concluded := battle.ConcludedResolution()
	if !concluded || resolution.WinnerTeamID != 1 || resolution.AllPlayersLost {
		t.Fatalf("all-departed Boss victory = %+v concluded:%t", resolution, concluded)
	}
}

func TestCompetitiveGameDataIncludesRuleOwnedObjectiveSceneItems(t *testing.T) {
	server := &Server{}
	profile := game.DefaultPlayerProfile()
	session := &connectionSession{CurrentGameID: 9, Profile: profile}
	participants := []match.CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 1},
		{PlayerID: 3, RoleID: 9, TeamID: 2},
		{PlayerID: 4, RoleID: 10, TeamID: 2},
	}
	treasure := mapdata.CompetitiveMap{
		ID: 1101, NativeRule: 5, Rule: mapdata.CompetitiveRuleTreasure,
		WallItemRules:   []mapdata.CompetitiveWallItemRule{{SceneID: 1, Minimum: 2, Maximum: 2, Probability: 1}},
		HiddenItemCells: competitiveTestHiddenCells(40),
	}
	data, err := server.newCompetitiveGameData(session, treasure, roomstate.CompetitiveFieldNoItem, 1, participants, false)
	if err != nil {
		t.Fatal(err)
	}
	want := []game.GameItemType{
		{ItemID: 1, Quantity: 1},
		{ItemID: 6, Quantity: 1},
		{ItemID: 150, Quantity: 16},
		{ItemID: 151, Quantity: 8},
		{ItemID: 152, Quantity: 4},
	}
	if !slices.Equal(data.NewItems, want) {
		t.Fatalf("treasure GAME_BEGIN NewItems = %+v, want %+v", data.NewItems, want)
	}

	// An item field retains contact pickups from its filtered native table,
	// while the objective pool is orthogonal and must remain or the map is
	// unplayable.
	data, err = server.newCompetitiveGameData(session, treasure, roomstate.CompetitiveFieldItem, 1, participants[:1], false)
	if err != nil {
		t.Fatal(err)
	}
	want = []game.GameItemType{
		{ItemID: 1, Quantity: 1},
		{ItemID: 6, Quantity: 1},
		{ItemID: 150, Quantity: 8},
		{ItemID: 151, Quantity: 4},
		{ItemID: 152, Quantity: 2},
	}
	if !slices.Equal(data.NewItems, want) {
		t.Fatalf("item-field treasure objective NewItems = %+v, want %+v", data.NewItems, want)
	}

	sculpture := mapdata.CompetitiveMap{ID: 1201, NativeRule: 6, Rule: mapdata.CompetitiveRuleSculpture, HiddenItemCells: competitiveTestHiddenCells(16)}
	data, err = server.newCompetitiveGameData(session, sculpture, roomstate.CompetitiveFieldNoItem, 1, participants, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(data.NewItems) != 2 || data.NewItems[0] != (game.GameItemType{ItemID: 161, Quantity: 6}) ||
		data.NewItems[1].Quantity != 1 || data.NewItems[1].ItemID < 162 || data.NewItems[1].ItemID > 164 {
		t.Fatalf("sculpture GAME_BEGIN NewItems = %+v, want six ordinary and one drawable special kind", data.NewItems)
	}

	tank := mapdata.CompetitiveMap{
		ID: 1701, NativeRule: 13, Rule: mapdata.CompetitiveRuleTank,
		WallItemRules:   []mapdata.CompetitiveWallItemRule{{SceneID: 1, Minimum: 8, Maximum: 8, Probability: 1}},
		HiddenItemCells: competitiveTestHiddenCells(20),
	}
	data, err = server.newCompetitiveGameData(session, tank, roomstate.CompetitiveFieldNoItem, 1, participants, false)
	if err != nil {
		t.Fatal(err)
	}
	want = []game.GameItemType{
		{ItemID: 1, Quantity: 7},
		{ItemID: 6, Quantity: 1},
		{ItemID: 201, Quantity: 2},
		{ItemID: 203, Quantity: 2},
		{ItemID: 204, Quantity: 2},
	}
	if !slices.Equal(data.NewItems, want) {
		t.Fatalf("tank GAME_BEGIN NewItems = %+v, want ordinary and rule-specific wall objects %+v", data.NewItems, want)
	}
}

func TestShippedItemFieldGameBeginKeepsContactAndObjectiveItems(t *testing.T) {
	catalog, err := mapdata.LoadCatalog(filepath.Join("..", "..", "..", "runtime", "client-patched"))
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	server := &Server{}
	profile := game.DefaultPlayerProfile()
	session := &connectionSession{UIN: 100001, CurrentGameID: 9, Profile: profile}
	participants := []match.CompetitiveParticipant{{PlayerID: profile.PlayerID, RoleID: profile.GameInfo.RoleID, TeamID: 1}}

	for _, mapID := range []uint32{514, 519, 520, 521} {
		selected, ok := catalog.CompetitiveMap(mapID)
		if !ok || selected.RequiredItemField != 1 {
			t.Fatalf("shipped ice item-field map %d = %+v/%t", mapID, selected, ok)
		}
		data, dataErr := server.newCompetitiveGameData(session, selected, roomstate.CompetitiveFieldItem, 1, participants, false)
		if dataErr != nil {
			t.Fatal(dataErr)
		}
		seen := map[uint32]bool{}
		for _, item := range data.NewItems {
			if _, actionPickup := sceneelement.NativeBattleActionPickup(sceneelement.ID(item.ItemID)); actionPickup {
				t.Fatalf("ice item-field map %d GAME_BEGIN retained action-slot pickup %d", mapID, item.ItemID)
			}
			seen[item.ItemID] = true
		}
		for _, sceneID := range []uint32{1, 2, 3} {
			if !seen[sceneID] {
				t.Fatalf("ice item-field map %d GAME_BEGIN lacks base contact pickup %d: %+v", mapID, sceneID, data.NewItems)
			}
		}
	}

	treasure, ok := catalog.CompetitiveMap(1114)
	if !ok || treasure.RequiredItemField != 1 || treasure.Rule != mapdata.CompetitiveRuleTreasure {
		t.Fatalf("shipped treasure item-field map 1114 = %+v/%t", treasure, ok)
	}
	data, err := server.newCompetitiveGameData(session, treasure, roomstate.CompetitiveFieldItem, 1, participants, false)
	if err != nil {
		t.Fatal(err)
	}
	quantities := map[uint32]int16{}
	for _, item := range data.NewItems {
		quantities[item.ItemID] = item.Quantity
	}
	if quantities[uint32(sceneelement.TreasureGemOne)] != 8 || quantities[uint32(sceneelement.TreasureGemTwo)] != 4 || quantities[uint32(sceneelement.TreasureGemThree)] != 2 {
		t.Fatalf("item-field treasure GAME_BEGIN objective quantities = %v", quantities)
	}
}

func competitiveTestHiddenCells(count int) []mapdata.CompetitiveCell {
	cells := make([]mapdata.CompetitiveCell, count)
	for index := range cells {
		cells[index] = mapdata.CompetitiveCell{Row: byte(index / 20), Col: byte(index % 20)}
	}
	return cells
}

func TestFreeCompetitiveRoomStartsUnbalancedTeamsOnSpecialMap(t *testing.T) {
	clientRoot := filepath.Join("..", "..", "..", "runtime", "client-patched")
	catalog, err := mapdata.LoadCatalog(clientRoot)
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	const boxMapID = uint32(1401)
	selected, ok := catalog.CompetitiveMap(boxMapID)
	if !ok || selected.Rule != mapdata.CompetitiveRuleBox {
		t.Fatalf("special competitive map %d = %+v, found %t", boxMapID, selected, ok)
	}
	server := &Server{
		mapCatalog: catalog, logWriter: io.Discard,
		battles:            make(map[uint32]*match.AdventureBattle),
		competitiveBattles: make(map[uint32]*match.CompetitiveBattle),
	}
	owner := &connectionSession{UIN: 1_000_001, Profile: game.DefaultPlayerProfile()}
	if err = server.createSessionRoom(owner, 1, byte(roomstate.GameTypeCompetitiveNoItem)); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().UpdateMatchSettings(sessionWorldUIN(owner), roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(boxMapID), GameType: roomstate.GameTypeCompetitiveNoItem,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().AttachMember(owner.RoomID, roomstate.Member{PlayerID: 2, RoleID: 8, TeamID: 1, Ready: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().AttachMember(owner.RoomID, roomstate.Member{PlayerID: 3, RoleID: 9, TeamID: 2, Ready: true}); err != nil {
		t.Fatal(err)
	}
	state, err := server.sessionRoom(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = state.ValidateStart(owner.Profile.PlayerID); err == nil {
		t.Fatal("standard special-map room accepted unbalanced teams")
	}
	if _, err = server.worldState().UpdateRoomProperties(sessionWorldUIN(owner), roomstate.Properties{Flag: roomstate.PropertyFlagFreeRule}); err != nil {
		t.Fatal(err)
	}

	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[:4], owner.UIN)
	binary.BigEndian.PutUint32(payload[4:], 1)
	request := testLocalRoutedPacketWithPayload(t, game.StartGameCommand, 3, 0xffff, 1, owner.UIN, payload)
	result := server.handleMatchStartMessage(ListenerConfig{Response: ResponseConfig{
		QQTStartGameSuccess: true, QQTGameBegin: true,
	}}, owner, "test", "local", "remote", request)
	if !result.handled || len(result.response) == 0 || len(result.followUp) == 0 {
		t.Fatalf("free special-map start = handled:%t response:%d follow-up:%d result:%s", result.handled, len(result.response), len(result.followUp), result.result)
	}
	packet, err := game.InspectLocalPacket(result.followUp)
	if err != nil {
		t.Fatal(err)
	}
	begin, err := game.ParseLengthPrefixedGameBeginDataNetwork(packet.Payload)
	if err != nil {
		t.Fatal(err)
	}
	teamCounts := map[byte]int{}
	for _, player := range begin.Players {
		teamCounts[player.TeamID]++
	}
	if begin.MapID != boxMapID || len(begin.Players) != 3 || teamCounts[1] != 2 || teamCounts[2] != 1 {
		t.Fatalf("free special-map GAME_BEGIN = map:%d players:%+v teams:%v", begin.MapID, begin.Players, teamCounts)
	}
}

func TestCreateNPCBossRequiresNativeBoxRule(t *testing.T) {
	clientRoot := filepath.Join("..", "..", "..", "runtime", "client-patched")
	catalog, err := mapdata.LoadCatalog(clientRoot)
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	server := &Server{
		mapCatalog: catalog, logWriter: io.Discard,
		battles: make(map[uint32]*match.AdventureBattle), competitiveBattles: make(map[uint32]*match.CompetitiveBattle),
	}
	owner := &connectionSession{UIN: 1_000_001, Profile: game.DefaultPlayerProfile()}
	if err = server.createSessionRoom(owner, 1, byte(roomstate.GameTypeCompetitiveNoItem)); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().AttachMember(owner.RoomID, roomstate.Member{PlayerID: 2, RoleID: 8, TeamID: 2, Ready: true}); err != nil {
		t.Fatal(err)
	}
	const boxMapID = uint32(1401)
	if _, err = server.worldState().UpdateMatchSettings(sessionWorldUIN(owner), roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(boxMapID), GameType: roomstate.GameTypeCompetitiveNoItem,
	}); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[:4], owner.UIN)
	binary.BigEndian.PutUint32(payload[4:], 1)
	start := testLocalRoutedPacketWithPayload(t, game.StartGameCommand, 3, 0xffff, 1, owner.UIN, payload)
	started := server.handleMatchStartMessage(ListenerConfig{Response: ResponseConfig{
		QQTStartGameSuccess: true, QQTGameBegin: true,
	}}, owner, "test", "local", "remote", start)
	if !started.handled || len(started.followUp) == 0 {
		t.Fatalf("box competitive start = %+v", started)
	}

	bosses := game.CreateNPCBoss{Bosses: []game.CompetitiveBossInfo{{
		BossID: 20_001, RoleID: 25, TeamID: 7, AIType: 2, Row: 4, Col: 5,
		OutfitItems: []game.BossItemInfo{{ItemID: 500, ItemCount: 3}, {ItemID: 501, ItemCount: 2}},
	}}}
	body, err := bosses.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	event, err := game.MarshalGameEventPayload(owner.UIN, 2, game.CreateNPCBossEvent, body)
	if err != nil {
		t.Fatal(err)
	}
	packet := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, event)
	result := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", packet)
	if !result.handled || len(result.response) == 0 || len(result.followUp) != 0 || result.afterResponse != nil {
		t.Fatalf("box CREATE_NPC_BOSS dispatch = %+v", result)
	}
	battle, err := server.competitiveBattle(owner.CurrentGameID)
	if err != nil {
		t.Fatal(err)
	}
	if !battle.HasNativeNPCEntity(20_001) || battle.HasBossEntity(20_001) {
		t.Fatal("validated rule-8 CREATE_NPC_BOSS did not register a non-objective native NPC")
	}
}

func TestWater11StartsCooperativeNativeBossOverlay(t *testing.T) {
	clientRoot := filepath.Join("..", "..", "..", "runtime", "client-patched")
	catalog, err := mapdata.LoadCatalog(clientRoot)
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	server := &Server{
		config: Config{CompetitiveBossRewards: map[string][]CompetitiveBossRewardConfig{
			"sailor": {
				{ItemID: uint32(sceneelement.SugarCoin50), ItemCount: 5},
				{ItemID: uint32(sceneelement.Experience20), ItemCount: 3},
				{ItemID: uint32(sceneelement.SugarCoin100), ItemCount: 2},
				{ItemID: uint32(sceneelement.Experience50), ItemCount: 1},
			},
		}},
		mapCatalog: catalog, logWriter: io.Discard,
		battles: make(map[uint32]*match.AdventureBattle), competitiveBattles: make(map[uint32]*match.CompetitiveBattle),
	}
	ownerProfile := game.DefaultPlayerProfile()
	ownerProfile.GameInfo.RoleID = 7
	ownerProfile.GameInfo.Point = ^uint32(0)
	owner := &connectionSession{UIN: 1_000_001, Profile: ownerProfile}
	if err = server.createSessionRoom(owner, 1, byte(roomstate.GameTypeCompetitiveNoItem)); err != nil {
		t.Fatal(err)
	}
	state, err := server.sessionRoom(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().AttachMember(owner.RoomID, roomstate.Member{PlayerID: 2, RoleID: 8, TeamID: 2, Ready: true}); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().UpdateMatchSettings(sessionWorldUIN(owner), roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(11), GameType: roomstate.GameTypeCompetitiveNoItem,
	}); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[:4], owner.UIN)
	binary.BigEndian.PutUint32(payload[4:], 1)
	request := testLocalRoutedPacketWithPayload(t, game.StartGameCommand, 3, 0xffff, 1, owner.UIN, payload)
	result := server.handleMatchStartMessage(ListenerConfig{Response: ResponseConfig{
		QQTStartGameSuccess: true, QQTGameBegin: true,
	}}, owner, "test", "local", "remote", request)
	if !result.handled || len(result.response) == 0 || len(result.followUp) == 0 || len(result.startFollowUp) == 0 {
		t.Fatalf("Water11 start = handled:%t response:%d begin:%d boss:%d result:%s", result.handled, len(result.response), len(result.followUp), len(result.startFollowUp), result.result)
	}
	beginPacket, err := game.InspectLocalPacket(result.followUp)
	if err != nil {
		t.Fatal(err)
	}
	begin, err := game.ParseLengthPrefixedGameBeginDataNetwork(beginPacket.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if begin.MapID != 11 || len(begin.Players) != 2 || begin.Players[0].TeamID != 1 || begin.Players[1].TeamID != 1 {
		t.Fatalf("Water11 GAME_BEGIN = %+v", begin)
	}
	bossPacket, err := game.InspectLocalPacket(result.startFollowUp)
	if err != nil {
		t.Fatal(err)
	}
	bossEvent, err := game.ParseNotifyGameEventPayload(bossPacket.Payload)
	if err != nil {
		t.Fatal(err)
	}
	bosses, err := game.ParseCreateNPCBossNetwork(bossEvent.Body)
	if err != nil {
		t.Fatal(err)
	}
	if bossEvent.Schema != game.CreateNPCBossEvent || bosses.Time != 0 || len(bosses.Bosses) != 1 {
		t.Fatalf("Water11 CREATE_NPC_BOSS = schema:0x%04X %+v", bossEvent.Schema, bosses)
	}
	for index, boss := range bosses.Bosses {
		if boss.BossID != uint16(30_001+index) || boss.RoleID != 32 || boss.TeamID != 7 || boss.AIType != 3 ||
			boss.Row != 0 || boss.Col != 0 || boss.HP != 5 || boss.Rate != 8 || boss.Bubble != 8 || boss.Power != 9 ||
			len(boss.NormalItems) != 4 || len(boss.OutfitItems) != 0 || len(boss.Skills) != 1 || len(boss.Metadata) != 0 {
			t.Fatalf("Water11 boss[%d] = %+v", index, boss)
		}
		wantSkills := []uint32{1}
		for skillIndex, skillID := range boss.Skills {
			if skillID != wantSkills[skillIndex] {
				t.Fatalf("Water11 boss[%d] skill[%d] = %d, want %d", index, skillIndex, skillID, wantSkills[skillIndex])
			}
		}
		wantItems := []game.BossItemInfo{
			{ItemID: uint32(sceneelement.SugarCoin50), ItemCount: 5},
			{ItemID: uint32(sceneelement.Experience20), ItemCount: 3},
			{ItemID: uint32(sceneelement.SugarCoin100), ItemCount: 2},
			{ItemID: uint32(sceneelement.Experience50), ItemCount: 1},
		}
		for itemIndex, item := range boss.NormalItems {
			if item != wantItems[itemIndex] {
				t.Fatalf("Water11 boss[%d] normal item[%d] = %+v, want %+v", index, itemIndex, item, wantItems[itemIndex])
			}
		}
	}
	// Match-only projection must not overwrite either player's selected room
	// colour; returning from the match therefore restores the room naturally.
	snapshot := state.Snapshot()
	if len(snapshot.Members) != 2 || snapshot.Members[0].TeamID != 1 || snapshot.Members[1].TeamID != 2 {
		t.Fatalf("Water11 room teams were mutated = %+v", snapshot.Members)
	}
	battle, err := server.competitiveBattle(begin.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if !battle.RequiresArbitratorConclusion() {
		t.Fatal("Water11 battle did not retain native-arbitrator conclusion policy")
	}
	// The reliable path must enforce the same BOSS_INFO program as the fast
	// gameplay path. Water11 advertises only the audited slow-glue skill 1; a
	// neighboring skill ID is not an implicit extension of that program.
	skillBody, err := (game.NPCUseSkillEvent{
		ObjectID: 30_001, Time: 1_000, PosX: 10, PosY: 20, SkillID: 1,
		Items: []game.NPCSkillItem{{SceneID: 43, Row: 3, Col: 4}},
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	skillPayload, err := game.MarshalGameEventPayload(owner.UIN, 4, game.NotifyNPCUseSkill, skillBody)
	if err != nil {
		t.Fatal(err)
	}
	skillPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, skillPayload)
	skillResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", skillPacket)
	if !skillResult.handled || len(skillResult.response) == 0 || len(skillResult.followUp) != 0 || skillResult.afterResponse != nil || skillResult.result != "qqt_game_event_ack_npc_30001_skill_1" {
		t.Fatalf("Water11 reliable Boss skill dispatch = %+v", skillResult)
	}
	forgedSkillBody := append([]byte(nil), skillBody...)
	binary.BigEndian.PutUint16(forgedSkillBody[10:12], 2)
	forgedSkillPayload, err := game.MarshalGameEventPayload(owner.UIN, 5, game.NotifyNPCUseSkill, forgedSkillBody)
	if err != nil {
		t.Fatal(err)
	}
	forgedSkillPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, forgedSkillPayload)
	forgedSkillResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", forgedSkillPacket)
	if !forgedSkillResult.handled || len(forgedSkillResult.response) != 0 || len(forgedSkillResult.followUp) != 0 {
		t.Fatalf("Water11 unadvertised Boss skill was accepted = %+v", forgedSkillResult)
	}
	electricSkillBody := append([]byte(nil), skillBody...)
	binary.BigEndian.PutUint16(electricSkillBody[10:12], 4)
	electricSkillPayload, err := game.MarshalGameEventPayload(owner.UIN, 6, game.NotifyNPCUseSkill, electricSkillBody)
	if err != nil {
		t.Fatal(err)
	}
	electricSkillPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, electricSkillPayload)
	electricSkillResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", electricSkillPacket)
	if !electricSkillResult.handled || len(electricSkillResult.response) != 0 || len(electricSkillResult.followUp) != 0 {
		t.Fatalf("Water11 electric-grid Boss skill was accepted = %+v", electricSkillResult)
	}
	transformationSkillBody := append([]byte(nil), skillBody...)
	binary.BigEndian.PutUint16(transformationSkillBody[10:12], 3)
	transformationSkillPayload, err := game.MarshalGameEventPayload(owner.UIN, 7, game.NotifyNPCUseSkill, transformationSkillBody)
	if err != nil {
		t.Fatal(err)
	}
	transformationSkillPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, transformationSkillPayload)
	transformationSkillResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", transformationSkillPacket)
	if !transformationSkillResult.handled || len(transformationSkillResult.response) != 0 || len(transformationSkillResult.followUp) != 0 {
		t.Fatalf("Water11 transformation Boss skill was accepted = %+v", transformationSkillResult)
	}
	// Native Boss collisions reuse REQUEST_KILL_PLAYER with the Boss object as
	// source and the avatar as destination. The arbitrator submits this event
	// on the NPC's behalf; rejecting source 30001 as "not a participant"
	// prevents cooperative all-dead failure from ever settling.
	// The native arbitrator reports another participant's syrup timeout with
	// IsAvatar=0. Participant identity comes from the active battle, not this
	// rendering flag; otherwise the following Boss kill is rejected forever.
	explodedBody, err := (game.PlayerExplodedEvent{
		PlayerID: 2, ClientTime: 1_050, PosX: 3, PosY: 4, IsAvatar: false,
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	explodedPayload, err := game.MarshalGameEventPayload(owner.UIN, 2, game.PlayerBeExploded, explodedBody)
	if err != nil {
		t.Fatal(err)
	}
	explodedPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, explodedPayload)
	explodedResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", explodedPacket)
	if !explodedResult.handled || len(explodedResult.response) == 0 || len(explodedResult.followUp) != 0 || explodedResult.afterResponse != nil {
		t.Fatalf("Water11 arbitrator player-exploded dispatch = %+v", explodedResult)
	}
	killBody, err := (game.PlayerInteractionEvent{
		PlayerID: 30_001, ClientTime: 1100, DestinationPlayerID: 2, PosX: 3, PosY: 4,
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	killPayload, err := game.MarshalGameEventPayload(owner.UIN, 2, game.RequestKillPlayer, killBody)
	if err != nil {
		t.Fatal(err)
	}
	killPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, killPayload)
	killResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", killPacket)
	if !killResult.handled || len(killResult.response) == 0 || len(killResult.followUp) != 0 || killResult.afterResponse != nil ||
		killResult.completeCompetitiveAfterSend || killResult.competitiveRoomSettlement != nil {
		t.Fatalf("Water11 Boss kill dispatch = %+v", killResult)
	}
	harmBody := make([]byte, 13)
	binary.BigEndian.PutUint16(harmBody[0:2], 30_001)
	binary.BigEndian.PutUint32(harmBody[2:6], 1200)
	binary.BigEndian.PutUint16(harmBody[6:8], 300)
	binary.BigEndian.PutUint16(harmBody[8:10], 400)
	// Captured Water11 sailor hits can use the zero-valued native marker.
	binary.BigEndian.PutUint16(harmBody[11:13], 0)
	harmPayload, err := game.MarshalGameEventPayload(owner.UIN, 2, game.PlayerBeHarmed, harmBody)
	if err != nil {
		t.Fatal(err)
	}
	harmPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, harmPayload)
	harmResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", harmPacket)
	if !harmResult.handled || len(harmResult.response) == 0 || len(harmResult.followUp) != 0 || harmResult.afterResponse != nil ||
		harmResult.result != "qqt_game_event_ack_ordinary_boss_30001_harmed_0_remaining_0" {
		t.Fatalf("Water11 Boss harm dispatch = %+v", harmResult)
	}
	nonArbitratorHarm := &connectionSession{
		UIN: 1_000_002, Profile: game.DefaultPlayerProfile(),
		CurrentGameID: owner.CurrentGameID, CurrentMapID: owner.CurrentMapID,
	}
	nonArbitratorHarm.Profile.PlayerID = 2
	if _, err = server.requireCompetitiveHarmTarget(nonArbitratorHarm, 30_001, 0); err == nil {
		t.Fatal("Water11 non-arbitrator Boss harm was accepted")
	}
	deathBody := make([]byte, 11)
	binary.BigEndian.PutUint16(deathBody[0:2], 30_001)
	binary.BigEndian.PutUint32(deathBody[2:6], 1234)
	deathPayload, err := game.MarshalGameEventPayload(owner.UIN, 3, game.NotifyPlayerDieEvent, deathBody)
	if err != nil {
		t.Fatal(err)
	}
	deathPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, deathPayload)
	deathResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", deathPacket)
	if !deathResult.handled || len(deathResult.response) == 0 || len(deathResult.followUp) != 0 || deathResult.afterResponse != nil || len(deathResult.startFollowUp) == 0 ||
		!deathResult.completeCompetitiveAfterSend || deathResult.competitiveRoomSettlement == nil || deathResult.startFollowUpDelay != competitiveBossVictoryConclusionDelay {
		t.Fatalf("Water11 Boss death dispatch = %+v", deathResult)
	}
	overPacket, err := game.InspectLocalPacket(deathResult.startFollowUp)
	if err != nil {
		t.Fatal(err)
	}
	overEvent, err := game.ParseNotifyGameEventPayload(overPacket.Payload)
	if err != nil {
		t.Fatal(err)
	}
	over, err := game.ParseGameOverData(overEvent.Body)
	if err != nil {
		t.Fatal(err)
	}
	if over.GameMode != game.SettlementGameModeCompetitive || len(over.Results) != 2 ||
		over.Results[0].Result != game.GameResultWin || over.Results[1].Result != game.GameResultWin {
		t.Fatalf("Water11 Boss success GAME_OVER = %+v", over)
	}
	ownerDeathBody := make([]byte, 11)
	binary.BigEndian.PutUint16(ownerDeathBody[0:2], owner.Profile.PlayerID)
	binary.BigEndian.PutUint32(ownerDeathBody[2:6], 1300)
	ownerDeathPayload, err := game.MarshalGameEventPayload(owner.UIN, 4, game.NotifyPlayerDieEvent, ownerDeathBody)
	if err != nil {
		t.Fatal(err)
	}
	ownerDeathPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, ownerDeathPayload)
	ownerDeathResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", ownerDeathPacket)
	if !ownerDeathResult.handled || len(ownerDeathResult.response) == 0 || len(ownerDeathResult.startFollowUp) != 0 ||
		ownerDeathResult.completeCompetitiveAfterSend || ownerDeathResult.result != "qqt_game_event_ack_competitive_boss_victory_grace_death_1_all_dead_true" {
		t.Fatalf("Water11 all-dead victory-grace dispatch = %+v", ownerDeathResult)
	}
	wake, active := battle.BossVictoryGraceSignal()
	if !active {
		t.Fatal("Water11 Boss victory grace signal disappeared before ordered delivery")
	}
	select {
	case <-wake:
	default:
		t.Fatal("Water11 all-dead transition did not wake the pending victory settlement")
	}
	owner.CurrentMapID = 11
	if err = server.validateCompetitiveGameOverSource(owner, battle); err != nil {
		t.Fatalf("Water11 arbitrator GAME_OVER source: %v", err)
	}
	nonArbitrator := &connectionSession{Profile: game.DefaultPlayerProfile(), CurrentMapID: 11}
	nonArbitrator.Profile.PlayerID = 2
	if err = server.validateCompetitiveGameOverSource(nonArbitrator, battle); err == nil {
		t.Fatal("Water11 non-arbitrator GAME_OVER source was accepted")
	}
}

func TestMine11StartsFirstMateOnlyWhenEveryPlayerOwnsTreasureMap(t *testing.T) {
	catalog, err := mapdata.LoadCatalog(filepath.Join("..", "..", "..", "runtime", "client-patched"))
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	server, owner, peer := newRoomPeerUDPTestServer(t)
	server.mapCatalog = catalog
	server.battles = make(map[uint32]*match.AdventureBattle)
	server.competitiveBattles = make(map[uint32]*match.CompetitiveBattle)
	server.config.CompetitiveBossRewards = map[string][]CompetitiveBossRewardConfig{
		"first_mate": {{ItemID: uint32(sceneelement.SugarCoin50), ItemCount: 1}},
	}
	owner.Profile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(460, 1)}
	peer.Profile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(460, 1)}
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err = store.LoadOrCreate(context.Background(), owner.UIN, owner.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LoadOrCreate(context.Background(), peer.UIN, peer.Profile); err != nil {
		t.Fatal(err)
	}
	server.playerStore = store
	state, err := server.sessionRoom(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.UpdateMatchSettings(owner.Profile.PlayerID, roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(411), GameType: roomstate.GameTypeCompetitiveNoItem,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = state.SetReady(peer.Profile.PlayerID, true); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[:4], owner.UIN)
	binary.BigEndian.PutUint32(payload[4:], uint32(owner.RoomID))
	request := testLocalRoutedPacketWithPayload(t, game.StartGameCommand, 3, 0xffff, 1, owner.UIN, payload)
	result := server.handleMatchStartMessage(ListenerConfig{Response: ResponseConfig{
		QQTStartGameSuccess: true, QQTGameBegin: true,
	}}, owner, "test", "local", "remote", request)
	if !result.handled || len(result.followUp) == 0 || len(result.startFollowUp) == 0 {
		t.Fatalf("Mine11 start = handled:%t begin:%d boss:%d result:%s", result.handled, len(result.followUp), len(result.startFollowUp), result.result)
	}
	beginPacket, err := game.InspectLocalPacket(result.followUp)
	if err != nil {
		t.Fatal(err)
	}
	begin, err := game.ParseLengthPrefixedGameBeginDataNetwork(beginPacket.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if begin.MapID != 411 || len(begin.Players) != 2 || begin.Players[0].TeamID != 1 || begin.Players[1].TeamID != 1 {
		t.Fatalf("Mine11 GAME_BEGIN = %+v", begin)
	}
	bossPacket, err := game.InspectLocalPacket(result.startFollowUp)
	if err != nil {
		t.Fatal(err)
	}
	bossEvent, err := game.ParseNotifyGameEventPayload(bossPacket.Payload)
	if err != nil {
		t.Fatal(err)
	}
	bosses, err := game.ParseCreateNPCBossNetwork(bossEvent.Body)
	if err != nil {
		t.Fatal(err)
	}
	if bossEvent.Schema != game.CreateNPCBossEvent || len(bosses.Bosses) != 1 || bosses.Bosses[0].RoleID != 33 || bosses.Bosses[0].HP != 10 {
		t.Fatalf("Mine11 First Mate = schema:0x%04X %+v", bossEvent.Schema, bosses)
	}
	battle, err := server.competitiveBattle(begin.GameID)
	if err != nil {
		t.Fatal(err)
	}
	if battle.BossID() != "first_mate" || !battle.IsCooperative() {
		t.Fatalf("Mine11 battle = Boss %q cooperative:%t", battle.BossID(), battle.IsCooperative())
	}
	if inventoryQuantity(owner.Profile.Inventory, 460) != 0 || inventoryQuantity(peer.Profile.Inventory, 460) != 0 {
		t.Fatalf("Mine11 summon items were not consumed atomically: owner=%+v peer=%+v", owner.Profile.Inventory, peer.Profile.Inventory)
	}
	for label, uin := range map[string]uint32{"owner": owner.UIN, "peer": peer.UIN} {
		item, found, loadErr := store.InventoryItem(context.Background(), uin, 460)
		if loadErr != nil || found || item.NumOfItem != 0 {
			t.Fatalf("%s durable summon item = found:%t item:%+v err:%v", label, found, item, loadErr)
		}
	}
}

func TestCompetitiveBossCandidateRequiresEveryActivePlayerInventory(t *testing.T) {
	participants := []match.CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
	}
	candidate := mapdata.CompetitiveBossCandidate{
		ID:         "first_mate",
		Activation: mapdata.CompetitiveBossActivationAllPlayersOwn,
		ItemOptions: []mapdata.CompetitiveBossItemOption{
			{Requirements: []mapdata.CompetitiveBossItemRequirement{{ItemID: 460, Count: 1}}},
		},
	}
	inventories := map[uint16][]game.ItemInfo{
		1: {game.NewPermanentItemInfo(460, 1)},
		2: {game.NewPermanentItemInfo(460, 0)},
	}
	satisfied, err := competitiveBossCandidateSatisfied(candidate, participants, inventories)
	if err != nil || satisfied {
		t.Fatalf("one missing summon item = satisfied:%t err:%v", satisfied, err)
	}
	// Activation checks ownership only. Consumption is a later, independently
	// proven transaction and must not mutate either participant's inventory.
	inventories[2][0] = game.NewPermanentItemInfo(460, 1)
	satisfied, err = competitiveBossCandidateSatisfied(candidate, participants, inventories)
	if err != nil || !satisfied {
		t.Fatalf("all players own summon item = satisfied:%t err:%v", satisfied, err)
	}
	if inventories[1][0].NumOfItem != 1 || inventories[2][0].NumOfItem != 1 {
		t.Fatalf("summon check consumed inventory: %+v", inventories)
	}
	// The installed itemCFG says fragments are recipe ingredients for a complete
	// map. They are not a direct summon alternative.
	inventories[2] = []game.ItemInfo{game.NewPermanentItemInfo(450, 30)}
	satisfied, err = competitiveBossCandidateSatisfied(candidate, participants, inventories)
	if err != nil || satisfied {
		t.Fatalf("First Mate fragments direct summon = satisfied:%t err:%v", satisfied, err)
	}
}

func TestCompetitiveBossSatisfiedPoolRandomlySelectsOneConsumptionPlan(t *testing.T) {
	participants := []match.CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
	}
	candidates := mapdata.LookupCompetitiveBossCandidates(905)
	if len(candidates) != 2 {
		t.Fatalf("map 905 Boss candidates = %d, want 2", len(candidates))
	}
	inventories := map[uint16][]game.ItemInfo{
		1: {game.NewPermanentItemInfo(402, 1), game.NewPermanentItemInfo(403, 1)},
		2: {game.NewPermanentItemInfo(402, 1), game.NewPermanentItemInfo(403, 1)},
	}
	pool, err := satisfiedCompetitiveBossCandidates(905, candidates, participants, inventories)
	if err != nil || len(pool) != 2 {
		t.Fatalf("satisfied Boss pool = %+v err:%v", pool, err)
	}
	for index, want := range []struct {
		id     string
		itemID uint16
	}{{"thief_griffin", 402}, {"christmas_griffin", 403}} {
		selected, selectErr := chooseCompetitiveBossCandidate(pool, bytes.NewReader([]byte{byte(index)}))
		if selectErr != nil || selected.Candidate.ID != want.id {
			t.Fatalf("selection %d = %+v err:%v, want %s", index, selected, selectErr, want.id)
		}
		if len(selected.Plan) != len(participants) {
			t.Fatalf("selection %s plan = %+v", want.id, selected.Plan)
		}
		for _, participant := range participants {
			requirements := selected.Plan[participant.PlayerID]
			if len(requirements) != 1 || requirements[0] != (mapdata.CompetitiveBossItemRequirement{ItemID: want.itemID, Count: 1}) {
				t.Fatalf("selection %s player %d plan = %+v", want.id, participant.PlayerID, requirements)
			}
		}
	}
}

func TestCompetitiveBossStartDataUsesCandidateEntityProfile(t *testing.T) {
	want := map[string]byte{
		"sailor": 32, "first_mate": 33, "hook": 34, "thief_griffin": 24,
		"christmas_griffin": 28, "ghost_griffin": 30, "little_nian": 29,
		"great_nian": 31, "cristiano": 35, "rooney": 36,
	}
	wantAIType := map[string]uint32{
		"sailor": 3, "first_mate": 3, "hook": 3, "thief_griffin": 3,
		"christmas_griffin": 3, "ghost_griffin": 3, "little_nian": 3,
		"great_nian": 3, "cristiano": 5, "rooney": 5,
	}
	wantCombat := map[string][3]uint32{
		"sailor": {8, 8, 9}, "first_mate": {8, 8, 9}, "hook": {8, 8, 9},
		"thief_griffin": {8, 8, 9}, "christmas_griffin": {8, 8, 9},
		"ghost_griffin": {8, 8, 9}, "little_nian": {8, 8, 9}, "great_nian": {8, 8, 9},
		"cristiano": {8, 8, 9}, "rooney": {8, 8, 9},
	}
	server := &Server{config: Config{CompetitiveBossRewards: map[string][]CompetitiveBossRewardConfig{}}}
	seen := make(map[string]struct{}, len(want))
	for _, mapID := range []uint32{11, 411, 12, 905, 510, 511, 512, 513, 702, 701} {
		for _, candidate := range mapdata.LookupCompetitiveBossCandidates(mapID) {
			if _, duplicate := seen[candidate.ID]; duplicate {
				continue
			}
			seen[candidate.ID] = struct{}{}
			data, enabled, err := server.competitiveBossStartData(competitiveMatchActivation{Candidate: candidate, Active: true}, 1)
			if err != nil || !enabled || len(data.Bosses) != 1 {
				t.Fatalf("Boss %s start = enabled:%t data:%+v err:%v", candidate.ID, enabled, data, err)
			}
			boss := data.Bosses[0]
			if boss.BossID != competitiveBossFirstID || boss.RoleID != want[candidate.ID] || boss.HP == 0 ||
				boss.TeamID != competitiveBossNeutralTeamID || boss.AIType != wantAIType[candidate.ID] {
				t.Fatalf("Boss %s entity = %+v", candidate.ID, boss)
			}
			combat := wantCombat[candidate.ID]
			if boss.Rate != combat[0] || boss.Bubble != combat[1] || uint32(boss.Power) != combat[2] {
				t.Fatalf("Boss %s combat tuple = %d/%d/%d, want %v", candidate.ID, boss.Rate, boss.Bubble, boss.Power, combat)
			}
			wantSkills := []uint32{1}
			switch candidate.ID {
			case "great_nian":
				wantSkills = []uint32{1, 3}
			case "first_mate", "hook":
				if candidate.ID == "first_mate" {
					wantSkills = []uint32{4, 6, 7, 9}
				} else {
					wantSkills = []uint32{4, 5, 6, 7, 8, 9}
				}
			case "cristiano":
				wantSkills = []uint32{0, 11, 12}
			case "rooney":
				wantSkills = []uint32{10, 11, 12}
			}
			if !slices.Equal(boss.Skills, wantSkills) {
				t.Fatalf("Boss %s skills = %v, want %v", candidate.ID, boss.Skills, wantSkills)
			}
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("tested Boss IDs = %v, want %d", seen, len(want))
	}
}

func TestCompetitiveBossStartDataSeparatesHitAndDeathInventories(t *testing.T) {
	candidate, ok := mapdata.LookupCompetitiveBossCandidate("sailor")
	if !ok {
		t.Fatal("sailor candidate is missing")
	}
	server := &Server{config: Config{CompetitiveBossRewards: map[string][]CompetitiveBossRewardConfig{
		"sailor": {
			{ItemID: uint32(sceneelement.SugarCoin50), ItemCount: 2},
			{ItemID: 451, ItemCount: 1},
		},
	}}}
	data, enabled, err := server.competitiveBossStartData(competitiveMatchActivation{Candidate: candidate, Active: true}, 1)
	if err != nil || !enabled || len(data.Bosses) != 1 {
		t.Fatalf("sailor start = enabled:%t data:%+v err:%v", enabled, data, err)
	}
	boss := data.Bosses[0]
	wantNormal := []game.BossItemInfo{{ItemID: uint32(sceneelement.SugarCoin50), ItemCount: 2}}
	wantOutfit := []game.BossItemInfo{{ItemID: 451, ItemCount: 1}}
	if !slices.Equal(boss.NormalItems, wantNormal) || !slices.Equal(boss.OutfitItems, wantOutfit) {
		t.Fatalf("sailor inventories = normal:%+v outfit:%+v", boss.NormalItems, boss.OutfitItems)
	}
}

func TestEveryCompetitiveBossCandidateActivationContract(t *testing.T) {
	participants := []match.CompetitiveParticipant{
		{PlayerID: 1, RoleID: 7, TeamID: 1},
		{PlayerID: 2, RoleID: 8, TeamID: 2},
	}
	seen := make(map[string]struct{})
	for _, mapID := range []uint32{11, 411, 12, 905, 510, 511, 512, 513, 702, 701} {
		for _, candidate := range mapdata.LookupCompetitiveBossCandidates(mapID) {
			if _, duplicate := seen[candidate.ID]; duplicate {
				continue
			}
			seen[candidate.ID] = struct{}{}
			inventories := make(map[uint16][]game.ItemInfo, len(participants))
			if candidate.Activation == mapdata.CompetitiveBossActivationAllPlayersOwn {
				if len(candidate.ItemOptions) == 0 || len(candidate.ItemOptions[0].Requirements) == 0 {
					t.Fatalf("Boss %s has no summon requirements", candidate.ID)
				}
				for _, participant := range participants {
					for _, requirement := range candidate.ItemOptions[0].Requirements {
						inventories[participant.PlayerID] = append(inventories[participant.PlayerID], game.NewPermanentItemInfo(requirement.ItemID, requirement.Count))
					}
				}
			}
			satisfied, err := competitiveBossCandidateSatisfied(candidate, participants, inventories)
			if err != nil || !satisfied {
				t.Fatalf("Boss %s complete summon set = satisfied:%t err:%v inventory:%+v", candidate.ID, satisfied, err, inventories)
			}
			if candidate.Activation == mapdata.CompetitiveBossActivationAllPlayersOwn {
				delete(inventories, participants[1].PlayerID)
				satisfied, err = competitiveBossCandidateSatisfied(candidate, participants, inventories)
				if err != nil || satisfied {
					t.Fatalf("Boss %s missing participant inventory = satisfied:%t err:%v", candidate.ID, satisfied, err)
				}
			}
		}
	}
	if len(seen) != 10 {
		t.Fatalf("Boss activation contracts covered %d IDs: %v", len(seen), seen)
	}
}

func TestKickBombMapStartsWithServerElimination(t *testing.T) {
	clientRoot := filepath.Join("..", "..", "..", "runtime", "client-patched")
	catalog, err := mapdata.LoadCatalog(clientRoot)
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	server := &Server{
		mapCatalog: catalog, logWriter: io.Discard,
		battles: make(map[uint32]*match.AdventureBattle), competitiveBattles: make(map[uint32]*match.CompetitiveBattle),
	}
	ownerProfile := game.DefaultPlayerProfile()
	ownerProfile.GameInfo.RoleID = 7
	ownerProfile.GameInfo.Point = ^uint32(0)
	owner := &connectionSession{UIN: 1_000_001, Profile: ownerProfile}
	if err = server.createSessionRoom(owner, 1, byte(roomstate.GameTypeCompetitiveNoItem)); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().AttachMember(owner.RoomID, roomstate.Member{PlayerID: 2, RoleID: 8, TeamID: 2, Ready: true}); err != nil {
		t.Fatal(err)
	}
	const kickBombMapID = uint32(701)
	if _, err = server.worldState().UpdateMatchSettings(sessionWorldUIN(owner), roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(kickBombMapID), GameType: roomstate.GameTypeCompetitiveNoItem,
	}); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[:4], owner.UIN)
	binary.BigEndian.PutUint32(payload[4:], 1)
	request := testLocalRoutedPacketWithPayload(t, game.StartGameCommand, 3, 0xffff, 1, owner.UIN, payload)
	result := server.handleMatchStartMessage(ListenerConfig{Response: ResponseConfig{
		QQTStartGameSuccess: true, QQTGameBegin: true,
	}}, owner, "test", "local", "remote", request)
	if !result.handled || len(result.response) == 0 || len(result.followUp) == 0 {
		t.Fatalf("special competitive start = handled:%t response:%d follow-up:%d result:%s", result.handled, len(result.response), len(result.followUp), result.result)
	}
	inspection, err := game.InspectLocalPacket(result.followUp)
	if err != nil {
		t.Fatal(err)
	}
	begin, err := game.ParseLengthPrefixedGameBeginDataNetwork(inspection.Payload)
	if err != nil {
		t.Fatal(err)
	}
	selected, ok := catalog.CompetitiveMap(begin.MapID)
	if !ok || selected.ID != kickBombMapID || selected.Rule != mapdata.CompetitiveRuleKickBomb || len(begin.NewItems) != 0 {
		t.Fatalf("special competitive GAME_BEGIN = %+v, map %+v, %v", begin, selected, ok)
	}
	battle, err := server.competitiveBattle(begin.GameID)
	if err != nil {
		t.Fatal(err)
	}
	owner.CurrentMapID = kickBombMapID
	if battle.RequiresArbitratorConclusion() {
		t.Fatal("kick-bomb battle still waits for an arbitrator GAME_OVER")
	}
	killedBody, err := (game.PlayerKilledEvent{PlayerInteractionEvent: game.PlayerInteractionEvent{
		PlayerID: owner.Profile.PlayerID, ClientTime: 2_500, DestinationPlayerID: 2, PosX: 4, PosY: 5,
	}}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	killedPayload, err := game.MarshalGameEventPayload(owner.UIN, 2, game.NotifyPlayerKilled, killedBody)
	if err != nil {
		t.Fatal(err)
	}
	killedPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, killedPayload)
	killedResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", killedPacket)
	if !killedResult.handled || len(killedResult.response) == 0 || len(killedResult.followUp) != 0 || killedResult.afterResponse != nil || len(killedResult.startFollowUp) == 0 ||
		!killedResult.completeCompetitiveAfterSend || killedResult.competitiveRoomSettlement == nil {
		t.Fatalf("kick-bomb last-player kill did not produce settlement: %+v", killedResult)
	}
	overPacket, err := game.InspectLocalPacket(killedResult.startFollowUp)
	if err != nil {
		t.Fatal(err)
	}
	overEvent, err := game.ParseNotifyGameEventPayload(overPacket.Payload)
	if err != nil {
		t.Fatal(err)
	}
	over, err := game.ParseGameOverData(overEvent.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(over.Results) != 2 || over.Results[0].Result != game.GameResultWin || over.Results[1].Result != game.GameResultLoss {
		t.Fatalf("kick-bomb GAME_OVER = %+v", over)
	}
	dispatch := game.DispatchBombData{
		PlayerID: owner.Profile.PlayerID, Time: 3_000,
		Bombs: []game.DispatchBomb{{SceneID: 11, Prop: 8, Row: 4, Col: 5}},
		Items: []game.DispatchBombItem{{ItemID: 20001, Row: 7, Col: 8}},
	}
	dispatchBody, err := dispatch.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	dispatchPayload, err := game.MarshalGameEventPayload(owner.UIN, 2, game.NotifyDispatchBomb, dispatchBody)
	if err != nil {
		t.Fatal(err)
	}
	dispatchPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, dispatchPayload)
	dispatchResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", dispatchPacket)
	if !dispatchResult.handled || len(dispatchResult.response) == 0 || len(dispatchResult.followUp) != 0 || dispatchResult.afterResponse != nil {
		t.Fatalf("kick-bomb dispatch = %+v", dispatchResult)
	}

	forged := dispatch
	forged.PlayerID = 2
	forgedBody, err := forged.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	forgedPayload, err := game.MarshalGameEventPayload(owner.UIN, 3, game.NotifyDispatchBomb, forgedBody)
	if err != nil {
		t.Fatal(err)
	}
	forgedPacket := testLocalRoutedPacketWithPayload(t, game.GameEventRequestCommand, 3, 0xffff, 1, owner.UIN, forgedPayload)
	forgedResult := server.handleGameEventMessage(ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner, "test", "local", "remote", forgedPacket)
	if len(forgedResult.response) != 0 || len(forgedResult.followUp) != 0 {
		t.Fatalf("forged non-arbitrator dispatch was accepted: %+v", forgedResult)
	}
}

func TestCompetitiveMapPointsGateAndVIPOverride(t *testing.T) {
	catalog, err := mapdata.LoadCatalog(filepath.Join("..", "..", "..", "runtime", "client-patched"))
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	selected, ok := catalog.CompetitiveMap(1001)
	if !ok || selected.RequiredPoints == 0 {
		t.Skip("client match01 points gate is unavailable")
	}
	server := &Server{mapCatalog: catalog, logWriter: io.Discard}
	profile := game.DefaultPlayerProfile()
	profile.GameInfo.Point = selected.RequiredPoints - 1
	owner := &connectionSession{UIN: 1_000_001, Profile: profile}
	if err = server.createSessionRoom(owner, 1, byte(roomstate.GameTypeCompetitiveNoItem)); err != nil {
		t.Fatal(err)
	}
	if err = server.validateSessionRoomMapSelection(owner, selected.ID); err == nil {
		t.Fatal("points-gated map selection unexpectedly succeeded")
	}
	owner.Profile.Inventory = append(owner.Profile.Inventory, game.NewPermanentItemInfo(vipCardItemID, 1))
	if err = server.validateSessionRoomMapSelection(owner, selected.ID); err != nil {
		t.Fatalf("VIP map override rejected: %v", err)
	}
	owner.Profile.Inventory = nil
	owner.Profile.GameInfo.Point = selected.RequiredPoints
	if err = server.validateSessionRoomMapSelection(owner, selected.ID); err != nil {
		t.Fatalf("exact map points requirement rejected: %v", err)
	}
}

func TestSinglePlayerAdmissionCardsAreServerAuthoritative(t *testing.T) {
	adventureServer := &Server{logWriter: io.Discard}
	adventureProfile := game.DefaultPlayerProfile()
	adventure := &connectionSession{UIN: 1_000_001, Profile: adventureProfile}
	if err := adventureServer.createSessionRoom(adventure, 1, byte(roomstate.GameTypeAdventure)); err != nil {
		t.Fatal(err)
	}
	adventureRoom, err := adventureServer.sessionRoom(adventure)
	if err != nil {
		t.Fatal(err)
	}
	if err = adventureServer.validateSessionRoomStart(adventure, adventureRoom); err == nil {
		t.Fatal("single-player adventure started without item 99")
	}
	adventure.Profile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(game.SinglePlayerAdventureCardItemID, 1)}
	if err = adventureServer.validateSessionRoomStart(adventure, adventureRoom); err != nil {
		t.Fatalf("single-player adventure with item 99: %v", err)
	}

	competitiveServer := &Server{logWriter: io.Discard}
	competitiveProfile := game.DefaultPlayerProfile()
	competitive := &connectionSession{UIN: 1_000_002, Profile: competitiveProfile}
	if err = competitiveServer.createSessionRoom(competitive, 1, byte(roomstate.GameTypeCompetitiveNoItem)); err != nil {
		t.Fatal(err)
	}
	competitiveRoom, err := competitiveServer.sessionRoom(competitive)
	if err != nil {
		t.Fatal(err)
	}
	if err = competitiveServer.validateSessionRoomStart(competitive, competitiveRoom); err == nil {
		t.Fatal("single-player competitive room started without the Boss card")
	}
	bossCard := game.NewPermanentItemInfo(game.SinglePlayerBossCardItemID, 1)
	bossCard.ItemStatus = game.ItemStatusCollected
	competitive.Profile.Inventory = []game.ItemInfo{bossCard}
	if err = competitiveServer.validateSessionRoomStart(competitive, competitiveRoom); err == nil {
		t.Fatal("single-player competitive room started while the Boss card was collected")
	}
	bossCard.ItemStatus = game.ItemStatusAvailable
	competitive.Profile.Inventory = []game.ItemInfo{bossCard}
	if err = competitiveServer.validateSessionRoomStart(competitive, competitiveRoom); err != nil {
		t.Fatalf("single-player competitive room with active uncollected Boss card: %v", err)
	}
}

func TestCompetitiveAIAdmissionRequiresActiveUncollectedCard(t *testing.T) {
	server := &Server{logWriter: io.Discard, config: Config{CompetitiveAI: CompetitiveAIConfig{Enabled: true}}}
	profile := game.DefaultPlayerProfile()
	owner := &connectionSession{UIN: 1_000_001, Profile: profile}
	if err := server.createSessionRoom(owner, 1, byte(roomstate.GameTypeCompetitiveNoItem)); err != nil {
		t.Fatal(err)
	}
	competitiveRoom, err := server.sessionRoom(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.validateSessionRoomStart(owner, competitiveRoom); err == nil {
		t.Fatal("competitive AI topology was deferred without the AI card")
	}

	card := game.NewPermanentItemInfo(game.CompetitiveAICardItemID, 1)
	card.ItemStatus = game.ItemStatusCollected
	owner.Profile.Inventory = []game.ItemInfo{card}
	if err = server.validateSessionRoomStart(owner, competitiveRoom); err == nil {
		t.Fatal("competitive AI topology was deferred while the AI card was collected")
	}

	card.ItemStatus = 0
	owner.Profile.Inventory = []game.ItemInfo{card}
	if err = server.validateSessionRoomStart(owner, competitiveRoom); err != nil {
		t.Fatalf("active uncollected AI card did not authorize deferred topology: %v", err)
	}
}

func TestSinglePlayerBossCardRequiresEligibleBossMap(t *testing.T) {
	clientRoot := filepath.Join("..", "..", "..", "runtime", "client-patched")
	catalog, err := mapdata.LoadCatalog(clientRoot)
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	start := func(t *testing.T, mapID uint32) matchStartMessageResult {
		t.Helper()
		server := &Server{
			mapCatalog: catalog, logWriter: io.Discard,
			battles: make(map[uint32]*match.AdventureBattle), competitiveBattles: make(map[uint32]*match.CompetitiveBattle),
		}
		profile := game.DefaultPlayerProfile()
		profile.GameInfo.RoleID = 7
		profile.GameInfo.Point = ^uint32(0)
		profile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(game.SinglePlayerBossCardItemID, 1)}
		owner := &connectionSession{UIN: 1_000_001, Profile: profile}
		if err := server.createSessionRoom(owner, 1, byte(roomstate.GameTypeCompetitiveNoItem)); err != nil {
			t.Fatal(err)
		}
		if _, err := server.worldState().UpdateMatchSettings(sessionWorldUIN(owner), roomstate.MatchSettings{
			Map: roomstate.FixedMapSelection(mapID), GameType: roomstate.GameTypeCompetitiveNoItem,
		}); err != nil {
			t.Fatal(err)
		}
		payload := make([]byte, 8)
		binary.BigEndian.PutUint32(payload[:4], owner.UIN)
		binary.BigEndian.PutUint32(payload[4:], uint32(owner.RoomID))
		request := testLocalRoutedPacketWithPayload(t, game.StartGameCommand, 3, 0xffff, 1, owner.UIN, payload)
		result := server.handleMatchStartMessage(ListenerConfig{Response: ResponseConfig{
			QQTStartGameSuccess: true, QQTGameBegin: true,
		}}, owner, "test", "local", "remote", request)
		if inventoryQuantity(owner.Profile.Inventory, game.SinglePlayerBossCardItemID) != 1 {
			t.Fatalf("single-player Boss card was consumed: %+v", owner.Profile.Inventory)
		}
		return result
	}

	ordinary := start(t, 1001)
	if !ordinary.handled || len(ordinary.response) == 0 || len(ordinary.followUp) == 0 || ordinary.result != "qqt_start_game_rejected" {
		t.Fatalf("ordinary map accepted the single-player Boss card: %+v", ordinary)
	}
	rejectionResponse, err := game.InspectLocalPacket(ordinary.response)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.BigEndian.Uint16(rejectionResponse.Payload); got != game.StartGameResultItemRequired {
		t.Fatalf("ordinary map rejection ResultID = 0x%04X, want 0x%04X", got, game.StartGameResultItemRequired)
	}
	rejectionNotice, err := game.InspectLocalPacket(ordinary.followUp)
	if err != nil {
		t.Fatal(err)
	}
	if rejectionNotice.Command != game.RoomChatNotifyCommand || binary.BigEndian.Uint16(rejectionNotice.Payload[:2]) != game.RoomChatSystemSourcePlayerID {
		t.Fatalf("ordinary map rejection notice = command 0x%04X payload %x", rejectionNotice.Command, rejectionNotice.Payload)
	}
	boss := start(t, 11)
	if !boss.handled || len(boss.response) == 0 || len(boss.followUp) == 0 || len(boss.startFollowUp) == 0 {
		t.Fatalf("eligible Water11 Boss start = handled:%t response:%d begin:%d Boss:%d result:%s", boss.handled, len(boss.response), len(boss.followUp), len(boss.startFollowUp), boss.result)
	}
}

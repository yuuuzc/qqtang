package probe

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"testing"

	"qqtang/internal/game/equipment"
	"qqtang/internal/game/itemcatalog"
	lobbystate "qqtang/internal/game/lobby"
	"qqtang/internal/game/mapdata"
	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

func TestPlayerListPrunesPresenceWithoutLivePrimarySession(t *testing.T) {
	profile := game.DefaultPlayerProfile()
	profile.PlayerID = 1
	profile.SectionID = 1
	server := &Server{liveSessions: make(map[net.Conn]*connectionSession), logWriter: io.Discard}
	section, err := server.worldState().Section(1)
	if err != nil {
		t.Fatal(err)
	}
	if err = section.UpsertPlayer(lobbystate.Presence{UIN: 1_000_001, PlayerID: 1, SectionID: 1, Nickname: "stale"}); err != nil {
		t.Fatal(err)
	}
	entries, err := server.playerListEntries(&connectionSession{UIN: 1_000_002, Profile: game.PlayerProfile{PlayerID: 2, SectionID: 1}}, game.PlayerListRequest{Number: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("stale online entries = %+v, want none", entries)
	}
	snapshot, err := server.worldState().LobbySnapshot(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Players) != 0 {
		t.Fatalf("stale lobby projection survived refresh: %+v", snapshot.Players)
	}
}

func TestFindFriendReturnsOfflineProfileAfterStalePlayerListPrune(t *testing.T) {
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	const friendUIN uint32 = 1_000_002
	friend := game.DefaultPlayerProfile()
	friend.PlayerID = 2
	friend.SectionID = 1
	friend.Nickname = "糖二"
	if err = store.Save(context.Background(), friendUIN, friend); err != nil {
		t.Fatal(err)
	}

	server := &Server{
		playerStore: store, liveSessions: make(map[net.Conn]*connectionSession), logWriter: io.Discard,
	}
	section, err := server.worldState().Section(1)
	if err != nil {
		t.Fatal(err)
	}
	if err = section.UpsertPlayer(lobbystate.Presence{
		UIN: friendUIN, PlayerID: friend.PlayerID, SectionID: friend.SectionID, Nickname: friend.Nickname,
	}); err != nil {
		t.Fatal(err)
	}
	requester := &connectionSession{
		UIN: 1_000_001, Profile: game.PlayerProfile{PlayerID: 1, SectionID: 1},
	}

	// The periodic list response heals server state but the legacy client keeps
	// its old row because that response is merge-only.
	if entries, listErr := server.playerListEntries(requester, game.PlayerListRequest{Number: 30}); listErr != nil {
		t.Fatal(listErr)
	} else if len(entries) != 0 {
		t.Fatalf("player-list entries = %+v, want stale target omitted", entries)
	}

	response, err := server.findFriendResponse(requester, friendUIN)
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != game.FindFriendStatusOffline {
		t.Fatalf("find-friend status = %d, want offline", response.Status)
	}
	if response.FriendUIN != friendUIN || response.PlayerID != friend.PlayerID || response.SectionID != friend.SectionID || response.PlayerName != friend.Nickname {
		t.Fatalf("offline identity = %+v", response)
	}
	if response.RoomID != 0 || response.RoomName != "" {
		t.Fatalf("offline room projection = %d/%q, want empty", response.RoomID, response.RoomName)
	}
	if _, err = response.MarshalNetworkBinary(); err != nil {
		t.Fatalf("offline response is not serializable: %v", err)
	}
}

func TestPlayerListProjectsRemoteRoleLoadoutIntoExternalItems(t *testing.T) {
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	catalog, err := equipment.NewCatalog([]itemcatalog.Entry{
		{ID: 22, Index: 4, Name: "hat", Categories: []string{"cap"}},
		{ID: 12087, Index: 92, Name: "card", Categories: []string{"namecard"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	targetProfile := game.DefaultPlayerProfile()
	targetProfile.PlayerID = 1
	targetProfile.SectionID = 1
	targetProfile.GameInfo.RoleID = 7
	targetProfile.Inventory = []game.ItemInfo{
		game.NewPermanentItemInfo(22, 1), game.NewPermanentItemInfo(12087, 1),
	}
	if err := store.Save(context.Background(), 1_000_001, targetProfile); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyEquipmentChanges(context.Background(), 1_000_001, []equipment.Change{
		{RoleID: 7, Slot: equipment.SlotHeadFront, ItemID: 22, Equipped: true},
		{RoleID: 7, Slot: equipment.SlotNamecard, ItemID: 12087, Equipped: true},
	}); err != nil {
		t.Fatal(err)
	}
	// Role choice is live session state and intentionally reloads as zero from
	// durable storage. The list requester is a different player, so this test
	// exercises the remote projection that the real lobby uses.
	durable, err := store.Load(context.Background(), 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	if durable.GameInfo.RoleID != 0 {
		t.Fatalf("durable selected role = %d, want non-persisted zero", durable.GameInfo.RoleID)
	}
	target := &connectionSession{UIN: 1_000_001, Profile: targetProfile}
	target.setSelectedRoleID(7)
	target.setUIN(target.UIN)
	server := &Server{
		playerStore: store, equipmentCatalog: catalog,
		liveSessions: map[net.Conn]*connectionSession{nil: target}, logWriter: io.Discard,
	}
	section, err := server.worldState().Section(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := section.UpsertPlayer(lobbystate.Presence{UIN: 1_000_001, PlayerID: 1, SectionID: 1, Nickname: "糖一"}); err != nil {
		t.Fatal(err)
	}
	requester := &connectionSession{
		UIN: 1_000_002, Profile: game.PlayerProfile{PlayerID: 2, SectionID: 1},
	}
	entries, err := server.playerListEntries(requester, game.PlayerListRequest{Number: 30})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Player.GameInfo.RoleID != 7 || len(entries[0].Player.ExtItemIDs) != 2 || entries[0].Player.ExtItemIDs[1] != 12087 {
		t.Fatalf("player-list equipment projection = %+v", entries)
	}
	friend, err := server.findFriendResponse(requester, 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	if friend.Status != game.FindFriendStatusOnline || friend.GameInfo.RoleID != 7 {
		t.Fatalf("online find-friend role projection = %+v", friend)
	}
}

func TestRoomListFilterDimensions(t *testing.T) {
	adventure := lobbystate.RoomSummary{
		RoomID: 1, OwnerID: 1, GameType: byte(roomstate.GameTypeAdventure),
		Players: 1, MaxPlayers: 4, Phase: lobbystate.RoomPhasePreparing,
	}
	if !roomMatchesListRequest(adventure, game.RoomListRequest{GameType: byte(roomstate.GameTypeAdventure), GameMode: game.RoomListFilterAllRooms}) {
		t.Fatal("all-rooms filter hid preparing adventure room")
	}
	if !roomMatchesListRequest(adventure, game.RoomListRequest{GameType: byte(roomstate.GameTypeAdventure), GameMode: game.RoomListFilterAllMaps}) {
		t.Fatal("all-maps filter hid preparing adventure room")
	}
	if !roomMatchesListRequest(adventure, game.RoomListRequest{GameType: byte(roomstate.GameTypeAdventure), GameMode: game.RoomListFilterLegacyAllMaps}) {
		t.Fatal("legacy all-maps filter hid preparing adventure room")
	}
	if roomMatchesListRequest(adventure, game.RoomListRequest{GameType: byte(roomstate.GameTypeAdventure), GameMode: game.RoomListFilterNormal}) {
		t.Fatal("competitive normal filter matched an adventure room")
	}
	playing := adventure
	playing.Phase = lobbystate.RoomPhaseInMatch
	if !roomMatchesListRequest(playing, game.RoomListRequest{GameType: byte(roomstate.GameTypeAdventure), GameMode: game.RoomListFilterAllRooms}) {
		t.Fatal("all-rooms filter hid in-match room")
	}
	if roomMatchesListRequest(playing, game.RoomListRequest{GameType: byte(roomstate.GameTypeAdventure), GameMode: game.RoomListFilterAllMaps}) {
		t.Fatal("all-maps filter exposed in-match room")
	}
	competitive := adventure
	competitive.GameType = byte(roomstate.GameTypeCompetitiveNoItem)
	competitive.GameMode = 1
	if !roomMatchesListRequest(competitive, game.RoomListRequest{GameType: byte(roomstate.GameTypeCompetitiveNoItem), GameMode: game.RoomListFilterNormal}) {
		t.Fatal("normal competitive filter hid matching normal room")
	}
}

func TestEverySelectableCompetitiveMapProjectsNativeRuleIntoSpecificRoomFilter(t *testing.T) {
	catalog, err := mapdata.LoadCatalog(filepath.Join("..", "..", "..", "runtime", "client-patched"))
	if err != nil {
		t.Skipf("verified runtime client is unavailable: %v", err)
	}
	mapIDs := catalog.CompetitiveIDs()
	if len(mapIDs) == 0 {
		t.Skip("installed client has no selectable competitive maps")
	}
	coveredRules := make(map[uint32]int)
	for _, mapID := range catalog.CompetitiveIDs() {
		selected, ok := catalog.CompetitiveMap(mapID)
		if !ok || !selected.Selectable {
			t.Fatalf("selectable competitive map %d disappeared from the catalog", mapID)
		}
		if selected.NativeRule == 0 || selected.NativeRule > 0xfd {
			t.Fatalf("map %d native rule %d cannot be represented by the client filter", selected.ID, selected.NativeRule)
		}
		if selected.RequiredItemField > 1 {
			t.Fatalf("map %d item field %d is outside the client tabs", selected.ID, selected.RequiredItemField)
		}
		gameType := roomstate.GameTypeCompetitiveNoItem
		if selected.RequiredItemField == 1 {
			gameType = roomstate.GameTypeCompetitiveItem
		}
		server := &Server{mapCatalog: catalog, logWriter: io.Discard}
		profile := game.DefaultPlayerProfile()
		profile.GameInfo.Point = ^uint32(0)
		session := &connectionSession{UIN: 1_000_001, Profile: profile}
		if err = server.createSessionRoom(session, 1, byte(gameType)); err != nil {
			t.Fatalf("map %d create room: %v", selected.ID, err)
		}
		if err = server.updateSessionRoomSettings(session, selected.ID, byte(selected.NativeRule), 0); err != nil {
			t.Fatalf("map %d project settings: %v", selected.ID, err)
		}
		snapshot, snapshotErr := server.worldState().LobbySnapshot(profile.SectionID, 0)
		if snapshotErr != nil {
			t.Fatalf("map %d lobby snapshot: %v", selected.ID, snapshotErr)
		}
		if len(snapshot.Rooms) != 1 || snapshot.Rooms[0].GameMode != byte(selected.NativeRule) {
			t.Fatalf("map %d lobby native mode = %+v, want %d", selected.ID, snapshot.Rooms, selected.NativeRule)
		}
		filter := game.RoomListGameModeFilter(selected.NativeRule + 2)
		if decoded, specific := filter.CompetitiveMode(); !specific || decoded != byte(selected.NativeRule) {
			t.Fatalf("map %d filter %d decodes to %d/%t, want %d/true", selected.ID, filter, decoded, specific, selected.NativeRule)
		}
		entries, listErr := server.roomListPage(session, game.RoomListRequest{
			GameType: byte(gameType), GameMode: filter, Number: 10,
		})
		if listErr != nil || len(entries) != 1 || entries[0].MapID != uint16(selected.ID) {
			t.Fatalf("map %d rule %d filtered rooms = %+v, %v", selected.ID, selected.NativeRule, entries, listErr)
		}
		coveredRules[selected.NativeRule]++
	}
	if len(coveredRules) < 2 {
		t.Fatalf("catalog exercised only native rules %v", coveredRules)
	}
}

func TestRoomListEntryUsesDynamicOpenSeatCapacity(t *testing.T) {
	entry, err := roomListEntry(lobbystate.RoomSummary{
		RoomID: 1, OwnerID: 1, GameType: byte(roomstate.GameTypeAdventure),
		Players: 2, MaxPlayers: 5, Phase: lobbystate.RoomPhasePreparing,
	})
	if err != nil {
		t.Fatal(err)
	}
	current, capacity := game.UnpackRoomPopulation(entry.NumOfPlayer)
	if current != 2 || capacity != 5 {
		t.Fatalf("room population = %d/%d, want 2/5", current, capacity)
	}
}

func TestRoomListEntryPacksNativePasswordAndRuleBits(t *testing.T) {
	entry, err := roomListEntry(lobbystate.RoomSummary{
		RoomID: 1, OwnerID: 1, GameType: byte(roomstate.GameTypeCompetitiveNoItem),
		Players: 2, MaxPlayers: 8, Phase: lobbystate.RoomPhasePreparing,
		HasPassword: true, UsesFreeRule: true, IsVIP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := game.RoomListFlagPreparing | game.RoomListFlagPassword | game.RoomListFlagFreeRule | game.RoomListFlagVIP
	if entry.RoomFlag != want {
		t.Fatalf("ROOM_INFO flags = 0x%02X, want 0x%02X", entry.RoomFlag, want)
	}

	entry, err = roomListEntry(lobbystate.RoomSummary{
		RoomID: 1, OwnerID: 1, GameType: byte(roomstate.GameTypeCompetitiveNoItem),
		Players: 2, MaxPlayers: 8, Phase: lobbystate.RoomPhaseInMatch,
		HasPassword: true, UsesFreeRule: true, IsVIP: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want = game.RoomListFlagInMatch | game.RoomListFlagPassword | game.RoomListFlagFreeRule | game.RoomListFlagVIP
	if entry.RoomFlag != want {
		t.Fatalf("in-match ROOM_INFO flags = 0x%02X, want 0x%02X", entry.RoomFlag, want)
	}
}

func TestRoomListEntryUsesNativeVisibleCardStates(t *testing.T) {
	inMatch, err := roomListEntry(lobbystate.RoomSummary{
		RoomID: 1, OwnerID: 1, GameType: byte(roomstate.GameTypeAdventure),
		Players: 1, MaxPlayers: 4, Phase: lobbystate.RoomPhaseInMatch,
	})
	if err != nil {
		t.Fatal(err)
	}
	if inMatch.RoomFlag != game.RoomListFlagInMatch {
		t.Fatalf("plain in-match ROOM_INFO flag = %d, want visible disabled state %d", inMatch.RoomFlag, game.RoomListFlagInMatch)
	}

	full, err := roomListEntry(lobbystate.RoomSummary{
		RoomID: 2, OwnerID: 1, GameType: byte(roomstate.GameTypeCompetitiveNoItem),
		Players: 8, MaxPlayers: 8, Phase: lobbystate.RoomPhasePreparing,
	})
	if err != nil {
		t.Fatal(err)
	}
	if full.RoomFlag != game.RoomListFlagPreparing {
		t.Fatalf("full preparing ROOM_INFO flag = %d, want preparing marker %d", full.RoomFlag, game.RoomListFlagPreparing)
	}
	current, capacity := game.UnpackRoomPopulation(full.NumOfPlayer)
	if current != capacity || current != 8 {
		t.Fatalf("full ROOM_INFO population = %d/%d, want 8/8", current, capacity)
	}
}

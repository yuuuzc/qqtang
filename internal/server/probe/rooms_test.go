package probe

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"net"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"qqtang/internal/game/equipment"
	"qqtang/internal/game/itemcatalog"
	"qqtang/internal/game/itemeffect"
	lobbystate "qqtang/internal/game/lobby"
	"qqtang/internal/game/match"
	"qqtang/internal/game/roledata"
	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/capture"
	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

func TestShopSaveTransfersRoleLoadoutToSelectedRole(t *testing.T) {
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	profile := game.DefaultPlayerProfile()
	profile.GameInfo.RoleID = 7
	profile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(22, 3)}
	if _, err := store.LoadOrCreate(context.Background(), 1_000_001, profile); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyEquipmentChanges(context.Background(), 1_000_001, []equipment.Change{{
		RoleID: 1, Slot: equipment.SlotHeadFront, ItemID: 22, Equipped: true,
	}}); err != nil {
		t.Fatal(err)
	}
	catalog, err := equipment.NewCatalog([]itemcatalog.Entry{{ID: 22, Index: 4, Name: "hat", Categories: []string{"cap"}}})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{playerStore: store, equipmentCatalog: catalog}
	session := &connectionSession{UIN: 1_000_001, Profile: profile}
	request := game.ItemStatusChangeRequest{UIN: 1_000_001, Items: []game.ItemStatusChange{{ItemID: 22, NewStatus: 1, NewRoleID: 7}}}
	if err := server.updateSessionItemStatuses(session, request); err != nil {
		t.Fatal(err)
	}
	assignments, err := store.LoadEquipment(context.Background(), 1_000_001)
	if err != nil || len(assignments) != 1 || assignments[0].RoleID != 7 {
		t.Fatalf("saved role loadouts = %+v, %v", assignments, err)
	}
	if session.Profile.Inventory[0].ItemStatus != 1 || session.Profile.Inventory[0].ItemRoleID != 7 {
		t.Fatalf("transferred legacy projection = %+v", session.Profile.Inventory[0])
	}
	// 收藏柜 is an ownership flag, not another inventory. Collecting an
	// equipped cosmetic must remove its loadout and survive a reload as status
	// 2; restoring it returns to status 0 without silently re-equipping it.
	request.Items[0].NewStatus = game.ItemStatusCollected
	request.Items[0].NewRoleID = 0
	if err := server.updateSessionItemStatuses(session, request); err != nil {
		t.Fatal(err)
	}
	assignments, err = store.LoadEquipment(context.Background(), 1_000_001)
	if err != nil || len(assignments) != 0 {
		t.Fatalf("loadout after role 7 removal = %+v, %v", assignments, err)
	}
	if session.Profile.Inventory[0].NumOfItem != 3 || session.Profile.Inventory[0].ItemStatus != game.ItemStatusCollected || session.Profile.Inventory[0].ItemRoleID != 0 {
		t.Fatalf("collected projection = %+v", session.Profile.Inventory[0])
	}
	reloaded, err := store.Load(context.Background(), 1_000_001)
	if err != nil || reloaded.Inventory[0].NumOfItem != 3 || reloaded.Inventory[0].ItemStatus != game.ItemStatusCollected {
		t.Fatalf("persisted collected item = %+v, %v", reloaded.Inventory, err)
	}
	request.Items[0].NewStatus = game.ItemStatusAvailable
	if err := server.updateSessionItemStatuses(session, request); err != nil {
		t.Fatal(err)
	}
	reloaded, err = store.Load(context.Background(), 1_000_001)
	if err != nil || reloaded.Inventory[0].ItemStatus != game.ItemStatusAvailable {
		t.Fatalf("restored collected item = %+v, %v", reloaded.Inventory, err)
	}
}

func TestAuxiliaryShopConnectionSavesIntoPrimaryWithoutClaimingSecondLogin(t *testing.T) {
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	const uin uint32 = 1_000_001
	profile := game.DefaultPlayerProfile()
	profile.GameInfo.RoleID = 7
	profile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(22, 1)}
	if err := store.Save(context.Background(), uin, profile); err != nil {
		t.Fatal(err)
	}
	catalog, err := equipment.NewCatalog([]itemcatalog.Entry{{ID: 22, Index: 4, Name: "hat", RegistryCategory: "cap", Categories: []string{"cap"}}})
	if err != nil {
		t.Fatal(err)
	}
	primary := &connectionSession{UIN: uin, Profile: profile, remoteAddress: "192.0.2.10:40000"}
	primary.liveUIN.Store(uin)
	shop := &connectionSession{Profile: game.DefaultPlayerProfile(), remoteAddress: "192.0.2.10:40001"}
	server := &Server{
		playerStore: store, equipmentCatalog: catalog,
		liveSessions: map[net.Conn]*connectionSession{nil: primary},
	}
	if err := server.bindAuxiliaryShopConnection(shop, uin); err != nil {
		t.Fatal(err)
	}
	if !shop.auxiliary || shop.UIN != uin || shop.liveUIN.Load() != 0 {
		t.Fatalf("shop binding = auxiliary:%v UIN:%d liveUIN:%d", shop.auxiliary, shop.UIN, shop.liveUIN.Load())
	}
	if err := server.updateSessionItemStatuses(shop, game.ItemStatusChangeRequest{
		UIN: uin, Items: []game.ItemStatusChange{{ItemID: 22, NewStatus: 1, NewRoleID: 7}},
	}); err != nil {
		t.Fatal(err)
	}
	if primary.Profile.Inventory[0].ItemStatus != 1 || primary.Profile.Inventory[0].ItemRoleID != 7 {
		t.Fatalf("primary profile did not receive shop save: %+v", primary.Profile.Inventory[0])
	}
	assignments, err := store.LoadEquipment(context.Background(), uin)
	if err != nil || len(assignments) != 1 || assignments[0].RoleID != 7 || assignments[0].ItemID != 22 {
		t.Fatalf("persisted assignments = %+v, %v", assignments, err)
	}
}

func TestEnterSessionRoomBuildsTwoPlayerSnapshot(t *testing.T) {
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ownerProfile := game.DefaultPlayerProfile()
	ownerProfile.Nickname = "糖二"
	ownerProfile.PlayerID = 2
	joinerProfile := game.DefaultPlayerProfile()
	joinerProfile.Nickname = "糖一"
	joinerProfile.PlayerID = 1
	joinerProfile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(22, 1)}
	const ownerUIN uint32 = 1_000_002
	const joinerUIN uint32 = 1_000_001
	for uin, profile := range map[uint32]game.PlayerProfile{ownerUIN: ownerProfile, joinerUIN: joinerProfile} {
		if err := store.Save(context.Background(), uin, profile); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.ApplyEquipmentChanges(context.Background(), joinerUIN, []equipment.Change{{
		RoleID: 1, Slot: equipment.SlotHeadFront, ItemID: 22, Equipped: true,
	}}); err != nil {
		t.Fatal(err)
	}
	catalog, err := equipment.NewCatalog([]itemcatalog.Entry{{ID: 22, Index: 4, Name: "hat", Categories: []string{"cap"}}})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		battles:   make(map[uint32]*match.AdventureBattle),
		logWriter: io.Discard, playerStore: store, equipmentCatalog: catalog,
	}
	section, err := server.worldState().Section(1)
	if err != nil {
		t.Fatal(err)
	}
	if err := section.UpsertPlayer(lobbystate.Presence{UIN: ownerUIN, PlayerID: 2, SectionID: 1, Nickname: "糖二"}); err != nil {
		t.Fatal(err)
	}
	if err := section.UpsertPlayer(lobbystate.Presence{UIN: joinerUIN, PlayerID: 1, SectionID: 1, Nickname: "糖一"}); err != nil {
		t.Fatal(err)
	}
	owner := &connectionSession{UIN: ownerUIN, Profile: ownerProfile}
	if err := server.createSessionRoom(owner, 1, byte(roomstate.GameTypeAdventure)); err != nil {
		t.Fatal(err)
	}
	joiner := &connectionSession{UIN: joinerUIN, Profile: joinerProfile}
	response, newlyJoined, err := server.enterSessionRoom(joiner, game.EnterRoomRequest{UIN: joinerUIN, RoomID: 1, RoleID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !newlyJoined {
		t.Fatal("first room entry was reported as a retained re-entry")
	}
	if joiner.RoomID != 1 || len(response.Players) != 2 || response.LocalSeatID != 2 || response.LocalTeamID != 1 {
		t.Fatalf("enter-room response = %+v", response)
	}
	if response.SeatStatus != [8]game.EnterRoomSeatStatus{game.EnterRoomSeatOccupied, game.EnterRoomSeatOccupied, game.EnterRoomSeatOpen, game.EnterRoomSeatOpen, game.EnterRoomSeatOpen, game.EnterRoomSeatOpen, game.EnterRoomSeatOpen, game.EnterRoomSeatOpen} {
		t.Fatalf("seat status = %v", response.SeatStatus)
	}
	if response.Players[0].Player.PlayerID != 2 || response.Players[0].Player.SeatID != 1 || response.Players[1].Player.PlayerID != 1 || response.Players[1].Player.SeatID != 2 {
		t.Fatalf("enter-room wire order is not seat order: %+v", response.Players)
	}
	if len(response.Players[1].Player.Items) != 1 || response.Players[1].Player.Items[0].ItemStatus != 1 || response.Players[1].Player.Items[0].ItemRoleID != 1 {
		t.Fatalf("joiner room equipment projection = %+v", response.Players[1].Player.Items)
	}
	if got := section.Snapshot(0); len(got.Rooms) != 1 || got.Rooms[0].Players != 2 {
		t.Fatalf("lobby snapshot = %+v", got)
	}
}

func TestEnterRoomMembersKeepsRejoiningLocalPlayerLast(t *testing.T) {
	snapshot := roomstate.Snapshot{
		OwnerID: 1,
		Members: []roomstate.Member{
			{PlayerID: 1, SeatID: 2},
			{PlayerID: 2, SeatID: 1},
			{PlayerID: 3, SeatID: 3},
		},
	}
	members := enterRoomMembers(snapshot, 2)
	if members[0].PlayerID != 1 || members[1].PlayerID != 3 || members[2].PlayerID != 2 {
		t.Fatalf("enter-room members = %+v", members)
	}
}

func testRoomServerSession(t *testing.T) (*Server, *connectionSession) {
	t.Helper()
	server := &Server{
		battles:   make(map[uint32]*match.AdventureBattle),
		logWriter: io.Discard,
	}
	profile := game.DefaultPlayerProfile()
	profile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(game.SinglePlayerAdventureCardItemID, 1)}
	session := &connectionSession{UIN: 1_000_001, Profile: profile}
	if err := server.createSessionRoom(session, 1, 2); err != nil {
		t.Fatal(err)
	}
	session.CurrentGameID = 77
	session.CurrentMapID = 1649
	participants, err := server.localAdventureParticipants(session)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.replaceAdventureBattle(77, 1649, profile.PlayerID, participants); err != nil {
		t.Fatal(err)
	}
	if err := server.startSessionRoomMatch(session); err != nil {
		t.Fatal(err)
	}
	return server, session
}

func TestModifyRoomAppliesExplicitStandardFreePropertyIndependentlyOfChangeMask(t *testing.T) {
	server := &Server{logWriter: io.Discard}
	session := &connectionSession{UIN: 1_000_001, Profile: game.DefaultPlayerProfile()}
	if err := server.createSessionRoom(session, 1, byte(roomstate.GameTypeCompetitiveNoItem)); err != nil {
		t.Fatal(err)
	}
	if _, err := server.worldState().UpdateRoomProperties(sessionWorldUIN(session), roomstate.Properties{
		Flag: roomstate.PropertyFlagFreeRule,
	}); err != nil {
		t.Fatal(err)
	}

	// This is the exact native shape observed when the owner selected Standard:
	// ModifyFlag only reports an unchanged/empty-name operation, while the
	// statically repaired producer carries the complete property in RoomFlag.
	standard, err := server.updateSessionRoomProperties(session, game.ModifyRoomRequest{
		UIN: session.UIN, Flags: game.ModifyRoomNameChanged, RoomFlag: byte(game.RoomPropertyStandard),
	})
	if err != nil {
		t.Fatal(err)
	}
	if standard.Properties.UsesFreeRule() {
		t.Fatalf("standard request retained free rule: %+v", standard.Properties)
	}

	// A rule-only change legitimately has a zero native change mask.
	free, err := server.updateSessionRoomProperties(session, game.ModifyRoomRequest{
		UIN: session.UIN, RoomFlag: byte(game.RoomPropertyFreeRule),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !free.Properties.UsesFreeRule() {
		t.Fatalf("free request did not apply explicit RoomFlag: %+v", free.Properties)
	}
}

func TestServerSelectedRoomRoleFlowsIntoAdventureParticipant(t *testing.T) {
	server := &Server{
		battles:   make(map[uint32]*match.AdventureBattle),
		logWriter: io.Discard,
	}
	profile := game.DefaultPlayerProfile()
	session := &connectionSession{UIN: 1_000_001, Profile: profile}
	if err := server.createSessionRoom(session, 1, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := server.updateSessionRoomRole(session, 5); err != nil {
		t.Fatal(err)
	}
	participants, err := server.localAdventureParticipants(session)
	if err != nil {
		t.Fatal(err)
	}
	if len(participants) != 1 || participants[0].RoleID != 5 {
		t.Fatalf("adventure participants = %+v, want selected room role 5", participants)
	}
	if session.Profile.GameInfo.RoleID != 5 {
		t.Fatalf("session profile role = %d, want selected role 5", session.Profile.GameInfo.RoleID)
	}
}

func TestServerSelectedRoomTeamProjectsToAdventureCooperativeTeam(t *testing.T) {
	server := &Server{
		battles:   make(map[uint32]*match.AdventureBattle),
		logWriter: io.Discard,
	}
	session := &connectionSession{UIN: 1_000_001, Profile: game.DefaultPlayerProfile()}
	if err := server.createSessionRoom(session, 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := server.updateSessionRoomTeam(session, 6); err != nil {
		t.Fatal(err)
	}
	participants, err := server.localAdventureParticipants(session)
	if err != nil {
		t.Fatal(err)
	}
	if len(participants) != 1 || participants[0].TeamID != adventureCooperativeTeamID {
		t.Fatalf("adventure participants = %+v, want cooperative TeamID %d", participants, adventureCooperativeTeamID)
	}
	state, err := server.sessionRoom(session)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Snapshot().Members[0].TeamID; got != 6 {
		t.Fatalf("room member TeamID = %d, want unchanged room colour 6", got)
	}
	if session.Profile.GameInfo.RoleID != game.DefaultPlayerProfile().GameInfo.RoleID {
		t.Fatal("room TeamID must not be persisted into player profile fields")
	}
}

func TestServerRandomRolePlaceholderStaysInRoomAndBecomesConcreteAtMatchSnapshot(t *testing.T) {
	rules, err := roledata.LoadRules(filepath.Join("..", "..", "..", "configs", "role-rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{
		battles:   make(map[uint32]*match.AdventureBattle),
		logWriter: io.Discard, roleRules: rules,
	}
	session := &connectionSession{UIN: 1_000_001, Profile: game.DefaultPlayerProfile()}
	if err := server.createSessionRoom(session, 1, 2); err != nil {
		t.Fatal(err)
	}
	selected, err := server.updateSessionRoomRole(session, 23)
	if err != nil {
		t.Fatal(err)
	}
	if selected != 23 || session.selectedRoleID() != 23 || session.Profile.GameInfo.RoleID != 23 {
		t.Fatalf("room random selection = %d, session role = %d, wire profile role = %d", selected, session.selectedRoleID(), session.Profile.GameInfo.RoleID)
	}
	state, err := server.sessionRoom(session)
	if err != nil || state.Snapshot().Members[0].RoleID != 23 {
		t.Fatalf("room random member = %+v, %v", state.Snapshot().Members, err)
	}
	participants, err := server.localAdventureParticipants(session)
	if err != nil {
		t.Fatal(err)
	}
	if len(participants) != 1 || participants[0].RoleID == 0 || participants[0].RoleID == 23 {
		t.Fatalf("adventure participants = %+v, want one concrete role", participants)
	}
	if state.Snapshot().Members[0].RoleID != 23 {
		t.Fatalf("match snapshot rewrote room random selection: %+v", state.Snapshot().Members)
	}
}

func TestServerSelectedRoomRoleIsSessionScoped(t *testing.T) {
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	profile := game.DefaultPlayerProfile()
	const uin = 1_000_001
	if err := store.Save(context.Background(), uin, profile); err != nil {
		t.Fatal(err)
	}
	server := &Server{
		battles:   make(map[uint32]*match.AdventureBattle),
		logWriter: io.Discard, playerStore: store,
	}
	session := &connectionSession{UIN: uin, Profile: profile}
	if err := server.createSessionRoom(session, 1, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := server.updateSessionRoomRole(session, 5); err != nil {
		t.Fatal(err)
	}
	reloaded, err := store.LoadOrCreate(context.Background(), uin, game.DefaultPlayerProfile())
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.GameInfo.RoleID != 0 {
		t.Fatalf("reloaded profile role = %d, want non-persisted zero", reloaded.GameInfo.RoleID)
	}
	if _, err := server.leaveSessionRoom(session, roomstate.LeaveVoluntary); err != nil {
		t.Fatal(err)
	}
	// A subsequent REQUEST_LOGIN supplies the new session's current role. The
	// focused room test projects that decoded value directly.
	reconnected := &connectionSession{UIN: uin, Profile: reloaded}
	reconnected.setSelectedRoleID(9)
	if err := server.createSessionRoom(reconnected, 2, 2); err != nil {
		t.Fatal(err)
	}
	participants, err := server.localAdventureParticipants(reconnected)
	if err != nil {
		t.Fatal(err)
	}
	if len(participants) != 1 || participants[0].RoleID != 9 {
		t.Fatalf("reconnected participants = %+v, want new login role 9", participants)
	}
}

func TestServerNewRoomDoesNotInheritPreviousFixedMap(t *testing.T) {
	server := &Server{
		battles:   make(map[uint32]*match.AdventureBattle),
		logWriter: io.Discard,
	}
	session := &connectionSession{UIN: 1_000_001, Profile: game.DefaultPlayerProfile()}
	if err := server.createSessionRoom(session, 1, 2); err != nil {
		t.Fatal(err)
	}
	state, err := server.sessionRoom(session)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Snapshot().Settings.Map; got != roomstate.RandomMapSelection() {
		t.Fatalf("new room map selection = %+v, want random", got)
	}
	const selectedMapID uint32 = 1
	const selectedMatchGameType byte = 23
	if err := server.updateSessionRoomSettings(session, selectedMapID, selectedMatchGameType, 0); err != nil {
		t.Fatal(err)
	}
	settings := state.Snapshot().Settings
	if got := settings.Map; got != roomstate.FixedMapSelection(selectedMapID) {
		t.Fatalf("updated room map selection = %+v", got)
	}
	if settings.GameType != roomstate.GameTypeAdventure || settings.SelectedMapType != roomstate.SelectedMapType(selectedMatchGameType) {
		t.Fatalf("room game-type domains were merged: %+v", settings)
	}
	if settings.RoomListGameType() != byte(roomstate.GameTypeAdventure) {
		t.Fatalf("fixed-map ROOM_INFO game type = %d, want adventure game type %d", settings.RoomListGameType(), roomstate.GameTypeAdventure)
	}
	if _, err := server.leaveSessionRoom(session, roomstate.LeaveVoluntary); err != nil {
		t.Fatal(err)
	}
	if err := server.createSessionRoom(session, 2, 2); err != nil {
		t.Fatal(err)
	}
	state, err = server.sessionRoom(session)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Snapshot().Settings.Map; got != roomstate.RandomMapSelection() {
		t.Fatalf("recreated room inherited old map selection: %+v", got)
	}
}

func TestLogoutSessionRemovesRoomAndLobbyPresence(t *testing.T) {
	server := &Server{
		battles:   make(map[uint32]*match.AdventureBattle),
		logWriter: io.Discard,
	}
	profile := game.DefaultPlayerProfile()
	const uin uint32 = 1_000_001
	session := &connectionSession{UIN: uin, Profile: profile}
	session.setUIN(uin)
	section, err := server.worldState().Section(profile.SectionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = section.UpsertPlayer(lobbystate.Presence{UIN: uin, PlayerID: profile.PlayerID, SectionID: profile.SectionID}); err != nil {
		t.Fatal(err)
	}
	if err = server.createSessionRoom(session, 1, byte(roomstate.GameTypeAdventure)); err != nil {
		t.Fatal(err)
	}
	if err = server.logoutSession(session, "test"); err != nil {
		t.Fatal(err)
	}
	if session.UIN != 0 || session.RoomID != 0 || session.liveUIN.Load() != 0 || session.liveRoomID.Load() != 0 {
		t.Fatalf("session was not cleared: %+v", session)
	}
	_, roomExists := server.worldState().Room(1)
	if roomExists {
		t.Fatal("logged-out owner's empty room remained active")
	}
	if snapshot := section.Snapshot(0); len(snapshot.Players) != 0 || len(snapshot.Rooms) != 0 {
		t.Fatalf("lobby state after logout = %+v", snapshot)
	}
}

func TestLogoutSessionRetainsNavigationWithoutTransportSnapshot(t *testing.T) {
	server := &Server{
		battles:             make(map[uint32]*match.AdventureBattle),
		liveSessions:        make(map[net.Conn]*connectionSession),
		authenticationState: newAuthenticationState(),
		logWriter:           io.Discard,
	}
	profile := game.DefaultPlayerProfile()
	const uin uint32 = 1_000_001
	session := &connectionSession{
		UIN: uin, Profile: profile, remoteAddress: "192.0.2.10:41000",
		CurrentMapID: 7, CurrentGameID: 8,
	}
	session.setUIN(uin)
	section, err := server.worldState().Section(profile.SectionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = section.UpsertPlayer(lobbystate.Presence{UIN: uin, PlayerID: profile.PlayerID, SectionID: profile.SectionID}); err != nil {
		t.Fatal(err)
	}
	if err = server.logoutSession(session, "test"); err != nil {
		t.Fatal(err)
	}
	if session.UIN != 0 || session.liveUIN.Load() != 0 {
		t.Fatalf("logged-out session retained live ownership: UIN=%d live=%d", session.UIN, session.liveUIN.Load())
	}
	key := accountAuthGrant{RemoteHost: "192.0.2.10", UIN: uin}
	server.authMu.Lock()
	navigationExpiry, navigationPresent := server.navigationAuth[key]
	server.authMu.Unlock()
	if !navigationPresent || !time.Now().Before(navigationExpiry) {
		t.Fatal("logout did not retain a valid account-navigation grant")
	}
	if server.claimLiveUINFromLease(&connectionSession{}, "192.0.2.10:42000", uin) {
		t.Fatal("district logout retained a transport snapshot")
	}
}

func TestServerAdventureParticipantsComeFromRoomSnapshot(t *testing.T) {
	ownerServer, ownerClient := net.Pipe()
	peerServer, peerClient := net.Pipe()
	defer ownerServer.Close()
	defer ownerClient.Close()
	defer peerServer.Close()
	defer peerClient.Close()
	server := &Server{
		battles:      make(map[uint32]*match.AdventureBattle),
		liveSessions: make(map[net.Conn]*connectionSession), logWriter: io.Discard,
	}
	profile := game.DefaultPlayerProfile()
	profile.GameInfo.ExtPoint = 5_200_000
	session := &connectionSession{UIN: 1_000_001, Profile: profile}
	session.liveUIN.Store(session.UIN)
	server.liveSessions[ownerServer] = session
	if err := server.createSessionRoom(session, 1, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := server.worldState().AttachMember(session.RoomID, roomstate.Member{PlayerID: 2, RoleID: 9, TeamID: 2}); err != nil {
		t.Fatal(err)
	}
	peerProfile := game.DefaultPlayerProfile()
	peerProfile.PlayerID = 2
	peerProfile.GameInfo.ExtPoint = 428_326_799
	peer := &connectionSession{UIN: 1_000_002, Profile: peerProfile, RoomID: 1}
	peer.liveUIN.Store(peer.UIN)
	peer.liveRoomID.Store(1)
	server.liveSessions[peerServer] = peer
	participants, err := server.localAdventureParticipants(session)
	if err != nil {
		t.Fatal(err)
	}
	if len(participants) != 2 || participants[0].PlayerID != profile.PlayerID || participants[1].PlayerID != 2 || participants[1].RoleID != 9 || participants[0].TeamID != adventureCooperativeTeamID || participants[1].TeamID != adventureCooperativeTeamID {
		t.Fatalf("adventure participants = %+v, want both room members/roles projected to cooperative team %d", participants, adventureCooperativeTeamID)
	}
	if participants[0].ExtPoint != profile.GameInfo.ExtPoint || participants[1].ExtPoint != peerProfile.GameInfo.ExtPoint {
		t.Fatalf("adventure participant points = %+v, want live profiles %d/%d", participants, profile.GameInfo.ExtPoint, peerProfile.GameInfo.ExtPoint)
	}
}

func TestServerNormalAdventureCompletionRetainsRoom(t *testing.T) {
	server, session := testRoomServerSession(t)
	if err := server.completeAdventureMatch(session, "test", nil); err != nil {
		t.Fatal(err)
	}
	state, err := server.sessionRoom(session)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := state.Snapshot()
	if snapshot.Phase != roomstate.PhasePreparing || snapshot.ActiveGameID != 0 || len(snapshot.Members) != 1 {
		t.Fatalf("room after normal match = %+v", snapshot)
	}
	if session.CurrentGameID != 0 || session.CurrentMapID != 0 || session.RoomID != 1 {
		t.Fatalf("session after normal match = %+v", session)
	}
	if _, err := server.adventureBattle(77); err == nil {
		t.Fatal("completed battle remained registered")
	}
}

func TestCompletedMatchLeaveRequestIsRealDeparture(t *testing.T) {
	server, session := testRoomServerSession(t)
	session.UIN = 1_000_001
	session.setUIN(session.UIN)
	if err := server.completeAdventureMatch(session, "test", nil); err != nil {
		t.Fatal(err)
	}
	payload := make([]byte, 8)
	binary.BigEndian.PutUint32(payload[0:4], session.UIN)
	binary.BigEndian.PutUint32(payload[4:8], 1234)
	request := testLocalRoutedPacketWithPayload(t, game.LeaveRoomCommand, 3, 0xFFFF, session.RoomID, session.UIN, payload)
	config := ListenerConfig{Response: ResponseConfig{QQTRoomList: true}}
	result := server.handleRoomMessage(config, session, "test", request)
	if !result.handled || len(result.response) == 0 {
		t.Fatalf("post-match lobby-return result = %+v", result)
	}
	if session.CurrentGameID != 0 || session.CurrentMapID != 0 || session.RoomID != 0 {
		t.Fatalf("session after completed-match leave = %+v", session)
	}
	if _, roomRetained := server.worldState().Room(1); roomRetained {
		t.Fatal("empty room remained after completed-match leave")
	}
}

func TestServerCreateIntentDiscardsStalePreparingRoom(t *testing.T) {
	server, session := testRoomServerSession(t)
	if err := server.completeAdventureMatch(session, "test", nil); err != nil {
		t.Fatal(err)
	}
	if err := server.prepareSessionForRoomCreate(session); err != nil {
		t.Fatal(err)
	}
	if session.RoomID != 0 || session.CurrentMapID != 0 {
		t.Fatalf("session after create intent = %+v", session)
	}
	_, oldRoomExists := server.worldState().Room(1)
	if oldRoomExists {
		t.Fatal("stale preparing room remained after a new create intent")
	}
}

func TestServerCreateIntentDoesNotLeaveActiveMatch(t *testing.T) {
	server, session := testRoomServerSession(t)
	if err := server.prepareSessionForRoomCreate(session); err == nil {
		t.Fatal("active match unexpectedly allowed implicit room leave")
	}
	if session.RoomID != 1 || session.CurrentGameID != 77 {
		t.Fatalf("active session mutated by rejected create intent = %+v", session)
	}
}

func TestPreparedItemConsumptionReturnsAuthoritativeUpdatedItem(t *testing.T) {
	server, session := testRoomServerSession(t)
	session.UIN = 1_000_001
	session.Profile.Inventory = []game.ItemInfo{{
		ItemID: game.LargeStaminaPotionItemID, NumOfItem: 500,
		ItemStatus: 1, AvailPeriod: game.LocalPermanentAvailablePeriod,
	}}
	updated, actor, err := server.consumeSessionPreparedItem(session, game.PreparedUsePropEvent{
		PlayerID: session.Profile.PlayerID, ItemID: game.LargeStaminaPotionItemID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if actor != session || updated.NumOfItem != 499 || len(session.Profile.Inventory) != 1 || session.Profile.Inventory[0].NumOfItem != 499 {
		t.Fatalf("updated/session item = %+v/%+v", updated, session.Profile.Inventory)
	}
}

func TestAdventureArbitratorConsumesForwardedParticipantsPreparedItem(t *testing.T) {
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ownerProfile := game.DefaultPlayerProfile()
	peerProfile := game.DefaultPlayerProfile()
	peerProfile.PlayerID = 2
	item := game.NewPermanentItemInfo(game.LargeStaminaPotionItemID, 2)
	item.ItemStatus = game.ItemStatusActive
	peerProfile.Inventory = []game.ItemInfo{item}
	if err = store.Save(context.Background(), 1_000_001, ownerProfile); err != nil {
		t.Fatal(err)
	}
	if err = store.Save(context.Background(), 1_000_002, peerProfile); err != nil {
		t.Fatal(err)
	}
	ownerServer, ownerClient := net.Pipe()
	peerServer, peerClient := net.Pipe()
	defer ownerServer.Close()
	defer ownerClient.Close()
	defer peerServer.Close()
	defer peerClient.Close()
	owner := &connectionSession{UIN: 1_000_001, Profile: ownerProfile, connection: ownerServer}
	peer := &connectionSession{UIN: 1_000_002, Profile: peerProfile, connection: peerServer}
	owner.setUIN(owner.UIN)
	peer.setUIN(peer.UIN)
	server := &Server{
		playerStore: store, battles: make(map[uint32]*match.AdventureBattle), logWriter: io.Discard,
		liveSessions: map[net.Conn]*connectionSession{ownerServer: owner, peerServer: peer},
	}
	if err = server.createSessionRoom(owner, 1, byte(roomstate.GameTypeAdventure)); err != nil {
		t.Fatal(err)
	}
	if err = server.worldState().EnterLobby(peer.UIN, peer.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().JoinRoom(peer.UIN, peer.Profile, owner.RoomID, peer.selectedRoleID(), 1); err != nil {
		t.Fatal(err)
	}
	peer.setRoomID(owner.RoomID)
	peer.worldUIN = peer.UIN
	if _, err = server.worldState().SetReady(peer.UIN, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 77
	if _, err = server.worldState().StartMatchWithID(owner.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	participants, err := server.localAdventureParticipants(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.replaceAdventureBattle(gameID, 1649, owner.Profile.PlayerID, participants); err != nil {
		t.Fatal(err)
	}
	server.projectRoomMatch(owner.RoomID, gameID, 1649)
	remaining, actor, err := server.consumeSessionPreparedItem(owner, game.PreparedUsePropEvent{
		PlayerID: peer.Profile.PlayerID, ItemID: game.LargeStaminaPotionItemID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if actor != peer || remaining.NumOfItem != 1 {
		t.Fatalf("forwarded actor/item = %p/%+v, want peer/1", actor, remaining)
	}
	peer.mu.Lock()
	peer.applyPendingProfile()
	peer.mu.Unlock()
	if len(peer.Profile.Inventory) != 1 || peer.Profile.Inventory[0].NumOfItem != 1 {
		t.Fatalf("peer inventory after forwarded use = %+v", peer.Profile.Inventory)
	}
	reloaded, err := store.Load(context.Background(), peer.UIN)
	if err != nil || len(reloaded.Inventory) != 1 || reloaded.Inventory[0].NumOfItem != 1 {
		t.Fatalf("durable peer inventory after forwarded use = %+v, %v", reloaded.Inventory, err)
	}
}

func TestPreparedAdventureItemsExcludeEquippedCosmetics(t *testing.T) {
	catalog, err := equipment.NewCatalog([]itemcatalog.Entry{{
		ID: 2067, Index: 187, Name: "中山装", Kind: "avatar-cosmetic", RegistryCategory: "cladorn", Categories: []string{"cladorn"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{equipmentCatalog: catalog}
	profile := game.DefaultPlayerProfile()
	cosmetic := game.NewPermanentItemInfo(2067, 1)
	cosmetic.ItemStatus, cosmetic.ItemRoleID = 1, 7
	potion := game.NewPermanentItemInfo(game.LargeStaminaPotionItemID, 500)
	potion.ItemStatus, potion.ItemRoleID = 1, 7
	profile.Inventory = []game.ItemInfo{cosmetic, potion}
	items, err := server.preparedAdventureItems(profile, 7)
	if err != nil {
		t.Fatal(err)
	}
	want := []game.GameItemType{{ItemID: game.LargeStaminaPotionItemID, Quantity: 500}}
	if len(items) != len(want) || items[0] != want[0] {
		t.Fatalf("prepared items = %+v, want %+v", items, want)
	}
}

func TestPreparedCompetitiveItemsUseOnlyCompetitiveCompatibleProps(t *testing.T) {
	server := &Server{itemKindsByID: map[uint16]string{
		20003: "inventory-consumable", 20020: "inventory-consumable", 20043: "inventory-consumable", 20058: "inventory-consumable",
	}}
	profile := game.DefaultPlayerProfile()
	profile.Inventory = []game.ItemInfo{
		{ItemID: 20003, NumOfItem: 1, ItemStatus: 1},
		{ItemID: 20020, NumOfItem: 2, ItemStatus: 1},
		{ItemID: 20043, NumOfItem: 3, ItemStatus: 1},
		{ItemID: 20058, NumOfItem: 4, ItemStatus: 1},
	}
	items, err := server.preparedMatchItems(profile, profile.GameInfo.RoleID, itemeffect.ScopeCompetitiveItem)
	if err != nil {
		t.Fatal(err)
	}
	want := []game.GameItemType{{ItemID: 20003, Quantity: 1}, {ItemID: 20020, Quantity: 2}, {ItemID: 20058, Quantity: 4}}
	if !reflect.DeepEqual(items, want) {
		t.Fatalf("competitive prepared items = %+v, want %+v", items, want)
	}
}

func TestServerAdventureLossRewardPersistsAfterSettlement(t *testing.T) {
	server, session := testRoomServerSession(t)
	store, err := persistence.OpenPlayerStore(filepath.Join(t.TempDir(), "players.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	server.playerStore = store
	session.UIN = 1_000_001
	beforeWins := session.Profile.GameInfo.ExtWinNum
	beforeLosses := session.Profile.GameInfo.ExtLossNum
	beforePoints := session.Profile.GameInfo.ExtPoint
	beforeCompetitiveWins := session.Profile.GameInfo.WinNum
	beforeCompetitiveLosses := session.Profile.GameInfo.LossNum
	beforeCompetitiveDraws := session.Profile.GameInfo.EqualNum
	beforeCompetitivePoints := session.Profile.GameInfo.Point
	beforeCompetitiveDegree := session.Profile.GameInfo.Degree
	if err := store.Save(context.Background(), session.UIN, session.Profile); err != nil {
		t.Fatal(err)
	}
	if err := server.completeAdventureMatch(session, "test", &adventureSettlementCommit{
		Result: game.GameResultLoss, AdventurePoints: 30, CollectedItems: map[uint32]uint32{30044: 2},
	}); err != nil {
		t.Fatal(err)
	}
	if session.Profile.GameInfo.ExtWinNum != beforeWins || session.Profile.GameInfo.ExtLossNum != beforeLosses || session.Profile.GameInfo.ExtPoint != beforePoints+30 {
		t.Fatalf("in-memory profile after settlement = %+v", session.Profile.GameInfo)
	}
	if session.Profile.GameInfo.WinNum != beforeCompetitiveWins || session.Profile.GameInfo.LossNum != beforeCompetitiveLosses ||
		session.Profile.GameInfo.EqualNum != beforeCompetitiveDraws || session.Profile.GameInfo.Point != beforeCompetitivePoints ||
		session.Profile.GameInfo.Degree != beforeCompetitiveDegree {
		t.Fatalf("adventure settlement changed in-memory competitive record: %+v", session.Profile.GameInfo)
	}
	persisted, err := store.LoadOrCreate(context.Background(), session.UIN, game.DefaultPlayerProfile())
	if err != nil {
		t.Fatal(err)
	}
	if persisted.GameInfo.ExtWinNum != beforeWins || persisted.GameInfo.ExtLossNum != beforeLosses || persisted.GameInfo.ExtPoint != beforePoints+30 {
		t.Fatalf("persisted profile after settlement = %+v", persisted.GameInfo)
	}
	if persisted.GameInfo.WinNum != beforeCompetitiveWins || persisted.GameInfo.LossNum != beforeCompetitiveLosses ||
		persisted.GameInfo.EqualNum != beforeCompetitiveDraws || persisted.GameInfo.Point != beforeCompetitivePoints ||
		persisted.GameInfo.Degree != beforeCompetitiveDegree {
		t.Fatalf("adventure settlement changed persisted competitive record: %+v", persisted.GameInfo)
	}
	var collected uint32
	for _, item := range persisted.Inventory {
		if item.ItemID == 30044 {
			collected = item.NumOfItem
		}
	}
	if collected != 2 {
		t.Fatalf("persisted collected item 30044 quantity = %d, want 2", collected)
	}
}

func TestSaturatingAddUint32(t *testing.T) {
	if got := saturatingAddUint32(math.MaxUint32-1, 500); got != math.MaxUint32 {
		t.Fatalf("saturated value = %d, want %d", got, uint32(math.MaxUint32))
	}
}

func TestAdventureSettlementCapsExperienceAtClientMaximum(t *testing.T) {
	profile := game.DefaultPlayerProfile()
	profile.GameInfo.ExtPoint = game.MaxAdventureExperience
	beforeWins := profile.GameInfo.ExtWinNum
	beforeLosses := profile.GameInfo.ExtLossNum
	updated, err := projectAdventureSettlement(profile, adventureSettlementCommit{
		Result: game.GameResultLoss, AdventurePoints: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GameInfo.ExtPoint != game.MaxAdventureExperience {
		t.Fatalf("extended points = %d, want capped value %d", updated.GameInfo.ExtPoint, game.MaxAdventureExperience)
	}
	if updated.GameInfo.ExtWinNum != beforeWins || updated.GameInfo.ExtLossNum != beforeLosses {
		t.Fatalf("extended win/loss changed from %d/%d to %d/%d", beforeWins, beforeLosses, updated.GameInfo.ExtWinNum, updated.GameInfo.ExtLossNum)
	}
}

func TestAdventureVictoryDoesNotCreatePersistentWinRecord(t *testing.T) {
	profile := game.DefaultPlayerProfile()
	beforeWins := profile.GameInfo.ExtWinNum
	beforeLosses := profile.GameInfo.ExtLossNum
	updated, err := projectAdventureSettlement(profile, adventureSettlementCommit{
		Result: game.GameResultWin, AdventurePoints: 110,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.GameInfo.ExtWinNum != beforeWins || updated.GameInfo.ExtLossNum != beforeLosses || updated.GameInfo.ExtPoint != profile.GameInfo.ExtPoint+110 {
		t.Fatalf("adventure victory profile = %+v", updated.GameInfo)
	}
}

func TestAdventureSettlementForPlayerUsesOwnResult(t *testing.T) {
	data := game.GameOverData{GameMode: game.SettlementGameModeAdventure, Results: []game.GameResultData{
		{PlayerID: 2, Result: game.GameResultLoss, Point: 100, Fields: []game.GameResultField{{Score: 7}}},
		{PlayerID: 1, Result: game.GameResultWin, Point: 500, Fields: []game.GameResultField{
			{Count: 1, Score: 11}, {Count: 2, Score: 22}, {Count: 3, Score: 33}, {Count: 4, Score: 44},
		}},
	}}
	applyAdventureOutcomeCourageMultiplier(&data)
	settlement, err := adventureSettlementForPlayer(data, 1)
	if err != nil {
		t.Fatal(err)
	}
	if settlement.Result != game.GameResultWin || settlement.AdventurePoints != 165 || settlement.OutcomeMultiplierPercent != 150 {
		t.Fatalf("settlement = %+v", settlement)
	}
}

func TestAdventureOutcomeCourageMultiplier(t *testing.T) {
	for result, want := range map[game.GameResultCode]uint32{
		game.GameResultWin:  38,
		game.GameResultLoss: 18,
	} {
		data := game.GameOverData{
			GameMode: game.SettlementGameModeAdventure,
			Results: []game.GameResultData{{
				PlayerID: 1, Result: result, Fields: []game.GameResultField{{Score: 25}},
			}},
		}
		applyAdventureOutcomeCourageMultiplier(&data)
		settlement, err := adventureSettlementForPlayer(data, 1)
		if err != nil {
			t.Fatal(err)
		}
		if settlement.OutcomeMultiplierPercent != adventureOutcomeMultiplierPercent(result) || settlement.AdventurePoints != want {
			t.Fatalf("adventure result %d settlement = %+v, want total %d", result, settlement, want)
		}
		visible := data.Results[0]
		if len(visible.Fields) != 1 || visible.Fields[0].Score != want {
			t.Fatalf("adventure result %d visible score = %+v, want %d", result, visible.Fields, want)
		}
	}
}

func TestAdventureSettlementForPlayerRejectsMissingDuplicateAndUnknown(t *testing.T) {
	tests := []game.GameOverData{
		{GameMode: game.SettlementGameModeAdventure, Results: []game.GameResultData{{PlayerID: 2, Result: game.GameResultLoss}}},
		{GameMode: game.SettlementGameModeAdventure, Results: []game.GameResultData{{PlayerID: 1, Result: game.GameResultLoss}, {PlayerID: 1, Result: game.GameResultWin}}},
		{GameMode: game.SettlementGameModeAdventure, Results: []game.GameResultData{{PlayerID: 1, Result: 7}}},
		{GameMode: game.SettlementGameModeCompetitive, Results: []game.GameResultData{{PlayerID: 1, Result: game.GameResultLoss}}},
	}
	for index, data := range tests {
		if _, err := adventureSettlementForPlayer(data, 1); err == nil {
			t.Fatalf("case %d unexpectedly accepted", index)
		}
	}
}

func TestNormalizeCompetitiveNoWinnerAsDraw(t *testing.T) {
	participants := []match.CompetitiveParticipant{
		{PlayerID: 1, RoleID: 1, TeamID: 1},
		{PlayerID: 2, RoleID: 2, TeamID: 2},
	}
	battle, err := match.NewCompetitiveBattle(1, 1, 1, participants)
	if err != nil {
		t.Fatal(err)
	}
	data := game.GameOverData{GameMode: game.SettlementGameModeCompetitive, Results: []game.GameResultData{
		{PlayerID: 1, Result: game.GameResultLoss, Point: 7},
		{PlayerID: 2, Result: game.GameResultDraw, Point: 9},
	}}
	if !normalizeCompetitiveNoWinnerDraw(&data, battle) {
		t.Fatal("no-winner result was not normalized")
	}
	for _, result := range data.Results {
		if result.Result != game.GameResultDraw {
			t.Fatalf("normalized result = %+v", result)
		}
	}
	if data.Results[0].Point != 7 || data.Results[1].Point != 9 {
		t.Fatalf("normalization discarded mode scores: %+v", data.Results)
	}

	data.Results[0].Result = game.GameResultWin
	data.Results[1].Result = game.GameResultLoss
	if normalizeCompetitiveNoWinnerDraw(&data, battle) || data.Results[1].Result != game.GameResultLoss {
		t.Fatalf("winner result was rewritten: %+v", data.Results)
	}
}

func TestCompetitiveOutcomeBasePoints(t *testing.T) {
	for result, want := range map[game.GameResultCode]uint32{
		game.GameResultWin:  50,
		game.GameResultLoss: 10,
		game.GameResultDraw: 20,
	} {
		if got := competitiveOutcomeBasePoints(result); got != want {
			t.Fatalf("competitive result %d base points = %d, want %d", result, got, want)
		}
	}
	data := game.GameOverData{Results: []game.GameResultData{
		{Result: game.GameResultWin, Point: 7},
		{Result: game.GameResultLoss, Point: 8},
		{Result: game.GameResultDraw, Point: 9},
	}}
	addCompetitiveOutcomeBasePoints(&data)
	if data.Results[0].Point != 57 || data.Results[1].Point != 18 || data.Results[2].Point != 29 {
		t.Fatalf("competitive result points with outcome base = %+v", data.Results)
	}
	for index, want := range []uint32{50, 10, 20} {
		result := data.Results[index]
		if len(result.Fields) != game.GameResultKnownFieldCount || result.Fields[game.GameResultRewardFieldIndex].Score != want {
			t.Fatalf("competitive result %d visible reward score = %+v, want %d", index, result.Fields, want)
		}
	}
}

func TestCompetitiveGameOverIncludesTenPointsPerKillAndRescue(t *testing.T) {
	battle, err := match.NewCompetitiveBattle(1, 1, 1, []match.CompetitiveParticipant{
		{PlayerID: 1, RoleID: 1, TeamID: 1},
		{PlayerID: 2, RoleID: 2, TeamID: 1},
		{PlayerID: 3, RoleID: 3, TeamID: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = battle.RecordTrapped(2); err != nil {
		t.Fatal(err)
	}
	if err = battle.RecordRescue(1, 2); err != nil {
		t.Fatal(err)
	}
	for _, victimID := range []uint16{2, 1} {
		if err = battle.RecordTrapped(victimID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = battle.RecordKill(3, 2); err != nil {
		t.Fatal(err)
	}
	resolution, err := battle.RecordKill(3, 1)
	if err != nil || !resolution.NewlyConcluded {
		t.Fatalf("competitive resolution = %+v, %v", resolution, err)
	}
	data := competitiveGameOverData(1234, resolution)
	byPlayer := make(map[uint16]game.GameResultData, len(data.Results))
	for _, result := range data.Results {
		byPlayer[result.PlayerID] = result
	}
	rescuer, ok := byPlayer[1].AdventureStatistics()
	if !ok || rescuer.RescueCount != 1 || rescuer.RescueScore != 10 || byPlayer[1].Point != 20 {
		t.Fatalf("rescuer result = %+v statistics=%+v/%t", byPlayer[1], rescuer, ok)
	}
	killer, ok := byPlayer[3].AdventureStatistics()
	if !ok || killer.KillCount != 2 || killer.KillScore != 20 || byPlayer[3].Point != 70 {
		t.Fatalf("killer result = %+v statistics=%+v/%t", byPlayer[3], killer, ok)
	}
}

func TestNormalizeCompetitiveTeamOutcomeIncludesEliminatedWinnerTeammate(t *testing.T) {
	participants := []match.CompetitiveParticipant{
		{PlayerID: 1, RoleID: 1, TeamID: 1},
		{PlayerID: 2, RoleID: 2, TeamID: 1},
		{PlayerID: 3, RoleID: 3, TeamID: 2},
	}
	battle, err := match.NewCompetitiveBattle(1, 1, 1, participants)
	if err != nil {
		t.Fatal(err)
	}
	data := game.GameOverData{GameMode: game.SettlementGameModeCompetitive, Results: []game.GameResultData{
		{PlayerID: 1, Result: game.GameResultLoss},
		{PlayerID: 2, Result: game.GameResultWin},
		{PlayerID: 3, Result: game.GameResultLoss},
	}}
	changed, err := normalizeCompetitiveTeamOutcome(&data, battle)
	if err != nil || !changed {
		t.Fatalf("normalize competitive team outcome changed=%t err=%v", changed, err)
	}
	if data.Results[0].Result != game.GameResultWin || data.Results[1].Result != game.GameResultWin || data.Results[2].Result != game.GameResultLoss {
		t.Fatalf("competitive team outcomes = %+v", data.Results)
	}
}

func TestNormalizeAdventureTeamOutcomeIncludesDeadFinalStageTeammate(t *testing.T) {
	battle, err := match.NewAdventureBattleWithStage(1, 1649, 1, 1, 1, []match.AdventureParticipant{
		{PlayerID: 1, RoleID: 1, TeamID: 1},
		{PlayerID: 2, RoleID: 2, TeamID: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = battle.RecordDeath(1); err != nil {
		t.Fatal(err)
	}
	data := game.GameOverData{GameMode: game.SettlementGameModeAdventure, Results: []game.GameResultData{
		{PlayerID: 1, Result: game.GameResultLoss},
		{PlayerID: 2, Result: game.GameResultWin},
	}}
	changed, err := normalizeAdventureTeamOutcome(&data, battle)
	if err != nil || !changed {
		t.Fatalf("normalize adventure team outcome changed=%t err=%v", changed, err)
	}
	if data.Results[0].Result != game.GameResultWin || data.Results[1].Result != game.GameResultWin {
		t.Fatalf("adventure team outcomes = %+v", data.Results)
	}

	data.Results[0].Result = game.GameResultLoss
	data.Results[1].Result = game.GameResultLoss
	changed, err = normalizeAdventureTeamOutcome(&data, battle)
	if err != nil || changed || data.Results[0].Result != game.GameResultLoss || data.Results[1].Result != game.GameResultLoss {
		t.Fatalf("adventure team failure was rewritten: changed=%t err=%v results=%+v", changed, err, data.Results)
	}
}

func TestServerActiveAdventureExitReturnsToLobbyAndRemovesRoomMember(t *testing.T) {
	server, session := testRoomServerSession(t)
	if err := server.handleActiveAdventureExitToLobby(session, "test"); err != nil {
		t.Fatal(err)
	}
	if session.CurrentGameID != 0 || session.CurrentMapID != 0 || session.RoomID != 0 {
		t.Fatalf("session after active exit = %+v", session)
	}
	if _, err := server.adventureBattle(77); err == nil {
		t.Fatal("active-exit battle remained registered")
	}
	_, roomExists := server.worldState().Room(1)
	if roomExists {
		t.Fatal("empty room remained after its only member returned to lobby")
	}
}

func TestConnectionDepartureRemovesMemberAndTransfersRoomOwnership(t *testing.T) {
	server := &Server{logWriter: io.Discard}
	ownerProfile := game.DefaultPlayerProfile()
	owner := &connectionSession{UIN: 1_000_001, Profile: ownerProfile}
	owner.setUIN(owner.UIN)
	if err := server.createSessionRoom(owner, 1, byte(roomstate.GameTypeAdventure)); err != nil {
		t.Fatal(err)
	}

	peerProfile := game.DefaultPlayerProfile()
	peerProfile.PlayerID = 2
	peer := &connectionSession{UIN: 1_000_002, Profile: peerProfile}
	peer.setUIN(peer.UIN)
	if err := server.worldState().EnterLobby(peer.UIN, peer.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err := server.worldState().JoinRoom(peer.UIN, peer.Profile, owner.RoomID, peer.Profile.GameInfo.RoleID, 2); err != nil {
		t.Fatal(err)
	}
	peer.setRoomID(owner.RoomID)
	peer.worldUIN = peer.UIN

	server.handleConnectionDeparture(owner, "owner-disconnected")

	if owner.RoomID != 0 || owner.worldUIN != 0 {
		t.Fatalf("disconnected owner retained room projection: room=%d world_uin=%d", owner.RoomID, owner.worldUIN)
	}
	room, active := server.worldState().Room(1)
	if !active {
		t.Fatal("room with a surviving member was reclaimed")
	}
	snapshot := room.Snapshot()
	if snapshot.OwnerID != peer.Profile.PlayerID || len(snapshot.Members) != 1 || snapshot.Members[0].PlayerID != peer.Profile.PlayerID {
		t.Fatalf("room after owner disconnect = %+v", snapshot)
	}
}

func TestActiveAdventureDisconnectTransfersNativeArbitrator(t *testing.T) {
	ownerProfile := game.DefaultPlayerProfile()
	peerProfile := game.DefaultPlayerProfile()
	peerProfile.PlayerID = 2
	owner := &connectionSession{UIN: 1_000_001, Profile: ownerProfile, connectionID: "owner"}
	peer := &connectionSession{UIN: 1_000_002, Profile: peerProfile, connectionID: "peer"}
	owner.setUIN(owner.UIN)
	peer.setUIN(peer.UIN)
	ownerServer, ownerClient := net.Pipe()
	peerServer, peerClient := net.Pipe()
	defer ownerServer.Close()
	defer ownerClient.Close()
	defer peerServer.Close()
	defer peerClient.Close()
	owner.connection = ownerServer
	peer.connection = peerServer
	captureWriter, err := capture.Open(filepath.Join(t.TempDir(), "capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer captureWriter.Close()
	server := &Server{
		battles: make(map[uint32]*match.AdventureBattle),
		liveSessions: map[net.Conn]*connectionSession{
			ownerServer: owner,
			peerServer:  peer,
		},
		capture: captureWriter, logWriter: io.Discard,
	}
	if err = server.createSessionRoom(owner, 1, byte(roomstate.GameTypeAdventure)); err != nil {
		t.Fatal(err)
	}
	if err := server.worldState().EnterLobby(peer.UIN, peer.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err := server.worldState().JoinRoom(peer.UIN, peer.Profile, owner.RoomID, peer.Profile.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	peer.setRoomID(owner.RoomID)
	peer.worldUIN = peer.UIN
	if _, err := server.worldState().SetReady(peer.UIN, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 88
	if _, err := server.worldState().StartMatchWithID(owner.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	participants, err := server.localAdventureParticipants(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.replaceAdventureBattle(gameID, 1649, owner.Profile.PlayerID, participants); err != nil {
		t.Fatal(err)
	}
	server.projectRoomMatch(owner.RoomID, gameID, 1649)
	requestPayload := make([]byte, 8)
	binary.BigEndian.PutUint32(requestPayload[0:4], peer.UIN)
	peer.notePacket(testLocalRoutedPacketWithPayload(t, game.LeaveRoomCommand, 3, 0xFFFF, owner.RoomID, peer.UIN, requestPayload))

	packets := make(chan []byte, 8)
	readErr := make(chan error, 1)
	if err = peerClient.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			buffer := make([]byte, 64*1024)
			n, readPacketErr := peerClient.Read(buffer)
			if readPacketErr != nil {
				readErr <- readPacketErr
				return
			}
			packets <- append([]byte(nil), buffer[:n]...)
		}
	}()
	server.handleConnectionDeparture(owner, owner.connectionID)

	var leaveInspection game.LocalPacketInspection
	var arbitratorEvent game.NotifyGameEvent
	playerLeaveEvents := 0
	deadline := time.After(2 * time.Second)
	for leaveInspection.Command == 0 || arbitratorEvent.Schema == 0 {
		select {
		case packet := <-packets:
			inspection, inspectErr := game.InspectLocalPacket(packet)
			if inspectErr != nil {
				t.Fatal(inspectErr)
			}
			if inspection.Command == game.LeaveRoomNotifyCommand {
				leaveInspection = inspection
			}
			if inspection.Command == game.GameEventNotifyCommand {
				event, parseErr := game.ParseNotifyGameEventPayload(inspection.Payload)
				if parseErr != nil {
					t.Fatal(parseErr)
				}
				switch event.Schema {
				case game.NotifyChangeArbitrator:
					arbitratorEvent = event
				case game.NotifyPlayerLeave:
					playerLeaveEvents++
				}
			}
		case readPacketErr := <-readErr:
			t.Fatal(readPacketErr)
		case <-deadline:
			t.Fatal("survivor did not receive active room departure/arbitrator migration")
		}
	}
	if len(leaveInspection.Payload) != 8 {
		t.Fatalf("room departure payload length = %d", len(leaveInspection.Payload))
	}
	if playerID := binary.BigEndian.Uint16(leaveInspection.Payload[0:2]); playerID != owner.Profile.PlayerID {
		t.Fatalf("departed player ID = %d, want %d", playerID, owner.Profile.PlayerID)
	}
	if newOwnerID := binary.BigEndian.Uint16(leaveInspection.Payload[2:4]); newOwnerID != peer.Profile.PlayerID {
		t.Fatalf("new room owner ID = %d, want %d", newOwnerID, peer.Profile.PlayerID)
	}
	if newArbitratorID := binary.BigEndian.Uint16(leaveInspection.Payload[4:6]); newArbitratorID != peer.Profile.PlayerID {
		t.Fatalf("new arbitrator ID = %d, want %d", newArbitratorID, peer.Profile.PlayerID)
	}
	if len(arbitratorEvent.Body) != 6 {
		t.Fatalf("native arbitrator event = %+v body=%x", arbitratorEvent, arbitratorEvent.Body)
	}
	if got := binary.BigEndian.Uint16(arbitratorEvent.Body[0:2]); got != peer.Profile.PlayerID || binary.BigEndian.Uint32(arbitratorEvent.Body[2:6]) != 0 {
		t.Fatalf("native arbitrator event = %+v body=%x", arbitratorEvent, arbitratorEvent.Body)
	}
	if playerLeaveEvents != 0 {
		t.Fatalf("server sent %d duplicate in-scene player-leave events", playerLeaveEvents)
	}
	battle, err := server.adventureBattle(gameID)
	if err != nil || !battle.IsArbitrator(peer.Profile.PlayerID) {
		t.Fatalf("surviving player %d was not elected arbitrator, err=%v", peer.Profile.PlayerID, err)
	}
}

func TestActiveCompetitiveExitTransfersNativeArbitratorWhileRoundContinues(t *testing.T) {
	ownerProfile := game.DefaultPlayerProfile()
	peerProfile := game.DefaultPlayerProfile()
	peerProfile.PlayerID = 2
	thirdProfile := game.DefaultPlayerProfile()
	thirdProfile.PlayerID = 3
	fourthProfile := game.DefaultPlayerProfile()
	fourthProfile.PlayerID = 4
	owner := &connectionSession{UIN: 1_000_001, Profile: ownerProfile, connectionID: "owner"}
	peer := &connectionSession{UIN: 1_000_002, Profile: peerProfile, connectionID: "peer"}
	owner.setUIN(owner.UIN)
	peer.setUIN(peer.UIN)
	ownerServer, ownerClient := net.Pipe()
	peerServer, peerClient := net.Pipe()
	defer ownerServer.Close()
	defer ownerClient.Close()
	defer peerServer.Close()
	defer peerClient.Close()
	owner.connection = ownerServer
	peer.connection = peerServer
	captureWriter, err := capture.Open(filepath.Join(t.TempDir(), "capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer captureWriter.Close()
	server := &Server{
		competitiveBattles: make(map[uint32]*match.CompetitiveBattle),
		liveSessions: map[net.Conn]*connectionSession{
			ownerServer: owner,
			peerServer:  peer,
		},
		capture: captureWriter, logWriter: io.Discard,
	}
	if err := server.createSessionRoom(owner, 1, byte(roomstate.GameTypeCompetitiveNoItem)); err != nil {
		t.Fatal(err)
	}
	if err := server.worldState().EnterLobby(peer.UIN, peer.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err := server.worldState().JoinRoom(peer.UIN, peer.Profile, owner.RoomID, peer.Profile.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	peer.setRoomID(owner.RoomID)
	peer.worldUIN = peer.UIN
	if _, err := server.worldState().SetReady(peer.UIN, true); err != nil {
		t.Fatal(err)
	}
	const thirdUIN uint32 = 1_000_003
	if err = server.worldState().EnterLobby(thirdUIN, thirdProfile); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().JoinRoom(thirdUIN, thirdProfile, owner.RoomID, thirdProfile.GameInfo.RoleID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().SetReady(thirdUIN, true); err != nil {
		t.Fatal(err)
	}
	const fourthUIN uint32 = 1_000_004
	if err = server.worldState().EnterLobby(fourthUIN, fourthProfile); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().JoinRoom(fourthUIN, fourthProfile, owner.RoomID, fourthProfile.GameInfo.RoleID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().SetReady(fourthUIN, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 77
	if _, err := server.worldState().StartMatchWithID(owner.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	participants, err := server.localCompetitiveParticipants(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = server.replaceCompetitiveBattle(gameID, 1, owner.Profile.PlayerID, participants, match.CompetitiveRuleConfig{}); err != nil {
		t.Fatal(err)
	}
	server.projectRoomMatch(owner.RoomID, gameID, 1)
	requestPayload := make([]byte, 8)
	binary.BigEndian.PutUint32(requestPayload[0:4], peer.UIN)
	peer.notePacket(testLocalRoutedPacketWithPayload(t, game.LeaveRoomCommand, 3, 0xFFFF, owner.RoomID, peer.UIN, requestPayload))

	packets := make(chan []byte, 4)
	readErr := make(chan error, 1)
	if err = peerClient.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			buffer := make([]byte, 64*1024)
			n, readPacketErr := peerClient.Read(buffer)
			if readPacketErr != nil {
				readErr <- readPacketErr
				return
			}
			packets <- append([]byte(nil), buffer[:n]...)
		}
	}()
	if err = server.handleActiveAdventureExitToLobby(owner, owner.connectionID); err != nil {
		t.Fatal(err)
	}

	var leaveInspection game.LocalPacketInspection
	var arbitratorEvent game.NotifyGameEvent
	playerLeaveEvents := 0
	deadline := time.After(time.Second)
	for leaveInspection.Command == 0 || arbitratorEvent.Schema == 0 {
		select {
		case packet := <-packets:
			inspection, inspectErr := game.InspectLocalPacket(packet)
			if inspectErr != nil {
				t.Fatal(inspectErr)
			}
			if inspection.Command == game.LeaveRoomNotifyCommand {
				leaveInspection = inspection
			}
			if inspection.Command == game.GameEventNotifyCommand {
				event, parseErr := game.ParseNotifyGameEventPayload(inspection.Payload)
				if parseErr != nil {
					t.Fatal(parseErr)
				}
				switch event.Schema {
				case game.NotifyChangeArbitrator:
					arbitratorEvent = event
				case game.NotifyPlayerLeave:
					playerLeaveEvents++
				}
			}
		case err = <-readErr:
			t.Fatal(err)
		case <-deadline:
			t.Fatal("survivor did not receive room departure and native arbitrator migration")
		}
	}
	if leaveInspection.Command != game.LeaveRoomNotifyCommand || len(leaveInspection.Payload) != 8 {
		t.Fatalf("room departure notification = %+v", leaveInspection)
	}
	if playerID := binary.BigEndian.Uint16(leaveInspection.Payload[0:2]); playerID != owner.Profile.PlayerID {
		t.Fatalf("departed player ID = %d, want %d", playerID, owner.Profile.PlayerID)
	}
	if newOwnerID := binary.BigEndian.Uint16(leaveInspection.Payload[2:4]); newOwnerID != peer.Profile.PlayerID {
		t.Fatalf("new room owner ID = %d, want %d", newOwnerID, peer.Profile.PlayerID)
	}
	if len(arbitratorEvent.Body) != 6 || binary.BigEndian.Uint16(arbitratorEvent.Body[0:2]) != peer.Profile.PlayerID || binary.BigEndian.Uint32(arbitratorEvent.Body[2:6]) != 0 {
		t.Fatalf("native arbitrator event = %+v body=%x", arbitratorEvent, arbitratorEvent.Body)
	}
	if playerLeaveEvents != 0 {
		t.Fatalf("server sent %d duplicate in-scene player-leave events", playerLeaveEvents)
	}
	room, active := server.worldState().Room(1)
	if !active || len(room.Snapshot().Members) != 3 || room.Snapshot().OwnerID != peer.Profile.PlayerID {
		t.Fatalf("surviving room = active %t snapshot %+v", active, room.Snapshot())
	}
	battle, err := server.competitiveBattle(gameID)
	if err != nil || !battle.IsArbitrator(peer.Profile.PlayerID) {
		t.Fatalf("surviving competitive player %d was not elected arbitrator, err=%v", peer.Profile.PlayerID, err)
	}
}

func TestServerNextStageEliminationRemovesOnlySettledMember(t *testing.T) {
	server := &Server{battles: make(map[uint32]*match.AdventureBattle), logWriter: io.Discard}
	one := game.DefaultPlayerProfile()
	two := game.DefaultPlayerProfile()
	two.PlayerID = 2
	owner := &connectionSession{UIN: 1_000_001, Profile: one}
	peer := &connectionSession{UIN: 1_000_002, Profile: two}
	if err := server.createSessionRoomWithProperties(owner, 1, byte(roomstate.GameTypeAdventure), 0, roomstate.Properties{}); err != nil {
		t.Fatal(err)
	}
	if err := server.worldState().EnterLobby(peer.UIN, peer.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err := server.worldState().JoinRoom(peer.UIN, peer.Profile, 1, two.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	peer.setRoomID(1)
	peer.worldUIN = peer.UIN
	if _, err := server.worldState().SetReady(peer.UIN, true); err != nil {
		t.Fatal(err)
	}
	if _, err := server.worldState().StartMatchWithID(owner.UIN, 77); err != nil {
		t.Fatal(err)
	}
	state, _ := server.worldState().Room(1)
	if err := server.removeSettledOutRoomMembers(1, []match.AdventureSettlement{{
		Participant: match.AdventureParticipant{PlayerID: 2, TeamID: 1},
		Reason:      match.AdventureSettlementDiedBeforeNextStage,
	}}); err != nil {
		t.Fatal(err)
	}
	snapshot := state.Snapshot()
	if snapshot.Phase != roomstate.PhaseInMatch || snapshot.ActiveGameID != 77 || len(snapshot.Members) != 1 || snapshot.Members[0].PlayerID != 1 {
		t.Fatalf("room after between-stage elimination = %+v", snapshot)
	}
}

func TestAdventureStageEliminationProjectsRoomDepartureToSurvivor(t *testing.T) {
	peerServer, peerClient := net.Pipe()
	defer peerServer.Close()
	defer peerClient.Close()

	one := game.DefaultPlayerProfile()
	two := game.DefaultPlayerProfile()
	two.PlayerID = 2
	owner := &connectionSession{UIN: 1_000_001, Profile: one}
	peer := &connectionSession{
		UIN: 1_000_002, Profile: two, connection: peerServer, connectionID: "stage-survivor",
		localAddress: "127.0.0.1:18000", remoteAddress: "127.0.0.1:50000",
	}
	peer.liveUIN.Store(peer.UIN)
	peer.notePacket(testLocalRoutedPacket(t, 0x0107, 4, 0xFFFF, 0, peer.UIN))
	captureWriter, err := capture.Open(filepath.Join(t.TempDir(), "capture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer captureWriter.Close()
	server := &Server{liveSessions: map[net.Conn]*connectionSession{peerServer: peer}, capture: captureWriter, logWriter: io.Discard}
	if err := server.createSessionRoomWithProperties(owner, 1, byte(roomstate.GameTypeAdventure), 0, roomstate.Properties{}); err != nil {
		t.Fatal(err)
	}
	if err := server.worldState().EnterLobby(peer.UIN, peer.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err := server.worldState().JoinRoom(peer.UIN, peer.Profile, 1, two.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	peer.setRoomID(1)
	peer.worldUIN = peer.UIN
	departure, err := server.worldState().RemoveMember(1, owner.Profile.PlayerID, roomstate.LeaveEliminatedBetweenStages)
	if err != nil {
		t.Fatal(err)
	}

	read := make(chan []byte, 1)
	go func() {
		prefix := make([]byte, 4)
		if _, readErr := io.ReadFull(peerClient, prefix); readErr != nil {
			read <- nil
			return
		}
		packet := make([]byte, binary.BigEndian.Uint32(prefix))
		copy(packet, prefix)
		if _, readErr := io.ReadFull(peerClient, packet[4:]); readErr != nil {
			read <- nil
			return
		}
		read <- packet
	}()
	if err = server.projectAdventureStageDeparture(1, owner.UIN, peer.Profile.PlayerID, 1601, departure); err != nil {
		t.Fatal(err)
	}
	packet := <-read
	if len(packet) == 0 {
		t.Fatal("survivor received no room departure for eliminated teammate")
	}
	inspection, err := game.InspectLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != game.LeaveRoomNotifyCommand || len(inspection.Payload) != 8 {
		t.Fatalf("stage departure packet = command 0x%04X payload %x", inspection.Command, inspection.Payload)
	}
	if got := binary.BigEndian.Uint16(inspection.Payload[0:2]); got != owner.Profile.PlayerID {
		t.Fatalf("departed player = %d", got)
	}
	if got := binary.BigEndian.Uint16(inspection.Payload[2:4]); got != peer.Profile.PlayerID {
		t.Fatalf("new owner = %d", got)
	}
	if got := binary.BigEndian.Uint16(inspection.Payload[4:6]); got != peer.Profile.PlayerID {
		t.Fatalf("new arbitrator = %d", got)
	}
	if got := binary.BigEndian.Uint16(inspection.Payload[6:8]); got != 1601 {
		t.Fatalf("room map = %d", got)
	}
}

func TestCompleteAdventureRoomMatchProjectsEveryLiveParticipant(t *testing.T) {
	battle, err := match.NewAdventureBattle(77, 1649, 1, []match.AdventureParticipant{{PlayerID: 1, TeamID: 1}, {PlayerID: 2, TeamID: 1}})
	if err != nil {
		t.Fatal(err)
	}
	oneServer, oneClient := net.Pipe()
	twoServer, twoClient := net.Pipe()
	defer oneServer.Close()
	defer oneClient.Close()
	defer twoServer.Close()
	defer twoClient.Close()
	oneProfile := game.DefaultPlayerProfile()
	twoProfile := game.DefaultPlayerProfile()
	twoProfile.PlayerID = 2
	one := &connectionSession{UIN: 1_000_001, Profile: oneProfile}
	two := &connectionSession{UIN: 1_000_002, Profile: twoProfile}
	one.liveUIN.Store(one.UIN)
	two.liveUIN.Store(two.UIN)
	server := &Server{
		battles:      map[uint32]*match.AdventureBattle{77: battle},
		liveSessions: map[net.Conn]*connectionSession{oneServer: one, twoServer: two}, logWriter: io.Discard,
	}
	if err = server.createSessionRoomWithProperties(one, 1, byte(roomstate.GameTypeAdventure), 0, roomstate.Properties{}); err != nil {
		t.Fatal(err)
	}
	if err = server.worldState().EnterLobby(two.UIN, two.Profile); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().JoinRoom(two.UIN, two.Profile, 1, two.Profile.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	two.setRoomID(1)
	two.worldUIN = two.UIN
	if _, err = server.worldState().SetReady(two.UIN, true); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().StartMatchWithID(one.UIN, 77); err != nil {
		t.Fatal(err)
	}
	one.CurrentGameID, one.CurrentMapID = 77, 1649
	two.CurrentGameID, two.CurrentMapID = 77, 1649
	state, _ := server.worldState().Room(1)
	gameOver := game.GameOverData{
		GameMode: game.SettlementGameModeAdventure,
		Results: []game.GameResultData{
			{PlayerID: 1, Result: game.GameResultWin, Fields: (game.AdventureResultStatistics{RewardScore: 11}).GameResultFields()},
			{PlayerID: 2, Result: game.GameResultWin, Fields: (game.AdventureResultStatistics{RewardScore: 22}).GameResultFields()},
		},
	}
	if err = server.completeAdventureRoomMatch(one, "owner", adventureRoomSettlementCommit{GameOver: gameOver, Battle: battle}); err != nil {
		t.Fatal(err)
	}
	if state.Snapshot().Phase != roomstate.PhasePreparing || one.CurrentGameID != 0 || two.CurrentGameID != 0 {
		t.Fatalf("room/member completion = phase:%d one:%+v two:%+v", state.Snapshot().Phase, one, two)
	}
	// A genuinely new member can join the retained preparing room.
	threeServer, threeClient := net.Pipe()
	defer threeServer.Close()
	defer threeClient.Close()
	threeProfile := game.DefaultPlayerProfile()
	threeProfile.PlayerID = 3
	three := &connectionSession{UIN: 1_000_003, Profile: threeProfile}
	three.liveUIN.Store(three.UIN)
	server.liveSessions[threeServer] = three
	if err = server.worldState().EnterLobby(three.UIN, three.Profile); err != nil {
		t.Fatal(err)
	}
	joinedResponse, newlyJoined, err := server.enterSessionRoom(three, game.EnterRoomRequest{
		UIN: three.UIN, RoomID: 1, RoleID: three.Profile.GameInfo.RoleID,
	})
	if err != nil || !newlyJoined || len(joinedResponse.Players) != 3 {
		t.Fatalf("new retained-room join = joined:%t players:%d err:%v", newlyJoined, len(joinedResponse.Players), err)
	}
	if _, err = server.leaveSessionRoom(three, roomstate.LeaveVoluntary); err != nil {
		t.Fatal(err)
	}
	// Re-entering the same authoritative room is idempotent: no duplicate
	// member and no peer enter notification are produced.
	response, newlyJoined, err := server.enterSessionRoom(two, game.EnterRoomRequest{
		UIN: two.UIN, RoomID: two.RoomID, RoleID: two.Profile.GameInfo.RoleID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if newlyJoined || len(response.Players) != 2 || len(state.Snapshot().Members) != 2 {
		t.Fatalf("retained room re-entry = joined:%t response:%+v room:%+v", newlyJoined, response, state.Snapshot())
	}
	if _, err = server.adventureBattle(77); err == nil {
		t.Fatal("completed shared battle remained registered")
	}
}

func TestFailedGameOverWriteStillCompletesAdventureRoom(t *testing.T) {
	battle, err := match.NewAdventureBattle(77, 1649, 1, []match.AdventureParticipant{{PlayerID: 1, TeamID: 1}})
	if err != nil {
		t.Fatal(err)
	}
	serverSide, clientSide := net.Pipe()
	defer clientSide.Close()
	profile := game.DefaultPlayerProfile()
	session := &connectionSession{
		Profile: profile, connection: serverSide, connectionID: "failed-game-over",
		localAddress: "127.0.0.1:18000", remoteAddress: "127.0.0.1:50000",
	}
	session.setUIN(1_000_001)
	server := &Server{
		battles: map[uint32]*match.AdventureBattle{77: battle},
		liveSessions: map[net.Conn]*connectionSession{
			serverSide: session,
		},
		logWriter: io.Discard,
	}
	if err = server.createSessionRoom(session, 1, byte(roomstate.GameTypeAdventure)); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().StartMatchWithID(session.UIN, 77); err != nil {
		t.Fatal(err)
	}
	session.CurrentGameID, session.CurrentStageGameID, session.CurrentMapID = 77, 77, 1649
	state, _ := server.worldState().Room(1)
	gameOver := game.GameOverData{
		GameMode: game.SettlementGameModeAdventure,
		Results: []game.GameResultData{{
			PlayerID: 1, Result: game.GameResultWin,
			Fields: (game.AdventureResultStatistics{RewardScore: 10}).GameResultFields(),
		}},
	}
	if err = serverSide.Close(); err != nil {
		t.Fatal(err)
	}
	if ok := server.deliverTCPSequence(tcpSequenceDelivery{
		connection: serverSide, session: session, connectionID: session.connectionID,
		local: session.localAddress, remote: session.remoteAddress, response: []byte{1}, result: "game_over",
		completeAdventure:       true,
		adventureRoomSettlement: &adventureRoomSettlementCommit{GameOver: gameOver, Battle: battle},
	}); ok {
		t.Fatal("closed GAME_OVER transport was reported as writable")
	}
	if snapshot := state.Snapshot(); snapshot.Phase != roomstate.PhasePreparing || snapshot.ActiveGameID != 0 {
		t.Fatalf("room remained in settlement after write failure: %+v", snapshot)
	}
	if session.CurrentGameID != 0 || session.CurrentStageGameID != 0 || session.CurrentMapID != 0 {
		t.Fatalf("session retained completed game after write failure: %+v", session)
	}
	if _, err = server.adventureBattle(77); err == nil {
		t.Fatal("completed battle remained registered after GAME_OVER write failure")
	}
}

func TestAdventureFinalVictoryCompletesThroughRemainingMember(t *testing.T) {
	battle, err := match.NewAdventureBattleWithStage(77, 1649, 1, 1, 1, []match.AdventureParticipant{
		{PlayerID: 1, TeamID: 1},
		{PlayerID: 2, TeamID: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	peerProfile := game.DefaultPlayerProfile()
	peerProfile.PlayerID = 2
	peer := &connectionSession{Profile: peerProfile, connectionID: "remaining-peer"}
	peer.setUIN(1_000_002)
	server := &Server{
		battles:      map[uint32]*match.AdventureBattle{77: battle},
		liveSessions: map[net.Conn]*connectionSession{nil: peer},
		logWriter:    io.Discard,
	}
	if err = server.createSessionRoom(peer, 1, byte(roomstate.GameTypeAdventure)); err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().StartMatchWithID(peer.UIN, 77); err != nil {
		t.Fatal(err)
	}
	peer.CurrentGameID, peer.CurrentStageGameID, peer.CurrentMapID = 77, 77, 1649
	if progress, recordErr := battle.RecordNPCDeath(10); recordErr != nil || !progress.NewlyCompleted {
		t.Fatalf("final objective progress=%+v err=%v", progress, recordErr)
	}
	if pending, beginErr := battle.BeginFinalStageVictory(); beginErr != nil || !pending.NewlyConcluded {
		t.Fatalf("pending final victory=%+v err=%v", pending, beginErr)
	}
	departure, err := battle.RemoveParticipant(1, match.AdventureDepartureDisconnected)
	if err != nil || departure.ArbitratorPlayerID != 2 {
		t.Fatalf("trigger departure=%+v err=%v", departure, err)
	}
	server.completeAdventureFinalVictory(adventureFinalVictorySchedule{
		gameID: 77, roomID: 1, clientTime: 100, battle: battle,
	})
	state, _ := server.worldState().Room(1)
	if snapshot := state.Snapshot(); snapshot.Phase != roomstate.PhasePreparing || snapshot.ActiveGameID != 0 {
		t.Fatalf("remaining member did not complete final victory: %+v", snapshot)
	}
	if peer.CurrentGameID != 0 || peer.Profile.GameInfo.ExtPoint != peerProfile.GameInfo.ExtPoint+15 {
		t.Fatalf("remaining member settlement=%+v", peer)
	}
	if _, err = server.adventureBattle(77); err == nil {
		t.Fatal("final-victory battle remained registered")
	}
}

func TestLiveRoomRoutingSessionsDoesNotWaitForDispatcherMutex(t *testing.T) {
	profile := game.DefaultPlayerProfile()
	session := &connectionSession{Profile: profile}
	session.setUIN(1_000_001)
	session.setRoomID(7)
	server := &Server{liveSessions: map[net.Conn]*connectionSession{nil: session}}

	// REQUEST_LEAVE_ROOM is serialized under session.mu. The competitive AI
	// runtime can concurrently hold runtime.mu while resolving native peers; its
	// routing lookup must therefore use the published atomic identities only.
	session.mu.Lock()
	defer session.mu.Unlock()
	done := make(chan int, 1)
	go func() {
		done <- len(server.liveRoomRoutingSessions(7))
	}()
	select {
	case count := <-done:
		if count != 1 {
			t.Fatalf("room routing members = %d, want 1", count)
		}
	case <-time.After(time.Second):
		t.Fatal("room routing waited for the connection dispatcher mutex")
	}
}

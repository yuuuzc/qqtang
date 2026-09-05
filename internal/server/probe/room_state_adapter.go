package probe

import (
	"context"
	"fmt"
	"math"
	"sort"

	"qqtang/internal/game/equipment"
	lobbystate "qqtang/internal/game/lobby"
	"qqtang/internal/game/mapdata"
	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
)

func (server *Server) createSessionRoom(session *connectionSession, roomID uint16, mode byte) error {
	return server.createSessionRoomWithProperties(session, roomID, mode, 0, roomstate.Properties{})
}

func (server *Server) createSessionRoomWithProperties(session *connectionSession, roomID uint16, mode byte, continueID uint32, properties roomstate.Properties) error {
	if session == nil || session.Profile.PlayerID == 0 {
		return fmt.Errorf("cannot create room without a session player")
	}
	if session.RoomID != 0 {
		return fmt.Errorf("session player %d is already in room %d", session.Profile.PlayerID, session.RoomID)
	}
	uin := sessionWorldUIN(session)
	if err := server.worldState().EnterLobby(uin, session.Profile); err != nil {
		return err
	}
	snapshot, err := server.worldState().CreateRoomWithID(uin, session.Profile, roomID, roomstate.MatchSettings{
		Map: roomstate.RandomMapSelection(), GameType: roomstate.GameType(mode), ContinueID: continueID,
	}, properties)
	if err != nil {
		return err
	}
	session.setRoomID(roomID)
	session.detachedRoomID = 0
	session.worldUIN = uin
	if snapshot.RoomID != roomID {
		return fmt.Errorf("created room ID %d does not match requested ID %d", snapshot.RoomID, roomID)
	}
	return nil
}

func sessionWorldUIN(session *connectionSession) uint32 {
	if session == nil {
		return 0
	}
	if session.worldUIN != 0 {
		return session.worldUIN
	}
	if session.UIN != 0 {
		return session.UIN
	}
	// PlayerID is a room-local wire identity, not an authenticated account
	// identity. Falling back to it let an unauthenticated connection allocate
	// durable lobby/room state. Focused tests must supply an explicit synthetic
	// UIN just like production does after password authentication.
	return 0
}

func (server *Server) createAllocatedSessionRoom(session *connectionSession, mode byte, continueID uint32, properties roomstate.Properties) (uint16, error) {
	if session == nil || session.Profile.PlayerID == 0 {
		return 0, fmt.Errorf("cannot create room without a session player")
	}
	if session.RoomID != 0 {
		return 0, fmt.Errorf("session player %d is already in room %d", session.Profile.PlayerID, session.RoomID)
	}
	uin := sessionWorldUIN(session)
	if err := server.worldState().EnterLobby(uin, session.Profile); err != nil {
		return 0, err
	}
	snapshot, err := server.worldState().CreateRoom(uin, session.Profile, roomstate.MatchSettings{
		Map: roomstate.RandomMapSelection(), GameType: roomstate.GameType(mode), ContinueID: continueID,
	}, properties)
	if err != nil {
		return 0, err
	}
	session.setRoomID(snapshot.RoomID)
	session.detachedRoomID = 0
	session.worldUIN = uin
	return snapshot.RoomID, nil
}

// prepareSessionForRoomCreate treats a valid create-room request as the
// client's authoritative lobby intent. The legacy client does not always send
// a separately observable leave-room packet after returning from a retained
// post-match room, so a preparing room may be discarded here. An active match
// is never left implicitly.
func (server *Server) prepareSessionForRoomCreate(session *connectionSession) error {
	if session == nil {
		return fmt.Errorf("cannot prepare a nil session for room creation")
	}
	if session.CurrentGameID != 0 {
		return fmt.Errorf("session player %d is still in active game %d", session.Profile.PlayerID, session.CurrentGameID)
	}
	if session.RoomID != 0 {
		if _, err := server.leaveSessionRoom(session, roomstate.LeaveVoluntary); err != nil {
			return err
		}
	}
	session.CurrentMapID = 0
	session.CurrentStageGameID = 0
	return nil
}

// enterSessionRoom returns newlyJoined=false when the native client is
// rebuilding its room UI after GAME_OVER.  The client sends a normal
// ENTER_ROOM for that transition even though the authoritative room has
// deliberately retained its membership.  Treating that request as a duplicate
// used to reject the re-entry and the lobby dispatcher then removed the still
// valid member as an error rollback, leaving a ghost in every peer's room UI.
func (server *Server) enterSessionRoom(session *connectionSession, request game.EnterRoomRequest) (response game.EnterRoomResponseOld, newlyJoined bool, err error) {
	if session == nil || session.UIN == 0 || request.UIN != session.UIN {
		return game.EnterRoomResponseOld{}, false, fmt.Errorf("enter-room UIN %d does not match active session", request.UIN)
	}
	if session.CurrentGameID != 0 {
		return game.EnterRoomResponseOld{}, false, fmt.Errorf("player %d is already in game %d", session.Profile.PlayerID, session.CurrentGameID)
	}
	state, active := server.worldState().Room(request.RoomID)
	if !active {
		return game.EnterRoomResponseOld{}, false, rejectEnterRoom(game.EnterRoomResultRoomUnavailable, "room %d is not active", request.RoomID)
	}
	before := state.Snapshot()
	if before.Phase != roomstate.PhasePreparing {
		return game.EnterRoomResponseOld{}, false, rejectEnterRoom(game.EnterRoomResultGameInProgress, "room %d is not accepting members during phase %d", request.RoomID, before.Phase)
	}
	if session.RoomID != 0 {
		if session.RoomID != request.RoomID {
			return game.EnterRoomResponseOld{}, false, fmt.Errorf("player %d is already in room %d", session.Profile.PlayerID, session.RoomID)
		}
		retained := false
		for _, member := range before.Members {
			if member.PlayerID == session.Profile.PlayerID {
				retained = true
				break
			}
		}
		if !retained {
			return game.EnterRoomResponseOld{}, false, fmt.Errorf("player %d session points at room %d but authoritative membership is absent", session.Profile.PlayerID, request.RoomID)
		}
		response, err = server.enterRoomResponse(session, before)
		if err == nil {
			_, err = response.MarshalNetworkBinary()
		}
		if err != nil {
			return game.EnterRoomResponseOld{}, false, err
		}
		return response, false, nil
	}
	if len(before.Members) >= int(before.Capacity()) {
		return game.EnterRoomResponseOld{}, false, rejectEnterRoom(game.EnterRoomResultRoomFull, "room %d is full", request.RoomID)
	}
	if before.Properties.HasPassword && !request.PasswordMatches(before.Properties.Password) {
		return game.EnterRoomResponseOld{}, false, rejectEnterRoom(game.EnterRoomResultPasswordIncorrect, "room %d password does not match", request.RoomID)
	}
	roleID := request.RoleID
	if server.roleRules != nil {
		err = server.roleRules.ValidateRoomSelection(request.RoleID, session.Profile.Identity)
		if err != nil {
			return game.EnterRoomResponseOld{}, false, err
		}
	}
	updatedProfile := session.Profile
	updatedProfile.GameInfo.RoleID = roleID
	if err = updatedProfile.Validate(); err != nil {
		return game.EnterRoomResponseOld{}, false, fmt.Errorf("validate enter-room role profile: %w", err)
	}
	session.replaceProfile(updatedProfile)
	session.setSelectedRoleID(roleID)
	server.updateAuthenticatedSessionProfile(session.remoteAddress, session.UIN, session.Profile)
	if err = server.worldState().EnterLobby(sessionWorldUIN(session), session.Profile); err != nil {
		return game.EnterRoomResponseOld{}, false, err
	}
	joined, err := server.worldState().JoinRoom(sessionWorldUIN(session), session.Profile, request.RoomID, roleID, 1)
	if err != nil {
		return game.EnterRoomResponseOld{}, false, err
	}
	session.setRoomID(request.RoomID)
	session.detachedRoomID = 0
	session.worldUIN = sessionWorldUIN(session)
	response, err = server.enterRoomResponse(session, joined)
	if err == nil {
		_, err = response.MarshalNetworkBinary()
	}
	if err != nil {
		_, _ = server.leaveSessionRoom(session, roomstate.LeaveVoluntary)
		return game.EnterRoomResponseOld{}, false, err
	}
	return response, true, nil
}

func (server *Server) quickJoinSessionRoom(session *connectionSession, request game.JoinRoomRequest) (uint16, game.JoinRoomResponseOld, error) {
	if session == nil || session.UIN == 0 || request.UIN != session.UIN {
		return 0, game.JoinRoomResponseOld{}, fmt.Errorf("join-room UIN %d does not match active session", request.UIN)
	}
	if session.RoomID != 0 || session.CurrentGameID != 0 {
		return 0, game.JoinRoomResponseOld{}, fmt.Errorf("player %d is already in room %d or game %d", session.Profile.PlayerID, session.RoomID, session.CurrentGameID)
	}
	requestedType := roomstate.GameType(request.GameType)
	category, err := requestedType.Category()
	if err != nil {
		return 0, game.JoinRoomResponseOld{}, err
	}
	lobby, err := server.lobbySnapshot(session)
	if err != nil {
		return 0, game.JoinRoomResponseOld{}, err
	}
	for _, summary := range lobby.Rooms {
		if summary.GameType != request.GameType || summary.Phase != lobbystate.RoomPhasePreparing || summary.HasPassword {
			continue
		}
		state, active := server.worldState().Room(summary.RoomID)
		if !active {
			continue
		}
		snapshot := state.Snapshot()
		if snapshot.Phase != roomstate.PhasePreparing || snapshot.Properties.HasPassword || snapshot.Settings.GameType != requestedType {
			continue
		}
		capacity := int(snapshot.Capacity())
		if category == roomstate.CategoryAdventure && capacity > 4 {
			capacity = 4
		}
		if len(snapshot.Members) >= capacity {
			continue
		}
		type quickJoinResult struct {
			response game.EnterRoomResponseOld
			roomName string
		}
		joined, enterErr := callRoomActor(server, summary.RoomID, "quick-join-room", func() (quickJoinResult, error) {
			response, _, joinErr := server.enterSessionRoom(session, game.EnterRoomRequest{
				UIN: request.UIN, ClientTime: request.ClientTime, RoomID: summary.RoomID,
				RoleID: session.selectedRoleID(),
			})
			roomName := ""
			if joinErr == nil {
				roomName = state.Snapshot().Properties.Name
			}
			return quickJoinResult{response: response, roomName: roomName}, joinErr
		})
		if enterErr != nil {
			return 0, game.JoinRoomResponseOld{}, enterErr
		}
		return summary.RoomID, game.JoinRoomResponseOld{EnterRoomResponseOld: joined.response, RoomName: joined.roomName}, nil
	}
	return 0, game.JoinRoomResponseOld{}, fmt.Errorf("no joinable game-type %d room is available", request.GameType)
}

func (server *Server) enterRoomResponse(session *connectionSession, snapshot roomstate.Snapshot) (game.EnterRoomResponseOld, error) {
	if session == nil || session.Profile.SectionID == 0 {
		return game.EnterRoomResponseOld{}, fmt.Errorf("cannot build enter-room response without a section session")
	}
	if snapshot.Settings.Map.MapID > math.MaxUint16 {
		return game.EnterRoomResponseOld{}, fmt.Errorf("room map ID %d exceeds enter-room uint16 field", snapshot.Settings.Map.MapID)
	}
	section, err := server.worldState().Section(session.Profile.SectionID)
	if err != nil {
		return game.EnterRoomResponseOld{}, err
	}
	presenceByPlayer := make(map[uint16]lobbystate.Presence)
	for _, presence := range section.Snapshot(0).Players {
		presenceByPlayer[presence.PlayerID] = presence
	}
	response := game.EnterRoomResponseOld{
		RoomID: snapshot.RoomID, MapID: uint16(snapshot.Settings.Map.MapID),
		RoomFlag: snapshot.Properties.Flag, RoomOwnerID: snapshot.OwnerID,
		GameType: byte(snapshot.Settings.GameType), BackgroundID: snapshot.BackgroundID,
		WeddingModeID: snapshot.WeddingModeID,
	}
	for index := range response.SeatStatus {
		response.SeatStatus[index] = game.EnterRoomSeatOpen
	}
	for index, locked := range snapshot.LockedSeats {
		if locked {
			response.SeatStatus[index] = game.EnterRoomSeatLocked
		}
	}
	// RESPONSE_ENTER_ROOM_OLD enumerates the members already present before the
	// entering client's own record.  QQTPPP builds those remote peers first and
	// binds the local actor from the final record.  Sorting solely by PlayerID or
	// SeatID puts a rejoining client first whenever it takes a lower free seat;
	// that client then never registers the existing peer.  Room ownership and
	// seating remain independent fields and are not changed by this projection.
	members := enterRoomMembers(snapshot, session.Profile.PlayerID)
	for _, member := range members {
		presence, ok := presenceByPlayer[member.PlayerID]
		if !ok || presence.UIN == 0 || presence.RoomID != snapshot.RoomID {
			return game.EnterRoomResponseOld{}, fmt.Errorf("room player %d has no matching section presence", member.PlayerID)
		}
		profile := server.config.playerProfileForUIN(presence.UIN)
		if server.playerStore != nil {
			profile, err = server.playerStore.Load(context.Background(), presence.UIN)
			if err != nil {
				return game.EnterRoomResponseOld{}, fmt.Errorf("load room player %d profile: %w", member.PlayerID, err)
			}
		}
		profile.GameInfo.RoleID = member.RoleID
		var assignments []equipment.Assignment
		profile, assignments, err = server.projectProfileEquipment(context.Background(), presence.UIN, profile)
		if err != nil {
			return game.EnterRoomResponseOld{}, fmt.Errorf("project room player %d equipment: %w", member.PlayerID, err)
		}
		profile.Inventory = server.equipmentCatalog.ProjectInventoryForRoom(profile.Inventory, assignments)
		status := byte(0)
		if member.Ready {
			status = 1
		}
		response.SeatStatus[member.SeatID-1] = game.EnterRoomSeatOccupied
		roomPlayer := game.PlayerInfoInRoomOldFromProfile(presence.UIN, profile, member.TeamID, member.SeatID, status)
		if err = server.projectRoomPlayerPet(context.Background(), presence.UIN, profile.GameInfo.PetID, &roomPlayer); err != nil {
			return game.EnterRoomResponseOld{}, fmt.Errorf("project room player %d pet: %w", member.PlayerID, err)
		}
		response.Players = append(response.Players, game.EnterRoomPlayer{
			Player:    roomPlayer,
			Attach:    game.PlayerInfoInRoomAttach{Honor: profile.Honor},
			SpouseUIN: profile.SpouseUIN,
			Patterns:  append([]game.PatternPoint(nil), profile.PatternPoints...),
		})
		if member.PlayerID == session.Profile.PlayerID {
			response.LocalTeamID = member.TeamID
			response.LocalSeatID = member.SeatID
		}
	}
	if response.LocalSeatID == 0 {
		return game.EnterRoomResponseOld{}, fmt.Errorf("local player %d is absent from room %d", session.Profile.PlayerID, snapshot.RoomID)
	}
	return response, nil
}

func (server *Server) projectRoomPlayerPet(ctx context.Context, uin, activePetID uint32, player *game.PlayerInfoInRoomOld) error {
	if player == nil || activePetID == 0 || server.playerStore == nil {
		return nil
	}
	pets, err := server.playerStore.ListPets(ctx, uin)
	if err != nil {
		return err
	}
	for _, pet := range pets {
		if pet.PetID != activePetID {
			continue
		}
		if pet.PetState != game.PetStateActive {
			return fmt.Errorf("pet %d is selected in GAME_INFO but is not active", activePetID)
		}
		pet = server.projectPetForClient(pet)
		encoded, encodeErr := pet.AppendNetworkBinary(nil)
		if encodeErr != nil {
			return encodeErr
		}
		copy(player.PetBaseInfo[:], encoded)
		return nil
	}
	return fmt.Errorf("active pet %d is absent from player_pets", activePetID)
}

func enterRoomMembers(snapshot roomstate.Snapshot, localPlayerID uint16) []roomstate.Member {
	members := append([]roomstate.Member(nil), snapshot.Members...)
	sort.Slice(members, func(i, j int) bool {
		iLocal := members[i].PlayerID == localPlayerID
		jLocal := members[j].PlayerID == localPlayerID
		if iLocal != jLocal {
			return !iLocal
		}
		return members[i].SeatID < members[j].SeatID
	})
	return members
}

func (server *Server) sessionRoom(session *connectionSession) (*roomstate.State, error) {
	if session == nil || session.RoomID == 0 {
		return nil, fmt.Errorf("session is not in a room")
	}
	state, active := server.worldState().Room(session.RoomID)
	if !active {
		return nil, fmt.Errorf("room %d is not active", session.RoomID)
	}
	return state, nil
}

func (server *Server) activeSessionMatchCategory(session *connectionSession) (roomstate.Category, error) {
	if session == nil || session.CurrentGameID == 0 {
		return 0, fmt.Errorf("session has no active match")
	}
	state, err := server.sessionRoom(session)
	if err != nil {
		return 0, err
	}
	snapshot := state.Snapshot()
	if snapshot.ActiveGameID != session.CurrentGameID || snapshot.Phase != roomstate.PhaseInMatch {
		return 0, fmt.Errorf("session game %d does not match room %d active game %d phase %d", session.CurrentGameID, snapshot.RoomID, snapshot.ActiveGameID, snapshot.Phase)
	}
	return snapshot.Settings.GameType.Category()
}

func (server *Server) updateSessionRoomSettings(session *connectionSession, mapID uint32, mode byte, continueID uint32) error {
	state, err := server.sessionRoom(session)
	if err != nil {
		return err
	}
	current := state.Snapshot().Settings
	competitiveMode, playerLimit, err := server.roomListCompetitiveMode(current.GameType, mapID)
	if err != nil {
		return err
	}
	_, err = server.worldState().UpdateMatchSettings(sessionWorldUIN(session), roomstate.MatchSettings{
		Map: roomstate.FixedMapSelection(mapID), GameType: current.GameType,
		PlayerLimit: playerLimit, CompetitiveMode: competitiveMode,
		SelectedMapType: roomstate.SelectedMapType(mode), ContinueID: continueID,
	})
	return err
}

func (server *Server) roomListCompetitiveMode(gameType roomstate.GameType, mapID uint32) (byte, byte, error) {
	category, err := gameType.Category()
	if err != nil {
		return 0, 0, err
	}
	if category != roomstate.CategoryCompetitive {
		return 0, 0, nil
	}
	if server.mapCatalog == nil {
		return 0, 0, fmt.Errorf("competitive map catalog is unavailable")
	}
	selected, ok := server.mapCatalog.CompetitiveMap(mapID)
	if !ok || !selected.Selectable {
		return 0, 0, fmt.Errorf("competitive map %d is not installed or selectable", mapID)
	}
	if selected.NativeRule == 0 || selected.NativeRule > math.MaxUint8 {
		return 0, 0, fmt.Errorf("competitive map %d native rule %d is outside room-list byte", mapID, selected.NativeRule)
	}
	if selected.PlayerLimit == 0 || selected.PlayerLimit > roomstate.RoomSeatCount {
		return 0, 0, fmt.Errorf("competitive map %d player limit %d is outside 1..%d", mapID, selected.PlayerLimit, roomstate.RoomSeatCount)
	}
	return byte(selected.NativeRule), selected.PlayerLimit, nil
}

func (server *Server) validateSessionRoomMapSelection(session *connectionSession, mapID uint32) error {
	if mapID == 0 {
		return nil
	}
	state, err := server.sessionRoom(session)
	if err != nil {
		return err
	}
	snapshot := state.Snapshot()
	category, err := snapshot.Settings.GameType.Category()
	if err != nil {
		return err
	}
	switch category {
	case roomstate.CategoryAdventure:
		if _, ok := server.mapCatalog.SelectableMap(mapID); !ok {
			return fmt.Errorf("adventure map %d is not a selectable first stage", mapID)
		}
	case roomstate.CategoryCompetitive:
		selected, ok := server.mapCatalog.CompetitiveMap(mapID)
		if !ok || !selected.Selectable {
			return fmt.Errorf("competitive map %d is not installed or selectable", mapID)
		}
		if len(snapshot.Members) > int(selected.PlayerLimit) {
			return fmt.Errorf("competitive map %d supports %d players, room has %d", mapID, selected.PlayerLimit, len(snapshot.Members))
		}
		field, ok := snapshot.Settings.GameType.CompetitiveField()
		if !ok {
			return fmt.Errorf("room game type %d is not competitive", snapshot.Settings.GameType)
		}
		itemField := byte(0)
		if field == roomstate.CompetitiveFieldItem {
			itemField = 1
		}
		if selected.RequiredItemField != itemField {
			return fmt.Errorf("competitive map %d required item field %d does not match room field %d", mapID, selected.RequiredItemField, itemField)
		}
		if !profileOwnsActiveItem(session.Profile, vipCardItemID) && session.Profile.GameInfo.Point < selected.RequiredPoints {
			return fmt.Errorf("competitive map %d requires %d points, player has %d", mapID, selected.RequiredPoints, session.Profile.GameInfo.Point)
		}
	default:
		return fmt.Errorf("room category %d does not support a battle map", category)
	}
	return nil
}

func (server *Server) updateSessionRoomRandomMap(session *connectionSession, selectedMapType byte, continueID uint32) error {
	state, err := server.sessionRoom(session)
	if err != nil {
		return err
	}
	current := state.Snapshot().Settings
	_, err = server.worldState().UpdateMatchSettings(sessionWorldUIN(session), roomstate.MatchSettings{
		Map: roomstate.RandomMapSelection(), GameType: current.GameType,
		SelectedMapType: roomstate.SelectedMapType(selectedMapType), ContinueID: continueID,
	})
	return err
}

func (server *Server) updateSessionRoomGameType(session *connectionSession, gameType byte, continueID uint32) error {
	_, err := server.worldState().UpdateMatchSettings(sessionWorldUIN(session), roomstate.MatchSettings{
		Map: roomstate.RandomMapSelection(), GameType: roomstate.GameType(gameType),
		ContinueID: continueID,
	})
	return err
}

func (server *Server) resolveSessionRoomMap(session *connectionSession) (mapdata.AdventureMap, error) {
	state, err := server.sessionRoom(session)
	if err != nil {
		return mapdata.AdventureMap{}, err
	}
	selection := state.Snapshot().Settings.Map
	switch selection.Kind {
	case roomstate.MapSelectionRandom:
		selected, randomErr := server.mapCatalog.RandomSelectable()
		if randomErr != nil {
			return mapdata.AdventureMap{}, randomErr
		}
		return selected, nil
	case roomstate.MapSelectionFixed:
		selected, ok := server.mapCatalog.SelectableMap(selection.MapID)
		if !ok {
			return mapdata.AdventureMap{}, fmt.Errorf("fixed room map %d is not a selectable first stage", selection.MapID)
		}
		return selected, nil
	default:
		return mapdata.AdventureMap{}, fmt.Errorf("unknown room map selection kind %d", selection.Kind)
	}
}

func (server *Server) resolveSessionCompetitiveMap(session *connectionSession, participantCount int) (mapdata.CompetitiveMap, error) {
	state, err := server.sessionRoom(session)
	if err != nil {
		return mapdata.CompetitiveMap{}, err
	}
	settings := state.Snapshot().Settings
	field, ok := settings.GameType.CompetitiveField()
	if !ok {
		return mapdata.CompetitiveMap{}, fmt.Errorf("room game type %d is not competitive", settings.GameType)
	}
	itemField := byte(0)
	if field == roomstate.CompetitiveFieldItem {
		itemField = 1
	}
	switch settings.Map.Kind {
	case roomstate.MapSelectionRandom:
		return server.mapCatalog.RandomEligibleOrdinaryCompetitive(participantCount, itemField, session.Profile.GameInfo.Point, profileOwnsActiveItem(session.Profile, vipCardItemID))
	case roomstate.MapSelectionFixed:
		selected, found := server.mapCatalog.CompetitiveMap(settings.Map.MapID)
		if !found {
			return mapdata.CompetitiveMap{}, fmt.Errorf("fixed competitive map %d is not installed or selectable", settings.Map.MapID)
		}
		if int(selected.PlayerLimit) < participantCount {
			return mapdata.CompetitiveMap{}, fmt.Errorf("competitive map %d supports %d players, room has %d", selected.ID, selected.PlayerLimit, participantCount)
		}
		if selected.RequiredItemField != itemField {
			return mapdata.CompetitiveMap{}, fmt.Errorf("competitive map %d required item field %d does not match room field %d", selected.ID, selected.RequiredItemField, itemField)
		}
		if !profileOwnsActiveItem(session.Profile, vipCardItemID) && session.Profile.GameInfo.Point < selected.RequiredPoints {
			return mapdata.CompetitiveMap{}, fmt.Errorf("competitive map %d requires %d points, player has %d", selected.ID, selected.RequiredPoints, session.Profile.GameInfo.Point)
		}
		return selected, nil
	default:
		return mapdata.CompetitiveMap{}, fmt.Errorf("unknown competitive map selection kind %d", settings.Map.Kind)
	}
}

func (server *Server) updateSessionRoomRole(session *connectionSession, requestedRoleID byte) (byte, error) {
	state, err := server.sessionRoom(session)
	if err != nil {
		return 0, err
	}
	roleID := requestedRoleID
	if server.roleRules != nil {
		err = server.roleRules.ValidateRoomSelection(requestedRoleID, session.Profile.Identity)
		if err != nil {
			return 0, err
		}
	}
	snapshot := state.Snapshot()
	for _, member := range snapshot.Members {
		if member.PlayerID == session.Profile.PlayerID {
			member.RoleID = roleID
			updatedProfile := session.Profile
			updatedProfile.GameInfo.RoleID = roleID
			if err = updatedProfile.Validate(); err != nil {
				return 0, fmt.Errorf("validate selected role profile: %w", err)
			}
			if _, err = server.worldState().SetMember(sessionWorldUIN(session), roleID, 0); err != nil {
				return 0, err
			}
			session.replaceProfile(updatedProfile)
			session.setSelectedRoleID(roleID)
			server.updateAuthenticatedSessionProfile(session.remoteAddress, session.UIN, session.Profile)
			return roleID, nil
		}
	}
	return 0, fmt.Errorf("session player %d is not a member of room %d", session.Profile.PlayerID, session.RoomID)
}

func (server *Server) updateSessionRoomTeam(session *connectionSession, teamID byte) error {
	if teamID < game.MinRoomTeamID || teamID > game.MaxRoomTeamID {
		return fmt.Errorf("room TeamID %d is outside %d..%d", teamID, game.MinRoomTeamID, game.MaxRoomTeamID)
	}
	_, err := server.worldState().SetMember(sessionWorldUIN(session), 0, teamID)
	return err
}

func (server *Server) updateSessionRoomReady(session *connectionSession, ready bool) error {
	if session == nil || session.UIN == 0 || session.Profile.PlayerID == 0 {
		return fmt.Errorf("cannot change ready state without an active player session")
	}
	_, err := server.worldState().SetReady(sessionWorldUIN(session), ready)
	return err
}

func (server *Server) updateSessionRoomSeatStatus(session *connectionSession, seatID byte, locked bool) error {
	_, err := server.worldState().SetSeatLocked(sessionWorldUIN(session), seatID, locked)
	return err
}

func (server *Server) updateSessionRoomProperties(session *connectionSession, request game.ModifyRoomRequest) (roomstate.Snapshot, error) {
	if session == nil || session.UIN == 0 || request.UIN != session.UIN {
		return roomstate.Snapshot{}, fmt.Errorf("modify-room UIN %d does not match active session UIN", request.UIN)
	}
	state, err := server.sessionRoom(session)
	if err != nil {
		return roomstate.Snapshot{}, err
	}
	properties := state.Snapshot().Properties
	if request.NameChanged() {
		name, nameErr := request.RoomNameString()
		if nameErr != nil {
			return roomstate.Snapshot{}, nameErr
		}
		// One client initialization capture carries ApplyName with an empty
		// slot while preserving the current free-rule value. Treat that form as
		// a name no-op instead of erasing the room card.
		if name != "" {
			properties.Name = name
		}
	}
	// Standard/free and password-enabled are independent room properties. The
	// statically repaired client producer always writes their complete value,
	// including when the native name/password change mask is zero.
	properties.Flag = byte(request.PropertyFlag())
	properties.HasPassword = request.HasPassword()
	if properties.HasPassword {
		properties.Password = request.Password
	} else {
		properties.Password = [16]byte{}
	}
	return server.worldState().UpdateRoomProperties(sessionWorldUIN(session), properties)
}

package probe

import (
	"context"
	"fmt"
	"math"
	"sort"

	lobbystate "qqtang/internal/game/lobby"
	"qqtang/internal/protocol/game"
)

const (
	localLobbyServerID uint32 = 1
	localLobbyDialogID uint16 = 1
)

func (server *Server) lobbySnapshot(session *connectionSession) (lobbystate.Snapshot, error) {
	if session == nil || session.UIN == 0 || session.Profile.SectionID == 0 {
		return lobbystate.Snapshot{}, fmt.Errorf("lobby list requires a logged-in session")
	}
	return server.worldState().LobbySnapshot(session.Profile.SectionID, 0)
}

func (server *Server) playerListEntries(session *connectionSession, request game.PlayerListRequest) ([]game.PlayerListEntry, error) {
	snapshot, err := server.lobbySnapshot(session)
	if err != nil {
		return nil, err
	}
	entries := make([]game.PlayerListEntry, 0, len(snapshot.Players))
	stalePlayers := make([]lobbystate.Presence, 0)
	for _, presence := range snapshot.Players {
		// The lobby state is the durable projection used by room/list adapters,
		// but online presence ultimately belongs to a live primary game socket.
		// A process crash can race the legacy disconnect callback; never expose
		// that stale projection as an online player on the next periodic refresh.
		// Auxiliary shop sockets intentionally keep liveUIN at zero and therefore
		// cannot keep a disconnected account visible here.
		if !server.isLiveLobbyPlayer(session, presence.UIN, presence.SectionID) {
			stalePlayers = append(stalePlayers, presence)
			continue
		}
		profile := server.config.playerProfileForUIN(presence.UIN)
		switch {
		case presence.UIN == session.UIN:
			profile = session.Profile
		case server.playerStore != nil:
			profile, err = server.playerStore.Load(context.Background(), presence.UIN)
			if err != nil {
				return nil, fmt.Errorf("load online profile %d: %w", presence.UIN, err)
			}
		}
		// Presence owns the live section identity; SQLite owns durable profile
		// fields. They must agree, but preserving the live IDs prevents a stale
		// row from aliasing another online player during this response.
		profile.PlayerID = presence.PlayerID
		if profile.Nickname == "" {
			profile.Nickname = presence.Nickname
		}
		// The selected character is online session state, not durable account
		// state. SQLite therefore legitimately loads RoleID 0 for a remote
		// player; recover the live role before resolving role-scoped namecards
		// and other external equipment for every initial/periodic list page.
		if live := server.primarySessionForUIN(presence.UIN); live != nil {
			profile.GameInfo.RoleID = live.routingRoleID()
		}
		profile, assignments, projectionErr := server.projectProfileEquipment(context.Background(), presence.UIN, profile)
		if projectionErr != nil {
			return nil, fmt.Errorf("project online profile %d equipment: %w", presence.UIN, projectionErr)
		}
		entry := game.PlayerListEntryFromProfile(presence.UIN, profile)
		entry.Player.ExtItemIDs = server.equipmentCatalog.ItemIDsForRole(assignments, profile.GameInfo.RoleID)
		entries = append(entries, entry)
	}
	server.pruneStaleLobbyPlayers(session, stalePlayers)

	// Original lobby behavior orders by competitive experience. Degree is a
	// stable secondary projection; PlayerID guarantees deterministic ties.
	sort.Slice(entries, func(i, j int) bool {
		left, right := entries[i].Player, entries[j].Player
		if left.GameInfo.Point != right.GameInfo.Point {
			return left.GameInfo.Point > right.GameInfo.Point
		}
		if left.GameInfo.Degree != right.GameInfo.Degree {
			return left.GameInfo.Degree > right.GameInfo.Degree
		}
		return left.PlayerID < right.PlayerID
	})

	start := 0
	if request.StartPlayerID != 0 {
		for index := range entries {
			if entries[index].Player.PlayerID == request.StartPlayerID {
				start = index + 1
				break
			}
		}
	}
	if start >= len(entries) || request.Number == 0 {
		return nil, nil
	}
	limit := int(request.Number)
	if limit > game.PlayerListMaximumCount {
		limit = game.PlayerListMaximumCount
	}
	end := start + limit
	if end > len(entries) {
		end = len(entries)
	}
	return append([]game.PlayerListEntry(nil), entries[start:end]...), nil
}

// pruneStaleLobbyPlayers heals the durable lobby projection after an abrupt
// process exit raced or bypassed the legacy disconnect callback. The live
// primary game socket is the online-presence authority; shop/continuation
// sockets deliberately never satisfy isLiveLobbyPlayer. Pruning is idempotent
// and also increments the lobby revision, so every client's next periodic
// list refresh observes the departure instead of retaining an empty profile.
func (server *Server) pruneStaleLobbyPlayers(locked *connectionSession, players []lobbystate.Presence) {
	for _, presence := range players {
		server.liveMu.Lock()
		live := server.isLiveLobbyPlayerLocked(presence.UIN, presence.SectionID)
		if !live {
			// Keep the live-session ownership lock through compare-and-delete.
			// A reconnect either publishes liveUIN first and prevents this delete,
			// or starts afterwards and recreates its presence normally.
			server.worldState().LeaveLobby(presence.UIN, presence.SectionID)
		}
		server.liveMu.Unlock()
		if live {
			continue
		}
		server.log(logEvent{
			Level: "warn", Event: "stale_lobby_presence_pruned",
			AccountID: fmt.Sprint(presence.UIN), Result: fmt.Sprintf("section_%d", presence.SectionID),
		})
	}
}

func (server *Server) roomListEntries(session *connectionSession) ([]game.RoomListEntry, error) {
	snapshot, err := server.lobbySnapshot(session)
	if err != nil {
		return nil, err
	}
	rooms := make([]game.RoomListEntry, 0, len(snapshot.Rooms))
	for _, room := range snapshot.Rooms {
		entry, entryErr := roomListEntry(room)
		if entryErr != nil {
			return nil, entryErr
		}
		rooms = append(rooms, entry)
	}
	return rooms, nil
}

func roomListEntry(room lobbystate.RoomSummary) (game.RoomListEntry, error) {
	if room.MapID > math.MaxUint16 {
		return game.RoomListEntry{}, fmt.Errorf("room %d map ID %d exceeds ROOM_INFO uint16", room.RoomID, room.MapID)
	}
	population, err := game.PackRoomPopulation(room.Players, room.MaxPlayers)
	if err != nil {
		return game.RoomListEntry{}, fmt.Errorf("room %d: %w", room.RoomID, err)
	}
	// QQTSection consumes bit 0 as the preparing marker and derives
	// joinable/full from the packed population. A cleared bit renders an
	// in-match disabled card. Bits 1, 2 and 3 are password, free rule and VIP.
	var roomListFlag game.RoomListFlag
	switch room.Phase {
	case lobbystate.RoomPhasePreparing:
		roomListFlag |= game.RoomListFlagPreparing
	case lobbystate.RoomPhaseInMatch, lobbystate.RoomPhaseSettling:
		roomListFlag |= game.RoomListFlagInMatch
	default:
		return game.RoomListEntry{}, fmt.Errorf("room %d has unknown lobby phase %d", room.RoomID, room.Phase)
	}
	if room.HasPassword {
		roomListFlag |= game.RoomListFlagPassword
	}
	if room.UsesFreeRule {
		roomListFlag |= game.RoomListFlagFreeRule
	}
	if room.IsVIP {
		roomListFlag |= game.RoomListFlagVIP
	}
	return game.RoomListEntry{
		Name: room.Name, RoomID: room.RoomID, RoomFlag: roomListFlag,
		MapID: uint16(room.MapID), NumOfPlayer: population,
		GameType: room.GameType, ContinueID: room.ContinueID,
	}, nil
}

func (server *Server) roomListPage(session *connectionSession, request game.RoomListRequest) ([]game.RoomListEntry, error) {
	snapshot, err := server.lobbySnapshot(session)
	if err != nil {
		return nil, err
	}
	rooms := make([]game.RoomListEntry, 0, len(snapshot.Rooms))
	for _, room := range snapshot.Rooms {
		if !roomMatchesListRequest(room, request) {
			continue
		}
		entry, entryErr := roomListEntry(room)
		if entryErr != nil {
			return nil, entryErr
		}
		rooms = append(rooms, entry)
	}
	start := 0
	if request.StartRoomID != 0 {
		for start < len(rooms) && rooms[start].RoomID < request.StartRoomID {
			start++
		}
	}
	if start >= len(rooms) || request.Number == 0 {
		return nil, nil
	}
	limit := int(request.Number)
	if limit > game.RoomListMaximumCount {
		limit = game.RoomListMaximumCount
	}
	end := start + limit
	if end > len(rooms) {
		end = len(rooms)
	}
	return append([]game.RoomListEntry(nil), rooms[start:end]...), nil
}

func roomMatchesListRequest(room lobbystate.RoomSummary, request game.RoomListRequest) bool {
	if room.GameType != request.GameType {
		return false
	}
	switch request.GameMode {
	case game.RoomListFilterAllRooms:
		return true
	case game.RoomListFilterLegacyAllMaps, game.RoomListFilterAllMaps:
		return room.Phase == lobbystate.RoomPhasePreparing
	default:
		mode, specific := request.GameMode.CompetitiveMode()
		return specific && room.Phase == lobbystate.RoomPhasePreparing && room.GameMode == mode
	}
}

func (server *Server) findFriendResponse(session *connectionSession, friendUIN uint32) (game.FindFriendResponse, error) {
	snapshot, err := server.lobbySnapshot(session)
	if err != nil {
		return game.FindFriendResponse{}, err
	}
	var presence lobbystate.Presence
	var hasPresence bool
	var online bool
	for index := range snapshot.Players {
		if snapshot.Players[index].UIN != friendUIN {
			continue
		}
		// Preserve the last advertised identity before pruning it. The legacy
		// client deliberately asks FIND_FRIEND about a row that survived its
		// merge-only player-list refresh; an offline response is what invokes
		// the client's native row-removal routine.
		presence = snapshot.Players[index]
		hasPresence = true
		if server.isLiveLobbyPlayer(session, friendUIN, snapshot.Players[index].SectionID) {
			online = true
		} else {
			server.pruneStaleLobbyPlayers(session, []lobbystate.Presence{snapshot.Players[index]})
		}
		break
	}
	profile := server.config.playerProfileForUIN(friendUIN)
	if friendUIN == session.UIN {
		profile = session.Profile
	} else if server.playerStore != nil {
		profile, err = server.playerStore.Load(context.Background(), friendUIN)
		if err != nil {
			return game.FindFriendResponse{}, fmt.Errorf("load friend profile %d: %w", friendUIN, err)
		}
	}
	if hasPresence {
		profile.PlayerID = presence.PlayerID
		profile.SectionID = presence.SectionID
		if profile.Nickname == "" {
			profile.Nickname = presence.Nickname
		}
	}
	if online {
		if live := server.primarySessionForUIN(friendUIN); live != nil {
			profile.GameInfo.RoleID = live.routingRoleID()
		}
	}
	profile, _, err = server.projectProfileEquipment(context.Background(), friendUIN, profile)
	if err != nil {
		return game.FindFriendResponse{}, fmt.Errorf("project friend profile %d equipment: %w", friendUIN, err)
	}
	roomName := ""
	roomID := uint16(0)
	if online && hasPresence && presence.RoomID != 0 {
		roomID = presence.RoomID
		for _, room := range snapshot.Rooms {
			if room.RoomID == roomID {
				roomName = room.Name
				break
			}
		}
	}
	status := game.FindFriendStatusOffline
	if online {
		status = game.FindFriendStatusOnline
	}
	return findFriendResponseProjection(friendUIN, profile, status, roomID, roomName), nil
}

func findFriendResponseProjection(friendUIN uint32, profile game.PlayerProfile, status game.FindFriendStatus, roomID uint16, roomName string) game.FindFriendResponse {
	normalized := profile.ToClientLoginConfig()
	return game.FindFriendResponse{
		FriendUIN: friendUIN, PlayerName: profile.Nickname, PlayerID: profile.PlayerID,
		Gender: profile.Gender, IconID: profile.IconID, Identity: profile.Identity,
		ServerID: localLobbyServerID, DialogID: localLobbyDialogID,
		SectionID: profile.SectionID, RoomID: roomID, RoomName: roomName,
		Status: status, GameInfo: normalized.GameInfo,
		Items: normalized.Items, KinIndex: profile.KinIndex, KinName: profile.KinName,
		KinFlagID: game.KinFlagIDForWire(profile.KinIndex, profile.KinFlagID), Honor: profile.Honor,
		PatternPoints: append([]game.PatternPoint(nil), profile.PatternPoints...),
	}
}

// isLiveLobbyPlayer keeps protocol-independent lobby state honest at its
// network boundary. A nil live-session map is accepted only by focused state
// tests; every production Server created by New has the map initialized.
func (server *Server) isLiveLobbyPlayer(_ *connectionSession, uin uint32, sectionID uint16) bool {
	if uin == 0 || sectionID == 0 {
		return false
	}
	server.liveMu.RLock()
	live := server.isLiveLobbyPlayerLocked(uin, sectionID)
	server.liveMu.RUnlock()
	return live
}

// isLiveLobbyPlayerLocked must be called with liveMu held. Keeping the
// compare and stale-presence deletion under the same ownership lock prevents
// an old list snapshot from deleting a newly claimed login.
func (server *Server) isLiveLobbyPlayerLocked(uin uint32, sectionID uint16) bool {
	if server.liveSessions == nil {
		return true
	}
	for _, active := range server.liveSessions {
		// Auxiliary shop sessions never publish liveUIN. Routing identity is an
		// immutable/atomic projection, so two simultaneous player-list requests
		// cannot cross-lock their connection actors.
		if active != nil && active.liveUIN.Load() == uin && active.routingSectionID() == sectionID {
			return true
		}
	}
	return false
}

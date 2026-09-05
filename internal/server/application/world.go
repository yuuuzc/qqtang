package application

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"

	lobbystate "qqtang/internal/game/lobby"
	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
)

// World owns protocol-independent online state. It is intentionally usable by
// both the TCP adapter and headless integration scenarios.
type World struct {
	mu          sync.RWMutex
	lobby       *lobbystate.Hub
	rooms       map[uint16]*roomRecord
	memberships map[uint32]membership
	nextGameID  atomic.Uint32
}

type roomRecord struct {
	sectionID uint16
	state     *roomstate.State
}

type membership struct {
	sectionID uint16
	roomID    uint16
	playerID  uint16
}

// MembershipSnapshot is the read-only protocol-independent projection used by
// diagnostics and headless tests.  Connection adapters must not mutate World
// state through it.
type MembershipSnapshot struct {
	UIN       uint32 `json:"uin"`
	SectionID uint16 `json:"section_id"`
	RoomID    uint16 `json:"room_id"`
	PlayerID  uint16 `json:"player_id"`
}

// WorldSnapshot exposes one section's complete authoritative state without
// exposing the World's internal maps or locks.  Keeping this projection in the
// application layer preserves packet-level debugging while preventing tests
// and tools from depending on probe.Server implementation details.
type WorldSnapshot struct {
	Lobby       lobbystate.Snapshot  `json:"lobby"`
	Rooms       []roomstate.Snapshot `json:"rooms"`
	Memberships []MembershipSnapshot `json:"memberships"`
}

func NewWorld() *World {
	return &World{
		lobby: lobbystate.NewHub(), rooms: make(map[uint16]*roomRecord),
		memberships: make(map[uint32]membership),
	}
}

func (world *World) Section(sectionID uint16) (*lobbystate.Section, error) {
	if world == nil || world.lobby == nil {
		return nil, fmt.Errorf("application world is unavailable")
	}
	return world.lobby.Section(sectionID)
}

func (world *World) EnterLobby(uin uint32, profile game.PlayerProfile) error {
	if world == nil || uin == 0 || profile.PlayerID == 0 || profile.SectionID == 0 {
		return fmt.Errorf("enter lobby requires a world, UIN, player ID, and section ID")
	}
	section, err := world.Section(profile.SectionID)
	if err != nil {
		return err
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	current := world.memberships[uin]
	return section.UpsertPlayer(lobbystate.Presence{
		UIN: uin, PlayerID: profile.PlayerID, SectionID: profile.SectionID,
		RoomID: current.roomID, Nickname: profile.Nickname,
	})
}

// LobbyPresence resolves the sender identity from the section-owned online
// member table. A TCP connection profile is a cache and must not become the
// authority for lobby-visible player IDs or names.
func (world *World) LobbyPresence(uin uint32, sectionID uint16) (lobbystate.Presence, error) {
	if world == nil || uin == 0 || sectionID == 0 {
		return lobbystate.Presence{}, fmt.Errorf("lobby presence requires a world, UIN, and section ID")
	}
	section, err := world.Section(sectionID)
	if err != nil {
		return lobbystate.Presence{}, err
	}
	presence, ok := section.Player(uin)
	if !ok {
		return lobbystate.Presence{}, fmt.Errorf("UIN %d is not online in section %d", uin, sectionID)
	}
	return presence, nil
}

func (world *World) LeaveLobby(uin uint32, sectionID uint16) {
	if world == nil || sectionID == 0 {
		return
	}
	if section, err := world.Section(sectionID); err == nil {
		section.RemovePlayer(uin)
	}
}

func (world *World) CreateRoom(uin uint32, profile game.PlayerProfile, settings roomstate.MatchSettings, properties roomstate.Properties) (roomstate.Snapshot, error) {
	if world == nil || uin == 0 || profile.PlayerID == 0 || profile.SectionID == 0 {
		return roomstate.Snapshot{}, fmt.Errorf("create room requires an online player")
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	if current := world.memberships[uin]; current.roomID != 0 {
		return roomstate.Snapshot{}, fmt.Errorf("UIN %d is already in room %d", uin, current.roomID)
	}
	roomID, err := world.allocateRoomIDLocked()
	if err != nil {
		return roomstate.Snapshot{}, err
	}
	return world.createRoomLocked(uin, profile, roomID, settings, properties)
}

// CreateRoomWithID restores or adapts a room whose wire ID has already been
// selected. New protocol paths should normally call CreateRoom; this method is
// retained for deterministic replay and focused compatibility tests.
func (world *World) CreateRoomWithID(uin uint32, profile game.PlayerProfile, roomID uint16, settings roomstate.MatchSettings, properties roomstate.Properties) (roomstate.Snapshot, error) {
	if world == nil || uin == 0 || profile.PlayerID == 0 || profile.SectionID == 0 || roomID == 0 {
		return roomstate.Snapshot{}, fmt.Errorf("create room with ID requires an online player and room ID")
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	if current := world.memberships[uin]; current.roomID != 0 {
		return roomstate.Snapshot{}, fmt.Errorf("UIN %d is already in room %d", uin, current.roomID)
	}
	if world.rooms[roomID] != nil {
		return roomstate.Snapshot{}, fmt.Errorf("room %d already exists", roomID)
	}
	return world.createRoomLocked(uin, profile, roomID, settings, properties)
}

func (world *World) createRoomLocked(uin uint32, profile game.PlayerProfile, roomID uint16, settings roomstate.MatchSettings, properties roomstate.Properties) (roomstate.Snapshot, error) {
	state, err := roomstate.New(roomID, roomstate.Member{
		PlayerID: profile.PlayerID, RoleID: profile.GameInfo.RoleID, TeamID: 1, SeatID: 1,
		Identity: profile.Identity,
	}, settings)
	if err != nil {
		return roomstate.Snapshot{}, err
	}
	if _, err = state.UpdateProperties(profile.PlayerID, properties); err != nil {
		return roomstate.Snapshot{}, err
	}
	record := &roomRecord{sectionID: profile.SectionID, state: state}
	world.rooms[roomID] = record
	world.memberships[uin] = membership{sectionID: profile.SectionID, roomID: roomID, playerID: profile.PlayerID}
	if err = world.syncRoomLocked(record); err != nil {
		delete(world.rooms, roomID)
		delete(world.memberships, uin)
		return roomstate.Snapshot{}, err
	}
	if err = world.setPlayerRoomLocked(uin, profile.SectionID, roomID); err != nil {
		delete(world.rooms, roomID)
		delete(world.memberships, uin)
		if section, sectionErr := world.Section(profile.SectionID); sectionErr == nil {
			section.RemoveRoom(roomID)
		}
		return roomstate.Snapshot{}, err
	}
	return state.Snapshot(), nil
}

func (world *World) JoinRoom(uin uint32, profile game.PlayerProfile, roomID uint16, roleID, teamID byte) (roomstate.Snapshot, error) {
	if world == nil || uin == 0 || profile.PlayerID == 0 || roomID == 0 {
		return roomstate.Snapshot{}, fmt.Errorf("join room requires a player and room")
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	if current := world.memberships[uin]; current.roomID != 0 {
		return roomstate.Snapshot{}, fmt.Errorf("UIN %d is already in room %d", uin, current.roomID)
	}
	record := world.rooms[roomID]
	if record == nil || record.sectionID != profile.SectionID {
		return roomstate.Snapshot{}, fmt.Errorf("room %d is not active in section %d", roomID, profile.SectionID)
	}
	if teamID == 0 {
		teamID = 1
	}
	snapshot, err := record.state.Join(roomstate.Member{
		PlayerID: profile.PlayerID, RoleID: roleID, TeamID: teamID, Identity: profile.Identity,
	})
	if err != nil {
		return roomstate.Snapshot{}, err
	}
	world.memberships[uin] = membership{sectionID: profile.SectionID, roomID: roomID, playerID: profile.PlayerID}
	if err = world.syncRoomLocked(record); err != nil {
		_, _ = record.state.Leave(profile.PlayerID, roomstate.LeaveVoluntary)
		delete(world.memberships, uin)
		return roomstate.Snapshot{}, err
	}
	if err = world.setPlayerRoomLocked(uin, profile.SectionID, roomID); err != nil {
		_, _ = record.state.Leave(profile.PlayerID, roomstate.LeaveVoluntary)
		delete(world.memberships, uin)
		_ = world.syncRoomLocked(record)
		return roomstate.Snapshot{}, err
	}
	return snapshot, nil
}

// AttachMember restores a domain member when no authenticated UIN is
// available, for example while replaying an arbitrator settlement after that
// participant disconnected. It intentionally does not create a lobby
// presence or membership entry; live players must use JoinRoom.
func (world *World) AttachMember(roomID uint16, member roomstate.Member) (roomstate.Snapshot, error) {
	if world == nil || roomID == 0 || member.PlayerID == 0 {
		return roomstate.Snapshot{}, fmt.Errorf("attach member requires a world, room, and player")
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	record := world.rooms[roomID]
	if record == nil {
		return roomstate.Snapshot{}, fmt.Errorf("room %d is not active", roomID)
	}
	snapshot, err := record.state.Join(member)
	if err != nil {
		return roomstate.Snapshot{}, err
	}
	if err = world.syncRoomLocked(record); err != nil {
		_, _ = record.state.Leave(member.PlayerID, roomstate.LeaveVoluntary)
		return roomstate.Snapshot{}, err
	}
	return snapshot, nil
}

func (world *World) SetReady(uin uint32, ready bool) (roomstate.Snapshot, error) {
	return world.mutateMemberRoom(uin, func(state *roomstate.State, playerID uint16) (roomstate.Snapshot, error) {
		return state.SetReady(playerID, ready)
	})
}

func (world *World) SetMember(uin uint32, roleID, teamID byte) (roomstate.Snapshot, error) {
	return world.mutateMemberRoom(uin, func(state *roomstate.State, playerID uint16) (roomstate.Snapshot, error) {
		snapshot := state.Snapshot()
		for _, member := range snapshot.Members {
			if member.PlayerID != playerID {
				continue
			}
			if roleID != 0 {
				member.RoleID = roleID
			}
			if teamID != 0 {
				member.TeamID = teamID
			}
			return state.UpdateMember(member)
		}
		return roomstate.Snapshot{}, fmt.Errorf("player %d is not in room %d", playerID, snapshot.RoomID)
	})
}

func (world *World) SetSeatLocked(uin uint32, seatID byte, locked bool) (roomstate.Snapshot, error) {
	return world.mutateMemberRoom(uin, func(state *roomstate.State, playerID uint16) (roomstate.Snapshot, error) {
		return state.SetSeatLocked(playerID, seatID, locked)
	})
}

func (world *World) UpdateRoomProperties(uin uint32, properties roomstate.Properties) (roomstate.Snapshot, error) {
	return world.mutateMemberRoom(uin, func(state *roomstate.State, playerID uint16) (roomstate.Snapshot, error) {
		return state.UpdateProperties(playerID, properties)
	})
}

func (world *World) UpdateMatchSettings(uin uint32, settings roomstate.MatchSettings) (roomstate.Snapshot, error) {
	return world.mutateMemberRoom(uin, func(state *roomstate.State, playerID uint16) (roomstate.Snapshot, error) {
		return state.UpdateMatchSettings(playerID, settings)
	})
}

func (world *World) SetWeddingMode(uin uint32, mode uint16) (roomstate.Snapshot, error) {
	return world.mutateMemberRoom(uin, func(state *roomstate.State, playerID uint16) (roomstate.Snapshot, error) {
		return state.SetWeddingMode(playerID, mode)
	})
}

func (world *World) SetChatBackground(uin uint32, backgroundID uint32) (roomstate.Snapshot, error) {
	return world.mutateMemberRoom(uin, func(state *roomstate.State, playerID uint16) (roomstate.Snapshot, error) {
		return state.SetChatBackground(playerID, backgroundID)
	})
}

func (world *World) StartWedding(uin uint32, targetPlayerID uint16, mode uint16) (roomstate.Snapshot, error) {
	return world.mutateMemberRoom(uin, func(state *roomstate.State, playerID uint16) (roomstate.Snapshot, error) {
		return state.StartWedding(playerID, targetPlayerID, mode)
	})
}

func (world *World) AnswerWedding(uin uint32, targetPlayerID uint16, accepted bool) (roomstate.Snapshot, error) {
	return world.mutateMemberRoom(uin, func(state *roomstate.State, playerID uint16) (roomstate.Snapshot, error) {
		return state.AnswerWedding(playerID, targetPlayerID, accepted)
	})
}

func (world *World) StartMatch(uin uint32) (roomstate.Snapshot, uint32, error) {
	if world == nil {
		return roomstate.Snapshot{}, 0, fmt.Errorf("application world is unavailable")
	}
	gameID := world.AllocateGameID()
	snapshot, err := world.StartMatchWithID(uin, gameID)
	return snapshot, gameID, err
}

// AllocateGameID is the only allocator for match IDs.  The adapter may reserve
// an ID before serializing GAME_BEGIN, then commit it through StartMatchWithID.
func (world *World) AllocateGameID() uint32 {
	if world == nil {
		return 0
	}
	gameID := world.nextGameID.Add(1)
	if gameID == 0 {
		gameID = world.nextGameID.Add(1)
	}
	return gameID
}

func (world *World) StartMatchWithID(uin uint32, gameID uint32) (roomstate.Snapshot, error) {
	return world.StartMatchWithIDOptions(uin, gameID, roomstate.StartValidationOptions{})
}

// StartMatchWithIDOptions commits a match with the same explicit structural
// authorization used by the caller's account-backed admission check.  The
// zero options used by every existing call retain the ordinary multiplayer
// gate; only the single-player Boss-card path supplies an exception.
func (world *World) StartMatchWithIDOptions(uin uint32, gameID uint32, options roomstate.StartValidationOptions) (roomstate.Snapshot, error) {
	if world == nil || gameID == 0 {
		return roomstate.Snapshot{}, fmt.Errorf("start match requires a world and game ID")
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	membership, record, err := world.memberRoomLocked(uin)
	if err != nil {
		return roomstate.Snapshot{}, err
	}
	if err = record.state.ValidateStartWithOptions(membership.playerID, options); err != nil {
		return roomstate.Snapshot{}, err
	}
	snapshot, err := record.state.StartMatch(gameID)
	if err != nil {
		return roomstate.Snapshot{}, err
	}
	if err = world.syncRoomLocked(record); err != nil {
		return roomstate.Snapshot{}, err
	}
	return snapshot, nil
}

func (world *World) CompleteMatch(uin uint32, gameID uint32) (roomstate.Snapshot, error) {
	if world == nil || gameID == 0 {
		return roomstate.Snapshot{}, fmt.Errorf("complete match requires a world and game ID")
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	_, record, err := world.memberRoomLocked(uin)
	if err != nil {
		return roomstate.Snapshot{}, err
	}
	if _, err = record.state.BeginSettlement(gameID); err != nil {
		return roomstate.Snapshot{}, err
	}
	snapshot, err := record.state.CompleteMatch(gameID)
	if err != nil {
		return roomstate.Snapshot{}, err
	}
	if err = world.syncRoomLocked(record); err != nil {
		return roomstate.Snapshot{}, err
	}
	return snapshot, nil
}

func (world *World) BeginSettlement(uin uint32, gameID uint32) (roomstate.Snapshot, error) {
	return world.mutateMemberRoom(uin, func(state *roomstate.State, _ uint16) (roomstate.Snapshot, error) {
		return state.BeginSettlement(gameID)
	})
}

func (world *World) FinishSettlement(uin uint32, gameID uint32) (roomstate.Snapshot, error) {
	return world.mutateMemberRoom(uin, func(state *roomstate.State, _ uint16) (roomstate.Snapshot, error) {
		return state.CompleteMatch(gameID)
	})
}

func (world *World) LeaveRoom(uin uint32, reason roomstate.LeaveReason) (roomstate.Departure, error) {
	if world == nil {
		return roomstate.Departure{}, fmt.Errorf("application world is unavailable")
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	membership, record, err := world.memberRoomLocked(uin)
	if err != nil {
		return roomstate.Departure{}, err
	}
	return world.removeMemberLocked(uin, membership, record, membership.roomID, membership.playerID, reason)
}

// ReconcileRoomDeparture is the narrow recovery path for a connection whose
// session, membership index, and room member table have drifted apart. Normal
// departures must use LeaveRoom. This method is intentionally idempotent: it
// removes whichever stale side still exists and then republishes the lobby
// projection, preventing an adapter error from leaving an unjoinable ghost.
func (world *World) ReconcileRoomDeparture(uin uint32, roomID, playerID uint16, reason roomstate.LeaveReason) (roomstate.Departure, error) {
	if world == nil || uin == 0 || roomID == 0 || playerID == 0 {
		return roomstate.Departure{}, fmt.Errorf("reconcile room departure requires a world, UIN, room, and player")
	}
	world.mu.Lock()
	defer world.mu.Unlock()

	membership := world.memberships[uin]
	record := world.rooms[roomID]
	sectionID := membership.sectionID
	if record != nil {
		sectionID = record.sectionID
	}
	departure := roomstate.Departure{
		Member: roomstate.Member{PlayerID: playerID}, Reason: reason, Empty: record == nil,
	}
	if record != nil {
		snapshot := record.state.Snapshot()
		present := false
		for _, member := range snapshot.Members {
			if member.PlayerID == playerID {
				departure.Member = member
				present = true
				break
			}
		}
		if present {
			var err error
			departure, err = record.state.Leave(playerID, reason)
			if err != nil {
				return roomstate.Departure{}, err
			}
		} else {
			departure.Empty = len(snapshot.Members) == 0
		}
	}

	if membership.roomID == roomID || membership.playerID == playerID {
		delete(world.memberships, uin)
	}
	if sectionID != 0 {
		if err := world.setPlayerRoomLocked(uin, sectionID, 0); err != nil {
			return roomstate.Departure{}, err
		}
	}
	if record == nil {
		return departure, nil
	}
	if departure.Empty {
		delete(world.rooms, roomID)
		if section, err := world.Section(record.sectionID); err == nil {
			section.RemoveRoom(roomID)
		}
		return departure, nil
	}
	if err := world.syncRoomLocked(record); err != nil {
		return roomstate.Departure{}, err
	}
	return departure, nil
}

func (world *World) Room(roomID uint16) (*roomstate.State, bool) {
	if world == nil || roomID == 0 {
		return nil, false
	}
	world.mu.RLock()
	defer world.mu.RUnlock()
	record := world.rooms[roomID]
	return func() *roomstate.State {
		if record == nil {
			return nil
		}
		return record.state
	}(), record != nil
}

// RemoveMember removes a player by protocol PlayerID. It is used for
// between-stage elimination where the owning TCP session may already be gone.
func (world *World) RemoveMember(roomID, playerID uint16, reason roomstate.LeaveReason) (roomstate.Departure, error) {
	if world == nil || roomID == 0 || playerID == 0 {
		return roomstate.Departure{}, fmt.Errorf("remove member requires a world, room, and player")
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	record := world.rooms[roomID]
	if record == nil {
		return roomstate.Departure{}, fmt.Errorf("room %d is not active", roomID)
	}
	var uin uint32
	var member membership
	for candidateUIN, candidate := range world.memberships {
		if candidate.roomID == roomID && candidate.playerID == playerID {
			uin = candidateUIN
			member = candidate
			break
		}
	}
	return world.removeMemberLocked(uin, member, record, roomID, playerID, reason)
}

func (world *World) removeMemberLocked(uin uint32, member membership, record *roomRecord, roomID, playerID uint16, reason roomstate.LeaveReason) (roomstate.Departure, error) {
	departure, err := record.state.Leave(playerID, reason)
	if err != nil {
		return roomstate.Departure{}, err
	}
	if uin != 0 {
		delete(world.memberships, uin)
		if err = world.setPlayerRoomLocked(uin, member.sectionID, 0); err != nil {
			return roomstate.Departure{}, err
		}
	}
	section, err := world.Section(record.sectionID)
	if err != nil {
		return roomstate.Departure{}, err
	}
	if departure.Empty {
		delete(world.rooms, roomID)
		section.RemoveRoom(roomID)
	} else if err = world.syncRoomLocked(record); err != nil {
		return roomstate.Departure{}, err
	}
	return departure, nil
}

func (world *World) LobbySnapshot(sectionID uint16, afterChatSequence uint64) (lobbystate.Snapshot, error) {
	section, err := world.Section(sectionID)
	if err != nil {
		return lobbystate.Snapshot{}, err
	}
	return section.Snapshot(afterChatSequence), nil
}

func (world *World) Snapshot(sectionID uint16, afterChatSequence uint64) (WorldSnapshot, error) {
	lobby, err := world.LobbySnapshot(sectionID, afterChatSequence)
	if err != nil {
		return WorldSnapshot{}, err
	}
	result := WorldSnapshot{Lobby: lobby}
	world.mu.RLock()
	for _, record := range world.rooms {
		if record != nil && record.sectionID == sectionID && record.state != nil {
			result.Rooms = append(result.Rooms, record.state.Snapshot())
		}
	}
	for uin, member := range world.memberships {
		if member.sectionID == sectionID {
			result.Memberships = append(result.Memberships, MembershipSnapshot{
				UIN: uin, SectionID: member.sectionID, RoomID: member.roomID, PlayerID: member.playerID,
			})
		}
	}
	world.mu.RUnlock()
	sort.Slice(result.Rooms, func(i, j int) bool { return result.Rooms[i].RoomID < result.Rooms[j].RoomID })
	sort.Slice(result.Memberships, func(i, j int) bool { return result.Memberships[i].UIN < result.Memberships[j].UIN })
	return result, nil
}

func (world *World) mutateMemberRoom(uin uint32, mutate func(*roomstate.State, uint16) (roomstate.Snapshot, error)) (roomstate.Snapshot, error) {
	if world == nil || mutate == nil {
		return roomstate.Snapshot{}, fmt.Errorf("room mutation requires a world and operation")
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	membership, record, err := world.memberRoomLocked(uin)
	if err != nil {
		return roomstate.Snapshot{}, err
	}
	snapshot, err := mutate(record.state, membership.playerID)
	if err != nil {
		return roomstate.Snapshot{}, err
	}
	if err = world.syncRoomLocked(record); err != nil {
		return roomstate.Snapshot{}, err
	}
	return snapshot, nil
}

func (world *World) memberRoomLocked(uin uint32) (membership, *roomRecord, error) {
	membership := world.memberships[uin]
	if membership.roomID == 0 {
		return membership, nil, fmt.Errorf("UIN %d is not in a room", uin)
	}
	record := world.rooms[membership.roomID]
	if record == nil {
		return membership, nil, fmt.Errorf("room %d for UIN %d is unavailable", membership.roomID, uin)
	}
	return membership, record, nil
}

func (world *World) allocateRoomIDLocked() (uint16, error) {
	// The 5.2 room browser exposes the finite 001..100 room-number space.
	// Reusing the lowest empty number keeps the authoritative allocator aligned
	// with that UI and avoids a long-running server eventually advertising IDs
	// that the client cannot represent on its room pages.
	for roomID := uint16(1); roomID <= 100; roomID++ {
		if world.rooms[roomID] == nil {
			return roomID, nil
		}
	}
	return 0, fmt.Errorf("local room ID space 001..100 is exhausted")
}

func (world *World) syncRoomLocked(record *roomRecord) error {
	if record == nil || record.state == nil {
		return fmt.Errorf("cannot sync an empty room record")
	}
	section, err := world.Section(record.sectionID)
	if err != nil {
		return err
	}
	snapshot := record.state.Snapshot()
	phase, err := lobbyRoomPhase(snapshot.Phase)
	if err != nil {
		return err
	}
	ownerVIP := false
	for _, member := range snapshot.Members {
		if member.PlayerID == snapshot.OwnerID {
			ownerVIP = member.Identity&uint32(game.IdentityPurpleDiamond) != 0
			break
		}
	}
	return section.UpsertRoom(lobbystate.RoomSummary{
		RoomID: snapshot.RoomID, OwnerID: snapshot.OwnerID, Name: snapshot.Properties.Name,
		HasPassword: snapshot.Properties.HasPassword, UsesFreeRule: snapshot.Properties.UsesFreeRule(),
		IsVIP:    ownerVIP,
		GameType: snapshot.Settings.RoomListGameType(), GameMode: snapshot.Settings.RoomListGameMode(),
		MapID: snapshot.Settings.Map.MapID, ContinueID: snapshot.Settings.ContinueID,
		Players: byte(len(snapshot.Members)), MaxPlayers: snapshot.Capacity(), Phase: phase,
	})
}

func (world *World) setPlayerRoomLocked(uin uint32, sectionID, roomID uint16) error {
	section, err := world.Section(sectionID)
	if err != nil {
		return err
	}
	return section.SetPlayerRoom(uin, roomID)
}

func lobbyRoomPhase(phase roomstate.Phase) (lobbystate.RoomPhase, error) {
	switch phase {
	case roomstate.PhasePreparing:
		return lobbystate.RoomPhasePreparing, nil
	case roomstate.PhaseInMatch:
		return lobbystate.RoomPhaseInMatch, nil
	case roomstate.PhaseSettling:
		return lobbystate.RoomPhaseSettling, nil
	default:
		return 0, fmt.Errorf("unknown room phase %d", phase)
	}
}

const mathMaxUint16 = 1<<16 - 1

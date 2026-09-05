package room

import (
	"fmt"
	"sort"
	"sync"
)

// Phase separates the long-lived preparation room from one short-lived
// match. Completing a match returns the same room to PhasePreparing.
type Phase byte

const (
	PhasePreparing Phase = iota + 1
	PhaseInMatch
	PhaseSettling
)

// LeaveReason records why a member was removed from the room. Normal match
// completion is intentionally absent because it must retain every member.
type LeaveReason byte

const (
	LeaveVoluntary LeaveReason = iota + 1
	LeaveKicked
	LeaveDisconnected
	LeaveEliminatedBetweenStages
)

type Member struct {
	PlayerID uint16
	RoleID   byte
	TeamID   byte
	SeatID   byte
	Ready    bool
	// Identity is the authenticated profile identity advertised for this
	// member. Room-list projection uses the current owner's Purple Diamond bit;
	// keeping it on the member also preserves the flag when ownership changes.
	Identity uint32
}

const RoomSeatCount = 8

// MapSelectionKind distinguishes the room UI's default random choice from an
// explicit map selected by the owner. A random selection intentionally does
// not carry a placeholder map ID.
type MapSelectionKind byte

const (
	MapSelectionRandom MapSelectionKind = iota + 1
	MapSelectionFixed
)

type MapSelection struct {
	Kind  MapSelectionKind
	MapID uint32
}

func RandomMapSelection() MapSelection {
	return MapSelection{Kind: MapSelectionRandom}
}

func FixedMapSelection(mapID uint32) MapSelection {
	return MapSelection{Kind: MapSelectionFixed, MapID: mapID}
}

func (selection MapSelection) Validate() error {
	switch selection.Kind {
	case MapSelectionRandom:
		if selection.MapID != 0 {
			return fmt.Errorf("random room map selection must not carry map ID %d", selection.MapID)
		}
	case MapSelectionFixed:
		if selection.MapID == 0 {
			return fmt.Errorf("fixed room map selection requires a non-zero map ID")
		}
	default:
		return fmt.Errorf("unknown room map selection kind %d", selection.Kind)
	}
	return nil
}

// GameType is emitted by REQUEST_CREATE_ROOM and SetGameType. Values 0, 1,
// and 3 are competitive subtypes; 2 is adventure and 4 is chat. It may change
// through an explicit room-mode operation, but selecting a map does not change
// it.
type GameType byte

const (
	GameTypeCompetitiveNoItem GameType = iota
	GameTypeCompetitiveItem
	GameTypeAdventure
	GameTypeCompetitiveLoot
	GameTypeChat
)

type Category byte

const (
	CategoryCompetitive Category = iota + 1
	CategoryAdventure
	CategoryChat
)

// CompetitiveFieldType is the second-level competitive lobby choice. The
// treasure field is not a map or a fourth top-level category: it can be paired
// with competitive game modes such as normal, bomb, and bun when that mode is
// enabled by the 5.2 client data.
type CompetitiveFieldType byte

const (
	CompetitiveFieldNoItem   CompetitiveFieldType = 0
	CompetitiveFieldItem     CompetitiveFieldType = 1
	CompetitiveFieldTreasure CompetitiveFieldType = 3
)

func (gameType GameType) Category() (Category, error) {
	switch gameType {
	case GameTypeCompetitiveNoItem, GameTypeCompetitiveItem, GameTypeCompetitiveLoot:
		return CategoryCompetitive, nil
	case GameTypeAdventure:
		return CategoryAdventure, nil
	case GameTypeChat:
		return CategoryChat, nil
	default:
		return 0, fmt.Errorf("unknown room game type %d", gameType)
	}
}

func (gameType GameType) CompetitiveField() (CompetitiveFieldType, bool) {
	switch gameType {
	case GameTypeCompetitiveNoItem, GameTypeCompetitiveItem, GameTypeCompetitiveLoot:
		return CompetitiveFieldType(gameType), true
	default:
		return 0, false
	}
}

// SelectedMapType is the independent value emitted in the field historically
// named GameType by REQUEST_MODIFY_ROOMINFO. Live Magic Kingdom 1 selections
// have emitted 21 while REQUEST_ROOM and REQUEST_CREATE_ROOM still identify
// the room as adventure type 2. It is therefore map metadata, not a room
// family, and must never replace GameType in ROOM_INFO.
type SelectedMapType byte

// MatchSettings is the mutable next-match configuration owned by the room.
// Selected map and both independent game-type domains are live room state,
// not persistent player data.
type MatchSettings struct {
	Map      MapSelection
	GameType GameType
	// PlayerLimit is the selected map's authoritative capacity from
	// mapDesc.py. Zero means the next map is random or its catalog does not
	// define a narrower cap; manual seat locks remain an independent limit.
	PlayerLimit byte
	// CompetitiveMode is the native competitive rule ID published in the
	// third-level room-list filter (ordinary=1, kick-bomb=2, and so on). It is
	// resolved from the installed client map catalog when an owner selects a
	// fixed competitive map. Random, adventure, and chat rooms keep it zero.
	CompetitiveMode byte
	SelectedMapType SelectedMapType
	ContinueID      uint32
}

func (settings MatchSettings) Validate() error {
	if err := settings.Map.Validate(); err != nil {
		return err
	}
	if _, err := settings.GameType.Category(); err != nil {
		return err
	}
	if settings.PlayerLimit > RoomSeatCount {
		return fmt.Errorf("room map player limit %d exceeds %d seats", settings.PlayerLimit, RoomSeatCount)
	}
	if settings.Map.Kind == MapSelectionRandom && settings.PlayerLimit != 0 {
		return fmt.Errorf("random room map cannot carry player limit %d", settings.PlayerLimit)
	}
	return nil
}

func (settings MatchSettings) RoomListGameType() byte {
	return byte(settings.GameType)
}

func (settings MatchSettings) RoomListGameMode() byte {
	return settings.CompetitiveMode
}

// Properties are room-lifetime attributes shared by every lobby adapter.
// Password contents stay in authoritative room state and are never exposed by
// room-list projections; those retain only password presence.
type Properties struct {
	Name        string
	Flag        byte
	HasPassword bool
	// Password is retained only in authoritative in-memory room state so a
	// later enter-room request can be validated. Lobby projections expose only
	// HasPassword and must never serialize this slot.
	Password [16]byte
}

const (
	// PropertyFlagPassword and PropertyFlagFreeRule are the two bits used by
	// REQUEST_CREATE_ROOM, REQUEST_MODIFY_ROOM and RESPONSE_ENTER_ROOM_OLD.
	// They are deliberately separate from ROOM_INFO's lobby-list flag byte,
	// whose bits additionally encode whether the room is waiting and VIP.
	PropertyFlagPassword byte = 1 << 0
	PropertyFlagFreeRule byte = 1 << 1
	propertyKnownFlags        = PropertyFlagPassword | PropertyFlagFreeRule
)

func (properties Properties) Valid() bool {
	return properties.Flag&^propertyKnownFlags == 0
}

func (properties Properties) UsesFreeRule() bool {
	return properties.Flag&PropertyFlagFreeRule != 0
}

type Snapshot struct {
	RoomID             uint16
	OwnerID            uint16
	Phase              Phase
	ActiveGameID       uint32
	Revision           uint64
	Properties         Properties
	Settings           MatchSettings
	Members            []Member
	LockedSeats        [RoomSeatCount]bool
	BackgroundID       uint32
	WeddingModeID      uint16
	WeddingInitiatorID uint16
	WeddingTargetID    uint16
	WeddingPending     bool
	WeddingActive      bool
}

// Capacity is the number of currently open seats advertised on the lobby
// room card. Occupied seats are necessarily open and therefore included.
func (snapshot Snapshot) Capacity() byte {
	var open byte
	for _, locked := range snapshot.LockedSeats {
		if !locked {
			open++
		}
	}
	if snapshot.Settings.PlayerLimit != 0 && open > snapshot.Settings.PlayerLimit {
		open = snapshot.Settings.PlayerLimit
	}
	return open
}

type Departure struct {
	Member     Member
	Reason     LeaveReason
	NewOwnerID uint16
	Empty      bool
}

// State owns room membership and next-match configuration independently of
// battle simulation. Protocol handlers translate client events into these
// state transitions instead of mutating connection-local flags directly.
type State struct {
	mu                 sync.Mutex
	roomID             uint16
	ownerID            uint16
	phase              Phase
	activeGameID       uint32
	revision           uint64
	settings           MatchSettings
	properties         Properties
	members            map[uint16]Member
	lockedSeats        [RoomSeatCount]bool
	backgroundID       uint32
	weddingModeID      uint16
	weddingInitiatorID uint16
	weddingTargetID    uint16
	weddingPending     bool
	weddingActive      bool
}

func New(roomID uint16, owner Member, settings MatchSettings) (*State, error) {
	if roomID == 0 || owner.PlayerID == 0 || owner.TeamID == 0 {
		return nil, fmt.Errorf("room, owner player, and owner team IDs must be non-zero")
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	state := &State{
		roomID: roomID, ownerID: owner.PlayerID, phase: PhasePreparing,
		revision: 1, settings: settings,
		members: make(map[uint16]Member),
	}
	// Seat availability is driven by REQUEST_SET_SEAT_STATUS. In particular,
	// the 5.2 client leaves all eight seats open for a random adventure room and
	// only closes seats after a concrete adventure map is selected. Pre-locking
	// seats merely from GameType makes the creator and later joiners observe
	// different room snapshots.
	if owner.SeatID == 0 {
		owner.SeatID = state.firstAvailableSeat()
	}
	if err := state.validateAvailableSeat(owner.SeatID, 0); err != nil {
		return nil, fmt.Errorf("owner seat: %w", err)
	}
	state.members[owner.PlayerID] = owner
	return state, nil
}

func (state *State) Snapshot() Snapshot {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.snapshot()
}

func (state *State) UpdateProperties(actorID uint16, properties Properties) (Snapshot, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if actorID != state.ownerID {
		return Snapshot{}, fmt.Errorf("player %d is not room %d owner", actorID, state.roomID)
	}
	if state.phase != PhasePreparing {
		return Snapshot{}, fmt.Errorf("room %d properties cannot change during phase %d", state.roomID, state.phase)
	}
	if !properties.Valid() {
		return Snapshot{}, fmt.Errorf("room %d properties contain unsupported flag bits 0x%02X", state.roomID, properties.Flag)
	}
	state.properties = properties
	state.revision++
	return state.snapshot(), nil
}

func (state *State) Join(member Member) (Snapshot, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.phase != PhasePreparing {
		return Snapshot{}, fmt.Errorf("room %d is not accepting members during phase %d", state.roomID, state.phase)
	}
	if member.PlayerID == 0 || member.TeamID == 0 {
		return Snapshot{}, fmt.Errorf("room member player and team IDs must be non-zero")
	}
	if _, exists := state.members[member.PlayerID]; exists {
		return Snapshot{}, fmt.Errorf("player %d is already in room %d", member.PlayerID, state.roomID)
	}
	capacity := state.snapshot().Capacity()
	if len(state.members) >= int(capacity) {
		return Snapshot{}, fmt.Errorf("room %d reached its preparation capacity %d", state.roomID, capacity)
	}
	if member.SeatID == 0 {
		member.SeatID = state.firstAvailableSeat()
	}
	if err := state.validateAvailableSeat(member.SeatID, 0); err != nil {
		return Snapshot{}, err
	}
	state.members[member.PlayerID] = member
	state.revision++
	return state.snapshot(), nil
}

// UpdateMember applies a validated room-member snapshot after protocol-level
// authorization. Role, team, ready state, and later appearance fields belong
// to room state and are snapshotted when a match starts.
func (state *State) UpdateMember(member Member) (Snapshot, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.phase != PhasePreparing {
		return Snapshot{}, fmt.Errorf("room %d member cannot change during phase %d", state.roomID, state.phase)
	}
	if member.PlayerID == 0 || member.TeamID == 0 {
		return Snapshot{}, fmt.Errorf("room member player and team IDs must be non-zero")
	}
	existing, exists := state.members[member.PlayerID]
	if !exists {
		return Snapshot{}, fmt.Errorf("player %d is not in room %d", member.PlayerID, state.roomID)
	}
	if member.SeatID == 0 {
		member.SeatID = existing.SeatID
	}
	if member.SeatID != existing.SeatID {
		if err := state.validateAvailableSeat(member.SeatID, member.PlayerID); err != nil {
			return Snapshot{}, err
		}
	}
	state.members[member.PlayerID] = member
	state.revision++
	return state.snapshot(), nil
}

// SetReady is the only transition that changes a member's preparation state.
// It is idempotent because the legacy client may repeat READY/CANCEL_READY
// after a UI retry. The room owner uses the same UI entry to start a match;
// the server handler distinguishes that start command before reaching here.
func (state *State) SetReady(playerID uint16, ready bool) (Snapshot, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.phase != PhasePreparing {
		return Snapshot{}, fmt.Errorf("room %d ready state cannot change during phase %d", state.roomID, state.phase)
	}
	member, exists := state.members[playerID]
	if !exists {
		return Snapshot{}, fmt.Errorf("player %d is not in room %d", playerID, state.roomID)
	}
	if member.Ready != ready {
		member.Ready = ready
		state.members[playerID] = member
		state.revision++
	}
	return state.snapshot(), nil
}

// StartValidationOptions carries server-authorized exceptions to the native
// room-size gate. Qualification (for example an owned admission item) remains
// outside room state; this type changes only the structural one-player rule.
type StartValidationOptions struct {
	AllowSinglePlayerCompetitive bool
	// VirtualCompetitiveTeamIDs are server-owned participants projected only
	// into the short-lived GAME_BEGIN snapshot. They never become room members,
	// never need a ready flag, and cannot own the room or native arbitrator.
	VirtualCompetitiveTeamIDs []byte
	// DeferCompetitiveTopology validates ownership, phase and human readiness
	// before the server has selected a random map and planned virtual players.
	// The exact projected topology must still pass a second validation before
	// World.StartMatch mutates the room phase.
	DeferCompetitiveTopology bool
}

// ValidateStart enforces the multiplayer room gate before the server sends a
// correlated start success. Only the owner starts a match, every other member
// must be ready, and the legacy adventure runtime accepts at most four
// participants even if the owner temporarily opens more preparation seats.
func (state *State) ValidateStart(actorID uint16) error {
	return state.ValidateStartWithOptions(actorID, StartValidationOptions{})
}

// ValidateStartWithOptions applies the same invariant set while allowing the
// server to authorize the local single-player Boss-card exception. Multiplayer
// team balance, readiness, ownership and phase rules are unchanged.
func (state *State) ValidateStartWithOptions(actorID uint16, options StartValidationOptions) error {
	state.mu.Lock()
	defer state.mu.Unlock()
	if actorID != state.ownerID {
		return fmt.Errorf("player %d is not room %d owner", actorID, state.roomID)
	}
	if state.phase != PhasePreparing || state.activeGameID != 0 {
		return fmt.Errorf("room %d is not ready to start", state.roomID)
	}
	if len(state.members) == 0 {
		return fmt.Errorf("room %d has no members", state.roomID)
	}
	category, err := state.settings.GameType.Category()
	if err != nil {
		return err
	}
	switch category {
	case CategoryAdventure:
		if len(state.members) > 4 {
			return fmt.Errorf("adventure room %d has %d members; maximum is 4", state.roomID, len(state.members))
		}
	case CategoryCompetitive:
		if options.DeferCompetitiveTopology {
			break
		}
		participantCount := len(state.members) + len(options.VirtualCompetitiveTeamIDs)
		if participantCount > int(state.snapshot().Capacity()) {
			return fmt.Errorf("competitive room %d projects %d participants above capacity %d", state.roomID, participantCount, state.snapshot().Capacity())
		}
		singlePlayer := participantCount == 1
		if participantCount < 2 && !(singlePlayer && options.AllowSinglePlayerCompetitive) {
			return fmt.Errorf("competitive room %d requires at least two players", state.roomID)
		}
		if !singlePlayer {
			teamSizes := make(map[byte]int)
			for _, member := range state.members {
				teamSizes[member.TeamID]++
			}
			for _, teamID := range options.VirtualCompetitiveTeamIDs {
				if teamID == 0 || teamID > RoomSeatCount {
					return fmt.Errorf("competitive room %d virtual team ID %d is outside 1..%d", state.roomID, teamID, RoomSeatCount)
				}
				teamSizes[teamID]++
			}
			if len(teamSizes) < 2 {
				return fmt.Errorf("competitive room %d requires at least two teams", state.roomID)
			}
			if state.settings.GameType == GameTypeCompetitiveLoot {
				for teamID, size := range teamSizes {
					if size != 1 {
						return fmt.Errorf("treasure competitive room %d team %d has %d players; every player must use a separate team", state.roomID, teamID, size)
					}
				}
			} else if !state.properties.UsesFreeRule() {
				expected := 0
				for teamID, size := range teamSizes {
					if expected == 0 {
						expected = size
						continue
					}
					if size != expected {
						return fmt.Errorf("standard competitive room %d has unbalanced team %d size %d; want %d", state.roomID, teamID, size, expected)
					}
				}
			}
		}
	case CategoryChat:
		return fmt.Errorf("chat room %d cannot start a match", state.roomID)
	}
	for playerID, member := range state.members {
		if playerID != state.ownerID && !member.Ready {
			return fmt.Errorf("room player %d is not ready", playerID)
		}
	}
	return nil
}

// SetSeatLocked changes one of the room's eight joinable positions. Only the
// owner may change an empty seat while the room is preparing. Repeating the
// current state is accepted because the adventure client sends its four
// default lock requests even when the server initialized the same defaults.
func (state *State) SetSeatLocked(actorID uint16, seatID byte, locked bool) (Snapshot, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if actorID != state.ownerID {
		return Snapshot{}, fmt.Errorf("player %d is not room %d owner", actorID, state.roomID)
	}
	if state.phase != PhasePreparing {
		return Snapshot{}, fmt.Errorf("room %d seats cannot change during phase %d", state.roomID, state.phase)
	}
	if seatID < 1 || seatID > RoomSeatCount {
		return Snapshot{}, fmt.Errorf("room seat ID %d is outside 1..%d", seatID, RoomSeatCount)
	}
	if locked {
		for _, member := range state.members {
			if member.SeatID == seatID {
				return Snapshot{}, fmt.Errorf("room seat %d is occupied by player %d", seatID, member.PlayerID)
			}
		}
	}
	index := seatID - 1
	if state.lockedSeats[index] != locked {
		state.lockedSeats[index] = locked
		state.revision++
	}
	return state.snapshot(), nil
}

func (state *State) firstAvailableSeat() byte {
	for seatID := byte(1); seatID <= RoomSeatCount; seatID++ {
		if state.lockedSeats[seatID-1] {
			continue
		}
		occupied := false
		for _, member := range state.members {
			if member.SeatID == seatID {
				occupied = true
				break
			}
		}
		if !occupied {
			return seatID
		}
	}
	return 0
}

func (state *State) validateAvailableSeat(seatID byte, samePlayerID uint16) error {
	if seatID < 1 || seatID > RoomSeatCount {
		return fmt.Errorf("room %d has no open seat", state.roomID)
	}
	if state.lockedSeats[seatID-1] {
		return fmt.Errorf("room %d seat %d is locked", state.roomID, seatID)
	}
	for _, member := range state.members {
		if member.SeatID == seatID && member.PlayerID != samePlayerID {
			return fmt.Errorf("room %d seat %d is occupied by player %d", state.roomID, seatID, member.PlayerID)
		}
	}
	return nil
}

func (state *State) UpdateMatchSettings(actorID uint16, settings MatchSettings) (Snapshot, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if actorID != state.ownerID {
		return Snapshot{}, fmt.Errorf("player %d is not room %d owner", actorID, state.roomID)
	}
	if state.phase != PhasePreparing {
		return Snapshot{}, fmt.Errorf("room %d is not preparing", state.roomID)
	}
	if err := settings.Validate(); err != nil {
		return Snapshot{}, err
	}
	if settings.PlayerLimit != 0 && len(state.members) > int(settings.PlayerLimit) {
		return Snapshot{}, fmt.Errorf("room %d has %d members, selected map supports %d", state.roomID, len(state.members), settings.PlayerLimit)
	}
	state.settings = settings
	state.revision++
	return state.snapshot(), nil
}

// SetWeddingMode changes the client-supported Chinese/Western ceremony theme.
// It is owner-controlled room state and is only valid for a preparing chat room.
func (state *State) SetWeddingMode(actorID uint16, mode uint16) (Snapshot, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if actorID != state.ownerID {
		return Snapshot{}, fmt.Errorf("player %d is not room %d owner", actorID, state.roomID)
	}
	if state.phase != PhasePreparing || state.settings.GameType != GameTypeChat || state.weddingPending || state.weddingActive {
		return Snapshot{}, fmt.Errorf("room %d is not a preparing chat room", state.roomID)
	}
	if mode > 1 {
		return Snapshot{}, fmt.Errorf("wedding mode %d is outside 0..1", mode)
	}
	if state.weddingModeID != mode {
		state.weddingModeID = mode
		state.revision++
	}
	return state.snapshot(), nil
}

// SetChatBackground changes the background selected for a preparing chat
// room. Asset entitlement belongs to the protocol/application boundary; room
// state only owns authorization and the authoritative selected value.
func (state *State) SetChatBackground(actorID uint16, backgroundID uint32) (Snapshot, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if actorID != state.ownerID {
		return Snapshot{}, fmt.Errorf("player %d is not room %d owner", actorID, state.roomID)
	}
	if state.phase != PhasePreparing || state.settings.GameType != GameTypeChat {
		return Snapshot{}, fmt.Errorf("room %d is not a preparing chat room", state.roomID)
	}
	if state.backgroundID != backgroundID {
		state.backgroundID = backgroundID
		state.revision++
	}
	return state.snapshot(), nil
}

func (state *State) StartWedding(actorID, targetID uint16, mode uint16) (Snapshot, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if actorID != state.ownerID {
		return Snapshot{}, fmt.Errorf("player %d is not room %d owner", actorID, state.roomID)
	}
	if state.phase != PhasePreparing || state.settings.GameType != GameTypeChat || state.weddingPending || state.weddingActive {
		return Snapshot{}, fmt.Errorf("room %d cannot start a wedding now", state.roomID)
	}
	if mode > 1 || targetID == 0 || targetID == actorID {
		return Snapshot{}, fmt.Errorf("invalid wedding target %d or mode %d", targetID, mode)
	}
	if _, exists := state.members[targetID]; !exists {
		return Snapshot{}, fmt.Errorf("wedding target player %d is not in room %d", targetID, state.roomID)
	}
	state.weddingModeID = mode
	state.weddingInitiatorID = actorID
	state.weddingTargetID = targetID
	state.weddingPending = true
	state.revision++
	return state.snapshot(), nil
}

func (state *State) AnswerWedding(actorID, targetID uint16, accepted bool) (Snapshot, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if !state.weddingPending || state.weddingTargetID != actorID || state.weddingInitiatorID != targetID {
		return Snapshot{}, fmt.Errorf("room %d has no matching pending wedding", state.roomID)
	}
	state.weddingPending = false
	state.weddingActive = accepted
	if !accepted {
		state.weddingInitiatorID = 0
		state.weddingTargetID = 0
	}
	state.revision++
	return state.snapshot(), nil
}

func (state *State) StartMatch(gameID uint32) (Snapshot, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if gameID == 0 {
		return Snapshot{}, fmt.Errorf("match game ID must be non-zero")
	}
	if state.phase != PhasePreparing || state.activeGameID != 0 {
		return Snapshot{}, fmt.Errorf("room %d already has active game %d", state.roomID, state.activeGameID)
	}
	if len(state.members) == 0 {
		return Snapshot{}, fmt.Errorf("room %d has no members", state.roomID)
	}
	state.phase = PhaseInMatch
	state.activeGameID = gameID
	state.revision++
	return state.snapshot(), nil
}

func (state *State) BeginSettlement(gameID uint32) (Snapshot, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.phase != PhaseInMatch || state.activeGameID != gameID {
		return Snapshot{}, fmt.Errorf("game %d is not active in room %d", gameID, state.roomID)
	}
	state.phase = PhaseSettling
	state.revision++
	return state.snapshot(), nil
}

// CompleteMatch destroys only the active match snapshot. Room membership and
// next-match settings survive; ready flags are reset for the next round.
func (state *State) CompleteMatch(gameID uint32) (Snapshot, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if (state.phase != PhaseInMatch && state.phase != PhaseSettling) || state.activeGameID != gameID {
		return Snapshot{}, fmt.Errorf("game %d is not active or settling in room %d", gameID, state.roomID)
	}
	state.phase = PhasePreparing
	state.activeGameID = 0
	for playerID, member := range state.members {
		member.Ready = false
		state.members[playerID] = member
	}
	state.revision++
	return state.snapshot(), nil
}

func (state *State) Leave(playerID uint16, reason LeaveReason) (Departure, error) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if reason < LeaveVoluntary || reason > LeaveEliminatedBetweenStages {
		return Departure{}, fmt.Errorf("unknown room leave reason %d", reason)
	}
	member, exists := state.members[playerID]
	if !exists {
		return Departure{}, fmt.Errorf("player %d is not in room %d", playerID, state.roomID)
	}
	delete(state.members, playerID)
	if playerID == state.weddingInitiatorID || playerID == state.weddingTargetID {
		state.weddingInitiatorID = 0
		state.weddingTargetID = 0
		state.weddingPending = false
		state.weddingActive = false
	}
	departure := Departure{Member: member, Reason: reason, Empty: len(state.members) == 0}
	if departure.Empty {
		state.ownerID = 0
	} else if state.ownerID == playerID {
		// Reset the departed owner before selecting the smallest remaining
		// PlayerID. Comparing against the old owner kept a stale owner whenever
		// it happened to be numerically smaller than every remaining member.
		state.ownerID = 0
		for remainingID := range state.members {
			if state.ownerID == 0 || remainingID < state.ownerID {
				state.ownerID = remainingID
			}
		}
		departure.NewOwnerID = state.ownerID
	}
	state.revision++
	return departure, nil
}

func (state *State) snapshot() Snapshot {
	snapshot := Snapshot{
		RoomID: state.roomID, OwnerID: state.ownerID, Phase: state.phase,
		ActiveGameID: state.activeGameID, Revision: state.revision,
		Properties: state.properties, Settings: state.settings,
		Members:            make([]Member, 0, len(state.members)),
		LockedSeats:        state.lockedSeats,
		BackgroundID:       state.backgroundID,
		WeddingModeID:      state.weddingModeID,
		WeddingInitiatorID: state.weddingInitiatorID,
		WeddingTargetID:    state.weddingTargetID,
		WeddingPending:     state.weddingPending,
		WeddingActive:      state.weddingActive,
	}
	for _, member := range state.members {
		snapshot.Members = append(snapshot.Members, member)
	}
	sort.Slice(snapshot.Members, func(i, j int) bool { return snapshot.Members[i].PlayerID < snapshot.Members[j].PlayerID })
	return snapshot
}

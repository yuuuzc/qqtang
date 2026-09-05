// Package lobby owns the protocol-independent state shown by section/lobby
// screens. Network commands are adapters over this state; they must not own
// online players, room summaries, or chat history themselves.
package lobby

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

const DefaultChatHistoryLimit = 256

type Presence struct {
	UIN       uint32 `json:"uin"`
	PlayerID  uint16 `json:"player_id"`
	SectionID uint16 `json:"section_id"`
	RoomID    uint16 `json:"room_id,omitempty"`
	Nickname  string `json:"nickname,omitempty"`
}

// RoomPhase is the lobby projection of the room lifecycle. It stays
// protocol-independent; the ROOM_INFO adapter maps only Preparing to the
// wire-level waiting bit. QQTSection derives the rendered room-card state from
// that bit and the current/maximum population nibbles.
type RoomPhase byte

const (
	RoomPhasePreparing RoomPhase = iota + 1
	RoomPhaseInMatch
	RoomPhaseSettling
)

type RoomSummary struct {
	RoomID       uint16    `json:"room_id"`
	OwnerID      uint16    `json:"owner_id"`
	Name         string    `json:"name,omitempty"`
	HasPassword  bool      `json:"has_password"`
	UsesFreeRule bool      `json:"uses_free_rule"`
	IsVIP        bool      `json:"is_vip"`
	GameType     byte      `json:"game_type"`
	GameMode     byte      `json:"game_mode,omitempty"`
	MapID        uint32    `json:"map_id,omitempty"`
	ContinueID   uint32    `json:"continue_id,omitempty"`
	Players      byte      `json:"players"`
	MaxPlayers   byte      `json:"max_players"`
	Phase        RoomPhase `json:"phase"`
	Revision     uint64    `json:"revision"`
}

type ChatMessage struct {
	Sequence uint64    `json:"sequence"`
	Time     time.Time `json:"time"`
	UIN      uint32    `json:"uin"`
	Text     string    `json:"text"`
}

type Snapshot struct {
	SectionID uint16        `json:"section_id"`
	Revision  uint64        `json:"revision"`
	Players   []Presence    `json:"players"`
	Rooms     []RoomSummary `json:"rooms"`
	Chat      []ChatMessage `json:"chat,omitempty"`
}

// Section is one logical lobby/section inside the single local Go process.
// Revision lets a periodic client refresh adapter cheaply detect unchanged
// online-player and room-list state.
type Section struct {
	mu           sync.RWMutex
	id           uint16
	revision     uint64
	chatSequence uint64
	players      map[uint32]Presence
	rooms        map[uint16]RoomSummary
	chat         []ChatMessage
	chatLimit    int
}

type Hub struct {
	mu       sync.Mutex
	sections map[uint16]*Section
}

func NewHub() *Hub { return &Hub{sections: make(map[uint16]*Section)} }

func (hub *Hub) Section(sectionID uint16) (*Section, error) {
	if sectionID == 0 {
		return nil, fmt.Errorf("lobby section ID must be non-zero")
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	section := hub.sections[sectionID]
	if section == nil {
		section = &Section{
			id: sectionID, players: make(map[uint32]Presence), rooms: make(map[uint16]RoomSummary),
			chatLimit: DefaultChatHistoryLimit,
		}
		hub.sections[sectionID] = section
	}
	return section, nil
}

func (section *Section) UpsertPlayer(player Presence) error {
	if player.UIN == 0 || player.PlayerID == 0 || player.SectionID != section.id {
		return fmt.Errorf("invalid section %d presence: %+v", section.id, player)
	}
	section.mu.Lock()
	defer section.mu.Unlock()
	section.players[player.UIN] = player
	section.revision++
	return nil
}

func (section *Section) SetPlayerRoom(uin uint32, roomID uint16) error {
	section.mu.Lock()
	defer section.mu.Unlock()
	player, ok := section.players[uin]
	if !ok {
		return fmt.Errorf("UIN %d is not online in section %d", uin, section.id)
	}
	if player.RoomID == roomID {
		return nil
	}
	player.RoomID = roomID
	section.players[uin] = player
	section.revision++
	return nil
}

// Player returns the authoritative lobby identity for one online account.
// Protocol adapters use this projection instead of connection-local profile
// snapshots, which may lag behind a reconnect or section refresh.
func (section *Section) Player(uin uint32) (Presence, bool) {
	if section == nil || uin == 0 {
		return Presence{}, false
	}
	section.mu.RLock()
	defer section.mu.RUnlock()
	player, ok := section.players[uin]
	return player, ok
}

func (section *Section) RemovePlayer(uin uint32) {
	section.mu.Lock()
	defer section.mu.Unlock()
	if _, ok := section.players[uin]; ok {
		delete(section.players, uin)
		section.revision++
	}
}

func (section *Section) UpsertRoom(room RoomSummary) error {
	if room.RoomID == 0 || room.OwnerID == 0 || room.MaxPlayers == 0 || room.Players > room.MaxPlayers ||
		room.Phase < RoomPhasePreparing || room.Phase > RoomPhaseSettling {
		return fmt.Errorf("invalid room summary: %+v", room)
	}
	section.mu.Lock()
	defer section.mu.Unlock()
	section.revision++
	room.Revision = section.revision
	section.rooms[room.RoomID] = room
	return nil
}

func (section *Section) RemoveRoom(roomID uint16) {
	section.mu.Lock()
	defer section.mu.Unlock()
	if _, ok := section.rooms[roomID]; ok {
		delete(section.rooms, roomID)
		section.revision++
	}
}

func (section *Section) AppendChat(uin uint32, text string, now time.Time) (ChatMessage, error) {
	if uin == 0 || text == "" {
		return ChatMessage{}, fmt.Errorf("lobby chat needs a non-zero UIN and non-empty text")
	}
	section.mu.Lock()
	defer section.mu.Unlock()
	if _, ok := section.players[uin]; !ok {
		return ChatMessage{}, fmt.Errorf("UIN %d is not online in section %d", uin, section.id)
	}
	section.chatSequence++
	message := ChatMessage{Sequence: section.chatSequence, Time: now.UTC(), UIN: uin, Text: text}
	section.chat = append(section.chat, message)
	if len(section.chat) > section.chatLimit {
		section.chat = append([]ChatMessage(nil), section.chat[len(section.chat)-section.chatLimit:]...)
	}
	section.revision++
	return message, nil
}

func (section *Section) Snapshot(afterChatSequence uint64) Snapshot {
	section.mu.RLock()
	defer section.mu.RUnlock()
	result := Snapshot{SectionID: section.id, Revision: section.revision}
	result.Players = make([]Presence, 0, len(section.players))
	for _, player := range section.players {
		result.Players = append(result.Players, player)
	}
	sort.Slice(result.Players, func(i, j int) bool { return result.Players[i].PlayerID < result.Players[j].PlayerID })
	result.Rooms = make([]RoomSummary, 0, len(section.rooms))
	for _, room := range section.rooms {
		result.Rooms = append(result.Rooms, room)
	}
	sort.Slice(result.Rooms, func(i, j int) bool { return result.Rooms[i].RoomID < result.Rooms[j].RoomID })
	for _, message := range section.chat {
		if message.Sequence > afterChatSequence {
			result.Chat = append(result.Chat, message)
		}
	}
	return result
}

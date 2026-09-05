package game

import (
	"encoding/binary"
	"fmt"
)

const (
	playerExplodedWireSize    = 11
	playerInteractionWireSize = 12
	playerItemEventWireSize   = 14
	npcDropHeaderWireSize     = 7
)

// PlayerExplodedEvent is the fixed body emitted and consumed as 0x0FA5 by this
// Client.exe. The authority returns the identical 0x0FA5 through reliable TCP
// as an audit. The adjacent 0x0FA6 table entry is retained for compatibility,
// but it is not the active actor-hit consumer in this build.
type PlayerExplodedEvent struct {
	PlayerID   uint16
	ClientTime uint32
	PosX       uint16
	PosY       uint16
	IsAvatar   bool
}

// PlayerInteractionEvent is the common fixed body used by the adjacent
// REQUEST_KILL_PLAYER, REQUEST_SAVE_PLAYER and NOTIFY_PLAYER_BE_SAVED
// records in QQTMsgData.bin. It describes player-to-player interaction; NPC
// deaths and NPC drops use different schemas.
type PlayerInteractionEvent struct {
	PlayerID            uint16
	ClientTime          uint32
	DestinationPlayerID uint16
	PosX                uint16
	PosY                uint16
}

// PlayerKilledEvent extends the common player interaction body with the
// QQT_GAME_ITEM array carried only by NOTIFY_PLAYER_BE_KILLED.
type PlayerKilledEvent struct {
	PlayerInteractionEvent
	Items []GameItem
}

// PlayerItemEvent is the fixed REQUEST_GET_ITEM/NOTIFY_PLAYER_GET_ITEM body.
// ItemID identifies a map object definition; PosX/PosY are the reported world
// position used by the client-side authority checks.
type PlayerItemEvent struct {
	PlayerID   uint16
	ClientTime uint32
	ItemID     uint32
	PosX       uint16
	PosY       uint16
}

func (event PlayerItemEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 || event.ItemID == 0 {
		return nil, fmt.Errorf("player-item player/item IDs %d/%d must be non-zero", event.PlayerID, event.ItemID)
	}
	body := make([]byte, 0, playerItemEventWireSize)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.ClientTime)
	body = binary.BigEndian.AppendUint32(body, event.ItemID)
	body = binary.BigEndian.AppendUint16(body, event.PosX)
	body = binary.BigEndian.AppendUint16(body, event.PosY)
	return body, nil
}

// NPCDropItemEvent mirrors NOTIFY_NPC_DROPITEM. QQTMsgData names the first
// field PlayerID, while the client consumer treats IDs 30000..30200 as NPC
// object IDs. ObjectID makes that runtime meaning explicit without rewriting
// the original field name in the evidence table.
type NPCDropItemEvent struct {
	ObjectID uint16
	PosX     uint16
	PosY     uint16
	Items    []GameItem
}

func ParsePlayerExplodedEvent(event GameEvent) (PlayerExplodedEvent, error) {
	if event.Schema != PlayerBeExploded {
		return PlayerExplodedEvent{}, fmt.Errorf("player-exploded schema 0x%04X, want 0x%04X", event.Schema, PlayerBeExploded)
	}
	return parsePlayerExplodedBody(event.Body)
}

func ParsePlayerExplodedNotification(event GameEvent) (PlayerExplodedEvent, error) {
	if event.Schema != NotifyPlayerExploded {
		return PlayerExplodedEvent{}, fmt.Errorf("player-exploded notify schema 0x%04X, want 0x%04X", event.Schema, NotifyPlayerExploded)
	}
	return parsePlayerExplodedBody(event.Body)
}

func parsePlayerExplodedBody(body []byte) (PlayerExplodedEvent, error) {
	if len(body) != playerExplodedWireSize {
		return PlayerExplodedEvent{}, fmt.Errorf("player-exploded body length %d, want %d", len(body), playerExplodedWireSize)
	}
	decoded := PlayerExplodedEvent{
		PlayerID:   binary.BigEndian.Uint16(body[0:2]),
		ClientTime: binary.BigEndian.Uint32(body[2:6]),
		PosX:       binary.BigEndian.Uint16(body[6:8]),
		PosY:       binary.BigEndian.Uint16(body[8:10]),
		IsAvatar:   body[10] != 0,
	}
	if decoded.PlayerID == 0 {
		return PlayerExplodedEvent{}, fmt.Errorf("player-exploded player ID must be non-zero")
	}
	return decoded, nil
}

func (event PlayerExplodedEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 {
		return nil, fmt.Errorf("player-exploded player ID must be non-zero")
	}
	body := make([]byte, 0, playerExplodedWireSize)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.ClientTime)
	body = binary.BigEndian.AppendUint16(body, event.PosX)
	body = binary.BigEndian.AppendUint16(body, event.PosY)
	if event.IsAvatar {
		body = append(body, 1)
	} else {
		body = append(body, 0)
	}
	return body, nil
}

func (event PlayerInteractionEvent) MarshalNetworkBinary() ([]byte, error) {
	if err := event.validate(); err != nil {
		return nil, err
	}
	body := make([]byte, 0, playerInteractionWireSize)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.ClientTime)
	body = binary.BigEndian.AppendUint16(body, event.DestinationPlayerID)
	body = binary.BigEndian.AppendUint16(body, event.PosX)
	body = binary.BigEndian.AppendUint16(body, event.PosY)
	return body, nil
}

func (event PlayerKilledEvent) MarshalNetworkBinary() ([]byte, error) {
	body, err := event.PlayerInteractionEvent.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	if len(event.Items) > gameItemMaxCount {
		return nil, fmt.Errorf("NOTIFY_PLAYER_BE_KILLED item count %d exceeds %d", len(event.Items), gameItemMaxCount)
	}
	body = append(body, byte(len(event.Items)))
	for _, item := range event.Items {
		body = item.appendNetworkBinary(body)
	}
	return body, nil
}

func ParsePlayerInteractionEvent(event GameEvent) (PlayerInteractionEvent, error) {
	switch event.Schema {
	case RequestKillPlayer, RequestSavePlayer, NotifyPlayerSaved:
	default:
		return PlayerInteractionEvent{}, fmt.Errorf("player-interaction schema 0x%04X is not REQUEST_KILL_PLAYER, REQUEST_SAVE_PLAYER or NOTIFY_PLAYER_BE_SAVED", event.Schema)
	}
	if len(event.Body) != playerInteractionWireSize {
		return PlayerInteractionEvent{}, fmt.Errorf("player-interaction body length %d, want %d", len(event.Body), playerInteractionWireSize)
	}
	decoded := parsePlayerInteractionBody(event.Body)
	if err := decoded.validate(); err != nil {
		return PlayerInteractionEvent{}, err
	}
	return decoded, nil
}

func ParsePlayerKilledEvent(event GameEvent) (PlayerKilledEvent, error) {
	if event.Schema != NotifyPlayerKilled {
		return PlayerKilledEvent{}, fmt.Errorf("player-killed schema 0x%04X, want 0x%04X", event.Schema, NotifyPlayerKilled)
	}
	if len(event.Body) < playerInteractionWireSize+1 {
		return PlayerKilledEvent{}, fmt.Errorf("NOTIFY_PLAYER_BE_KILLED body length %d, want at least %d", len(event.Body), playerInteractionWireSize+1)
	}
	interaction := parsePlayerInteractionBody(event.Body[:playerInteractionWireSize])
	if err := interaction.validate(); err != nil {
		return PlayerKilledEvent{}, err
	}
	items, err := parseGameItems(event.Body[playerInteractionWireSize+1:], event.Body[playerInteractionWireSize])
	if err != nil {
		return PlayerKilledEvent{}, fmt.Errorf("NOTIFY_PLAYER_BE_KILLED items: %w", err)
	}
	return PlayerKilledEvent{PlayerInteractionEvent: interaction, Items: items}, nil
}

func ParsePlayerItemEvent(event GameEvent) (PlayerItemEvent, error) {
	if event.Schema != RequestGetItem && event.Schema != NotifyPlayerGetItem {
		return PlayerItemEvent{}, fmt.Errorf("player-item schema 0x%04X is not REQUEST_GET_ITEM or NOTIFY_PLAYER_GET_ITEM", event.Schema)
	}
	if len(event.Body) != playerItemEventWireSize {
		return PlayerItemEvent{}, fmt.Errorf("player-item body length %d, want %d", len(event.Body), playerItemEventWireSize)
	}
	decoded := PlayerItemEvent{
		PlayerID:   binary.BigEndian.Uint16(event.Body[0:2]),
		ClientTime: binary.BigEndian.Uint32(event.Body[2:6]),
		ItemID:     binary.BigEndian.Uint32(event.Body[6:10]),
		PosX:       binary.BigEndian.Uint16(event.Body[10:12]),
		PosY:       binary.BigEndian.Uint16(event.Body[12:14]),
	}
	if decoded.PlayerID == 0 || decoded.ItemID == 0 {
		return PlayerItemEvent{}, fmt.Errorf("player-item player/item IDs %d/%d must be non-zero", decoded.PlayerID, decoded.ItemID)
	}
	return decoded, nil
}

func ParseNPCDropItemEvent(event GameEvent) (NPCDropItemEvent, error) {
	if event.Schema != NotifyNPCDropItem {
		return NPCDropItemEvent{}, fmt.Errorf("NPC-drop schema 0x%04X, want 0x%04X", event.Schema, NotifyNPCDropItem)
	}
	if len(event.Body) < npcDropHeaderWireSize {
		return NPCDropItemEvent{}, fmt.Errorf("NOTIFY_NPC_DROPITEM body length %d, want at least %d", len(event.Body), npcDropHeaderWireSize)
	}
	items, err := parseGameItems(event.Body[npcDropHeaderWireSize:], event.Body[6])
	if err != nil {
		return NPCDropItemEvent{}, fmt.Errorf("NOTIFY_NPC_DROPITEM items: %w", err)
	}
	decoded := NPCDropItemEvent{
		ObjectID: binary.BigEndian.Uint16(event.Body[0:2]),
		PosX:     binary.BigEndian.Uint16(event.Body[2:4]),
		PosY:     binary.BigEndian.Uint16(event.Body[4:6]),
		Items:    items,
	}
	if decoded.ObjectID == 0 {
		return NPCDropItemEvent{}, fmt.Errorf("NOTIFY_NPC_DROPITEM object ID must be non-zero")
	}
	return decoded, nil
}

func (event NPCDropItemEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.ObjectID == 0 {
		return nil, fmt.Errorf("NOTIFY_NPC_DROPITEM object ID must be non-zero")
	}
	if len(event.Items) > gameItemMaxCount {
		return nil, fmt.Errorf("NOTIFY_NPC_DROPITEM item count %d exceeds %d", len(event.Items), gameItemMaxCount)
	}
	body := make([]byte, 0, npcDropHeaderWireSize+len(event.Items)*gameItemWireSize)
	body = binary.BigEndian.AppendUint16(body, event.ObjectID)
	body = binary.BigEndian.AppendUint16(body, event.PosX)
	body = binary.BigEndian.AppendUint16(body, event.PosY)
	body = append(body, byte(len(event.Items)))
	for _, item := range event.Items {
		body = item.appendNetworkBinary(body)
	}
	return body, nil
}

func parsePlayerInteractionBody(body []byte) PlayerInteractionEvent {
	return PlayerInteractionEvent{
		PlayerID:            binary.BigEndian.Uint16(body[0:2]),
		ClientTime:          binary.BigEndian.Uint32(body[2:6]),
		DestinationPlayerID: binary.BigEndian.Uint16(body[6:8]),
		PosX:                binary.BigEndian.Uint16(body[8:10]),
		PosY:                binary.BigEndian.Uint16(body[10:12]),
	}
}

func (event PlayerInteractionEvent) validate() error {
	if event.PlayerID == 0 || event.DestinationPlayerID == 0 {
		return fmt.Errorf("player-interaction source/destination IDs %d/%d must be non-zero", event.PlayerID, event.DestinationPlayerID)
	}
	if event.PlayerID == event.DestinationPlayerID {
		return fmt.Errorf("player-interaction source and destination must differ")
	}
	return nil
}

package game

import (
	"encoding/binary"
	"fmt"
)

const (
	PlayerUseBombBodySize      = 15
	bombExplodeBombWireSize    = 13
	bombExplodeMapElemWireSize = 4
	bombExplodeItemWireSize    = 6
	bombExplodeMaxBombs        = 64
	bombExplodeMaxMapElems     = 32
	bombExplodeMaxItems        = 32
)

// PlayerUseBombEvent mirrors schema 0x0FA3 / PLAYER_USE_BOMB. Native Boss
// arbitrators use the same event as human players when their configured skill
// places a bubble. Keeping this typed prevents the Boss proxy exception from
// becoming a general arbitrary-message relay.
type PlayerUseBombEvent struct {
	PlayerID   uint16
	ClientTime uint32
	BombID     uint32
	Row        byte
	Column     byte
	Power      uint16
	Property   byte
}

func (event PlayerUseBombEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 {
		return nil, fmt.Errorf("PLAYER_USE_BOMB player ID must be non-zero")
	}
	if event.BombID == 0 {
		return nil, fmt.Errorf("PLAYER_USE_BOMB bomb ID must be non-zero")
	}
	body := make([]byte, 0, PlayerUseBombBodySize)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.ClientTime)
	body = binary.BigEndian.AppendUint32(body, event.BombID)
	body = append(body, event.Row, event.Column)
	body = binary.BigEndian.AppendUint16(body, event.Power)
	body = append(body, event.Property)
	return body, nil
}

func ParsePlayerUseBombEvent(event GameEvent) (PlayerUseBombEvent, error) {
	if event.Schema != PlayerUseBomb {
		return PlayerUseBombEvent{}, fmt.Errorf("player-use-bomb schema 0x%04X, want 0x%04X", event.Schema, PlayerUseBomb)
	}
	if len(event.Body) != PlayerUseBombBodySize {
		return PlayerUseBombEvent{}, fmt.Errorf("PLAYER_USE_BOMB body length %d, want %d", len(event.Body), PlayerUseBombBodySize)
	}
	result := PlayerUseBombEvent{
		PlayerID:   binary.BigEndian.Uint16(event.Body[0:2]),
		ClientTime: binary.BigEndian.Uint32(event.Body[2:6]),
		BombID:     binary.BigEndian.Uint32(event.Body[6:10]),
		Row:        event.Body[10],
		Column:     event.Body[11],
		Power:      binary.BigEndian.Uint16(event.Body[12:14]),
		Property:   event.Body[14],
	}
	if result.PlayerID == 0 {
		return PlayerUseBombEvent{}, fmt.Errorf("PLAYER_USE_BOMB player ID must be non-zero")
	}
	if result.BombID == 0 {
		return PlayerUseBombEvent{}, fmt.Errorf("PLAYER_USE_BOMB bomb ID must be non-zero")
	}
	return result, nil
}

type ExplodedBomb struct {
	PlayerID   uint16
	ClientTime uint32
	BombID     byte
	Row        byte
	Column     byte
	RowMin     byte
	RowMax     byte
	ColumnMin  byte
	ColumnMax  byte
}

type ExplodedMapElement struct {
	MapElementID uint16
	Row          byte
	Column       byte
}

type ExplodedItem struct {
	ItemID uint32
	Row    byte
	Column byte
}

// BombExplodeEvent mirrors schema 0x0FA4 / NOTIFY_BOMB_EXPLODE including the
// three exact bounded native arrays. Keeping entries typed is necessary for a
// server-owned virtual actor, which has no client arbitrator to serialize its
// explosion, destroyed map elements and destroyed visible scene items. The
// Items vector is not the item hidden behind a newly destroyed wall:
// FUN_00607760 resolves every entry to an existing scene object and removes it.
// Wall contents are instead reproduced from GAME_BEGIN.ItemSeed/NewItems and
// become visible when their containing map element is destroyed.
type BombExplodeEvent struct {
	PlayerID   uint16
	ClientTime uint32
	Bombs      []ExplodedBomb
	MapElems   []ExplodedMapElement
	Items      []ExplodedItem
	// Count fields are retained for replay summaries and older callers. The
	// parser always sets them from the wire; authored events derive their wire
	// counts from the typed slices.
	BombCount    int
	MapElemCount int
	ItemCount    int
}

func (event BombExplodeEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 {
		return nil, fmt.Errorf("NOTIFY_BOMB_EXPLODE player ID must be non-zero")
	}
	if len(event.Bombs) == 0 || len(event.Bombs) > bombExplodeMaxBombs {
		return nil, fmt.Errorf("NOTIFY_BOMB_EXPLODE bomb count %d is outside 1..%d", len(event.Bombs), bombExplodeMaxBombs)
	}
	if len(event.MapElems) > bombExplodeMaxMapElems || len(event.Items) > bombExplodeMaxItems {
		return nil, fmt.Errorf("NOTIFY_BOMB_EXPLODE map/item counts %d/%d exceed %d/%d", len(event.MapElems), len(event.Items), bombExplodeMaxMapElems, bombExplodeMaxItems)
	}
	body := make([]byte, 0, 9+len(event.Bombs)*bombExplodeBombWireSize+len(event.MapElems)*bombExplodeMapElemWireSize+len(event.Items)*bombExplodeItemWireSize)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.ClientTime)
	body = append(body, byte(len(event.Bombs)))
	for _, bomb := range event.Bombs {
		if bomb.PlayerID == 0 || bomb.BombID == 0 {
			return nil, fmt.Errorf("NOTIFY_BOMB_EXPLODE contains invalid bomb player/style %d/%d", bomb.PlayerID, bomb.BombID)
		}
		body = binary.BigEndian.AppendUint16(body, bomb.PlayerID)
		body = binary.BigEndian.AppendUint32(body, bomb.ClientTime)
		body = append(body, bomb.BombID, bomb.Row, bomb.Column, bomb.RowMin, bomb.RowMax, bomb.ColumnMin, bomb.ColumnMax)
	}
	body = append(body, byte(len(event.MapElems)))
	for _, element := range event.MapElems {
		if element.MapElementID == 0 {
			return nil, fmt.Errorf("NOTIFY_BOMB_EXPLODE contains zero map element ID")
		}
		body = binary.BigEndian.AppendUint16(body, element.MapElementID)
		body = append(body, element.Row, element.Column)
	}
	body = append(body, byte(len(event.Items)))
	for _, item := range event.Items {
		if item.ItemID == 0 {
			return nil, fmt.Errorf("NOTIFY_BOMB_EXPLODE contains zero item ID")
		}
		body = binary.BigEndian.AppendUint32(body, item.ItemID)
		body = append(body, item.Row, item.Column)
	}
	return body, nil
}

func ParseBombExplodeEvent(event GameEvent) (BombExplodeEvent, error) {
	if event.Schema != NotifyBombExplode {
		return BombExplodeEvent{}, fmt.Errorf("bomb-explode schema 0x%04X, want 0x%04X", event.Schema, NotifyBombExplode)
	}
	if len(event.Body) < 9 {
		return BombExplodeEvent{}, fmt.Errorf("NOTIFY_BOMB_EXPLODE body length %d is too short", len(event.Body))
	}
	result := BombExplodeEvent{
		PlayerID:   binary.BigEndian.Uint16(event.Body[0:2]),
		ClientTime: binary.BigEndian.Uint32(event.Body[2:6]),
	}
	bombCount := int(event.Body[6])
	if result.PlayerID == 0 {
		return BombExplodeEvent{}, fmt.Errorf("NOTIFY_BOMB_EXPLODE player ID must be non-zero")
	}
	if bombCount == 0 || bombCount > bombExplodeMaxBombs {
		return BombExplodeEvent{}, fmt.Errorf("NOTIFY_BOMB_EXPLODE bomb count %d is outside 1..%d", bombCount, bombExplodeMaxBombs)
	}
	offset := 7
	result.Bombs = make([]ExplodedBomb, bombCount)
	result.BombCount = bombCount
	for index := range result.Bombs {
		if len(event.Body)-offset < bombExplodeBombWireSize {
			return BombExplodeEvent{}, fmt.Errorf("NOTIFY_BOMB_EXPLODE body ends inside %d bombs", bombCount)
		}
		entry := event.Body[offset : offset+bombExplodeBombWireSize]
		result.Bombs[index] = ExplodedBomb{
			PlayerID: binary.BigEndian.Uint16(entry[0:2]), ClientTime: binary.BigEndian.Uint32(entry[2:6]),
			BombID: entry[6], Row: entry[7], Column: entry[8], RowMin: entry[9], RowMax: entry[10], ColumnMin: entry[11], ColumnMax: entry[12],
		}
		offset += bombExplodeBombWireSize
	}
	if len(event.Body) <= offset {
		return BombExplodeEvent{}, fmt.Errorf("NOTIFY_BOMB_EXPLODE body has no map element count")
	}
	mapElemCount := int(event.Body[offset])
	offset++
	if mapElemCount > bombExplodeMaxMapElems {
		return BombExplodeEvent{}, fmt.Errorf("NOTIFY_BOMB_EXPLODE map element count %d exceeds %d", mapElemCount, bombExplodeMaxMapElems)
	}
	result.MapElems = make([]ExplodedMapElement, mapElemCount)
	result.MapElemCount = mapElemCount
	for index := range result.MapElems {
		if len(event.Body)-offset < bombExplodeMapElemWireSize {
			return BombExplodeEvent{}, fmt.Errorf("NOTIFY_BOMB_EXPLODE body ends inside %d map elements", mapElemCount)
		}
		entry := event.Body[offset : offset+bombExplodeMapElemWireSize]
		result.MapElems[index] = ExplodedMapElement{MapElementID: binary.BigEndian.Uint16(entry[0:2]), Row: entry[2], Column: entry[3]}
		offset += bombExplodeMapElemWireSize
	}
	if len(event.Body) <= offset {
		return BombExplodeEvent{}, fmt.Errorf("NOTIFY_BOMB_EXPLODE body has no item count")
	}
	itemCount := int(event.Body[offset])
	offset++
	if itemCount > bombExplodeMaxItems {
		return BombExplodeEvent{}, fmt.Errorf("NOTIFY_BOMB_EXPLODE item count %d exceeds %d", itemCount, bombExplodeMaxItems)
	}
	result.Items = make([]ExplodedItem, itemCount)
	result.ItemCount = itemCount
	for index := range result.Items {
		if len(event.Body)-offset < bombExplodeItemWireSize {
			return BombExplodeEvent{}, fmt.Errorf("NOTIFY_BOMB_EXPLODE body ends inside %d items", itemCount)
		}
		entry := event.Body[offset : offset+bombExplodeItemWireSize]
		result.Items[index] = ExplodedItem{ItemID: binary.BigEndian.Uint32(entry[0:4]), Row: entry[4], Column: entry[5]}
		offset += bombExplodeItemWireSize
	}
	if len(event.Body) != offset {
		return BombExplodeEvent{}, fmt.Errorf("NOTIFY_BOMB_EXPLODE body length %d, want %d for %d/%d/%d entries", len(event.Body), offset, bombCount, mapElemCount, itemCount)
	}
	return result, nil
}

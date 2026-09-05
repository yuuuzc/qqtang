package game

import (
	"encoding/binary"
	"fmt"
)

const (
	dispatchBombMaxBombs     = 32
	dispatchBombMaxItems     = 64
	dispatchBombWireSize     = 7
	dispatchBombItemWireSize = 6
)

// DispatchBomb describes one kickable bubble sent by the elected arbitrator
// in NOTIFY_DISPATCH_BOMB (0x10E1). SceneID selects the native scene object;
// Prop is the bubble property consumed by the original client rule.
type DispatchBomb struct {
	SceneID uint32
	Prop    byte
	Row     byte
	Col     byte
}

// DispatchBombItem describes one ordinary item that arrives in the same
// airborne batch as the kickable bubbles.
type DispatchBombItem struct {
	ItemID uint32
	Row    byte
	Col    byte
}

// DispatchBombData is the dynamic wire body of NOTIFY_DISPATCH_BOMB. The
// native message table confirms the two bounded arrays: at most 32 seven-byte
// Bombs and at most 64 six-byte Items.
type DispatchBombData struct {
	PlayerID uint16
	Time     uint32
	Bombs    []DispatchBomb
	Items    []DispatchBombItem
}

func validCompetitiveCell(row, col byte) bool {
	return row < competitiveMapHeight && col < competitiveMapWidth
}

func (data DispatchBombData) validate() error {
	if data.PlayerID == 0 {
		return fmt.Errorf("dispatch-bomb player ID must be non-zero")
	}
	if len(data.Bombs) > dispatchBombMaxBombs {
		return fmt.Errorf("dispatch-bomb count %d exceeds %d", len(data.Bombs), dispatchBombMaxBombs)
	}
	if len(data.Items) > dispatchBombMaxItems {
		return fmt.Errorf("dispatch-bomb item count %d exceeds %d", len(data.Items), dispatchBombMaxItems)
	}
	for index, bomb := range data.Bombs {
		if bomb.SceneID == 0 {
			return fmt.Errorf("dispatch-bomb[%d] scene ID must be non-zero", index)
		}
		if !validCompetitiveCell(bomb.Row, bomb.Col) {
			return fmt.Errorf("dispatch-bomb[%d] cell %d,%d is outside 13x15", index, bomb.Row, bomb.Col)
		}
	}
	for index, item := range data.Items {
		if item.ItemID == 0 {
			return fmt.Errorf("dispatch-bomb item[%d] ID must be non-zero", index)
		}
		if !validCompetitiveCell(item.Row, item.Col) {
			return fmt.Errorf("dispatch-bomb item[%d] cell %d,%d is outside 13x15", index, item.Row, item.Col)
		}
	}
	return nil
}

func (data DispatchBombData) MarshalNetworkBinary() ([]byte, error) {
	if err := data.validate(); err != nil {
		return nil, err
	}
	body := make([]byte, 0, 8+len(data.Bombs)*dispatchBombWireSize+len(data.Items)*dispatchBombItemWireSize)
	body = binary.BigEndian.AppendUint16(body, data.PlayerID)
	body = binary.BigEndian.AppendUint32(body, data.Time)
	body = append(body, byte(len(data.Bombs)))
	for _, bomb := range data.Bombs {
		body = binary.BigEndian.AppendUint32(body, bomb.SceneID)
		body = append(body, bomb.Prop, bomb.Row, bomb.Col)
	}
	body = append(body, byte(len(data.Items)))
	for _, item := range data.Items {
		body = binary.BigEndian.AppendUint32(body, item.ItemID)
		body = append(body, item.Row, item.Col)
	}
	return body, nil
}

func ParseDispatchBombEvent(event GameEvent) (DispatchBombData, error) {
	if event.Schema != NotifyDispatchBomb {
		return DispatchBombData{}, fmt.Errorf("dispatch-bomb schema 0x%04X, want 0x%04X", event.Schema, NotifyDispatchBomb)
	}
	if len(event.Body) < 8 {
		return DispatchBombData{}, fmt.Errorf("dispatch-bomb body length %d is too short", len(event.Body))
	}
	data := DispatchBombData{PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), Time: binary.BigEndian.Uint32(event.Body[2:6])}
	bombCount := int(event.Body[6])
	if bombCount > dispatchBombMaxBombs {
		return DispatchBombData{}, fmt.Errorf("dispatch-bomb count %d exceeds %d", bombCount, dispatchBombMaxBombs)
	}
	offset := 7
	if len(event.Body) < offset+bombCount*dispatchBombWireSize+1 {
		return DispatchBombData{}, fmt.Errorf("dispatch-bomb body ends inside %d bombs", bombCount)
	}
	data.Bombs = make([]DispatchBomb, bombCount)
	for index := range data.Bombs {
		data.Bombs[index] = DispatchBomb{
			SceneID: binary.BigEndian.Uint32(event.Body[offset : offset+4]),
			Prop:    event.Body[offset+4], Row: event.Body[offset+5], Col: event.Body[offset+6],
		}
		offset += dispatchBombWireSize
	}
	itemCount := int(event.Body[offset])
	offset++
	if itemCount > dispatchBombMaxItems {
		return DispatchBombData{}, fmt.Errorf("dispatch-bomb item count %d exceeds %d", itemCount, dispatchBombMaxItems)
	}
	if len(event.Body)-offset != itemCount*dispatchBombItemWireSize {
		return DispatchBombData{}, fmt.Errorf("dispatch-bomb item bytes %d, want %d", len(event.Body)-offset, itemCount*dispatchBombItemWireSize)
	}
	data.Items = make([]DispatchBombItem, itemCount)
	for index := range data.Items {
		data.Items[index] = DispatchBombItem{
			ItemID: binary.BigEndian.Uint32(event.Body[offset : offset+4]),
			Row:    event.Body[offset+4], Col: event.Body[offset+5],
		}
		offset += dispatchBombItemWireSize
	}
	if err := data.validate(); err != nil {
		return DispatchBombData{}, err
	}
	return data, nil
}

func BuildLocalDispatchBombNotify(requestPacket []byte, roomID uint16, gameDataSequence uint32, data DispatchBombData) ([]byte, error) {
	body, err := data.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalGameEventPush(requestPacket, roomID, gameDataSequence, NotifyDispatchBomb, body, nil)
}

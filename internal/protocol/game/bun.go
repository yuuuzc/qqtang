package game

import (
	"encoding/binary"
	"fmt"
)

const bunActionWireSize = 12

// BunActionEvent is the shared body of 0x0FB6..0x0FB9. These names come from
// QQTMsgData's own field table. Rule 6 reuses the packet family for sculpture
// materials, but that does not change the last two wire fields into a generic
// action discriminator and objective ID.
type BunActionEvent struct {
	PlayerID   uint16
	ClientTime uint32
	PosX       uint16
	PosY       uint16
	BunID      byte
	BunTeamID  byte
}

func ParseBunActionEvent(event GameEvent) (BunActionEvent, error) {
	switch event.Schema {
	case RequestGetBun, NotifyPlayerGetBun, RequestPutBun, NotifyPlayerPutBun:
	default:
		return BunActionEvent{}, fmt.Errorf("bun action schema 0x%04X is unsupported", event.Schema)
	}
	if len(event.Body) != bunActionWireSize {
		return BunActionEvent{}, fmt.Errorf("bun action body length %d, want %d", len(event.Body), bunActionWireSize)
	}
	result := BunActionEvent{
		PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), ClientTime: binary.BigEndian.Uint32(event.Body[2:6]),
		PosX: binary.BigEndian.Uint16(event.Body[6:8]), PosY: binary.BigEndian.Uint16(event.Body[8:10]),
		BunID: event.Body[10], BunTeamID: event.Body[11],
	}
	if result.PlayerID == 0 {
		return BunActionEvent{}, fmt.Errorf("bun action player ID must be non-zero")
	}
	return result, nil
}

func (event BunActionEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 {
		return nil, fmt.Errorf("bun action player ID must be non-zero")
	}
	body := make([]byte, 0, bunActionWireSize)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.ClientTime)
	body = binary.BigEndian.AppendUint16(body, event.PosX)
	body = binary.BigEndian.AppendUint16(body, event.PosY)
	body = append(body, event.BunID, event.BunTeamID)
	return body, nil
}

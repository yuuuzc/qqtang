package game

import (
	"encoding/binary"
	"fmt"
)

const moveBombBodySize = 20

// MoveBombEvent is the byte-identical REQUEST_MOVE_BOMB (0x0FB4) and
// NOTIFY_PLAYER_MOVE_BOMB (0x139C) body used by ordinary-rule contact kicks.
// Time is a native uint16 in this message, while BombTime is the uint32 bomb
// identity timestamp. This layout comes from the client's ten-field message
// descriptor and is confirmed by the Panda QBV request/notify pairs.
type MoveBombEvent struct {
	PlayerID     uint16
	ClientTime   uint16
	FromRow      uint16
	FromCol      uint16
	ToRow        uint16
	ToCol        uint16
	Direction    byte
	BombPlayerID uint16
	BombTime     uint32
	BombPower    byte
}

func ParseMoveBombEvent(event GameEvent) (MoveBombEvent, error) {
	if event.Schema != RequestMoveBomb && event.Schema != NotifyPlayerMoveBomb {
		return MoveBombEvent{}, fmt.Errorf("move-bomb schema 0x%04X, want 0x%04X or 0x%04X", event.Schema, RequestMoveBomb, NotifyPlayerMoveBomb)
	}
	if len(event.Body) != moveBombBodySize {
		return MoveBombEvent{}, fmt.Errorf("move-bomb body length %d, want %d", len(event.Body), moveBombBodySize)
	}
	decoded := MoveBombEvent{
		PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), ClientTime: binary.BigEndian.Uint16(event.Body[2:4]),
		FromRow: binary.BigEndian.Uint16(event.Body[4:6]), FromCol: binary.BigEndian.Uint16(event.Body[6:8]),
		ToRow: binary.BigEndian.Uint16(event.Body[8:10]), ToCol: binary.BigEndian.Uint16(event.Body[10:12]),
		Direction: event.Body[12], BombPlayerID: binary.BigEndian.Uint16(event.Body[13:15]),
		BombTime: binary.BigEndian.Uint32(event.Body[15:19]), BombPower: event.Body[19],
	}
	if decoded.PlayerID == 0 || decoded.BombPlayerID == 0 || decoded.FromRow >= 13 || decoded.FromCol >= 15 || decoded.ToRow >= 13 || decoded.ToCol >= 15 || decoded.Direction > 3 {
		return MoveBombEvent{}, fmt.Errorf("move-bomb player/bomb-player/cells/direction %d/%d/%d,%d/%d,%d/%d is invalid",
			decoded.PlayerID, decoded.BombPlayerID, decoded.FromRow, decoded.FromCol, decoded.ToRow, decoded.ToCol, decoded.Direction)
	}
	return decoded, nil
}

func (event MoveBombEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 || event.BombPlayerID == 0 || event.FromRow >= 13 || event.FromCol >= 15 || event.ToRow >= 13 || event.ToCol >= 15 || event.Direction > 3 {
		return nil, fmt.Errorf("move-bomb player/bomb-player/cells/direction %d/%d/%d,%d/%d,%d/%d is invalid",
			event.PlayerID, event.BombPlayerID, event.FromRow, event.FromCol, event.ToRow, event.ToCol, event.Direction)
	}
	body := make([]byte, 0, moveBombBodySize)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint16(body, event.ClientTime)
	body = binary.BigEndian.AppendUint16(body, event.FromRow)
	body = binary.BigEndian.AppendUint16(body, event.FromCol)
	body = binary.BigEndian.AppendUint16(body, event.ToRow)
	body = binary.BigEndian.AppendUint16(body, event.ToCol)
	body = append(body, event.Direction)
	body = binary.BigEndian.AppendUint16(body, event.BombPlayerID)
	body = binary.BigEndian.AppendUint32(body, event.BombTime)
	body = append(body, event.BombPower)
	return body, nil
}

package game

import (
	"encoding/binary"
	"fmt"
)

const playerReliveWireSize = 8

// PlayerReliveEvent mirrors NOTIFY_PLAYER_RELIVE (0x0FBA). The native match
// arbitrator emits it after the rule-specific respawn delay; Row and Col are
// the destination values selected by the client rule object. They cannot be
// constrained to the ordinary competitive 13x15 board: the native item-use
// path also emits this message, and the client receiver accepts the byte
// values directly when restoring the player position.
type PlayerReliveEvent struct {
	PlayerID   uint16
	ClientTime uint32
	Row        byte
	Col        byte
}

func ParsePlayerReliveEvent(event GameEvent) (PlayerReliveEvent, error) {
	if event.Schema != NotifyPlayerRelive {
		return PlayerReliveEvent{}, fmt.Errorf("player-relive schema 0x%04X, want 0x%04X", event.Schema, NotifyPlayerRelive)
	}
	if len(event.Body) != playerReliveWireSize {
		return PlayerReliveEvent{}, fmt.Errorf("NOTIFY_PLAYER_RELIVE body length %d, want %d", len(event.Body), playerReliveWireSize)
	}
	result := PlayerReliveEvent{
		PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), ClientTime: binary.BigEndian.Uint32(event.Body[2:6]),
		Row: event.Body[6], Col: event.Body[7],
	}
	if err := result.validate(); err != nil {
		return PlayerReliveEvent{}, err
	}
	return result, nil
}

func (event PlayerReliveEvent) MarshalNetworkBinary() ([]byte, error) {
	if err := event.validate(); err != nil {
		return nil, err
	}
	body := make([]byte, 0, playerReliveWireSize)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.ClientTime)
	body = append(body, event.Row, event.Col)
	return body, nil
}

func (event PlayerReliveEvent) validate() error {
	if event.PlayerID == 0 {
		return fmt.Errorf("NOTIFY_PLAYER_RELIVE player ID must be non-zero")
	}
	return nil
}

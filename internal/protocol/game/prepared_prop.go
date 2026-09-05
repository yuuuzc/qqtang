package game

import (
	"encoding/binary"
	"fmt"
)

const preparedUsePropWireSize = 26

const cancelUsePropWireSize = 10

const (
	propTriggerFixedWireSize    = 24
	propTriggerAffectedWireSize = 11
	propTriggerMaxAffected      = 10
)

// CancelUsePropEvent mirrors REQUEST_CANCEL_USE_PROP and its adjacent
// NOTIFY_CANCEL_USE_PROP. This is match-scene state only; cancelling a
// prepared/lasting prop does not restore an account inventory item.
type CancelUsePropEvent struct {
	PlayerID uint16
	PropID   uint32
	Time     uint32
}

func ParseCancelUsePropEvent(event GameEvent) (CancelUsePropEvent, error) {
	if event.Schema != RequestCancelUseProp && event.Schema != NotifyCancelUseProp {
		return CancelUsePropEvent{}, fmt.Errorf("cancel-use-prop schema 0x%04X is not 0x%04X/0x%04X", event.Schema, RequestCancelUseProp, NotifyCancelUseProp)
	}
	if len(event.Body) != cancelUsePropWireSize {
		return CancelUsePropEvent{}, fmt.Errorf("cancel-use-prop body length %d, want %d", len(event.Body), cancelUsePropWireSize)
	}
	decoded := CancelUsePropEvent{
		PlayerID: binary.BigEndian.Uint16(event.Body[0:2]),
		PropID:   binary.BigEndian.Uint32(event.Body[2:6]),
		Time:     binary.BigEndian.Uint32(event.Body[6:10]),
	}
	if decoded.PlayerID == 0 || decoded.PropID == 0 {
		return CancelUsePropEvent{}, fmt.Errorf("cancel-use-prop PlayerID and PropID must be non-zero")
	}
	return decoded, nil
}

func (event CancelUsePropEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 || event.PropID == 0 {
		return nil, fmt.Errorf("cancel-use-prop PlayerID and PropID must be non-zero")
	}
	body := make([]byte, 0, cancelUsePropWireSize)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.PropID)
	body = binary.BigEndian.AppendUint32(body, event.Time)
	return body, nil
}

func BuildLocalCancelUsePropNotify(requestPacket []byte, roomID uint16, gameDataSequence uint32, event CancelUsePropEvent) ([]byte, error) {
	body, err := event.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalGameEventPush(requestPacket, roomID, gameDataSequence, NotifyCancelUseProp, body, nil)
}

type PropTriggerAffectedPlayer struct {
	PlayerID uint16
	PosX     uint32
	PosY     uint32
	Dir      byte
}

// PropTriggerEvent is the full authoritative result of a scene prop firing.
// The native arbitrator constructs the affected-player table; the Go server
// validates its membership and relays it without recomputing collision or
// effect-specific physics.
type PropTriggerEvent struct {
	PropGUID        uint32
	GameTime        uint32
	UserID          uint16
	PosX            uint32
	PosY            uint32
	Dir             byte
	AffectedPlayers []PropTriggerAffectedPlayer
	TypeID          uint32
}

func ParsePropTriggerEvent(event GameEvent) (PropTriggerEvent, error) {
	if event.Schema != NotifyPropTrigger || len(event.Body) < propTriggerFixedWireSize {
		return PropTriggerEvent{}, fmt.Errorf("prop-trigger schema/body 0x%04X/%d is invalid", event.Schema, len(event.Body))
	}
	count := int(event.Body[19])
	if count > propTriggerMaxAffected || len(event.Body) != propTriggerFixedWireSize+count*propTriggerAffectedWireSize {
		return PropTriggerEvent{}, fmt.Errorf("prop-trigger affected/body %d/%d is invalid", count, len(event.Body))
	}
	decoded := PropTriggerEvent{
		PropGUID:        binary.BigEndian.Uint32(event.Body[0:4]),
		GameTime:        binary.BigEndian.Uint32(event.Body[4:8]),
		UserID:          binary.BigEndian.Uint16(event.Body[8:10]),
		PosX:            binary.BigEndian.Uint32(event.Body[10:14]),
		PosY:            binary.BigEndian.Uint32(event.Body[14:18]),
		Dir:             event.Body[18],
		TypeID:          binary.BigEndian.Uint32(event.Body[20+count*propTriggerAffectedWireSize : 24+count*propTriggerAffectedWireSize]),
		AffectedPlayers: make([]PropTriggerAffectedPlayer, count),
	}
	if decoded.PropGUID == 0 || decoded.UserID == 0 || decoded.TypeID == 0 {
		return PropTriggerEvent{}, fmt.Errorf("prop-trigger GUID/user/type %d/%d/%d must be non-zero", decoded.PropGUID, decoded.UserID, decoded.TypeID)
	}
	offset := 20
	seen := make(map[uint16]struct{}, count)
	for index := range decoded.AffectedPlayers {
		affected := PropTriggerAffectedPlayer{
			PlayerID: binary.BigEndian.Uint16(event.Body[offset : offset+2]),
			PosX:     binary.BigEndian.Uint32(event.Body[offset+2 : offset+6]),
			PosY:     binary.BigEndian.Uint32(event.Body[offset+6 : offset+10]),
			Dir:      event.Body[offset+10],
		}
		if affected.PlayerID == 0 {
			return PropTriggerEvent{}, fmt.Errorf("prop-trigger affected player[%d] is zero", index)
		}
		if _, duplicate := seen[affected.PlayerID]; duplicate {
			return PropTriggerEvent{}, fmt.Errorf("prop-trigger repeats affected player %d", affected.PlayerID)
		}
		seen[affected.PlayerID] = struct{}{}
		decoded.AffectedPlayers[index] = affected
		offset += propTriggerAffectedWireSize
	}
	return decoded, nil
}

// PreparedUsePropEvent mirrors REQUEST_PREPARED_USE_PROP and
// NOTIFY_PREPARED_USE_PROP. Flag1..Flag4 retain the official message-table
// names until their item-specific semantics are proven.
type PreparedUsePropEvent struct {
	PlayerID uint16
	ItemID   uint32
	PosX     uint16
	PosY     uint16
	Flag1    uint32
	Flag2    uint32
	Flag3    uint32
	Flag4    uint32
}

func ParsePreparedUsePropEvent(event GameEvent) (PreparedUsePropEvent, error) {
	if event.Schema != RequestPreparedUseProp && event.Schema != NotifyPreparedUseProp {
		return PreparedUsePropEvent{}, fmt.Errorf("prepared-use-prop schema 0x%04X is not 0x%04X/0x%04X", event.Schema, RequestPreparedUseProp, NotifyPreparedUseProp)
	}
	if len(event.Body) != preparedUsePropWireSize {
		return PreparedUsePropEvent{}, fmt.Errorf("prepared-use-prop body length %d, want %d", len(event.Body), preparedUsePropWireSize)
	}
	decoded := PreparedUsePropEvent{
		PlayerID: binary.BigEndian.Uint16(event.Body[0:2]),
		ItemID:   binary.BigEndian.Uint32(event.Body[2:6]),
		PosX:     binary.BigEndian.Uint16(event.Body[6:8]),
		PosY:     binary.BigEndian.Uint16(event.Body[8:10]),
		Flag1:    binary.BigEndian.Uint32(event.Body[10:14]),
		Flag2:    binary.BigEndian.Uint32(event.Body[14:18]),
		Flag3:    binary.BigEndian.Uint32(event.Body[18:22]),
		Flag4:    binary.BigEndian.Uint32(event.Body[22:26]),
	}
	if decoded.PlayerID == 0 || decoded.ItemID == 0 {
		return PreparedUsePropEvent{}, fmt.Errorf("prepared-use-prop PlayerID and ItemID must be non-zero")
	}
	return decoded, nil
}

func (event PreparedUsePropEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 || event.ItemID == 0 {
		return nil, fmt.Errorf("prepared-use-prop PlayerID and ItemID must be non-zero")
	}
	body := make([]byte, 0, preparedUsePropWireSize)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.ItemID)
	body = binary.BigEndian.AppendUint16(body, event.PosX)
	body = binary.BigEndian.AppendUint16(body, event.PosY)
	body = binary.BigEndian.AppendUint32(body, event.Flag1)
	body = binary.BigEndian.AppendUint32(body, event.Flag2)
	body = binary.BigEndian.AppendUint32(body, event.Flag3)
	body = binary.BigEndian.AppendUint32(body, event.Flag4)
	return body, nil
}

// BuildLocalPreparedUsePropNotify converts the client request into the
// adjacent server notification. Echoing schema 0x1174 leaves the client in its
// prepared-use action state; only 0x1175 reaches the item effect handler.
func BuildLocalPreparedUsePropNotify(requestPacket []byte, roomID uint16, gameDataSequence uint32, event PreparedUsePropEvent) ([]byte, error) {
	body, err := event.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalGameEventPush(requestPacket, roomID, gameDataSequence, NotifyPreparedUseProp, body, nil)
}

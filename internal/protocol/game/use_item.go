package game

import (
	"encoding/binary"
	"fmt"
)

const worldUseItemWireSize = 30

// WorldUseItemEvent is REQUEST_USE_ITEM/NOTIFY_PLAYER_USE_ITEM. This is the
// in-match world-item path: its ItemID identifies a transient item distributed
// into the battlefield, not an account inventory row. Prepared account props
// use the adjacent but distinct PreparedUsePropEvent schema.
type WorldUseItemEvent struct {
	PlayerID   uint16
	ClientTime uint32
	ItemID     uint32
	PosX       uint16
	PosY       uint16
	Flag1      uint32
	Flag2      uint32
	Flag3      uint32
	Flag4      uint32
}

func ParseWorldUseItemEvent(event GameEvent) (WorldUseItemEvent, error) {
	switch event.Schema {
	case RequestUseItem, NotifyPlayerUseItem, NotifyPlayerAffection:
	default:
		return WorldUseItemEvent{}, fmt.Errorf("world-use-item schema 0x%04X is not a supported use notification", event.Schema)
	}
	if len(event.Body) != worldUseItemWireSize {
		return WorldUseItemEvent{}, fmt.Errorf("world-use-item body length %d, want %d", len(event.Body), worldUseItemWireSize)
	}
	decoded := WorldUseItemEvent{
		PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), ClientTime: binary.BigEndian.Uint32(event.Body[2:6]),
		ItemID: binary.BigEndian.Uint32(event.Body[6:10]), PosX: binary.BigEndian.Uint16(event.Body[10:12]),
		PosY: binary.BigEndian.Uint16(event.Body[12:14]), Flag1: binary.BigEndian.Uint32(event.Body[14:18]),
		Flag2: binary.BigEndian.Uint32(event.Body[18:22]), Flag3: binary.BigEndian.Uint32(event.Body[22:26]),
		Flag4: binary.BigEndian.Uint32(event.Body[26:30]),
	}
	if decoded.PlayerID == 0 || decoded.ItemID == 0 {
		return WorldUseItemEvent{}, fmt.Errorf("world-use-item player/item IDs %d/%d must be non-zero", decoded.PlayerID, decoded.ItemID)
	}
	return decoded, nil
}

func (event WorldUseItemEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 || event.ItemID == 0 {
		return nil, fmt.Errorf("world-use-item player and item IDs must be non-zero")
	}
	body := make([]byte, 0, worldUseItemWireSize)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.ClientTime)
	body = binary.BigEndian.AppendUint32(body, event.ItemID)
	body = binary.BigEndian.AppendUint16(body, event.PosX)
	body = binary.BigEndian.AppendUint16(body, event.PosY)
	body = binary.BigEndian.AppendUint32(body, event.Flag1)
	body = binary.BigEndian.AppendUint32(body, event.Flag2)
	body = binary.BigEndian.AppendUint32(body, event.Flag3)
	body = binary.BigEndian.AppendUint32(body, event.Flag4)
	return body, nil
}

// BuildLocalWorldUseItemNotify changes only the inner schema from request to
// notification. Account inventory must never be decremented by this builder's
// callers; battlefield distribution owns the temporary quantity.
func BuildLocalWorldUseItemNotify(requestPacket []byte, roomID uint16, gameDataSequence uint32, event WorldUseItemEvent) ([]byte, error) {
	body, err := event.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalGameEventPush(requestPacket, roomID, gameDataSequence, NotifyPlayerUseItem, body, nil)
}

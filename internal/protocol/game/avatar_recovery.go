package game

import (
	"encoding/binary"
	"fmt"
)

const avatarRecoveryBodySize = 10

// AvatarRecoveryEvent is NOTIFY_RECOVER_PLAYER_AVATAR (0x10E0). PlayerID is
// the stable battlefield identity, not the temporary rendered avatar. The
// native arbitrator supplies the authoritative expiry time and position.
type AvatarRecoveryEvent struct {
	PlayerID uint16
	Time     uint32
	PosX     uint16
	PosY     uint16
}

func (event AvatarRecoveryEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 {
		return nil, fmt.Errorf("avatar-recovery player ID must be non-zero")
	}
	body := make([]byte, avatarRecoveryBodySize)
	binary.BigEndian.PutUint16(body[0:2], event.PlayerID)
	binary.BigEndian.PutUint32(body[2:6], event.Time)
	binary.BigEndian.PutUint16(body[6:8], event.PosX)
	binary.BigEndian.PutUint16(body[8:10], event.PosY)
	return body, nil
}

func ParseAvatarRecoveryEvent(event GameEvent) (AvatarRecoveryEvent, error) {
	if event.Schema != NotifyRecoverAvatar || len(event.Body) != avatarRecoveryBodySize {
		return AvatarRecoveryEvent{}, fmt.Errorf("avatar-recovery schema/body 0x%04X/%d, want 0x%04X/%d", event.Schema, len(event.Body), NotifyRecoverAvatar, avatarRecoveryBodySize)
	}
	decoded := AvatarRecoveryEvent{
		PlayerID: binary.BigEndian.Uint16(event.Body[0:2]),
		Time:     binary.BigEndian.Uint32(event.Body[2:6]),
		PosX:     binary.BigEndian.Uint16(event.Body[6:8]),
		PosY:     binary.BigEndian.Uint16(event.Body[8:10]),
	}
	if decoded.PlayerID == 0 {
		return AvatarRecoveryEvent{}, fmt.Errorf("avatar-recovery player ID must be non-zero")
	}
	return decoded, nil
}

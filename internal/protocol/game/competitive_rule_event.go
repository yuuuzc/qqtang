package game

import (
	"encoding/binary"
	"fmt"
)

const (
	wrestleActionWireSize  = 9
	kickBombActionWireSize = 13
	eatBombActionWireSize  = 20
	produceItemWireSize    = 5
	competitiveMapWidth    = 15
	competitiveMapHeight   = 13
)

// PassiveEffect* are projected once at competitive match start. The patched
// client caches the ownership mask per player and consumes it from both the
// local 0x1159 producer and the remote 0x1159 consumer. No action-time visual
// supplement is sent by the server.
const (
	PassiveEffectLightning byte = 1 << iota
	PassiveEffectSwordArc
	PassiveEffectKickImpact
)

const (
	PassiveFunctionRuleMachine byte = iota + 1
	PassiveFunctionRuleKickBomb
	passiveFunctionProjectionMarker byte = 0xFE
	passiveFunctionProjectionMagic1 byte = 'Q'
	passiveFunctionProjectionMagic2 byte = 'F'
	passiveFunctionProjectionReset  byte = 0x80
)

// PassiveFunctionProjection travels through the common PLAYER_THROW_BOMB
// receive entry and is consumed by the static client adapter before the native
// handler sees it. The three-byte FE/Q/F prefix cannot be a valid native kick
// body, rule bit 7 resets the previous match cache, PlayerID is the participant
// key, and the DWORD carries the passive effect mask.
type PassiveFunctionProjection struct {
	Rule     byte
	PlayerID uint16
	Effects  byte
	Reset    bool
}

func (projection PassiveFunctionProjection) MarshalNetworkBinary() ([]byte, error) {
	if projection.Rule != PassiveFunctionRuleMachine && projection.Rule != PassiveFunctionRuleKickBomb {
		return nil, fmt.Errorf("passive function rule %d is unsupported", projection.Rule)
	}
	if projection.PlayerID == 0 {
		return nil, fmt.Errorf("passive function projection requires a non-zero player ID")
	}
	allowed := byte(PassiveEffectLightning)
	if projection.Rule == PassiveFunctionRuleKickBomb {
		allowed = PassiveEffectSwordArc | PassiveEffectKickImpact
	}
	effects := projection.Effects & allowed
	rule := projection.Rule
	if projection.Reset {
		rule |= passiveFunctionProjectionReset
	}
	body := []byte{passiveFunctionProjectionMarker, passiveFunctionProjectionMagic1, passiveFunctionProjectionMagic2, rule}
	body = binary.BigEndian.AppendUint16(body, projection.PlayerID)
	body = binary.BigEndian.AppendUint32(body, uint32(effects))
	body = append(body, 0, 0)
	return body, nil
}

// EatBombAction is the common body of REQUEST_EAT_BOMB and
// NOTIFY_PLAYER_EAT_BOMB. The unusual 16-bit Time field is taken directly
// from the 5.2 message table and is distinct from BombTime.
type EatBombAction struct {
	PlayerID     uint16
	Time         uint16
	PlayerAvatar uint32
	PosX         uint16
	PosY         uint16
	BombPlayerID uint16
	BombTime     uint32
	BombRow      byte
	BombCol      byte
}

func ParseEatBombAction(event GameEvent) (EatBombAction, error) {
	if (event.Schema != RequestEatBomb && event.Schema != NotifyPlayerEatBomb) || len(event.Body) != eatBombActionWireSize {
		return EatBombAction{}, fmt.Errorf("eat-bomb schema/body 0x%04X/%d is invalid", event.Schema, len(event.Body))
	}
	decoded := EatBombAction{
		PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), Time: binary.BigEndian.Uint16(event.Body[2:4]),
		PlayerAvatar: binary.BigEndian.Uint32(event.Body[4:8]), PosX: binary.BigEndian.Uint16(event.Body[8:10]),
		PosY: binary.BigEndian.Uint16(event.Body[10:12]), BombPlayerID: binary.BigEndian.Uint16(event.Body[12:14]),
		BombTime: binary.BigEndian.Uint32(event.Body[14:18]), BombRow: event.Body[18], BombCol: event.Body[19],
	}
	if decoded.PlayerID == 0 || decoded.BombPlayerID == 0 || !validCompetitiveCell(decoded.BombRow, decoded.BombCol) {
		return EatBombAction{}, fmt.Errorf("eat-bomb player/bomb-player/cell %d/%d/%d,%d is invalid", decoded.PlayerID, decoded.BombPlayerID, decoded.BombRow, decoded.BombCol)
	}
	return decoded, nil
}

func (action EatBombAction) MarshalNetworkBinary() ([]byte, error) {
	if action.PlayerID == 0 || action.BombPlayerID == 0 || !validCompetitiveCell(action.BombRow, action.BombCol) {
		return nil, fmt.Errorf("eat-bomb player/bomb-player/cell %d/%d/%d,%d is invalid", action.PlayerID, action.BombPlayerID, action.BombRow, action.BombCol)
	}
	body := make([]byte, 0, eatBombActionWireSize)
	body = binary.BigEndian.AppendUint16(body, action.PlayerID)
	body = binary.BigEndian.AppendUint16(body, action.Time)
	body = binary.BigEndian.AppendUint32(body, action.PlayerAvatar)
	body = binary.BigEndian.AppendUint16(body, action.PosX)
	body = binary.BigEndian.AppendUint16(body, action.PosY)
	body = binary.BigEndian.AppendUint16(body, action.BombPlayerID)
	body = binary.BigEndian.AppendUint32(body, action.BombTime)
	body = append(body, action.BombRow, action.BombCol)
	return body, nil
}

func BuildLocalPlayerEatBombNotify(requestPacket []byte, roomID uint16, gameDataSequence uint32, action EatBombAction) ([]byte, error) {
	body, err := action.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalGameEventPush(requestPacket, roomID, gameDataSequence, NotifyPlayerEatBomb, body, nil)
}

// KickBombAction is PLAYER_THROW_BOMB (0x1159). The client computes the
// destination after a player contacts an existing kickable bubble. The
// server validates the participant and broadcasts the action; it must not be
// confused with ProduceItem, which creates the periodically arriving bubbles.
type KickBombAction struct {
	BombRowAndCol     byte
	BombDestRowAndCol byte
	BombProp          byte
	Direction         byte
	Power             byte
	Time              uint32
	PlayerID          uint16
	BombPower         uint16
}

func ParseKickBombAction(event GameEvent) (KickBombAction, error) {
	if event.Schema != PlayerThrowBomb {
		return KickBombAction{}, fmt.Errorf("kick-bomb schema 0x%04X is unsupported", event.Schema)
	}
	if len(event.Body) != kickBombActionWireSize {
		return KickBombAction{}, fmt.Errorf("kick-bomb body length %d, want %d", len(event.Body), kickBombActionWireSize)
	}
	action := KickBombAction{
		BombRowAndCol: event.Body[0], BombDestRowAndCol: event.Body[1], BombProp: event.Body[2],
		Direction: event.Body[3], Power: event.Body[4], Time: binary.BigEndian.Uint32(event.Body[5:9]),
		PlayerID: binary.BigEndian.Uint16(event.Body[9:11]), BombPower: binary.BigEndian.Uint16(event.Body[11:13]),
	}
	if action.PlayerID == 0 {
		return KickBombAction{}, fmt.Errorf("kick-bomb player ID must be non-zero")
	}
	return action, nil
}

func (action KickBombAction) MarshalNetworkBinary() ([]byte, error) {
	if action.PlayerID == 0 {
		return nil, fmt.Errorf("kick-bomb player ID must be non-zero")
	}
	body := []byte{action.BombRowAndCol, action.BombDestRowAndCol, action.BombProp, action.Direction, action.Power}
	body = binary.BigEndian.AppendUint32(body, action.Time)
	body = binary.BigEndian.AppendUint16(body, action.PlayerID)
	body = binary.BigEndian.AppendUint16(body, action.BombPower)
	return body, nil
}

// ProduceItem is NOTIFY_PRODUCE_ITEM (0x115A). QQTMsgData.bin defines its
// exact server-to-client body as a packed row/column byte and a uint32 item ID.
type ProduceItem struct {
	RowAndCol byte
	ItemID    uint32
}

func ParseProduceItem(event GameEvent) (ProduceItem, error) {
	if event.Schema != NotifyProduceItem {
		return ProduceItem{}, fmt.Errorf("produce-item schema 0x%04X is unsupported", event.Schema)
	}
	if len(event.Body) != produceItemWireSize {
		return ProduceItem{}, fmt.Errorf("produce-item body length %d, want %d", len(event.Body), produceItemWireSize)
	}
	item := ProduceItem{RowAndCol: event.Body[0], ItemID: binary.BigEndian.Uint32(event.Body[1:5])}
	if err := item.validate(); err != nil {
		return ProduceItem{}, err
	}
	return item, nil
}

func (item ProduceItem) validate() error {
	if item.RowAndCol>>4 >= competitiveMapHeight || item.RowAndCol&0x0f >= competitiveMapWidth {
		return fmt.Errorf("produce-item packed cell 0x%02X is outside 15x13", item.RowAndCol)
	}
	if item.ItemID == 0 {
		return fmt.Errorf("produce-item ID must be non-zero")
	}
	return nil
}

func (item ProduceItem) MarshalNetworkBinary() ([]byte, error) {
	if err := item.validate(); err != nil {
		return nil, err
	}
	body := []byte{item.RowAndCol}
	body = binary.BigEndian.AppendUint32(body, item.ItemID)
	return body, nil
}

// WrestleAction is the common wire body of REQUEST_WRESTLE (0x115C) and
// NOTIFY_WRESTLE_SKILL (0x115B). QQTMsgData.bin confirms both records have
// the same five fields and nine-byte layout; only the schema direction
// changes.
type WrestleAction struct {
	SkillType byte
	FirstTeam byte
	PlayerPos byte
	PlayerID  uint16
	Time      uint32
}

func ParseWrestleAction(event GameEvent) (WrestleAction, error) {
	if event.Schema != RequestWrestle && event.Schema != NotifyWrestleSkill {
		return WrestleAction{}, fmt.Errorf("wrestle schema 0x%04X is unsupported", event.Schema)
	}
	if len(event.Body) != wrestleActionWireSize {
		return WrestleAction{}, fmt.Errorf("wrestle body length %d, want %d", len(event.Body), wrestleActionWireSize)
	}
	action := WrestleAction{
		SkillType: event.Body[0], FirstTeam: event.Body[1], PlayerPos: event.Body[2],
		PlayerID: binary.BigEndian.Uint16(event.Body[3:5]), Time: binary.BigEndian.Uint32(event.Body[5:9]),
	}
	if action.PlayerID == 0 {
		return WrestleAction{}, fmt.Errorf("wrestle player ID must be non-zero")
	}
	return action, nil
}

func (action WrestleAction) MarshalNetworkBinary() ([]byte, error) {
	if action.PlayerID == 0 {
		return nil, fmt.Errorf("wrestle player ID must be non-zero")
	}
	body := make([]byte, 0, wrestleActionWireSize)
	body = append(body, action.SkillType, action.FirstTeam, action.PlayerPos)
	body = binary.BigEndian.AppendUint16(body, action.PlayerID)
	body = binary.BigEndian.AppendUint32(body, action.Time)
	return body, nil
}

func BuildLocalWrestleNotify(requestPacket []byte, roomID uint16, gameDataSequence uint32, action WrestleAction) ([]byte, error) {
	body, err := action.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalGameEventPush(requestPacket, roomID, gameDataSequence, NotifyWrestleSkill, body, nil)
}

package game

import (
	"encoding/binary"
	"fmt"
)

const (
	PlayerMoveSchema           uint16 = 0x0FA2
	maxPlayerMoveSequences            = 16
	playerMoveHeaderWireSize          = 7
	playerMoveSequenceWireSize        = 20
)

// PlayerMoveSequence is the 20-byte PLAYER_MOVESEQ record. WalkAndDirection
// remains an opaque combined byte until its bit allocation is proven.
type PlayerMoveSequence struct {
	Sequence         uint16
	TimeStamp        uint32
	CurrentPosX      uint16
	CurrentPosY      uint16
	CornerPosX       uint16
	CornerPosY       uint16
	EndPosX          uint16
	EndPosY          uint16
	WalkAndDirection byte
	Speed            byte
}

// PlayerMoveEntry joins the parallel PlayerIDs[] and MoveSeqs[] arrays from
// the native PLAYER_MOVE object. The original wire format emits every player
// ID first, followed by every sequence record; it is not entry-interleaved.
type PlayerMoveEntry struct {
	PlayerID uint16
	Move     PlayerMoveSequence
}

// PlayerMove is schema 0x0FA2. SeqReply is kept under the exact QQTMsgData
// field name because its acknowledgement semantics are not yet dynamically
// verified.
type PlayerMove struct {
	PlayerID uint16
	SeqReply uint32
	Entries  []PlayerMoveEntry
}

func (movement PlayerMove) MarshalNetworkBinary() ([]byte, error) {
	if len(movement.Entries) > maxPlayerMoveSequences {
		return nil, fmt.Errorf("PLAYER_MOVE entry count %d exceeds %d", len(movement.Entries), maxPlayerMoveSequences)
	}
	payload := make([]byte, 0, playerMoveHeaderWireSize+len(movement.Entries)*(2+playerMoveSequenceWireSize))
	payload = binary.BigEndian.AppendUint16(payload, movement.PlayerID)
	payload = append(payload, byte(len(movement.Entries)))
	payload = binary.BigEndian.AppendUint32(payload, movement.SeqReply)
	for _, entry := range movement.Entries {
		payload = binary.BigEndian.AppendUint16(payload, entry.PlayerID)
	}
	for _, entry := range movement.Entries {
		payload = appendPlayerMoveSequence(payload, entry.Move)
	}
	return payload, nil
}

func ParsePlayerMove(payload []byte) (PlayerMove, error) {
	if len(payload) < playerMoveHeaderWireSize {
		return PlayerMove{}, fmt.Errorf("PLAYER_MOVE payload length %d, want at least %d", len(payload), playerMoveHeaderWireSize)
	}
	count := int(payload[2])
	if count > maxPlayerMoveSequences {
		return PlayerMove{}, fmt.Errorf("PLAYER_MOVE entry count %d exceeds %d", count, maxPlayerMoveSequences)
	}
	wantLength := playerMoveHeaderWireSize + count*(2+playerMoveSequenceWireSize)
	if len(payload) != wantLength {
		return PlayerMove{}, fmt.Errorf("PLAYER_MOVE payload length %d, want %d for %d entries", len(payload), wantLength, count)
	}
	movement := PlayerMove{
		PlayerID: binary.BigEndian.Uint16(payload[0:2]),
		SeqReply: binary.BigEndian.Uint32(payload[3:7]),
		Entries:  make([]PlayerMoveEntry, count),
	}
	offset := playerMoveHeaderWireSize
	for index := range count {
		movement.Entries[index].PlayerID = binary.BigEndian.Uint16(payload[offset : offset+2])
		offset += 2
	}
	for index := range count {
		movement.Entries[index].Move = parsePlayerMoveSequence(payload[offset : offset+playerMoveSequenceWireSize])
		offset += playerMoveSequenceWireSize
	}
	return movement, nil
}

func appendPlayerMoveSequence(payload []byte, movement PlayerMoveSequence) []byte {
	payload = binary.BigEndian.AppendUint16(payload, movement.Sequence)
	payload = binary.BigEndian.AppendUint32(payload, movement.TimeStamp)
	payload = binary.BigEndian.AppendUint16(payload, movement.CurrentPosX)
	payload = binary.BigEndian.AppendUint16(payload, movement.CurrentPosY)
	payload = binary.BigEndian.AppendUint16(payload, movement.CornerPosX)
	payload = binary.BigEndian.AppendUint16(payload, movement.CornerPosY)
	payload = binary.BigEndian.AppendUint16(payload, movement.EndPosX)
	payload = binary.BigEndian.AppendUint16(payload, movement.EndPosY)
	return append(payload, movement.WalkAndDirection, movement.Speed)
}

func parsePlayerMoveSequence(payload []byte) PlayerMoveSequence {
	return PlayerMoveSequence{
		Sequence:         binary.BigEndian.Uint16(payload[0:2]),
		TimeStamp:        binary.BigEndian.Uint32(payload[2:6]),
		CurrentPosX:      binary.BigEndian.Uint16(payload[6:8]),
		CurrentPosY:      binary.BigEndian.Uint16(payload[8:10]),
		CornerPosX:       binary.BigEndian.Uint16(payload[10:12]),
		CornerPosY:       binary.BigEndian.Uint16(payload[12:14]),
		EndPosX:          binary.BigEndian.Uint16(payload[14:16]),
		EndPosY:          binary.BigEndian.Uint16(payload[16:18]),
		WalkAndDirection: payload[18],
		Speed:            payload[19],
	}
}

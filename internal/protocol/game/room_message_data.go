package game

import (
	"encoding/binary"
	"fmt"
)

const (
	// RoomMessageDataSchema is ROOM_MSG_DATA / D2E_ENCODE. QQTPPP Type 2
	// carries this object directly. The TCP 0x0065 upload adds a separate
	// batch count plus GameTime/Flag fields around each encoded object; those
	// batch-only bytes must not be forwarded as the Type-2 payload.
	RoomMessageDataSchema     uint16 = 0x0BBA
	roomMessageDataHeaderSize        = 12
	maxRoomMessageDataLength         = 800
)

// MarshalRoomMessageData encodes the exact ROOM_MSG_DATA object expected by
// the original room peer path: Time, DataID, DataLen, Sequence and Data.
func (event RoomFastEvent) MarshalRoomMessageData() ([]byte, error) {
	if len(event.Data) > maxRoomMessageDataLength {
		return nil, fmt.Errorf("ROOM_MSG_DATA length %d exceeds %d", len(event.Data), maxRoomMessageDataLength)
	}
	payload := make([]byte, 0, roomMessageDataHeaderSize+len(event.Data))
	payload = binary.BigEndian.AppendUint32(payload, event.Time)
	payload = binary.BigEndian.AppendUint16(payload, uint16(event.DataID))
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(event.Data)))
	payload = binary.BigEndian.AppendUint32(payload, event.Sequence)
	payload = append(payload, event.Data...)
	return payload, nil
}

// ParseRoomMessageData decodes one complete ROOM_MSG_DATA object. GameTime
// and Flag remain zero because they belong only to the surrounding TCP batch.
func ParseRoomMessageData(payload []byte) (RoomFastEvent, error) {
	if len(payload) < roomMessageDataHeaderSize {
		return RoomFastEvent{}, fmt.Errorf("ROOM_MSG_DATA length %d is shorter than %d", len(payload), roomMessageDataHeaderSize)
	}
	dataLength := int(binary.BigEndian.Uint16(payload[6:8]))
	if dataLength > maxRoomMessageDataLength {
		return RoomFastEvent{}, fmt.Errorf("ROOM_MSG_DATA data length %d exceeds %d", dataLength, maxRoomMessageDataLength)
	}
	if len(payload) != roomMessageDataHeaderSize+dataLength {
		return RoomFastEvent{}, fmt.Errorf("ROOM_MSG_DATA length %d, want %d", len(payload), roomMessageDataHeaderSize+dataLength)
	}
	return RoomFastEvent{
		Time:     binary.BigEndian.Uint32(payload[0:4]),
		DataID:   RoomFastEventID(binary.BigEndian.Uint16(payload[4:6])),
		Sequence: binary.BigEndian.Uint32(payload[8:12]),
		Data:     append([]byte(nil), payload[12:]...),
	}, nil
}

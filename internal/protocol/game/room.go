package game

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	RoomListRequestSchema  = 0x03EF
	RoomListResponseSchema = 0x07D7
	roomListRequestSize    = 15
	roomListFixedSize      = 8
	RoomListMaximumCount   = 400
	// ROOM_INFO stores RoomName in a 20-byte native C-string slot. NameLen
	// includes the trailing NUL carried on the wire, leaving at most 19 bytes
	// for visible GBK text.
	RoomNameMaximumBytes = 20
)

// RoomListRequest mirrors REQUEST_ROOM. The UI has three levels: the
// competitive/adventure/chat page, the no-item/item/treasure room class (not
// present for chat), and the all-room/all-map/normal/etc. map-rule filter.
// Live 0x0066 traces plus uiSelRoom.py prove GameType is the room class and
// GameMode is the third-level filter. The native bridge encodes explicit map
// rules as UI rule + 2 (3=normal, 4=kick-bomb, ...); 0 is all rooms, 2 is all
// maps, and initialization may still emit legacy all-maps value 1. Neither
// field may be reused as the selected adventure MapID.
// Live lobby changes also arrive through 0x008C PUSHROOMINFO.
type RoomListRequest struct {
	UIN         uint32
	Time        uint32
	Operation   RoomListOperation
	StartRoomID uint16
	Number      uint16
	GameMode    RoomListGameModeFilter
	GameType    byte
}

type RoomListEntry struct {
	Name        string
	RoomID      uint16
	RoomFlag    RoomListFlag
	MapID       uint16
	NumOfPlayer byte
	GameType    byte
	ContinueID  uint32
}

// PackRoomPopulation mirrors the client room card's high-nibble current /
// low-nibble capacity representation. Sending the scalar value 1 renders as
// 0/1; a one-player adventure room is 0x14 and renders as 1/4.
func PackRoomPopulation(current, capacity byte) (byte, error) {
	if current > 0x0F || capacity == 0 || capacity > 0x0F || current > capacity {
		return 0, fmt.Errorf("invalid room population current=%d capacity=%d", current, capacity)
	}
	return current<<4 | capacity, nil
}

func UnpackRoomPopulation(value byte) (current, capacity byte) {
	return value >> 4, value & 0x0F
}

type RoomListResponse struct {
	Operation   RoomListOperation
	Rooms       []RoomListEntry
	GameMode    RoomListGameModeFilter
	StartRoomID uint16
}

func DecodeLocalRoomListRequest(packet []byte) (RoomListRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return RoomListRequest{}, err
	}
	if request.Command != RoomListCommand {
		return RoomListRequest{}, fmt.Errorf("room-list command 0x%04X, want 0x%04X", request.Command, RoomListCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != roomListRequestSize {
		return RoomListRequest{}, fmt.Errorf("room-list request payload length %d, want %d", len(payload), roomListRequestSize)
	}
	decoded := RoomListRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]),
		Operation: RoomListOperation(payload[8]), StartRoomID: binary.BigEndian.Uint16(payload[9:11]),
		Number: binary.BigEndian.Uint16(payload[11:13]), GameMode: RoomListGameModeFilter(payload[13]), GameType: payload[14],
	}
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN {
		return RoomListRequest{}, fmt.Errorf("room-list envelope UIN %d != payload UIN %d", request.EnvelopeUIN, decoded.UIN)
	}
	return decoded, nil
}

func decodeLegacyGBKSlot(slot []byte) (string, error) {
	trimmed := bytes.TrimRight(slot, "\x00")
	if len(trimmed) == 0 {
		return "", nil
	}
	decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(trimmed)
	if err != nil {
		return "", fmt.Errorf("decode GBK text: %w", err)
	}
	return string(decoded), nil
}

func encodeLegacyGBKText(text string, maximum int) ([]byte, error) {
	encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("encode %q as GBK: %w", text, err)
	}
	if len(encoded) > maximum {
		return nil, fmt.Errorf("GBK text %q uses %d bytes, maximum is %d", text, len(encoded), maximum)
	}
	return encoded, nil
}

func (entry RoomListEntry) MarshalNetworkBinary() ([]byte, error) {
	if entry.RoomID == 0 {
		return nil, fmt.Errorf("ROOM_INFO RoomID must be non-zero")
	}
	if !entry.RoomFlag.Valid() {
		return nil, fmt.Errorf("ROOM_INFO RoomFlag 0x%02X contains unsupported bits", entry.RoomFlag)
	}
	name, err := encodeLegacyGBKText(entry.Name, RoomNameMaximumBytes-1)
	if err != nil {
		return nil, err
	}
	encoded := make([]byte, 0, 13+len(name))
	encoded = append(encoded, byte(len(name)+1))
	encoded = append(encoded, name...)
	encoded = append(encoded, 0)
	encoded = binary.BigEndian.AppendUint16(encoded, entry.RoomID)
	encoded = append(encoded, byte(entry.RoomFlag))
	encoded = binary.BigEndian.AppendUint16(encoded, entry.MapID)
	encoded = append(encoded, entry.NumOfPlayer, entry.GameType)
	encoded = binary.BigEndian.AppendUint32(encoded, entry.ContinueID)
	return encoded, nil
}

func (response RoomListResponse) MarshalNetworkBinary() ([]byte, error) {
	if len(response.Rooms) > RoomListMaximumCount {
		return nil, fmt.Errorf("room-list count %d exceeds %d", len(response.Rooms), RoomListMaximumCount)
	}
	payload := make([]byte, 0, roomListFixedSize+32*len(response.Rooms))
	payload = binary.BigEndian.AppendUint16(payload, 0) // ResultID
	payload = append(payload, byte(response.Operation))
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(response.Rooms)))
	for _, room := range response.Rooms {
		encoded, err := room.MarshalNetworkBinary()
		if err != nil {
			return nil, err
		}
		payload = append(payload, encoded...)
	}
	payload = append(payload, byte(response.GameMode))
	payload = binary.BigEndian.AppendUint16(payload, response.StartRoomID)
	return payload, nil
}

func BuildRoomListPayload(rooms []RoomListEntry) ([]byte, error) {
	return (RoomListResponse{Rooms: rooms}).MarshalNetworkBinary()
}

func BuildRoomPushPayload(rooms []RoomListEntry) ([]byte, error) {
	if len(rooms) > RoomListMaximumCount {
		return nil, fmt.Errorf("room-push count %d exceeds %d", len(rooms), RoomListMaximumCount)
	}
	payload := binary.BigEndian.AppendUint16(nil, uint16(len(rooms)))
	for _, room := range rooms {
		encoded, err := room.MarshalNetworkBinary()
		if err != nil {
			return nil, err
		}
		payload = append(payload, encoded...)
	}
	return payload, nil
}

func BuildEmptyRoomListPayload() []byte {
	payload, _ := BuildRoomListPayload(nil)
	return payload
}

func BuildRoomListResponse(requestPacket []byte, rooms []RoomListEntry) ([]byte, error) {
	return buildRoomListResponse(requestPacket, rooms, nil)
}

func BuildRoomListResponseWithReader(requestPacket []byte, rooms []RoomListEntry, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildRoomListResponse(requestPacket, rooms, entropy)
}

func buildRoomListResponse(requestPacket []byte, rooms []RoomListEntry, entropy io.Reader) ([]byte, error) {
	listRequest, err := DecodeLocalRoomListRequest(requestPacket)
	if err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload, err := (RoomListResponse{
		Operation: listRequest.Operation, Rooms: rooms,
		GameMode: listRequest.GameMode, StartRoomID: listRequest.StartRoomID,
	}).MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, RoomListCommand, payload, entropy)
}

func BuildEmptyRoomListResponse(requestPacket []byte) ([]byte, error) {
	return BuildRoomListResponse(requestPacket, nil)
}

func BuildEmptyRoomListResponseWithReader(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	return BuildRoomListResponseWithReader(requestPacket, nil, entropy)
}

func BuildLocalRoomPush(requestPacket []byte, sectionID uint16, rooms []RoomListEntry) ([]byte, error) {
	return buildLocalRoomPush(requestPacket, sectionID, rooms, nil)
}

func buildLocalRoomPush(requestPacket []byte, sectionID uint16, rooms []RoomListEntry, entropy io.Reader) ([]byte, error) {
	if sectionID == 0 {
		return nil, fmt.Errorf("room-push section ID must be non-zero")
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload, err := BuildRoomPushPayload(rooms)
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(requestPacket, request, RoomPushCommand, payload, localMessageRoute{
		Route: 2, SectionID: sectionID,
	}, entropy)
}

package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	modifyRoomInfoPayloadSize = 15
	modifyRoomInfoNotifySize  = 7
	modifyRoomInfoRoute       = 3
)

// ModifyRoomInfoRequest is the confirmed fixed REQUEST_MODIFY_ROOMINFO form
// emitted after the room owner chooses an adventure map.
type ModifyRoomInfoRequest struct {
	UIN        uint32
	ClientTime uint32
	MapID      uint16
	GameType   byte
	ContinueID uint32
}

func DecodeLocalModifyRoomInfoRequest(packet []byte) (ModifyRoomInfoRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return ModifyRoomInfoRequest{}, err
	}
	if request.Command != ModifyRoomInfoCommand {
		return ModifyRoomInfoRequest{}, fmt.Errorf("modify-room-info command 0x%04X, want 0x%04X", request.Command, ModifyRoomInfoCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != modifyRoomInfoPayloadSize {
		return ModifyRoomInfoRequest{}, fmt.Errorf("modify-room-info payload length %d, want %d", len(payload), modifyRoomInfoPayloadSize)
	}
	decoded := ModifyRoomInfoRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
		MapID:      binary.BigEndian.Uint16(payload[8:10]),
		GameType:   payload[10],
		ContinueID: binary.BigEndian.Uint32(payload[11:15]),
	}
	if decoded.UIN != request.EnvelopeUIN {
		return ModifyRoomInfoRequest{}, fmt.Errorf("modify-room-info UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	return decoded, nil
}

func BuildLocalModifyRoomInfoSuccess(requestPacket []byte) ([]byte, error) {
	return buildLocalModifyRoomInfoSuccess(requestPacket, nil)
}

func BuildLocalModifyRoomInfoSuccessWithReader(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalModifyRoomInfoSuccess(requestPacket, entropy)
}

func buildLocalModifyRoomInfoSuccess(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalModifyRoomInfoRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	// RESPONSE_MODIFY_ROOMINFO contains only Result(uint16).
	return buildLocalResponse(requestPacket, request, ModifyRoomInfoCommand, []byte{0, 0}, entropy)
}

func BuildLocalModifyRoomInfoNotification(recipientPacket []byte, roomID uint16, mapID uint16, gameType byte, continueID uint32) ([]byte, error) {
	if roomID == 0 {
		return nil, fmt.Errorf("NOTIFY_CHANGE_MAP room ID must be non-zero")
	}
	recipient, err := decodeLocalPacket(recipientPacket)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, 0, modifyRoomInfoNotifySize)
	payload = binary.BigEndian.AppendUint16(payload, mapID)
	payload = append(payload, gameType)
	payload = binary.BigEndian.AppendUint32(payload, continueID)
	return buildLocalNotificationFromRequestWithRoute(
		recipientPacket, recipient, ModifyRoomInfoNotifyCommand, payload,
		localMessageRoute{Route: modifyRoomInfoRoute, SectionID: roomID}, nil,
	)
}

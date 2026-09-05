package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	ChangeRoomTypeRequestSchema  = 0x0827
	ChangeRoomTypeResponseSchema = 0x0828
	ChangeRoomTypeNotifySchema   = 0x0435
	changeRoomTypeRequestSize    = 13
	changeRoomTypeNotifySize     = 5
	changeRoomTypeRoute          = 3
)

type ChangeRoomTypeRequest struct {
	UIN        uint32
	ClientTime uint32
	GameType   byte
	ContinueID uint32
}

func DecodeLocalChangeRoomTypeRequest(packet []byte) (ChangeRoomTypeRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return ChangeRoomTypeRequest{}, err
	}
	if request.Command != ChangeRoomTypeCommand {
		return ChangeRoomTypeRequest{}, fmt.Errorf("change-room-type command 0x%04X, want 0x%04X", request.Command, ChangeRoomTypeCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != changeRoomTypeRequestSize {
		return ChangeRoomTypeRequest{}, fmt.Errorf("change-room-type payload length %d, want %d", len(payload), changeRoomTypeRequestSize)
	}
	decoded := ChangeRoomTypeRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), ClientTime: binary.BigEndian.Uint32(payload[4:8]),
		GameType: payload[8], ContinueID: binary.BigEndian.Uint32(payload[9:13]),
	}
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN {
		return ChangeRoomTypeRequest{}, fmt.Errorf("change-room-type UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	if decoded.GameType > 4 {
		return ChangeRoomTypeRequest{}, fmt.Errorf("change-room-type GameType %d is outside 0..4", decoded.GameType)
	}
	return decoded, nil
}

func BuildLocalChangeRoomTypeSuccess(requestPacket []byte) ([]byte, error) {
	return buildLocalChangeRoomTypeSuccess(requestPacket, nil)
}

func buildLocalChangeRoomTypeSuccess(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalChangeRoomTypeRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	// RESPONSE_MODIFY_ROOMINFO is Result(uint16) followed by a counted reason.
	return buildLocalResponse(requestPacket, request, ChangeRoomTypeCommand, []byte{0, 0, 0}, entropy)
}

func BuildLocalChangeRoomTypeNotification(recipientPacket []byte, roomID uint16, gameType byte, continueID uint32) ([]byte, error) {
	if roomID == 0 {
		return nil, fmt.Errorf("NOTIYF_PLAYERRINFO_CHANGE room ID must be non-zero")
	}
	if gameType > 4 {
		return nil, fmt.Errorf("NOTIYF_PLAYERRINFO_CHANGE GameType %d is outside 0..4", gameType)
	}
	recipient, err := decodeLocalPacket(recipientPacket)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, 0, changeRoomTypeNotifySize)
	payload = append(payload, gameType)
	payload = binary.BigEndian.AppendUint32(payload, continueID)
	return buildLocalNotificationFromRequestWithRoute(
		recipientPacket, recipient, ChangeRoomTypeNotifyCommand, payload,
		localMessageRoute{Route: changeRoomTypeRoute, SectionID: roomID}, nil,
	)
}

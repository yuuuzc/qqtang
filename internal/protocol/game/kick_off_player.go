package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	KickOffPlayerRequestSchema  = 0x03FF
	KickOffPlayerResponseSchema = 0x07E7
	KickOffRoomNotifySchema     = 0x0404
	// KickOffRoomReasonCloseGameScene is the native no-dialog branch. The
	// QQTSection handler closes the active game scene through closeRoom(0) and
	// returns without entering the ordinary owner-kick message path.
	KickOffRoomReasonCloseGameScene uint16 = 4
	kickOffPlayerRequestSize               = 14
	kickOffRoomMinimumSize                 = 9
	kickOffRoomAttachMaximum               = 200
	kickOffPlayerRoute                     = 3
)

type KickOffPlayerResult uint16

const (
	KickOffPlayerSuccess KickOffPlayerResult = 0
	KickOffPlayerFailed  KickOffPlayerResult = 1
)

type KickOffPlayerRequest struct {
	UIN        uint32
	ClientTime uint32
	PlayerID   uint16
	PlayerUIN  uint32
}

type KickOffRoomNotification struct {
	ReasonID  uint16
	RoomID    uint16
	PlayerUIN uint32
	Attach    []byte
}

func DecodeLocalKickOffPlayerRequest(packet []byte) (KickOffPlayerRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return KickOffPlayerRequest{}, err
	}
	if request.Command != KickOffPlayerCommand {
		return KickOffPlayerRequest{}, fmt.Errorf("kick-off-player command 0x%04X, want 0x%04X", request.Command, KickOffPlayerCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != kickOffPlayerRequestSize {
		return KickOffPlayerRequest{}, fmt.Errorf("kick-off-player payload length %d, want %d", len(payload), kickOffPlayerRequestSize)
	}
	decoded := KickOffPlayerRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), ClientTime: binary.BigEndian.Uint32(payload[4:8]),
		PlayerID: binary.BigEndian.Uint16(payload[8:10]), PlayerUIN: binary.BigEndian.Uint32(payload[10:14]),
	}
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN || decoded.PlayerID == 0 || decoded.PlayerUIN == 0 {
		return KickOffPlayerRequest{}, fmt.Errorf("invalid kick-off-player identity %+v for envelope UIN %d", decoded, request.EnvelopeUIN)
	}
	return decoded, nil
}

func BuildLocalKickOffPlayerResponse(requestPacket []byte, result KickOffPlayerResult) ([]byte, error) {
	return buildLocalKickOffPlayerResponse(requestPacket, result, nil)
}

func BuildLocalKickOffPlayerResponseWithReader(requestPacket []byte, result KickOffPlayerResult, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalKickOffPlayerResponse(requestPacket, result, entropy)
}

func buildLocalKickOffPlayerResponse(requestPacket []byte, result KickOffPlayerResult, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalKickOffPlayerRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, 2)
	binary.BigEndian.PutUint16(payload, uint16(result))
	return buildLocalResponse(requestPacket, request, KickOffPlayerCommand, payload, entropy)
}

func BuildLocalKickOffRoomNotification(templatePacket []byte, roomID uint16, notification KickOffRoomNotification) ([]byte, error) {
	if roomID == 0 || notification.RoomID != roomID || notification.PlayerUIN == 0 || len(notification.Attach) > kickOffRoomAttachMaximum {
		return nil, fmt.Errorf("invalid kick-off-room notification %+v for room %d", notification, roomID)
	}
	template, err := decodeLocalPacket(templatePacket)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, kickOffRoomMinimumSize+len(notification.Attach))
	binary.BigEndian.PutUint16(payload[0:2], notification.ReasonID)
	binary.BigEndian.PutUint16(payload[2:4], notification.RoomID)
	binary.BigEndian.PutUint32(payload[4:8], notification.PlayerUIN)
	payload[8] = byte(len(notification.Attach))
	copy(payload[9:], notification.Attach)
	return buildLocalNotificationFromRequestWithRoute(
		templatePacket, template, KickOffRoomNotifyCommand, payload,
		localMessageRoute{Route: kickOffPlayerRoute, SectionID: roomID}, nil,
	)
}

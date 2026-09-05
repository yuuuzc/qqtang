package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

// QQTMsgData.bin uses the legacy word "Term" for the room's eight selectable
// teams. PLAYER_INFO_IN_ROOM_OLD packs this TeamID with the seat, while
// PLAYER_GAME_INFO carries the same value as its own byte at match start.
const (
	ChangeTeamRequestSchema     = 0x0400 // REQUEST_CHANGE_TERM
	ChangeTeamResponseSchema    = 0x07E8 // RESPONSE_CHANGE_TERM
	ChangeTeamNotifySchema      = 0x0409 // NOTIFY_CHANGE_TERM
	ChangeTeamAckSchema         = 0x07F1 // ACK_CHANGE_TERM
	changeTeamPayloadSize       = 10
	changeTeamNotifyPayloadSize = 7
	changeTeamRoomRoute         = 3
	MinRoomTeamID               = 1
	MaxRoomTeamID               = 8
)

type ChangeTeamRequest struct {
	UIN        uint32
	ClientTime uint32
	TeamID     byte
}

type ChangeTeamNotification struct {
	PlayerUIN uint32
	PlayerID  uint16
	NewTeamID byte
}

func DecodeLocalChangeTeamRequest(packet []byte) (ChangeTeamRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return ChangeTeamRequest{}, err
	}
	if request.Command != ChangeTeamCommand {
		return ChangeTeamRequest{}, fmt.Errorf("change-team command 0x%04X, want 0x%04X", request.Command, ChangeTeamCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != changeTeamPayloadSize {
		return ChangeTeamRequest{}, fmt.Errorf("change-team payload length %d, want %d", len(payload), changeTeamPayloadSize)
	}
	decoded := ChangeTeamRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
	}
	if decoded.UIN != request.EnvelopeUIN {
		return ChangeTeamRequest{}, fmt.Errorf("change-team UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	teamID := binary.BigEndian.Uint16(payload[8:10])
	if teamID < MinRoomTeamID || teamID > MaxRoomTeamID {
		return ChangeTeamRequest{}, fmt.Errorf("change-team TeamID %d is outside %d..%d", teamID, MinRoomTeamID, MaxRoomTeamID)
	}
	decoded.TeamID = byte(teamID)
	return decoded, nil
}

func BuildLocalChangeTeamSuccess(requestPacket []byte) ([]byte, error) {
	return buildLocalChangeTeamSuccess(requestPacket, nil)
}

func BuildLocalChangeTeamSuccessWithReader(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalChangeTeamSuccess(requestPacket, entropy)
}

func buildLocalChangeTeamSuccess(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalChangeTeamRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, ChangeTeamCommand, []byte{0, 0}, entropy)
}

func (notification ChangeTeamNotification) MarshalNetworkBinary() ([]byte, error) {
	if notification.PlayerUIN == 0 || notification.PlayerID == 0 {
		return nil, fmt.Errorf("NOTIFY_CHANGE_TERM player UIN and player ID must be non-zero")
	}
	if notification.NewTeamID < MinRoomTeamID || notification.NewTeamID > MaxRoomTeamID {
		return nil, fmt.Errorf("NOTIFY_CHANGE_TERM NewTermID %d is outside %d..%d", notification.NewTeamID, MinRoomTeamID, MaxRoomTeamID)
	}
	payload := make([]byte, changeTeamNotifyPayloadSize)
	binary.BigEndian.PutUint32(payload[0:4], notification.PlayerUIN)
	binary.BigEndian.PutUint16(payload[4:6], notification.PlayerID)
	payload[6] = notification.NewTeamID
	return payload, nil
}

func BuildLocalChangeTeamNotification(requestPacket []byte, roomID uint16, notification ChangeTeamNotification) ([]byte, error) {
	return buildLocalChangeTeamNotification(requestPacket, roomID, notification, nil)
}

func BuildLocalChangeTeamNotificationWithReader(requestPacket []byte, roomID uint16, notification ChangeTeamNotification, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalChangeTeamNotification(requestPacket, roomID, notification, entropy)
}

func BuildLocalChangeTeamNotificationForRecipient(recipientPacket []byte, roomID uint16, notification ChangeTeamNotification) ([]byte, error) {
	return buildLocalChangeTeamNotificationForRecipient(recipientPacket, roomID, notification, nil)
}

func buildLocalChangeTeamNotificationForRecipient(recipientPacket []byte, roomID uint16, notification ChangeTeamNotification, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(recipientPacket)
	if err != nil {
		return nil, err
	}
	if roomID == 0 {
		return nil, fmt.Errorf("NOTIFY_CHANGE_TERM room ID must be non-zero")
	}
	payload, err := notification.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(
		recipientPacket, request, ChangeTeamNotifyCommand, payload,
		localMessageRoute{Route: changeTeamRoomRoute, SectionID: roomID}, entropy,
	)
}

func buildLocalChangeTeamNotification(requestPacket []byte, roomID uint16, notification ChangeTeamNotification, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	if roomID == 0 {
		return nil, fmt.Errorf("NOTIFY_CHANGE_TERM room ID must be non-zero")
	}
	if notification.PlayerUIN != request.EnvelopeUIN {
		return nil, fmt.Errorf("NOTIFY_CHANGE_TERM player UIN %d does not match envelope UIN %d", notification.PlayerUIN, request.EnvelopeUIN)
	}
	payload, err := notification.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(
		requestPacket,
		request,
		ChangeTeamNotifyCommand,
		payload,
		localMessageRoute{Route: changeTeamRoomRoute, SectionID: roomID},
		entropy,
	)
}

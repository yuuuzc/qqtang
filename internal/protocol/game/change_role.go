package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	ChangeRoleRequestSchema     = 0x0401
	ChangeRoleResponseSchema    = 0x07E9
	ChangeRoleNotifySchema      = 0x040A
	ChangeRoleAckSchema         = 0x07F2
	changeRolePayloadSize       = 10
	changeRoleNotifyPayloadSize = 7
	changeRoleRoomRoute         = 3
)

// ChangeRoleRequest is the fixed REQUEST_CHANGE_ROLE payload observed in the
// local client captures. The UI passes an eight-bit role ID, while the legacy
// wire schema serializes that value as a big-endian uint16.
type ChangeRoleRequest struct {
	UIN        uint32
	ClientTime uint32
	RoleID     byte
}

// ChangeRoleNotification mirrors NOTIFY_CHANGE_ROLE. QQTMsgData.bin maps the
// seven-byte schema 0x040A to the independent server-to-client transport
// command 0x007B; it is not a second 0x0071 request response. Broadcasting
// this value makes the room member cache converge on the server's resolved
// concrete role (notably after resolving random placeholder role 23).
type ChangeRoleNotification struct {
	PlayerUIN uint32
	PlayerID  uint16
	NewRoleID byte
}

func (notification ChangeRoleNotification) MarshalNetworkBinary() ([]byte, error) {
	if notification.PlayerUIN == 0 {
		return nil, fmt.Errorf("NOTIFY_CHANGE_ROLE player UIN must be non-zero")
	}
	if notification.PlayerID == 0 {
		return nil, fmt.Errorf("NOTIFY_CHANGE_ROLE player ID must be non-zero")
	}
	if notification.NewRoleID == 0 {
		return nil, fmt.Errorf("NOTIFY_CHANGE_ROLE concrete role ID must be non-zero")
	}
	payload := make([]byte, changeRoleNotifyPayloadSize)
	binary.BigEndian.PutUint32(payload[0:4], notification.PlayerUIN)
	binary.BigEndian.PutUint16(payload[4:6], notification.PlayerID)
	payload[6] = notification.NewRoleID
	return payload, nil
}

func DecodeLocalChangeRoleRequest(packet []byte) (ChangeRoleRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return ChangeRoleRequest{}, err
	}
	if request.Command != ChangeRoleCommand {
		return ChangeRoleRequest{}, fmt.Errorf("change-role command 0x%04X, want 0x%04X", request.Command, ChangeRoleCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != changeRolePayloadSize {
		return ChangeRoleRequest{}, fmt.Errorf("change-role payload length %d, want %d", len(payload), changeRolePayloadSize)
	}
	decoded := ChangeRoleRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
	}
	if decoded.UIN != request.EnvelopeUIN {
		return ChangeRoleRequest{}, fmt.Errorf("change-role UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	roleID := binary.BigEndian.Uint16(payload[8:10])
	if roleID == 0 || roleID > 0xFF {
		return ChangeRoleRequest{}, fmt.Errorf("change-role role ID %d is outside 1..255", roleID)
	}
	decoded.RoleID = byte(roleID)
	return decoded, nil
}

func BuildLocalChangeRoleSuccess(requestPacket []byte) ([]byte, error) {
	return buildLocalChangeRoleSuccess(requestPacket, nil)
}

func BuildLocalChangeRoleSuccessWithReader(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalChangeRoleSuccess(requestPacket, entropy)
}

func buildLocalChangeRoleSuccess(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalChangeRoleRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	// The pending request maps this correlated command to
	// ID_RESPONSE_CHANGE_ROLE in QQTMsgData.bin. The two-byte Result=0 form
	// matches the neighboring room-operation responses; live acceptance remains
	// part of the next role-selection regression run.
	return buildLocalResponse(requestPacket, request, ChangeRoleCommand, []byte{0, 0}, entropy)
}

func BuildLocalChangeRoleNotification(requestPacket []byte, roomID uint16, notification ChangeRoleNotification) ([]byte, error) {
	return buildLocalChangeRoleNotification(requestPacket, roomID, notification, nil)
}

func BuildLocalChangeRoleNotificationWithReader(requestPacket []byte, roomID uint16, notification ChangeRoleNotification, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalChangeRoleNotification(requestPacket, roomID, notification, entropy)
}

func BuildLocalChangeRoleNotificationForRecipient(recipientPacket []byte, roomID uint16, notification ChangeRoleNotification) ([]byte, error) {
	request, err := decodeLocalPacket(recipientPacket)
	if err != nil {
		return nil, err
	}
	if roomID == 0 {
		return nil, fmt.Errorf("NOTIFY_CHANGE_ROLE room ID must be non-zero")
	}
	payload, err := notification.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(
		recipientPacket, request, ChangeRoleNotifyCommand, payload,
		localMessageRoute{Route: changeRoleRoomRoute, SectionID: roomID}, nil,
	)
}

func buildLocalChangeRoleNotification(requestPacket []byte, roomID uint16, notification ChangeRoleNotification, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	if roomID == 0 {
		return nil, fmt.Errorf("NOTIFY_CHANGE_ROLE room ID must be non-zero")
	}
	if notification.PlayerUIN != request.EnvelopeUIN {
		return nil, fmt.Errorf("NOTIFY_CHANGE_ROLE player UIN %d does not match envelope UIN %d", notification.PlayerUIN, request.EnvelopeUIN)
	}
	payload, err := notification.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(
		requestPacket,
		request,
		ChangeRoleNotifyCommand,
		payload,
		localMessageRoute{Route: changeRoleRoomRoute, SectionID: roomID},
		entropy,
	)
}

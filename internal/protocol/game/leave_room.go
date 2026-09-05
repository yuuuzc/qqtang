package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	LeaveRoomRequestSchema  = 0x03F9 // REQUEST_LEAVE_ROOM
	LeaveRoomResponseSchema = 0x07E1 // RESPONSE_LEAVE_ROOM
	LeaveRoomNotifySchema   = 0x03FA // NOTIFY_LEAVE_ROOM
	leaveRoomRequestSize    = 8
	leaveRoomNotifySize     = 8
	leaveRoomRoute          = 3
)

type LeaveRoomRequest struct {
	UIN        uint32
	ClientTime uint32
}

// LeaveRoomNotification is ready for room-wide fan-out. The current room-list
// adapter already observes the same authoritative departure through periodic
// refreshes; direct fan-out is added when cross-connection room membership is
// connected.
type LeaveRoomNotification struct {
	PlayerID        uint16
	NewRoomOwnerID  uint16
	NewArbitratorID uint16
	MapID           uint16
}

func DecodeLocalLeaveRoomRequest(packet []byte) (LeaveRoomRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return LeaveRoomRequest{}, err
	}
	if request.Command != LeaveRoomCommand {
		return LeaveRoomRequest{}, fmt.Errorf("leave-room command 0x%04X, want 0x%04X", request.Command, LeaveRoomCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != leaveRoomRequestSize {
		return LeaveRoomRequest{}, fmt.Errorf("leave-room payload length %d, want %d", len(payload), leaveRoomRequestSize)
	}
	decoded := LeaveRoomRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
	}
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN {
		return LeaveRoomRequest{}, fmt.Errorf("leave-room UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	return decoded, nil
}

func BuildLocalLeaveRoomSuccess(requestPacket []byte) ([]byte, error) {
	return buildLocalLeaveRoomSuccess(requestPacket, nil)
}

func BuildLocalLeaveRoomSuccessWithReader(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalLeaveRoomSuccess(requestPacket, entropy)
}

func buildLocalLeaveRoomSuccess(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalLeaveRoomRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, LeaveRoomCommand, []byte{0, 0}, entropy)
}

func (notification LeaveRoomNotification) MarshalNetworkBinary() ([]byte, error) {
	if notification.PlayerID == 0 {
		return nil, fmt.Errorf("NOTIFY_LEAVE_ROOM PlayerID must be non-zero")
	}
	payload := make([]byte, leaveRoomNotifySize)
	binary.BigEndian.PutUint16(payload[0:2], notification.PlayerID)
	binary.BigEndian.PutUint16(payload[2:4], notification.NewRoomOwnerID)
	binary.BigEndian.PutUint16(payload[4:6], notification.NewArbitratorID)
	binary.BigEndian.PutUint16(payload[6:8], notification.MapID)
	return payload, nil
}

func BuildLocalLeaveRoomNotification(requestPacket []byte, roomID uint16, notification LeaveRoomNotification) ([]byte, error) {
	return buildLocalLeaveRoomNotification(requestPacket, roomID, notification, nil)
}

func BuildLocalLeaveRoomNotificationWithReader(requestPacket []byte, roomID uint16, notification LeaveRoomNotification, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalLeaveRoomNotification(requestPacket, roomID, notification, entropy)
}

func buildLocalLeaveRoomNotification(requestPacket []byte, roomID uint16, notification LeaveRoomNotification, entropy io.Reader) ([]byte, error) {
	if roomID == 0 {
		return nil, fmt.Errorf("NOTIFY_LEAVE_ROOM room ID must be non-zero")
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload, err := notification.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(
		requestPacket,
		request,
		LeaveRoomNotifyCommand,
		payload,
		localMessageRoute{Route: leaveRoomRoute, SectionID: roomID},
		entropy,
	)
}

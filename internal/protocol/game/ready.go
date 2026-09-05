package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	ReadyRequestSchema        = 0x0402 // REQUEST_READY
	ReadyResponseSchema       = 0x07EA // RESPONSE_READY
	CancelReadyRequestSchema  = 0x0403 // REQUEST_CANCEL_READY
	CancelReadyResponseSchema = 0x07EB // RESPONSE_CANCEL_READY
	ReadyStateNotifySchema    = 0x040B // NOTIFY_PLAYER_READY_STATE
	ReadyStateAckSchema       = 0x07F3 // ACK_PLAYER_READY_STATE
	readyRequestPayloadSize   = 8
	readyNotifyPayloadSize    = 7
	readyRoomRoute            = 3
)

type ReadyRequest struct {
	UIN        uint32
	ClientTime uint32
	Ready      bool
}

type ReadyStateNotification struct {
	PlayerUIN uint32
	PlayerID  uint16
	Ready     bool
}

func DecodeLocalReadyRequest(packet []byte) (ReadyRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return ReadyRequest{}, err
	}
	var ready bool
	switch request.Command {
	case ReadyCommand:
		ready = true
	case CancelReadyCommand:
		ready = false
	default:
		return ReadyRequest{}, fmt.Errorf("ready command 0x%04X is neither 0x%04X nor 0x%04X", request.Command, ReadyCommand, CancelReadyCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != readyRequestPayloadSize {
		return ReadyRequest{}, fmt.Errorf("ready payload length %d, want %d", len(payload), readyRequestPayloadSize)
	}
	decoded := ReadyRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
		Ready:      ready,
	}
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN {
		return ReadyRequest{}, fmt.Errorf("ready UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	return decoded, nil
}

func BuildLocalReadySuccess(requestPacket []byte) ([]byte, error) {
	return buildLocalReadySuccess(requestPacket, nil)
}

func BuildLocalReadySuccessWithReader(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalReadySuccess(requestPacket, entropy)
}

func buildLocalReadySuccess(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalReadyRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, request.Command, []byte{0, 0}, entropy)
}

func (notification ReadyStateNotification) MarshalNetworkBinary() ([]byte, error) {
	if notification.PlayerUIN == 0 || notification.PlayerID == 0 {
		return nil, fmt.Errorf("NOTIFY_PLAYER_READY_STATE player UIN and player ID must be non-zero")
	}
	payload := make([]byte, readyNotifyPayloadSize)
	binary.BigEndian.PutUint32(payload[0:4], notification.PlayerUIN)
	binary.BigEndian.PutUint16(payload[4:6], notification.PlayerID)
	if notification.Ready {
		payload[6] = 1
	}
	return payload, nil
}

func BuildLocalReadyStateNotification(requestPacket []byte, roomID uint16, notification ReadyStateNotification) ([]byte, error) {
	return buildLocalReadyStateNotification(requestPacket, roomID, notification, nil)
}

func BuildLocalReadyStateNotificationWithReader(requestPacket []byte, roomID uint16, notification ReadyStateNotification, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalReadyStateNotification(requestPacket, roomID, notification, entropy)
}

func buildLocalReadyStateNotification(requestPacket []byte, roomID uint16, notification ReadyStateNotification, entropy io.Reader) ([]byte, error) {
	if roomID == 0 {
		return nil, fmt.Errorf("NOTIFY_PLAYER_READY_STATE room ID must be non-zero")
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
		requestPacket, request, ReadyStateNotifyCommand, payload,
		localMessageRoute{Route: readyRoomRoute, SectionID: roomID}, entropy,
	)
}

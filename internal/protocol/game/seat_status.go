package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	SetSeatStatusRequestSchema  = 0x0417 // REQUEST_SET_SEAT_STATUS
	SetSeatStatusResponseSchema = 0x07FF // RESPONSE_SET_SEAT_STATUS
	SetSeatStatusNotifySchema   = 0x0418 // NOTIFY_SET_SEAT_STATUS
	setSeatStatusRequestSize    = 10
	setSeatStatusNotifySize     = 2
	setSeatStatusRoomRoute      = 3
	MinRoomSeatID               = 1
	MaxRoomSeatID               = 8
)

type SetSeatStatusRequest struct {
	UIN        uint32
	ClientTime uint32
	SeatID     byte
	NewStatus  RoomSeatStatus
}

func (request SetSeatStatusRequest) Locked() bool {
	return request.NewStatus == RoomSeatStatusLocked
}

func DecodeLocalSetSeatStatusRequest(packet []byte) (SetSeatStatusRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return SetSeatStatusRequest{}, err
	}
	if request.Command != SetSeatStatusCommand {
		return SetSeatStatusRequest{}, fmt.Errorf("set-seat-status command 0x%04X, want 0x%04X", request.Command, SetSeatStatusCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != setSeatStatusRequestSize {
		return SetSeatStatusRequest{}, fmt.Errorf("set-seat-status payload length %d, want %d", len(payload), setSeatStatusRequestSize)
	}
	decoded := SetSeatStatusRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
		SeatID:     payload[8],
		NewStatus:  RoomSeatStatus(payload[9]),
	}
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN {
		return SetSeatStatusRequest{}, fmt.Errorf("set-seat-status UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	if decoded.SeatID < MinRoomSeatID || decoded.SeatID > MaxRoomSeatID {
		return SetSeatStatusRequest{}, fmt.Errorf("set-seat-status SeatID %d is outside %d..%d", decoded.SeatID, MinRoomSeatID, MaxRoomSeatID)
	}
	if decoded.NewStatus != RoomSeatStatusOpen && decoded.NewStatus != RoomSeatStatusLocked {
		return SetSeatStatusRequest{}, fmt.Errorf("set-seat-status NewStatus %d is unsupported", decoded.NewStatus)
	}
	return decoded, nil
}

func BuildLocalSetSeatStatusSuccess(requestPacket []byte) ([]byte, error) {
	return buildLocalSetSeatStatusSuccess(requestPacket, nil)
}

func BuildLocalSetSeatStatusSuccessWithReader(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalSetSeatStatusSuccess(requestPacket, entropy)
}

func buildLocalSetSeatStatusSuccess(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalSetSeatStatusRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, SetSeatStatusCommand, []byte{0, 0}, entropy)
}

func BuildLocalSetSeatStatusNotification(requestPacket []byte, roomID uint16, seatID byte, status RoomSeatStatus) ([]byte, error) {
	return buildLocalSetSeatStatusNotification(requestPacket, roomID, seatID, status, nil)
}

func BuildLocalSetSeatStatusNotificationWithReader(requestPacket []byte, roomID uint16, seatID byte, status RoomSeatStatus, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalSetSeatStatusNotification(requestPacket, roomID, seatID, status, entropy)
}

func buildLocalSetSeatStatusNotification(requestPacket []byte, roomID uint16, seatID byte, status RoomSeatStatus, entropy io.Reader) ([]byte, error) {
	if roomID == 0 {
		return nil, fmt.Errorf("NOTIFY_SET_SEAT_STATUS room ID must be non-zero")
	}
	if seatID < MinRoomSeatID || seatID > MaxRoomSeatID {
		return nil, fmt.Errorf("NOTIFY_SET_SEAT_STATUS SeatID %d is outside %d..%d", seatID, MinRoomSeatID, MaxRoomSeatID)
	}
	if status != RoomSeatStatusOpen && status != RoomSeatStatusLocked {
		return nil, fmt.Errorf("NOTIFY_SET_SEAT_STATUS NewStatus %d is unsupported", status)
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload := []byte{seatID, byte(status)}
	if len(payload) != setSeatStatusNotifySize {
		return nil, fmt.Errorf("NOTIFY_SET_SEAT_STATUS payload length %d, want %d", len(payload), setSeatStatusNotifySize)
	}
	return buildLocalNotificationFromRequestWithRoute(
		requestPacket,
		request,
		SetSeatStatusNotifyCommand,
		payload,
		localMessageRoute{Route: setSeatStatusRoomRoute, SectionID: roomID},
		entropy,
	)
}

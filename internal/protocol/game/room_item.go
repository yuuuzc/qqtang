package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	UseItemInRoomRequestSchema  = 0x0415
	UseItemInRoomResponseSchema = 0x07FD
	UseItemInRoomNotifySchema   = 0x0416
	useItemInRoomRequestSize    = 12
	useItemInRoomNotifySize     = 10
	useItemInRoomRoute          = 3
)

// UseItemInRoomRequest is the account-inventory operation issued outside a
// running match. It is deliberately separate from REQUEST_USE_ITEM (0x0FAF),
// whose ItemID names a temporary battlefield element and must not consume the
// account inventory.
type UseItemInRoomRequest struct {
	UIN        uint32
	ClientTime uint32
	ItemID     uint32
}

// UseItemInRoomNotification mirrors NOTIFY_USE_ITEM_IN_ROOM. QQTMsgData.bin
// names the middle uint16 field Dlg; its complete enumeration is not yet
// recovered, so retain the structural name DialogCode instead of guessing.
type UseItemInRoomNotification struct {
	UIN        uint32
	DialogCode uint16
	ItemID     uint32
}

func DecodeLocalUseItemInRoomRequest(packet []byte) (UseItemInRoomRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return UseItemInRoomRequest{}, err
	}
	if request.Command != UseItemInRoomCommand {
		return UseItemInRoomRequest{}, fmt.Errorf("use-item-in-room command 0x%04X, want 0x%04X", request.Command, UseItemInRoomCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != useItemInRoomRequestSize {
		return UseItemInRoomRequest{}, fmt.Errorf("use-item-in-room payload length %d, want %d", len(payload), useItemInRoomRequestSize)
	}
	decoded := UseItemInRoomRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
		ItemID:     binary.BigEndian.Uint32(payload[8:12]),
	}
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN {
		return UseItemInRoomRequest{}, fmt.Errorf("use-item-in-room UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	if decoded.ItemID == 0 {
		return UseItemInRoomRequest{}, fmt.Errorf("use-item-in-room ItemID must be non-zero")
	}
	return decoded, nil
}

func (notification UseItemInRoomNotification) MarshalNetworkBinary() ([]byte, error) {
	if notification.UIN == 0 || notification.ItemID == 0 {
		return nil, fmt.Errorf("NOTIFY_USE_ITEM_IN_ROOM requires non-zero UIN and ItemID")
	}
	payload := make([]byte, useItemInRoomNotifySize)
	binary.BigEndian.PutUint32(payload[0:4], notification.UIN)
	binary.BigEndian.PutUint16(payload[4:6], notification.DialogCode)
	binary.BigEndian.PutUint32(payload[6:10], notification.ItemID)
	return payload, nil
}

func BuildLocalUseItemInRoomResponse(requestPacket []byte, resultID uint16) ([]byte, error) {
	return buildLocalUseItemInRoomResponse(requestPacket, resultID, nil)
}

func BuildLocalUseItemInRoomResponseWithReader(requestPacket []byte, resultID uint16, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalUseItemInRoomResponse(requestPacket, resultID, entropy)
}

func buildLocalUseItemInRoomResponse(requestPacket []byte, resultID uint16, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalUseItemInRoomRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload := binary.BigEndian.AppendUint16(nil, resultID)
	return buildLocalResponse(requestPacket, request, UseItemInRoomCommand, payload, entropy)
}

func BuildLocalUseItemInRoomNotificationWithReader(requestPacket []byte, roomID uint16, notification UseItemInRoomNotification, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalUseItemInRoomNotification(requestPacket, roomID, notification, entropy)
}

func buildLocalUseItemInRoomNotification(requestPacket []byte, roomID uint16, notification UseItemInRoomNotification, entropy io.Reader) ([]byte, error) {
	decoded, err := DecodeLocalUseItemInRoomRequest(requestPacket)
	if err != nil {
		return nil, err
	}
	if roomID == 0 {
		return nil, fmt.Errorf("NOTIFY_USE_ITEM_IN_ROOM room ID must be non-zero")
	}
	if notification.UIN != decoded.UIN || notification.ItemID != decoded.ItemID {
		return nil, fmt.Errorf("NOTIFY_USE_ITEM_IN_ROOM request/notification UIN or ItemID mismatch")
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
		requestPacket, request, UseItemInRoomNotifyCommand, payload,
		localMessageRoute{Route: useItemInRoomRoute, SectionID: roomID}, entropy,
	)
}

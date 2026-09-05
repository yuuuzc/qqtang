package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	RequestBackgroundListSchema  uint32 = 0x0BE0
	ResponseBackgroundListSchema uint32 = 0x0BE1
	RequestUseBackgroundSchema   uint32 = 0x0BE2
	ResponseUseBackgroundSchema  uint32 = 0x0BE3
	NotifyUseBackgroundSchema    uint32 = 0x0BE4

	backgroundRequestSize      = 12
	backgroundListMaximumCount = 50
	chatBackgroundRoute        = 4

	UseBackgroundResultSuccess uint16 = 0
	UseBackgroundResultFailed  uint16 = 1
)

// The shipped 5.2 client contains preview resources chat0.img through
// chat9.img and derives the full room resource name from the returned ID.
// Keep this as the current asset-backed catalog, not a protocol-wide limit.
var builtInChatBackgroundIDs = [...]uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

// Chat backgrounds 5 and 7 are the shipped Chinese and western wedding
// scenes. Selecting either scene enters the corresponding native wedding
// panel; all other backgrounds retain the ordinary password/settings panel.
func ChatBackgroundWeddingMode(id uint32) (uint16, bool) {
	switch id {
	case 5:
		return 1, true
	case 7:
		return 0, true
	default:
		return 0, false
	}
}

type BackgroundListRequest struct {
	UIN     uint32
	Time    uint32
	Reserve uint32
}

type UseBackgroundRequest struct {
	UIN    uint32
	Time   uint32
	ItemID uint32
}

func BuiltInChatBackgroundIDs() []uint32 {
	return append([]uint32(nil), builtInChatBackgroundIDs[:]...)
}

func IsBuiltInChatBackgroundID(id uint32) bool {
	for _, candidate := range builtInChatBackgroundIDs {
		if candidate == id {
			return true
		}
	}
	return false
}

func DecodeLocalBackgroundListRequest(packet []byte) (BackgroundListRequest, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return BackgroundListRequest{}, err
	}
	if decoded.Command != BackgroundListCommand {
		return BackgroundListRequest{}, fmt.Errorf("background-list command 0x%04X, want 0x%04X", decoded.Command, BackgroundListCommand)
	}
	payload := decoded.Plaintext[localInnerHeaderSize:]
	if len(payload) != backgroundRequestSize {
		return BackgroundListRequest{}, fmt.Errorf("background-list payload length %d, want %d", len(payload), backgroundRequestSize)
	}
	request := BackgroundListRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]),
		Reserve: binary.BigEndian.Uint32(payload[8:12]),
	}
	if request.UIN == 0 || request.UIN != decoded.EnvelopeUIN {
		return BackgroundListRequest{}, fmt.Errorf("background-list UIN %d does not match envelope UIN %d", request.UIN, decoded.EnvelopeUIN)
	}
	return request, nil
}

func DecodeLocalUseBackgroundRequest(packet []byte) (UseBackgroundRequest, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return UseBackgroundRequest{}, err
	}
	if decoded.Command != UseBackgroundCommand {
		return UseBackgroundRequest{}, fmt.Errorf("use-background command 0x%04X, want 0x%04X", decoded.Command, UseBackgroundCommand)
	}
	payload := decoded.Plaintext[localInnerHeaderSize:]
	if len(payload) != backgroundRequestSize {
		return UseBackgroundRequest{}, fmt.Errorf("use-background payload length %d, want %d", len(payload), backgroundRequestSize)
	}
	request := UseBackgroundRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]),
		ItemID: binary.BigEndian.Uint32(payload[8:12]),
	}
	if request.UIN == 0 || request.UIN != decoded.EnvelopeUIN {
		return UseBackgroundRequest{}, fmt.Errorf("use-background UIN %d does not match envelope UIN %d", request.UIN, decoded.EnvelopeUIN)
	}
	return request, nil
}

func BuildLocalBackgroundListResponse(packet []byte, ids []uint32) ([]byte, error) {
	return buildLocalBackgroundListResponse(packet, ids, nil)
}

func BuildLocalBackgroundListResponseWithReader(packet []byte, ids []uint32, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalBackgroundListResponse(packet, ids, entropy)
}

func buildLocalBackgroundListResponse(packet []byte, ids []uint32, entropy io.Reader) ([]byte, error) {
	requestFields, err := DecodeLocalBackgroundListRequest(packet)
	if err != nil {
		return nil, err
	}
	if len(ids) > backgroundListMaximumCount {
		return nil, fmt.Errorf("background-list count %d exceeds %d", len(ids), backgroundListMaximumCount)
	}
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return nil, err
	}
	payload := binary.BigEndian.AppendUint32(nil, requestFields.UIN)
	payload = binary.BigEndian.AppendUint32(payload, requestFields.Time)
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(ids)))
	for _, id := range ids {
		payload = binary.BigEndian.AppendUint32(payload, id)
	}
	return buildLocalResponse(packet, request, BackgroundListCommand, payload, entropy)
}

func BuildLocalUseBackgroundResponse(packet []byte, result uint16) ([]byte, error) {
	requestFields, err := DecodeLocalUseBackgroundRequest(packet)
	if err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return nil, err
	}
	payload := binary.BigEndian.AppendUint32(nil, requestFields.UIN)
	payload = binary.BigEndian.AppendUint32(payload, requestFields.Time)
	payload = binary.BigEndian.AppendUint16(payload, result)
	return buildLocalResponse(packet, request, UseBackgroundCommand, payload, nil)
}

func BuildLocalUseBackgroundNotification(template []byte, roomID uint16, requestFields UseBackgroundRequest) ([]byte, error) {
	if roomID == 0 || requestFields.UIN == 0 {
		return nil, fmt.Errorf("use-background notification requires room and actor UIN")
	}
	request, err := decodeLocalPacket(template)
	if err != nil {
		return nil, err
	}
	payload := binary.BigEndian.AppendUint32(nil, requestFields.UIN)
	payload = binary.BigEndian.AppendUint32(payload, requestFields.Time)
	payload = binary.BigEndian.AppendUint32(payload, requestFields.ItemID)
	return buildLocalNotificationFromRequestWithRoute(template, request, UseBackgroundNotifyCommand, payload,
		localMessageRoute{Route: chatBackgroundRoute, SectionID: roomID}, nil)
}

package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

// The hidden treasure room uses top-level transport commands 0x00AB..0x00AE.
// Its 0x116C..0x1171 schemas are not REQUEST_PLAY/GameEvent payloads.
const (
	TreasureRoomMapID        uint32 = 1111
	MaxTreasureRoomItemCount        = 30
)

type TreasureRoomRequest struct {
	UIN        uint32
	ClientTime uint32
}

type TreasureRoomResponse struct {
	ResultID uint16
	MapID    uint32
	Items    []ItemInfo
}

type TreasureItemRequest struct {
	UIN        uint32
	ClientTime uint32
	ItemID     uint32
}

type TreasureItemResponse struct {
	ResultID uint16
}

func ParseTreasureRoomRequest(payload []byte) (TreasureRoomRequest, error) {
	if len(payload) != 8 {
		return TreasureRoomRequest{}, fmt.Errorf("REQUST_IN_TREASURE payload length %d, want 8", len(payload))
	}
	request := TreasureRoomRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
	}
	if request.UIN == 0 {
		return TreasureRoomRequest{}, fmt.Errorf("REQUST_IN_TREASURE UIN must be non-zero")
	}
	return request, nil
}

func (response TreasureRoomResponse) MarshalNetworkBinary() ([]byte, error) {
	if len(response.Items) > MaxTreasureRoomItemCount {
		return nil, fmt.Errorf("RESPONSE_IN_TREASURE item count %d exceeds %d", len(response.Items), MaxTreasureRoomItemCount)
	}
	if response.ResultID == 0 && response.MapID == 0 {
		return nil, fmt.Errorf("successful RESPONSE_IN_TREASURE requires a map ID")
	}
	payload := make([]byte, 8, 8+len(response.Items)*ItemInfoBinarySize)
	binary.BigEndian.PutUint16(payload[0:2], response.ResultID)
	binary.BigEndian.PutUint32(payload[2:6], response.MapID)
	binary.BigEndian.PutUint16(payload[6:8], uint16(len(response.Items)))
	for _, item := range response.Items {
		payload = item.AppendNetworkBinary(payload)
	}
	return payload, nil
}

func ParseTreasureRoomResponse(payload []byte) (TreasureRoomResponse, error) {
	if len(payload) < 8 {
		return TreasureRoomResponse{}, fmt.Errorf("RESPONSE_IN_TREASURE payload length %d, need at least 8", len(payload))
	}
	count := int(binary.BigEndian.Uint16(payload[6:8]))
	if count > MaxTreasureRoomItemCount {
		return TreasureRoomResponse{}, fmt.Errorf("RESPONSE_IN_TREASURE item count %d exceeds %d", count, MaxTreasureRoomItemCount)
	}
	want := 8 + count*ItemInfoBinarySize
	if len(payload) != want {
		return TreasureRoomResponse{}, fmt.Errorf("RESPONSE_IN_TREASURE payload length %d, want %d for %d items", len(payload), want, count)
	}
	response := TreasureRoomResponse{
		ResultID: binary.BigEndian.Uint16(payload[0:2]),
		MapID:    binary.BigEndian.Uint32(payload[2:6]),
		Items:    make([]ItemInfo, 0, count),
	}
	for offset := 8; offset < want; offset += ItemInfoBinarySize {
		item, err := ParseItemInfoNetwork(payload[offset : offset+ItemInfoBinarySize])
		if err != nil {
			return TreasureRoomResponse{}, err
		}
		response.Items = append(response.Items, item)
	}
	if response.ResultID == 0 && response.MapID == 0 {
		return TreasureRoomResponse{}, fmt.Errorf("successful RESPONSE_IN_TREASURE has zero map ID")
	}
	return response, nil
}

func ParseTreasureItemRequest(payload []byte) (TreasureItemRequest, error) {
	if len(payload) != 12 {
		return TreasureItemRequest{}, fmt.Errorf("REQUST_GETITEM_TREASURE payload length %d, want 12", len(payload))
	}
	request := TreasureItemRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
		ItemID:     binary.BigEndian.Uint32(payload[8:12]),
	}
	if request.UIN == 0 || request.ItemID == 0 {
		return TreasureItemRequest{}, fmt.Errorf("REQUST_GETITEM_TREASURE requires non-zero UIN and item ID")
	}
	return request, nil
}

func ParseTreasureLeaveNotification(payload []byte) (TreasureRoomRequest, error) {
	request, err := ParseTreasureRoomRequest(payload)
	if err != nil {
		return TreasureRoomRequest{}, fmt.Errorf("NOTIFY_LEAVE_TREASURE: %w", err)
	}
	return request, nil
}

func DecodeLocalTreasureRoomRequest(packet []byte) (TreasureRoomRequest, error) {
	return decodeLocalTreasureRoomRequest(packet, EnterTreasureCommand, ParseTreasureRoomRequest)
}

func DecodeLocalTreasureLeaveNotification(packet []byte) (TreasureRoomRequest, error) {
	return decodeLocalTreasureRoomRequest(packet, LeaveTreasureCommand, ParseTreasureLeaveNotification)
}

func decodeLocalTreasureRoomRequest(packet []byte, command uint16, parse func([]byte) (TreasureRoomRequest, error)) (TreasureRoomRequest, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return TreasureRoomRequest{}, err
	}
	if decoded.Command != command {
		return TreasureRoomRequest{}, fmt.Errorf("treasure command 0x%04X, want 0x%04X", decoded.Command, command)
	}
	request, err := parse(decoded.Plaintext[localInnerHeaderSize:])
	if err != nil {
		return TreasureRoomRequest{}, err
	}
	if request.UIN != decoded.EnvelopeUIN {
		return TreasureRoomRequest{}, fmt.Errorf("treasure envelope UIN %d != payload UIN %d", decoded.EnvelopeUIN, request.UIN)
	}
	return request, nil
}

func DecodeLocalTreasureItemRequest(packet []byte) (TreasureItemRequest, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return TreasureItemRequest{}, err
	}
	if decoded.Command != GetTreasureItemCommand {
		return TreasureItemRequest{}, fmt.Errorf("treasure-item command 0x%04X, want 0x%04X", decoded.Command, GetTreasureItemCommand)
	}
	request, err := ParseTreasureItemRequest(decoded.Plaintext[localInnerHeaderSize:])
	if err != nil {
		return TreasureItemRequest{}, err
	}
	if request.UIN != decoded.EnvelopeUIN {
		return TreasureItemRequest{}, fmt.Errorf("treasure-item envelope UIN %d != payload UIN %d", decoded.EnvelopeUIN, request.UIN)
	}
	return request, nil
}

func BuildLocalTreasureRoomResponse(requestPacket []byte, response TreasureRoomResponse) ([]byte, error) {
	return buildLocalTreasureRoomResponse(requestPacket, response, nil)
}

func BuildLocalTreasureRoomResponseWithReader(requestPacket []byte, response TreasureRoomResponse, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalTreasureRoomResponse(requestPacket, response, entropy)
}

func buildLocalTreasureRoomResponse(requestPacket []byte, response TreasureRoomResponse, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalTreasureRoomRequest(requestPacket); err != nil {
		return nil, err
	}
	decoded, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload, err := response.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, decoded, EnterTreasureCommand, payload, entropy)
}

func BuildLocalTreasureItemResponse(requestPacket []byte, result TreasureItemResponse) ([]byte, error) {
	return buildLocalTreasureItemResponse(requestPacket, result, nil)
}

func BuildLocalTreasureItemResponseWithReader(requestPacket []byte, result TreasureItemResponse, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalTreasureItemResponse(requestPacket, result, entropy)
}

func buildLocalTreasureItemResponse(requestPacket []byte, result TreasureItemResponse, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalTreasureItemRequest(requestPacket); err != nil {
		return nil, err
	}
	decoded, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload := binary.BigEndian.AppendUint16(nil, result.ResultID)
	return buildLocalResponse(requestPacket, decoded, GetTreasureItemCommand, payload, entropy)
}

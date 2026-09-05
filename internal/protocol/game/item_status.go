package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	ItemStatusChangeRequestSchema = 0x0414
	itemStatusChangeHeaderSize    = 10
	itemStatusChangeEntrySize     = 6
)

// ItemStatusChange mirrors ITEM_CHANGE_INFO. ItemID is a DWORD in the
// request even though the account inventory's ITEM_INFO uses a uint16 ID.
type ItemStatusChange struct {
	ItemID    uint32
	NewStatus byte
	NewRoleID byte
}

// ItemStatusChangeRequest mirrors REQUEST_ITEM_STATUS_CHANGE. The legacy room
// UI sends it after the player adds or removes a carried item.
type ItemStatusChangeRequest struct {
	UIN        uint32
	ClientTime uint32
	Items      []ItemStatusChange
}

func DecodeLocalItemStatusChangeRequest(packet []byte) (ItemStatusChangeRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return ItemStatusChangeRequest{}, err
	}
	if request.Command != ItemStatusChangeCommand {
		return ItemStatusChangeRequest{}, fmt.Errorf("item-status-change command 0x%04X, want 0x%04X", request.Command, ItemStatusChangeCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) < itemStatusChangeHeaderSize {
		return ItemStatusChangeRequest{}, fmt.Errorf("item-status-change payload length %d is shorter than %d", len(payload), itemStatusChangeHeaderSize)
	}
	decoded := ItemStatusChangeRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
	}
	if decoded.UIN != request.EnvelopeUIN {
		return ItemStatusChangeRequest{}, fmt.Errorf("item-status-change UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	count := int(binary.BigEndian.Uint16(payload[8:10]))
	if count > MaxItemInfoCount {
		return ItemStatusChangeRequest{}, fmt.Errorf("item-status-change count %d exceeds %d", count, MaxItemInfoCount)
	}
	wantLength := itemStatusChangeHeaderSize + count*itemStatusChangeEntrySize
	if len(payload) != wantLength {
		return ItemStatusChangeRequest{}, fmt.Errorf("item-status-change payload length %d, want %d for %d items", len(payload), wantLength, count)
	}
	decoded.Items = make([]ItemStatusChange, 0, count)
	for offset := itemStatusChangeHeaderSize; offset < len(payload); offset += itemStatusChangeEntrySize {
		decoded.Items = append(decoded.Items, ItemStatusChange{
			ItemID:    binary.BigEndian.Uint32(payload[offset : offset+4]),
			NewStatus: payload[offset+4],
			NewRoleID: payload[offset+5],
		})
	}
	return decoded, nil
}

func BuildLocalItemStatusChangeSuccess(requestPacket []byte) ([]byte, error) {
	return buildLocalItemStatusChangeSuccess(requestPacket, nil)
}

func BuildLocalItemStatusChangeSuccessWithReader(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalItemStatusChangeSuccess(requestPacket, entropy)
}

func buildLocalItemStatusChangeSuccess(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalItemStatusChangeRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	// RESPONSE_ITEM_STATUS_CHANGE (0x07FC) contains only ResultID.
	return buildLocalResponse(requestPacket, request, ItemStatusChangeCommand, []byte{0, 0}, entropy)
}

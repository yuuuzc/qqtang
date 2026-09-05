package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	BreakEggRequestSchema  uint32 = 0x0BDB
	BreakEggResponseSchema uint32 = 0x0BDC

	breakEggRequestSize       = 16
	breakEggAttachInfoMaximum = 255
)

// BreakEggRequest mirrors the REQUEST_BREAK_EGG (0x0BDB) payload carried by
// transport command 0x00FE. EggID and HammerID are DWORDs even though local
// account inventory item IDs currently fit in uint16.
type BreakEggRequest struct {
	UIN        uint32
	ClientTime uint32
	HammerID   uint32
	EggID      uint32
}

func DecodeLocalBreakEggRequest(packet []byte) (BreakEggRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return BreakEggRequest{}, err
	}
	if request.Command != BreakEggRequestCommand {
		return BreakEggRequest{}, fmt.Errorf("break-egg command 0x%04X, want 0x%04X", request.Command, BreakEggRequestCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != breakEggRequestSize {
		return BreakEggRequest{}, fmt.Errorf("break-egg request payload length %d, want %d", len(payload), breakEggRequestSize)
	}
	decoded := BreakEggRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
		HammerID:   binary.BigEndian.Uint32(payload[8:12]),
		EggID:      binary.BigEndian.Uint32(payload[12:16]),
	}
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN {
		return BreakEggRequest{}, fmt.Errorf("break-egg UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	return decoded, nil
}

type BreakEggResponse struct {
	ResultID   uint16
	ShowItemID uint32
	AttachInfo string
}

func BuildLocalBreakEggResponse(packet []byte, response BreakEggResponse) ([]byte, error) {
	return buildLocalBreakEggResponse(packet, response, nil)
}

func BuildLocalBreakEggResponseWithReader(packet []byte, response BreakEggResponse, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalBreakEggResponse(packet, response, entropy)
}

func buildLocalBreakEggResponse(packet []byte, response BreakEggResponse, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return nil, err
	}
	if _, err = DecodeLocalBreakEggRequest(packet); err != nil {
		return nil, err
	}
	attachInfo, err := encodeLegacyGBKText(response.AttachInfo, breakEggAttachInfoMaximum)
	if err != nil {
		return nil, err
	}
	// RESPONSE_BREAK_EGG (0x0BDC) is a compact counted structure. QQTMsgData's 263-byte
	// size is the decoded maximum (7 fixed bytes + a 256-byte backing array),
	// while the transport serializer emits only AttachInfoLen bytes.
	payload := make([]byte, 0, 7+len(attachInfo))
	payload = binary.BigEndian.AppendUint16(payload, response.ResultID)
	payload = binary.BigEndian.AppendUint32(payload, response.ShowItemID)
	payload = append(payload, byte(len(attachInfo)))
	payload = append(payload, attachInfo...)
	return buildLocalMessageFromRequest(packet, request, BreakEggResponseCommand, payload, entropy)
}

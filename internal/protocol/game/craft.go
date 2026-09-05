package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	CombineForgeRequestSchema  uint32 = 0x0825
	CombineForgeResponseSchema uint32 = 0x0826
	ForgeRequestSchema         uint32 = 0x0BBB
	ForgeResponseSchema        uint32 = 0x0BBC

	combineForgeMaterialMaximum    = 20
	combineForgeRequestFixedSize   = 19
	combineForgeRequestMaximumSize = combineForgeRequestFixedSize + 4*combineForgeMaterialMaximum
	combineForgeResponseFixedSize  = 15
	forgeRequestSize               = 17
	forgeResponseHeaderSize        = 15
	craftReasonMaximum             = 200
)

type CombineForgeRequest struct {
	UIN         uint32
	ClientTime  uint32
	Type        byte
	ItemID      uint32
	FromItemID  uint32
	MaterialIDs []uint32
}

func DecodeLocalCombineForgeRequest(packet []byte) (CombineForgeRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return CombineForgeRequest{}, err
	}
	if request.Command != CombineForgeCommand {
		return CombineForgeRequest{}, fmt.Errorf("combine-forge command 0x%04X, want 0x%04X", request.Command, CombineForgeCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) < combineForgeRequestFixedSize || len(payload) > combineForgeRequestMaximumSize {
		return CombineForgeRequest{}, fmt.Errorf(
			"combine-forge request payload length %d is outside %d..%d",
			len(payload), combineForgeRequestFixedSize, combineForgeRequestMaximumSize,
		)
	}
	result := CombineForgeRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), ClientTime: binary.BigEndian.Uint32(payload[4:8]),
		Type: payload[8], ItemID: binary.BigEndian.Uint32(payload[9:13]), FromItemID: binary.BigEndian.Uint32(payload[13:17]),
	}
	if result.UIN == 0 || result.UIN != request.EnvelopeUIN {
		return CombineForgeRequest{}, fmt.Errorf("combine-forge UIN %d does not match envelope UIN %d", result.UIN, request.EnvelopeUIN)
	}
	count := int(binary.BigEndian.Uint16(payload[17:19]))
	if count > combineForgeMaterialMaximum {
		return CombineForgeRequest{}, fmt.Errorf("combine-forge material count %d exceeds %d", count, combineForgeMaterialMaximum)
	}
	wantLength := combineForgeRequestFixedSize + 4*count
	if len(payload) != wantLength {
		return CombineForgeRequest{}, fmt.Errorf(
			"combine-forge request payload length %d, want %d for %d materials",
			len(payload), wantLength, count,
		)
	}
	result.MaterialIDs = make([]uint32, count)
	for index := range result.MaterialIDs {
		result.MaterialIDs[index] = binary.BigEndian.Uint32(payload[19+index*4 : 23+index*4])
	}
	return result, nil
}

type CombineForgeResponse struct {
	ResultID    uint16
	Type        byte
	ItemID      uint32
	FromItemID  uint32
	MaterialIDs []uint32
	Reason      string
}

func BuildLocalCombineForgeResponse(packet []byte, response CombineForgeResponse) ([]byte, error) {
	return buildLocalCombineForgeResponse(packet, response, nil)
}

func BuildLocalCombineForgeResponseWithReader(packet []byte, response CombineForgeResponse, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalCombineForgeResponse(packet, response, entropy)
}

func buildLocalCombineForgeResponse(packet []byte, response CombineForgeResponse, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return nil, err
	}
	if _, err = DecodeLocalCombineForgeRequest(packet); err != nil {
		return nil, err
	}
	if len(response.MaterialIDs) > combineForgeMaterialMaximum {
		return nil, fmt.Errorf("combine-forge response material count %d exceeds %d", len(response.MaterialIDs), combineForgeMaterialMaximum)
	}
	reason, err := encodeLegacyGBKText(response.Reason, craftReasonMaximum)
	if err != nil {
		return nil, err
	}
	// QQTMsgData describes the maximum decoded object image (20 materials and
	// a 200-byte reason), not a fixed wire record. Both counted fields are
	// compact on the wire. The client request observed for the three-material
	// medical-kit recipe is therefore 19+3*4 bytes rather than 99 bytes; the
	// response follows the same serializer contract.
	payload := make([]byte, 0, combineForgeResponseFixedSize+4*len(response.MaterialIDs)+len(reason))
	payload = binary.BigEndian.AppendUint16(payload, response.ResultID)
	payload = append(payload, response.Type)
	payload = binary.BigEndian.AppendUint32(payload, response.ItemID)
	payload = binary.BigEndian.AppendUint32(payload, response.FromItemID)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(response.MaterialIDs)))
	for _, materialID := range response.MaterialIDs {
		payload = binary.BigEndian.AppendUint32(payload, materialID)
	}
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(reason)))
	payload = append(payload, reason...)
	return buildLocalResponse(packet, request, CombineForgeCommand, payload, entropy)
}

type ForgeRequest struct {
	UIN        uint32
	ClientTime uint32
	Type       byte
	ItemID     uint32
	MaterialID uint32
}

func DecodeLocalForgeRequest(packet []byte) (ForgeRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return ForgeRequest{}, err
	}
	if request.Command != ForgeCommand {
		return ForgeRequest{}, fmt.Errorf("forge command 0x%04X, want 0x%04X", request.Command, ForgeCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != forgeRequestSize {
		return ForgeRequest{}, fmt.Errorf("forge request payload length %d, want %d", len(payload), forgeRequestSize)
	}
	result := ForgeRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), ClientTime: binary.BigEndian.Uint32(payload[4:8]), Type: payload[8],
		ItemID: binary.BigEndian.Uint32(payload[9:13]), MaterialID: binary.BigEndian.Uint32(payload[13:17]),
	}
	if result.UIN == 0 || result.UIN != request.EnvelopeUIN {
		return ForgeRequest{}, fmt.Errorf("forge UIN %d does not match envelope UIN %d", result.UIN, request.EnvelopeUIN)
	}
	return result, nil
}

type ForgeResponse struct {
	ResultID   uint16
	Type       byte
	ItemID     uint32
	MaterialID uint32
	Effect     byte
	Color      byte
	Reason     string
}

func BuildLocalForgeResponse(packet []byte, response ForgeResponse) ([]byte, error) {
	return buildLocalForgeResponse(packet, response, nil)
}

func BuildLocalForgeResponseWithReader(packet []byte, response ForgeResponse, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalForgeResponse(packet, response, entropy)
}

func buildLocalForgeResponse(packet []byte, response ForgeResponse, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return nil, err
	}
	if _, err = DecodeLocalForgeRequest(packet); err != nil {
		return nil, err
	}
	// QQTSection copies ReasonLen bytes into a 200-byte stack buffer and then
	// passes that buffer to sprintf("...'%s'"). It does not append its own NUL,
	// so the terminator must be included in the counted wire field.
	reason, err := encodeLegacyGBKText(response.Reason, craftReasonMaximum-1)
	if err != nil {
		return nil, err
	}
	reason = append(reason, 0)
	payload := make([]byte, 0, forgeResponseHeaderSize+len(reason))
	payload = binary.BigEndian.AppendUint16(payload, response.ResultID)
	payload = append(payload, response.Type)
	payload = binary.BigEndian.AppendUint32(payload, response.ItemID)
	payload = binary.BigEndian.AppendUint32(payload, response.MaterialID)
	payload = append(payload, response.Effect, response.Color)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(reason)))
	payload = append(payload, reason...)
	return buildLocalResponse(packet, request, ForgeCommand, payload, entropy)
}

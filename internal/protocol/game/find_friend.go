package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	FindFriendRequestSchema      = 0x03F3
	FindFriendResponseSchema     = 0x07DB
	findFriendRequestPayloadSize = 12
	findFriendNameSlotSize       = 20
	findFriendKinNameMaximum     = 17
)

type FindFriendRequest struct {
	UIN       uint32
	Time      uint32
	FriendUIN uint32
}

// FindFriendResponse mirrors RESPONSE_FIND_FRIEND. Items and PatternPoints
// are variable arrays; their counts are derived during serialization.
type FindFriendResponse struct {
	FriendUIN     uint32
	PlayerName    string
	PlayerID      uint16
	Gender        byte
	IconID        byte
	Identity      uint32
	ServerID      uint32
	DialogID      uint16
	SectionID     uint16
	RoomID        uint16
	RoomName      string
	Status        FindFriendStatus
	GameInfo      GameInfo
	Items         []ItemInfo
	KinIndex      uint32
	KinName       string
	KinFlagID     KinFlagID
	Honor         uint32
	PatternPoints []PatternPoint
}

func DecodeLocalFindFriendRequest(packet []byte) (FindFriendRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return FindFriendRequest{}, err
	}
	if request.Command != FindFriendCommand {
		return FindFriendRequest{}, fmt.Errorf("find-friend command 0x%04X, want 0x%04X", request.Command, FindFriendCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != findFriendRequestPayloadSize {
		return FindFriendRequest{}, fmt.Errorf("find-friend payload length %d, want %d", len(payload), findFriendRequestPayloadSize)
	}
	decoded := FindFriendRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]),
		FriendUIN: binary.BigEndian.Uint32(payload[8:12]),
	}
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN {
		return FindFriendRequest{}, fmt.Errorf("find-friend envelope UIN %d != payload UIN %d", request.EnvelopeUIN, decoded.UIN)
	}
	if decoded.FriendUIN == 0 {
		return FindFriendRequest{}, fmt.Errorf("find-friend target UIN must be non-zero")
	}
	return decoded, nil
}

func appendFixedLegacyGBKSlot(dst []byte, text string, size int) ([]byte, error) {
	encoded, err := encodeLegacyGBKText(text, size)
	if err != nil {
		return nil, err
	}
	start := len(dst)
	dst = append(dst, make([]byte, size)...)
	copy(dst[start:], encoded)
	return dst, nil
}

func (response FindFriendResponse) MarshalNetworkBinary() ([]byte, error) {
	if response.FriendUIN == 0 || response.PlayerID == 0 || response.SectionID == 0 {
		return nil, fmt.Errorf("RESPONSE_FIND_FRIEND requires non-zero friend UIN, player ID, and section ID")
	}
	if response.PlayerName == "" {
		return nil, fmt.Errorf("RESPONSE_FIND_FRIEND player name must not be empty")
	}
	if response.Gender > 1 {
		return nil, fmt.Errorf("RESPONSE_FIND_FRIEND gender %d is outside 0..1", response.Gender)
	}
	if len(response.Items) > MaxItemInfoCount {
		return nil, fmt.Errorf("RESPONSE_FIND_FRIEND item count %d exceeds %d", len(response.Items), MaxItemInfoCount)
	}
	if err := response.GameInfo.ValidateLocalClientBounds(); err != nil {
		return nil, fmt.Errorf("RESPONSE_FIND_FRIEND GAME_INFO: %w", err)
	}
	kinName, err := encodeLegacyGBKText(response.KinName, findFriendKinNameMaximum)
	if err != nil {
		return nil, fmt.Errorf("RESPONSE_FIND_FRIEND kin name: %w", err)
	}

	payload := binary.BigEndian.AppendUint16(nil, 0) // ResultID
	payload = binary.BigEndian.AppendUint32(payload, response.FriendUIN)
	payload, err = appendFixedLegacyGBKSlot(payload, response.PlayerName, findFriendNameSlotSize)
	if err != nil {
		return nil, fmt.Errorf("RESPONSE_FIND_FRIEND player name: %w", err)
	}
	payload = binary.BigEndian.AppendUint16(payload, response.PlayerID)
	payload = append(payload, response.Gender, response.IconID)
	payload = binary.BigEndian.AppendUint32(payload, response.Identity)
	payload = binary.BigEndian.AppendUint32(payload, response.ServerID)
	payload = binary.BigEndian.AppendUint16(payload, response.DialogID)
	payload = binary.BigEndian.AppendUint16(payload, response.SectionID)
	payload = binary.BigEndian.AppendUint16(payload, response.RoomID)
	payload, err = appendFixedLegacyGBKSlot(payload, response.RoomName, findFriendNameSlotSize)
	if err != nil {
		return nil, fmt.Errorf("RESPONSE_FIND_FRIEND room name: %w", err)
	}
	payload = append(payload, byte(response.Status))
	payload = response.GameInfo.AppendNetworkBinary(payload)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(response.Items)))
	for index, item := range response.Items {
		if item.ItemID == 0 || !item.Active() {
			return nil, fmt.Errorf("RESPONSE_FIND_FRIEND item %d is inactive or has zero ID", index)
		}
		payload = item.AppendNetworkBinary(payload)
	}
	payload = binary.BigEndian.AppendUint32(payload, response.KinIndex)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(kinName)))
	payload = append(payload, kinName...)
	payload = append(payload, response.KinFlagID[:]...)
	payload = binary.BigEndian.AppendUint32(payload, response.Honor)
	payload, err = appendPatternPoints(payload, response.PatternPoints)
	if err != nil {
		return nil, fmt.Errorf("RESPONSE_FIND_FRIEND pattern points: %w", err)
	}
	return payload, nil
}

func BuildLocalFindFriendResponse(requestPacket []byte, response FindFriendResponse) ([]byte, error) {
	return buildLocalFindFriendResponse(requestPacket, response, nil)
}

func BuildLocalFindFriendResponseWithReader(requestPacket []byte, response FindFriendResponse, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalFindFriendResponse(requestPacket, response, entropy)
}

func buildLocalFindFriendResponse(requestPacket []byte, response FindFriendResponse, entropy io.Reader) ([]byte, error) {
	requestData, err := DecodeLocalFindFriendRequest(requestPacket)
	if err != nil {
		return nil, err
	}
	if response.FriendUIN != requestData.FriendUIN {
		return nil, fmt.Errorf("find-friend response UIN %d != requested UIN %d", response.FriendUIN, requestData.FriendUIN)
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload, err := response.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, FindFriendCommand, payload, entropy)
}

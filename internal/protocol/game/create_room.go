package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	CreateRoomRequestSchema  = 0x03F2
	CreateRoomResponseSchema = 0x07DA
	createRoomRequestSize    = 50
	createRoomPayloadSize    = 4
)

type CreateRoomRequest struct {
	UIN        uint32
	Time       uint32
	RoomName   [20]byte
	Flag       RoomPropertyFlag
	Password   [16]byte
	GameType   byte
	ContinueID uint32
	Plaintext  []byte
}

func (request CreateRoomRequest) RoomNameString() (string, error) {
	return decodeLegacyGBKSlot(request.RoomName[:])
}

func (request CreateRoomRequest) HasPassword() bool {
	return request.Flag.HasPassword()
}

func (request CreateRoomRequest) UsesFreeRule() bool {
	return request.Flag.UsesFreeRule()
}

// DecodeLocalCreateRoomRequest mirrors the complete fixed REQUEST_CREATE_ROOM
// schema recovered from QQTMsgData.bin.
func DecodeLocalCreateRoomRequest(packet []byte) (CreateRoomRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return CreateRoomRequest{}, err
	}
	if request.Command != CreateRoomCommand {
		return CreateRoomRequest{}, fmt.Errorf("create-room command 0x%04X, want 0x%04X", request.Command, CreateRoomCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != createRoomRequestSize {
		return CreateRoomRequest{}, fmt.Errorf("create-room payload length %d, want %d", len(payload), createRoomRequestSize)
	}
	uin := binary.BigEndian.Uint32(payload[0:4])
	if uin != request.EnvelopeUIN {
		return CreateRoomRequest{}, fmt.Errorf("create-room envelope UIN %d != payload UIN %d", request.EnvelopeUIN, uin)
	}
	decoded := CreateRoomRequest{
		UIN: uin, Time: binary.BigEndian.Uint32(payload[4:8]), Flag: RoomPropertyFlag(payload[28]),
		GameType: payload[45], ContinueID: binary.BigEndian.Uint32(payload[46:50]),
		Plaintext: append([]byte(nil), request.Plaintext...),
	}
	copy(decoded.RoomName[:], payload[8:28])
	copy(decoded.Password[:], payload[29:45])
	if !decoded.Flag.Valid() {
		return CreateRoomRequest{}, fmt.Errorf("create-room property flag 0x%02X contains unsupported bits", decoded.Flag)
	}
	passwordPresent := false
	for _, value := range decoded.Password {
		if value != 0 {
			passwordPresent = true
			break
		}
	}
	if decoded.HasPassword() != passwordPresent {
		return CreateRoomRequest{}, fmt.Errorf("create-room password flag and password slot disagree")
	}
	return decoded, nil
}

// BuildCreateRoomSuccessPayload matches the client's native schema-0x07DA
// encoder: ResultID(uint16)=0 followed by RoomID(uint16).
func BuildCreateRoomSuccessPayload(roomID uint16) ([]byte, error) {
	if roomID == 0 {
		return nil, fmt.Errorf("created room ID must be non-zero")
	}
	payload := make([]byte, createRoomPayloadSize)
	binary.BigEndian.PutUint16(payload[2:4], roomID)
	return payload, nil
}

func BuildLocalCreateRoomSuccess(requestPacket []byte, roomID uint16) ([]byte, error) {
	return buildLocalCreateRoomSuccess(requestPacket, roomID, nil)
}

func BuildLocalCreateRoomSuccessWithReader(requestPacket []byte, roomID uint16, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalCreateRoomSuccess(requestPacket, roomID, entropy)
}

func buildLocalCreateRoomSuccess(requestPacket []byte, roomID uint16, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalCreateRoomRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload, err := BuildCreateRoomSuccessPayload(roomID)
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, CreateRoomCommand, payload, entropy)
}

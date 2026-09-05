package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

const (
	modifyRoomRequestSize      = 54
	modifyRoomResponseSize     = 2
	modifyRoomNotificationSize = 41
)

type ModifyRoomRequest struct {
	UIN        uint32
	ClientTime uint32
	Flags      ModifyRoomFlags
	RoomName   [20]byte
	RoomFlag   byte
	Password   [16]byte
	GameType   byte
	ContinueID uint32
}

func (request ModifyRoomRequest) RoomNameString() (string, error) {
	return decodeLegacyGBKSlot(request.RoomName[:])
}

func (request ModifyRoomRequest) HasPassword() bool {
	return request.PropertyFlag().HasPassword()
}

func (request ModifyRoomRequest) UsesFreeRule() bool {
	return request.PropertyFlag().UsesFreeRule()
}

func (request ModifyRoomRequest) NameChanged() bool {
	return request.Flags&ModifyRoomNameChanged != 0
}

// PropertyFlag returns uiRoom.py's explicit room property value: bit 0 is
// password enabled and bit 1 is the free-team rule.
func (request ModifyRoomRequest) PropertyFlag() RoomPropertyFlag {
	return RoomPropertyFlag(request.RoomFlag)
}

func DecodeLocalModifyRoomRequest(packet []byte) (ModifyRoomRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return ModifyRoomRequest{}, err
	}
	if request.Command != ModifyRoomCommand {
		return ModifyRoomRequest{}, fmt.Errorf("modify-room command 0x%04X, want 0x%04X", request.Command, ModifyRoomCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != modifyRoomRequestSize {
		return ModifyRoomRequest{}, fmt.Errorf("modify-room payload length %d, want %d", len(payload), modifyRoomRequestSize)
	}
	decoded := ModifyRoomRequest{
		UIN:        binary.BigEndian.Uint32(payload[0:4]),
		ClientTime: binary.BigEndian.Uint32(payload[4:8]),
		Flags:      ModifyRoomFlags(binary.BigEndian.Uint32(payload[8:12])),
		RoomFlag:   payload[32],
		GameType:   payload[49],
		ContinueID: binary.BigEndian.Uint32(payload[50:54]),
	}
	copy(decoded.RoomName[:], payload[12:32])
	copy(decoded.Password[:], payload[33:49])
	if decoded.UIN != request.EnvelopeUIN {
		return ModifyRoomRequest{}, fmt.Errorf("modify-room UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	if unknown := decoded.Flags &^ modifyRoomKnownFlags; unknown != 0 {
		return ModifyRoomRequest{}, fmt.Errorf("modify-room flags contain unknown bits 0x%08X", uint32(unknown))
	}
	if !decoded.PropertyFlag().Valid() {
		return ModifyRoomRequest{}, fmt.Errorf("modify-room property flag 0x%02X contains unsupported bits", decoded.RoomFlag)
	}
	passwordPresent := false
	for _, value := range decoded.Password {
		if value != 0 {
			passwordPresent = true
			break
		}
	}
	if decoded.HasPassword() != passwordPresent {
		return ModifyRoomRequest{}, fmt.Errorf("modify-room password flag and password slot disagree")
	}
	return decoded, nil
}

func BuildLocalModifyRoomSuccess(requestPacket []byte) ([]byte, error) {
	return buildLocalModifyRoomSuccess(requestPacket, nil)
}

func BuildLocalModifyRoomSuccessWithReader(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalModifyRoomSuccess(requestPacket, entropy)
}

func buildLocalModifyRoomSuccess(requestPacket []byte, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalModifyRoomRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, modifyRoomResponseSize) // Result(uint16)=0.
	return buildLocalResponse(requestPacket, request, ModifyRoomCommand, payload, entropy)
}

func BuildLocalModifyRoomNotificationForRecipient(recipientPacket []byte, roomID uint16, flags ModifyRoomFlags, roomName [20]byte, roomFlag byte, password [16]byte) ([]byte, error) {
	if roomID == 0 {
		return nil, fmt.Errorf("NOTIFY_MODIFY_ROOM room ID must be non-zero")
	}
	recipient, err := decodeLocalPacket(recipientPacket)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, 0, modifyRoomNotificationSize)
	payload = binary.BigEndian.AppendUint32(payload, uint32(flags))
	payload = append(payload, roomName[:]...)
	payload = append(payload, roomFlag)
	payload = append(payload, password[:]...)
	return buildLocalNotificationFromRequestWithRoute(
		recipientPacket, recipient, ModifyRoomNotifyCommand, payload,
		localMessageRoute{Route: 3, SectionID: roomID}, nil,
	)
}

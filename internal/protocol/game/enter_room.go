package game

import (
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	EnterRoomRequestSchema     = 0x03F7
	EnterRoomResponseOldSchema = 0x07DF
	EnterRoomNotifyOldSchema   = 0x03F8
	EnterRoomAckSchema         = 0x07E0
	enterRoomRoute             = 3
	enterRoomRequestSize       = 27
	enterRoomPasswordSize      = 16
	enterRoomMaximumPlayers    = 8
	playerInRoomAttachSize     = 12
	petBaseInfoSize            = 58
	enterRoomFailureSize       = 26
)

// EnterRoomSeatStatus is the RESPONSE_ENTER_ROOM_OLD snapshot encoding.
// It is deliberately separate from RoomSeatStatus: the old snapshot schema
// uses 0 for a locked empty seat and 1 for an open empty seat, while the
// SET_SEAT_STATUS command family uses 1 for open and 2 for locked.
type EnterRoomSeatStatus byte

const (
	EnterRoomSeatLocked   EnterRoomSeatStatus = 0
	EnterRoomSeatOpen     EnterRoomSeatStatus = 1
	EnterRoomSeatOccupied EnterRoomSeatStatus = 2
)

type EnterRoomRequest struct {
	UIN        uint32
	ClientTime uint32
	RoomID     uint16
	RoleID     byte
	Password   [enterRoomPasswordSize]byte
}

func (request EnterRoomRequest) PasswordMatches(expected [enterRoomPasswordSize]byte) bool {
	return subtle.ConstantTimeCompare(request.Password[:], expected[:]) == 1
}

func DecodeLocalEnterRoomRequest(packet []byte) (EnterRoomRequest, error) {
	request, err := decodeLocalPacket(packet)
	if err != nil {
		return EnterRoomRequest{}, err
	}
	if request.Command != EnterRoomCommand {
		return EnterRoomRequest{}, fmt.Errorf("enter-room command 0x%04X, want 0x%04X", request.Command, EnterRoomCommand)
	}
	payload := request.Plaintext[localInnerHeaderSize:]
	if len(payload) != enterRoomRequestSize {
		return EnterRoomRequest{}, fmt.Errorf("enter-room payload length %d, want %d", len(payload), enterRoomRequestSize)
	}
	decoded := EnterRoomRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), ClientTime: binary.BigEndian.Uint32(payload[4:8]),
		RoomID: binary.BigEndian.Uint16(payload[8:10]), RoleID: payload[10],
	}
	copy(decoded.Password[:], payload[11:27])
	if decoded.UIN == 0 || decoded.UIN != request.EnvelopeUIN {
		return EnterRoomRequest{}, fmt.Errorf("enter-room UIN %d does not match envelope UIN %d", decoded.UIN, request.EnvelopeUIN)
	}
	if decoded.RoomID == 0 {
		return EnterRoomRequest{}, fmt.Errorf("enter-room room ID must be non-zero")
	}
	if decoded.RoleID == 0 {
		return EnterRoomRequest{}, fmt.Errorf("enter-room role ID must be non-zero")
	}
	return decoded, nil
}

// BuildLocalEnterRoomFailure emits the complete zero-count shape required by
// RESPONSE_ENTER_ROOM_OLD. QQTSection checks ResultID before consuming room
// members, but its schema decoder still expects the remaining scalar fields.
func BuildLocalEnterRoomFailure(requestPacket []byte, resultID EnterRoomResultID, roomID uint16) ([]byte, error) {
	if resultID == EnterRoomResultSuccess {
		return nil, fmt.Errorf("enter-room failure result must be non-zero")
	}
	request, err := DecodeLocalEnterRoomRequest(requestPacket)
	if err != nil {
		return nil, err
	}
	if roomID == 0 {
		roomID = request.RoomID
	}
	decoded, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload := make([]byte, enterRoomFailureSize)
	binary.BigEndian.PutUint16(payload[0:2], uint16(resultID))
	binary.BigEndian.PutUint16(payload[2:4], roomID)
	return buildLocalResponse(requestPacket, decoded, EnterRoomCommand, payload, nil)
}

type PlayerInfoInRoomOld struct {
	UIN         uint32
	Nickname    string
	PlayerID    uint16
	TeamID      byte
	SeatID      byte
	Status      byte
	Gender      byte
	IconID      byte
	Identity    uint32
	GameInfo    GameInfo
	Items       []ItemInfo
	KinIndex    uint32
	KinName     string
	KinFlagID   KinFlagID
	PetBaseInfo [petBaseInfoSize]byte
}

type PlayerInfoInRoomAttach struct {
	Honor   uint32
	Attach1 uint32
	Attach2 uint32
}

type EnterRoomPlayer struct {
	Player    PlayerInfoInRoomOld
	Attach    PlayerInfoInRoomAttach
	SpouseUIN uint32
	Patterns  []PatternPoint
}

type EnterRoomResponseOld struct {
	RoomID        uint16
	LocalTeamID   byte
	LocalSeatID   byte
	MapID         uint16
	RoomFlag      byte
	RoomOwnerID   uint16
	SeatStatus    [enterRoomMaximumPlayers]EnterRoomSeatStatus
	Players       []EnterRoomPlayer
	GameType      byte
	BackgroundID  uint32
	WeddingModeID uint16
}

// EnterRoomNotificationOld is the four-field NOTIFY_ENTER_ROOM_OLD schema.
// Unlike the response, it carries only the joining player and lets every
// existing room client update one seat through ui_NotifyRoomSeatChange.
type EnterRoomNotificationOld struct {
	Player    PlayerInfoInRoomOld
	RoomID    uint16
	Attach    PlayerInfoInRoomAttach
	SpouseUIN uint32
}

func PlayerInfoInRoomOldFromProfile(uin uint32, profile PlayerProfile, teamID, seatID, status byte) PlayerInfoInRoomOld {
	client := profile.ToClientLoginConfig()
	return PlayerInfoInRoomOld{
		UIN: uin, Nickname: profile.Nickname, PlayerID: profile.PlayerID,
		TeamID: teamID, SeatID: seatID, Status: status,
		Gender: profile.Gender, IconID: profile.IconID, Identity: profile.Identity,
		GameInfo: client.GameInfo,
		Items:    client.Items,
		KinIndex: profile.KinIndex, KinName: profile.KinName,
		KinFlagID: KinFlagIDForWire(profile.KinIndex, profile.KinFlagID),
	}
}

func packTermAndSeat(teamID, seatID byte) (byte, error) {
	if teamID < MinRoomTeamID || teamID > MaxRoomTeamID {
		return 0, fmt.Errorf("room team ID %d is outside %d..%d", teamID, MinRoomTeamID, MaxRoomTeamID)
	}
	if seatID < MinRoomSeatID || seatID > MaxRoomSeatID {
		return 0, fmt.Errorf("room seat ID %d is outside %d..%d", seatID, MinRoomSeatID, MaxRoomSeatID)
	}
	return teamID<<4 | seatID, nil
}

func (player PlayerInfoInRoomOld) MarshalNetworkBinary() ([]byte, error) {
	if player.UIN == 0 || player.PlayerID == 0 {
		return nil, fmt.Errorf("PLAYER_INFO_IN_ROOM_OLD requires non-zero UIN and PlayerID")
	}
	nickname, err := encodeLegacyGBKText(player.Nickname, PlayerNicknameSlotSize)
	if err != nil || len(nickname) == 0 {
		return nil, fmt.Errorf("PLAYER_INFO_IN_ROOM_OLD nickname: %w", err)
	}
	kinName, err := encodeLegacyGBKText(player.KinName, playerKinNameMaximum)
	if err != nil {
		return nil, fmt.Errorf("PLAYER_INFO_IN_ROOM_OLD kin name: %w", err)
	}
	if len(player.Items) > MaxItemInfoCount {
		return nil, fmt.Errorf("PLAYER_INFO_IN_ROOM_OLD item count %d exceeds %d", len(player.Items), MaxItemInfoCount)
	}
	if err := player.GameInfo.ValidateLocalClientBounds(); err != nil {
		return nil, fmt.Errorf("PLAYER_INFO_IN_ROOM_OLD GAME_INFO: %w", err)
	}
	termAndSeat, err := packTermAndSeat(player.TeamID, player.SeatID)
	if err != nil {
		return nil, err
	}
	petSkillCount := binary.BigEndian.Uint32(player.PetBaseInfo[34:38])
	if petSkillCount > PetSkillsSlotSize {
		return nil, fmt.Errorf("PLAYER_INFO_IN_ROOM_OLD pet skill count %d exceeds %d", petSkillCount, PetSkillsSlotSize)
	}
	encoded := make([]byte, 0, 135+len(player.Items)*ItemInfoBinarySize+len(kinName)+int(petSkillCount))
	encoded = binary.BigEndian.AppendUint32(encoded, player.UIN)
	nicknameSlot := make([]byte, PlayerNicknameSlotSize)
	copy(nicknameSlot, nickname)
	encoded = append(encoded, nicknameSlot...)
	encoded = binary.BigEndian.AppendUint16(encoded, player.PlayerID)
	encoded = append(encoded, termAndSeat, player.Status, player.Gender, player.IconID)
	encoded = binary.BigEndian.AppendUint32(encoded, player.Identity)
	encoded = player.GameInfo.AppendNetworkBinary(encoded)
	encoded = binary.BigEndian.AppendUint16(encoded, uint16(len(player.Items)))
	for _, item := range player.Items {
		encoded = item.AppendNetworkBinary(encoded)
	}
	encoded = binary.BigEndian.AppendUint32(encoded, player.KinIndex)
	encoded = binary.BigEndian.AppendUint16(encoded, uint16(len(kinName)))
	encoded = append(encoded, kinName...)
	encoded = append(encoded, player.KinFlagID[:]...)
	// PET_BASE_INFO is a maximum-sized decoded structure. Its Skills member is
	// a counted array on the wire, so bytes after SkillCount are emitted only
	// for the skills that are actually present.
	petWireSize := PetInfoWireMinimumSize + int(petSkillCount)
	encoded = append(encoded, player.PetBaseInfo[:petWireSize]...)
	return encoded, nil
}

func (attach PlayerInfoInRoomAttach) AppendNetworkBinary(dst []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, attach.Honor)
	dst = binary.BigEndian.AppendUint32(dst, attach.Attach1)
	return binary.BigEndian.AppendUint32(dst, attach.Attach2)
}

func (response EnterRoomResponseOld) MarshalNetworkBinary() ([]byte, error) {
	if response.RoomID == 0 || response.RoomOwnerID == 0 {
		return nil, fmt.Errorf("RESPONSE_ENTER_ROOM_OLD requires room and owner IDs")
	}
	if len(response.Players) == 0 || len(response.Players) > enterRoomMaximumPlayers {
		return nil, fmt.Errorf("RESPONSE_ENTER_ROOM_OLD player count %d is outside 1..%d", len(response.Players), enterRoomMaximumPlayers)
	}
	localTermAndSeat, err := packTermAndSeat(response.LocalTeamID, response.LocalSeatID)
	if err != nil {
		return nil, fmt.Errorf("RESPONSE_ENTER_ROOM_OLD local member: %w", err)
	}
	payload := make([]byte, 0, 19+len(response.Players)*(135+playerInRoomAttachSize+4+1)+7)
	payload = binary.BigEndian.AppendUint16(payload, 0) // ResultID
	payload = binary.BigEndian.AppendUint16(payload, response.RoomID)
	payload = append(payload, localTermAndSeat)
	payload = binary.BigEndian.AppendUint16(payload, response.MapID)
	payload = append(payload, response.RoomFlag)
	payload = binary.BigEndian.AppendUint16(payload, response.RoomOwnerID)
	payload = append(payload, byte(len(response.Players)))
	for _, status := range response.SeatStatus {
		if status != EnterRoomSeatLocked && status != EnterRoomSeatOpen && status != EnterRoomSeatOccupied {
			return nil, fmt.Errorf("RESPONSE_ENTER_ROOM_OLD contains unsupported seat status %d", status)
		}
		payload = append(payload, byte(status))
	}
	// QQTMsgData's 9172-byte PLAYER_INFO_IN_ROOM_OLD size is the maximum
	// decoded object stride. The encoder writes counted members compactly on
	// the network, and each following player starts immediately after the
	// preceding player's actual bytes.
	for index := range response.Players {
		entry := response.Players[index]
		encoded, marshalErr := entry.Player.MarshalNetworkBinary()
		if marshalErr != nil {
			return nil, marshalErr
		}
		payload = append(payload, encoded...)
	}
	for _, entry := range response.Players {
		payload = entry.Attach.AppendNetworkBinary(payload)
	}
	payload = append(payload, response.GameType)
	payload = binary.BigEndian.AppendUint32(payload, response.BackgroundID)
	for _, entry := range response.Players {
		payload = binary.BigEndian.AppendUint32(payload, entry.SpouseUIN)
	}
	payload = binary.BigEndian.AppendUint16(payload, response.WeddingModeID)
	for index := range response.Players {
		payload, err = appendPatternPoints(payload, response.Players[index].Patterns)
		if err != nil {
			return nil, err
		}
	}
	return payload, nil
}

// weddingModeOffset returns WeddingModeID's position in a compact
// RESPONSE_ENTER_ROOM_OLD payload. RESPONSE_JOIN_ROOM_OLD shares the same
// fields but omits WeddingModeID, so it cannot use a maximum decoded stride.
func (response EnterRoomResponseOld) weddingModeOffset() (int, error) {
	offset := 19
	for index := range response.Players {
		encoded, err := response.Players[index].Player.MarshalNetworkBinary()
		if err != nil {
			return 0, err
		}
		offset += len(encoded)
	}
	offset += len(response.Players)*playerInRoomAttachSize + 1 + 4 + len(response.Players)*4
	return offset, nil
}

func (notification EnterRoomNotificationOld) MarshalNetworkBinary() ([]byte, error) {
	if notification.RoomID == 0 {
		return nil, fmt.Errorf("NOTIFY_ENTER_ROOM_OLD room ID must be non-zero")
	}
	payload, err := notification.Player.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	payload = binary.BigEndian.AppendUint16(payload, notification.RoomID)
	payload = notification.Attach.AppendNetworkBinary(payload)
	payload = binary.BigEndian.AppendUint32(payload, notification.SpouseUIN)
	return payload, nil
}

// BuildLocalEnterRoomNotification uses an arbitrary recent packet from the
// recipient connection as its authenticated local-session envelope. The
// joining player's UIN lives in the schema payload and need not match the
// recipient envelope UIN.
func BuildLocalEnterRoomNotification(recipientPacket []byte, notification EnterRoomNotificationOld) ([]byte, error) {
	return buildLocalEnterRoomNotification(recipientPacket, notification, nil)
}

func BuildLocalEnterRoomNotificationWithReader(recipientPacket []byte, notification EnterRoomNotificationOld, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalEnterRoomNotification(recipientPacket, notification, entropy)
}

func buildLocalEnterRoomNotification(recipientPacket []byte, notification EnterRoomNotificationOld, entropy io.Reader) ([]byte, error) {
	recipient, err := decodeLocalPacket(recipientPacket)
	if err != nil {
		return nil, err
	}
	payload, err := notification.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(
		recipientPacket,
		recipient,
		EnterRoomNotifyCommand,
		payload,
		localMessageRoute{Route: enterRoomRoute, SectionID: notification.RoomID},
		entropy,
	)
}

func BuildLocalEnterRoomSuccess(requestPacket []byte, response EnterRoomResponseOld) ([]byte, error) {
	return buildLocalEnterRoomSuccess(requestPacket, response, nil)
}

func BuildLocalEnterRoomSuccessWithReader(requestPacket []byte, response EnterRoomResponseOld, entropy io.Reader) ([]byte, error) {
	if entropy == nil {
		return nil, fmt.Errorf("entropy reader is nil")
	}
	return buildLocalEnterRoomSuccess(requestPacket, response, entropy)
}

func buildLocalEnterRoomSuccess(requestPacket []byte, response EnterRoomResponseOld, entropy io.Reader) ([]byte, error) {
	if _, err := DecodeLocalEnterRoomRequest(requestPacket); err != nil {
		return nil, err
	}
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	payload, err := response.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(requestPacket, request, EnterRoomCommand, payload, entropy)
}

package game

import (
	"encoding/binary"
	"fmt"
)

const (
	FriendListRequestSchema    uint32 = 0x10FE
	FriendListResponseSchema   uint32 = 0x10FF
	AddFriendRequestSchema     uint32 = 0x1100
	AddFriendResponseSchema    uint32 = 0x1101
	RemoveFriendRequestSchema  uint32 = 0x1102
	RemoveFriendResponseSchema uint32 = 0x1103
	FriendUpdateSchema         uint32 = 0x1104
	AnswerFriendSchema         uint32 = 0x1105

	FriendMaximumCount        = 200
	FriendExternalItemMaximum = 30
	FriendNicknameSlotSize    = 20
	FriendRequestWordMaximum  = 512
	FriendUpdatePayloadSize   = 163
	FriendListPayloadSize     = 810
	AddFriendHeaderSize       = 14
	AddFriendPayloadMaximum   = AddFriendHeaderSize + FriendRequestWordMaximum

	FriendResultSuccess  uint16 = 0
	FriendResultRejected uint16 = 1
)

type FriendListRequest struct {
	UIN  uint32
	Time uint32
}

type AddFriendRequest struct {
	UIN       uint32
	Time      uint32
	TargetUIN uint32
	Word      string
}

type RemoveFriendRequest struct {
	UIN       uint32
	Time      uint32
	TargetUIN uint32
}

type AnswerFriendRequest struct {
	ResultID     uint16
	RequesterUIN uint32
	Time         uint32
	ResponderUIN uint32
}

// FriendUpdate is the exact 163-byte FRIEND_INFO/NOTIFY_UPDATE_FRIEND body.
// UIN is the list owner; FriendUIN identifies the entry being projected.
type FriendUpdate struct {
	UIN        uint32
	FriendUIN  uint32
	Point      uint32
	ExtItemIDs []uint32
	Online     bool
	Nickname   string
	Gender     byte
	Identity   uint32
	ExtPoint   uint32
}

func DecodeLocalFriendListRequest(packet []byte) (FriendListRequest, error) {
	decoded, payload, err := decodeFriendPacket(packet, FriendListCommand, 8, 8)
	if err != nil {
		return FriendListRequest{}, err
	}
	request := FriendListRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8])}
	if err := validateFriendEnvelope(decoded, request.UIN); err != nil {
		return FriendListRequest{}, err
	}
	return request, nil
}

func DecodeLocalAddFriendRequest(packet []byte) (AddFriendRequest, error) {
	decoded, payload, err := decodeFriendPacket(packet, AddFriendCommand, AddFriendHeaderSize, AddFriendPayloadMaximum)
	if err != nil {
		return AddFriendRequest{}, err
	}
	length := int(binary.BigEndian.Uint16(payload[12:14]))
	if length > FriendRequestWordMaximum {
		return AddFriendRequest{}, fmt.Errorf("add-friend word length %d exceeds %d", length, FriendRequestWordMaximum)
	}
	if length > len(payload)-AddFriendHeaderSize {
		return AddFriendRequest{}, fmt.Errorf("add-friend word length %d exceeds available payload %d", length, len(payload)-AddFriendHeaderSize)
	}
	word, err := decodeLegacyGBKSlot(payload[14 : 14+length])
	if err != nil {
		return AddFriendRequest{}, fmt.Errorf("decode add-friend word: %w", err)
	}
	// The original client sends only Header+Word bytes. Older fixtures and
	// compatible clients may instead pad the schema's 512-byte maximum word
	// slot with zeroes. Accept both representations without allowing a second
	// undeclared message in the trailing bytes.
	for _, trailing := range payload[AddFriendHeaderSize+length:] {
		if trailing != 0 {
			return AddFriendRequest{}, fmt.Errorf("add-friend request has non-zero bytes after declared word length %d", length)
		}
	}
	request := AddFriendRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]),
		TargetUIN: binary.BigEndian.Uint32(payload[8:12]), Word: word,
	}
	if err := validateFriendEnvelope(decoded, request.UIN); err != nil {
		return AddFriendRequest{}, err
	}
	if request.TargetUIN == 0 || request.TargetUIN == request.UIN {
		return AddFriendRequest{}, fmt.Errorf("add-friend target UIN %d is invalid", request.TargetUIN)
	}
	return request, nil
}

func DecodeLocalRemoveFriendRequest(packet []byte) (RemoveFriendRequest, error) {
	decoded, payload, err := decodeFriendPacket(packet, RemoveFriendCommand, 12, 12)
	if err != nil {
		return RemoveFriendRequest{}, err
	}
	request := RemoveFriendRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]),
		TargetUIN: binary.BigEndian.Uint32(payload[8:12]),
	}
	if err := validateFriendEnvelope(decoded, request.UIN); err != nil {
		return RemoveFriendRequest{}, err
	}
	if request.TargetUIN == 0 || request.TargetUIN == request.UIN {
		return RemoveFriendRequest{}, fmt.Errorf("remove-friend target UIN %d is invalid", request.TargetUIN)
	}
	return request, nil
}

func DecodeLocalAnswerFriendRequest(packet []byte) (AnswerFriendRequest, error) {
	decoded, payload, err := decodeFriendPacket(packet, AnswerFriendCommand, 14, 14)
	if err != nil {
		return AnswerFriendRequest{}, err
	}
	request := AnswerFriendRequest{
		ResultID: binary.BigEndian.Uint16(payload[0:2]), RequesterUIN: binary.BigEndian.Uint32(payload[2:6]),
		Time: binary.BigEndian.Uint32(payload[6:10]), ResponderUIN: binary.BigEndian.Uint32(payload[10:14]),
	}
	// Unlike every other friend request, RESPONSE_ANSWER_FRIEND keeps the
	// original requester in its first UIN field. The authenticated sender is
	// TargetUin/ResponderUIN. Validating the envelope against RequesterUIN
	// rejected every real answer from the stock client.
	if err := validateFriendEnvelope(decoded, request.ResponderUIN); err != nil {
		return AnswerFriendRequest{}, err
	}
	if request.RequesterUIN == 0 || request.ResponderUIN == 0 || request.ResponderUIN == request.RequesterUIN || request.ResultID > FriendResultRejected {
		return AnswerFriendRequest{}, fmt.Errorf("answer-friend fields are invalid")
	}
	return request, nil
}

func BuildLocalFriendListResponse(packet []byte, resultID uint16, request FriendListRequest, friends []uint32) ([]byte, error) {
	decoded, _, err := decodeFriendPacket(packet, FriendListCommand, 8, 8)
	if err != nil {
		return nil, err
	}
	payload, err := marshalFriendListPayload(resultID, request.UIN, friends)
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(packet, decoded, FriendListCommand, payload, nil)
}

// BuildLocalFriendListNotification restores the durable UIN roster before
// NOTIFY_UPDATE_FRIEND details are replayed at login.  The stock client only
// applies a 0x00A5 update to an entry already installed by RESPONSE_FRIENDS;
// sending details alone is therefore insufficient on a fresh process login.
func BuildLocalFriendListNotification(template []byte, ownerUIN uint32, friends []uint32) ([]byte, error) {
	decoded, err := decodeLocalPacket(template)
	if err != nil {
		return nil, err
	}
	payload, err := marshalFriendListPayload(FriendResultSuccess, ownerUIN, friends)
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(template, decoded, FriendListCommand, payload,
		localMessageRoute{Route: localPlayerProfileRoute, SectionID: localPlayerProfileSectionID}, nil)
}

func marshalFriendListPayload(resultID uint16, ownerUIN uint32, friends []uint32) ([]byte, error) {
	if resultID > FriendResultRejected {
		return nil, fmt.Errorf("friend list result %d is invalid", resultID)
	}
	if ownerUIN == 0 {
		return nil, fmt.Errorf("friend list owner UIN must be non-zero")
	}
	if len(friends) > FriendMaximumCount {
		return nil, fmt.Errorf("friend list count %d exceeds %d", len(friends), FriendMaximumCount)
	}
	payload := binary.BigEndian.AppendUint16(nil, resultID)
	payload = binary.BigEndian.AppendUint32(payload, ownerUIN)
	payload = binary.BigEndian.AppendUint16(payload, FriendMaximumCount)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(friends)))
	for index := 0; index < FriendMaximumCount; index++ {
		friendUIN := uint32(0)
		if index < len(friends) {
			friendUIN = friends[index]
			if friendUIN == 0 || friendUIN == ownerUIN {
				return nil, fmt.Errorf("friend list contains invalid UIN %d", friendUIN)
			}
		}
		payload = binary.BigEndian.AppendUint32(payload, friendUIN)
	}
	if len(payload) != FriendListPayloadSize {
		return nil, fmt.Errorf("friend list payload length %d, want %d", len(payload), FriendListPayloadSize)
	}
	return payload, nil
}

// BuildLocalAddFriendRequestNotification forwards the original request object
// to its target as an unsolicited 0x00A3. QQTSection selects REQUEST_ADD_FRIEND
// (0x1100) for an uncorrelated 0x00A3 and raises the chat-bar envelope from
// that object. RESPONSE_ADD_FRIEND (0x1101/FriendInfo) is only selected for a
// response correlated to the client's own request and cannot substitute for
// an incoming application.
func BuildLocalAddFriendRequestNotification(template []byte, request AddFriendRequest) ([]byte, error) {
	decoded, err := decodeLocalPacket(template)
	if err != nil {
		return nil, err
	}
	word, err := encodeLegacyGBKText(request.Word, FriendRequestWordMaximum)
	if err != nil {
		return nil, err
	}
	if request.UIN == 0 || request.TargetUIN == 0 || request.UIN == request.TargetUIN {
		return nil, fmt.Errorf("add-friend notification fields are invalid")
	}
	payload := binary.BigEndian.AppendUint32(nil, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.Time)
	payload = binary.BigEndian.AppendUint32(payload, request.TargetUIN)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(word)))
	payload = append(payload, word...)
	return buildLocalNotificationFromRequestWithRoute(template, decoded, AddFriendCommand, payload, localMessageRoute{Route: localPlayerProfileRoute, SectionID: localPlayerProfileSectionID}, nil)
}

func BuildLocalRemoveFriendResponse(packet []byte, resultID uint16, request RemoveFriendRequest) ([]byte, error) {
	decoded, _, err := decodeFriendPacket(packet, RemoveFriendCommand, 12, 12)
	if err != nil {
		return nil, err
	}
	payload := binary.BigEndian.AppendUint16(nil, resultID)
	payload = binary.BigEndian.AppendUint32(payload, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.TargetUIN)
	return buildLocalResponse(packet, decoded, RemoveFriendCommand, payload, nil)
}

func BuildLocalFriendUpdateNotification(template []byte, update FriendUpdate) ([]byte, error) {
	decoded, err := decodeLocalPacket(template)
	if err != nil {
		return nil, err
	}
	payload, err := update.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(template, decoded, FriendUpdateNotifyCommand, payload, localMessageRoute{Route: localPlayerProfileRoute, SectionID: localPlayerProfileSectionID}, nil)
}

// BuildLocalFriendAnswerResult is used both as the answerer's acknowledgement
// and as the original requester's result notification. The command is 0x00A6
// even when an add-request failure is correlated to command 0x00A3.
func BuildLocalFriendAnswerResult(packet []byte, resultID uint16, uin, when, targetUIN uint32, notification bool) ([]byte, error) {
	if uin == 0 || targetUIN == 0 || uin == targetUIN || resultID > FriendResultRejected {
		return nil, fmt.Errorf("friend answer-result fields are invalid")
	}
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return nil, err
	}
	payload := binary.BigEndian.AppendUint16(nil, resultID)
	payload = binary.BigEndian.AppendUint32(payload, uin)
	payload = binary.BigEndian.AppendUint32(payload, when)
	payload = binary.BigEndian.AppendUint32(payload, targetUIN)
	if notification {
		return buildLocalNotificationFromRequestWithRoute(packet, decoded, AnswerFriendCommand, payload, localMessageRoute{Route: localPlayerProfileRoute, SectionID: localPlayerProfileSectionID}, nil)
	}
	return buildLocalMessageFromRequest(packet, decoded, AnswerFriendCommand, payload, nil)
}

func (update FriendUpdate) MarshalNetworkBinary() ([]byte, error) {
	if update.UIN == 0 || update.FriendUIN == 0 || update.UIN == update.FriendUIN {
		return nil, fmt.Errorf("friend update requires different non-zero owner and friend UINs")
	}
	if update.Gender > 1 {
		return nil, fmt.Errorf("friend update gender %d is outside 0..1", update.Gender)
	}
	if len(update.ExtItemIDs) > FriendExternalItemMaximum {
		return nil, fmt.Errorf("friend update external item count %d exceeds %d", len(update.ExtItemIDs), FriendExternalItemMaximum)
	}
	payload := binary.BigEndian.AppendUint32(nil, update.UIN)
	payload = binary.BigEndian.AppendUint32(payload, update.FriendUIN)
	payload = binary.BigEndian.AppendUint32(payload, update.Point)
	payload = append(payload, byte(len(update.ExtItemIDs)))
	for index := 0; index < FriendExternalItemMaximum; index++ {
		itemID := uint32(0)
		if index < len(update.ExtItemIDs) {
			itemID = update.ExtItemIDs[index]
		}
		payload = binary.BigEndian.AppendUint32(payload, itemID)
	}
	if update.Online {
		payload = append(payload, 1)
	} else {
		payload = append(payload, 0)
	}
	var err error
	payload, err = appendFixedLegacyGBKSlot(payload, update.Nickname, FriendNicknameSlotSize)
	if err != nil {
		return nil, fmt.Errorf("friend update nickname: %w", err)
	}
	payload = append(payload, update.Gender)
	payload = binary.BigEndian.AppendUint32(payload, update.Identity)
	payload = binary.BigEndian.AppendUint32(payload, update.ExtPoint)
	if len(payload) != FriendUpdatePayloadSize {
		return nil, fmt.Errorf("friend update payload length %d, want %d", len(payload), FriendUpdatePayloadSize)
	}
	return payload, nil
}

func decodeFriendPacket(packet []byte, command uint16, minimumPayloadSize, maximumPayloadSize int) (localPacket, []byte, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return localPacket{}, nil, err
	}
	if decoded.Command != command {
		return localPacket{}, nil, fmt.Errorf("friend command 0x%04X, want 0x%04X", decoded.Command, command)
	}
	payload := decoded.Plaintext[localInnerHeaderSize:]
	if len(payload) < minimumPayloadSize || len(payload) > maximumPayloadSize {
		return localPacket{}, nil, fmt.Errorf("friend payload length %d is outside %d..%d", len(payload), minimumPayloadSize, maximumPayloadSize)
	}
	return decoded, payload, nil
}

func validateFriendEnvelope(packet localPacket, uin uint32) error {
	if uin == 0 || packet.EnvelopeUIN != uin {
		return fmt.Errorf("friend envelope UIN %d != payload UIN %d", packet.EnvelopeUIN, uin)
	}
	return nil
}

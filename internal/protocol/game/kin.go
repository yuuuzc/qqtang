package game

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

const (
	CreateKinRequestSchema           uint32 = 0x1771
	CreateKinResponseSchema          uint32 = 0x1772
	FetchKinBaseRequestSchema        uint32 = 0x1773
	FetchKinBaseResponseSchema       uint32 = 0x1774
	FetchKinMembersRequestSchema     uint32 = 0x1775
	FetchKinMembersResponseSchema    uint32 = 0x1776
	UpdateKinTitleRequestSchema      uint32 = 0x1777
	UpdateKinTitleResponseSchema     uint32 = 0x1778
	UpdateKinFlagRequestSchema       uint32 = 0x1779
	UpdateKinFlagResponseSchema      uint32 = 0x177A
	OperateKinRequestSchema          uint32 = 0x177B
	OperateKinResponseSchema         uint32 = 0x177C
	OperateKinNotificationSchema     uint32 = 0x177D
	ServerOperateKinRequestSchema    uint32 = 0x177F
	ServerOperateKinResponseSchema   uint32 = 0x1780
	ServerOperateKinNotifySchema     uint32 = 0x1781
	KinChatRequestSchema             uint32 = 0x1782
	KinChatNotificationSchema        uint32 = 0x1783
	KickKinMemberRequestSchema       uint32 = 0x1784
	KickKinMemberResponseSchema      uint32 = 0x1785
	ExitKinRequestSchema             uint32 = 0x1786
	ExitKinResponseSchema            uint32 = 0x1787
	DismissKinRequestSchema          uint32 = 0x1788
	DismissKinResponseSchema         uint32 = 0x1789
	KinEventNotificationSchema       uint32 = 0x178A
	AssignKinAuthorityRequestSchema  uint32 = 0x178B
	AssignKinAuthorityResponseSchema uint32 = 0x178C
	SetKinAuthorityRequestSchema     uint32 = 0x178D
	SetKinAuthorityResponseSchema    uint32 = 0x178E
	SetKinBadgeRequestSchema         uint32 = 0x178F
	SetKinBadgeResponseSchema        uint32 = 0x1790
	SetKinDeclarationRequestSchema   uint32 = 0x1793
	SetKinDeclarationResponseSchema  uint32 = 0x1794
	SetKinNotificationRequestSchema  uint32 = 0x1795
	SetKinNotificationResponseSchema uint32 = 0x1796

	KinNameSlotSize         = 17
	KinDeclarationSlotSize  = 257
	KinTitleSlotSize        = 200
	KinNotificationSlotSize = 200
	KinReasonSlotSize       = 512
	KinChatSlotSize         = 512
	KinNicknameSlotSize     = 20
	KinMemberMaximum        = 200

	// QQTSection's family invitation path uses zero for acceptance and the
	// unsigned -1 sentinel for rejection. These values are shared by the
	// initial 0x00BA invitation result and the invited player's 0x00BC answer.
	KinOperationAccepted uint32 = 0
	KinOperationRejected uint32 = ^uint32(0)

	// Client-native family cache events recovered from QQTSection's 0x178A
	// handler. Event 8 removes AttachUIN (and clears the whole cache when that
	// UIN is the recipient); event 5 clears a dismissed family.
	KinEventNotificationMemberJoined       uint32 = 1
	KinEventNotificationFamilyDismissed    uint32 = 5
	KinEventNotificationMemberRemoved      uint32 = 8
	KinEventNotificationUpdateAnnouncement uint32 = 9
)

type KinBase struct {
	Index                  uint32
	OwnerUIN               uint32
	CreatedUnix            uint32
	DismissUnix            uint32
	Status                 uint32
	Grade                  uint32
	FlagID                 KinFlagID
	MemberCount            uint16
	Name                   string
	Declaration            string
	Title                  string
	Section                uint32
	BaseUpdate             uint32
	ListUpdate             uint32
	Notification           string
	Honor                  uint32
	ActivePoint            uint32
	LastHonor              uint32
	LastActivePoint        uint32
	LastHonorOrder         uint32
	LastActivePointOrder   uint32
	BeforeHonorOrder       uint32
	BeforeActivePointOrder uint32
}

type KinMemberOld struct {
	UIN           uint32
	Nickname      string
	JoinedUnix    uint32
	Status        uint32
	StatusTime    uint32
	Grade         uint32
	OnlineTime    uint32
	LastLoginTime uint32
	Honor         uint32
	ActivePoint   uint32
}

type CreateKinRequest struct {
	UIN         uint32
	Time        uint32
	Status      uint32
	Reserve     uint16
	Name        string
	Declaration string
	Section     uint32
}

type FetchKinBaseRequest struct {
	UIN      uint32
	Time     uint32
	KinIndex uint32
	IsMember uint32
}

type FetchKinMembersRequest struct {
	UIN        uint32
	Time       uint32
	KinIndex   uint32
	ListUpdate uint32
}

type KinOperationRequest struct {
	UIN      uint32
	Time     uint32
	KinIndex uint32
	DstUIN   uint32
	Para     uint32
	Reserve  uint32
}

// KinServerOperationAnswer is RESPONSE_SERVEROPR_KIN (schema 0x1780) sent
// by the invited client over the bidirectional 0x00BC command. Despite the
// historical field names, UIN is the inviter and DstUIN is the responder.
type KinServerOperationAnswer struct {
	InviterUIN   uint32
	Time         uint32
	KinIndex     uint32
	ResponderUIN uint32
	Para         uint32
	Reserve      uint32
}

type KinChatRequest struct {
	UIN      uint32
	Time     uint32
	KinIndex uint32
	Message  string
}

type KinEventNotification struct {
	EventType      uint32
	KinIndex       uint32
	UIN            uint32
	Nickname       string
	AttachUIN      uint32
	AttachNickname string
	Description    string
}

type KinTargetRequest struct {
	UIN      uint32
	Time     uint32
	DstUIN   uint32
	KinIndex uint32
}

type KinSelfRequest struct {
	UIN      uint32
	Time     uint32
	KinIndex uint32
	Value    uint32
}

type KinAuthorityRequest struct {
	KinIndex    uint32
	UIN         uint32
	Time        uint32
	DstUIN      uint32
	AuthorityID uint32
}

type KinTextRequest struct {
	UIN      uint32
	Time     uint32
	KinIndex uint32
	Text     string
}

type KinBadgeRequest struct {
	KinIndex       uint32
	UIN            uint32
	Time           uint32
	BadgeID        uint32
	DefinedBadgeID uint32
}

type KinTopRequest struct {
	UIN              uint32
	Time             uint32
	ClientOrderTime  uint32
	KinIndex         uint32
	HonorOrder       uint32
	ActivePointOrder uint32
}

type KinOrderInfo struct {
	KinIndex    uint32
	Name        string
	Value       uint32
	Order       uint16
	BeforeOrder uint16
}

func DecodeLocalCreateKinRequest(packet []byte) (CreateKinRequest, error) {
	decoded, payload, err := decodeKinPacket(packet, CreateKinCommand, 37, 294)
	if err != nil {
		return CreateKinRequest{}, err
	}
	contentLength := int(binary.BigEndian.Uint16(payload[14:16]))
	if contentLength > KinDeclarationSlotSize {
		return CreateKinRequest{}, fmt.Errorf("kin declaration length %d exceeds %d", contentLength, KinDeclarationSlotSize)
	}
	name, err := decodeLegacyGBKSlot(payload[16:33])
	if err != nil {
		return CreateKinRequest{}, fmt.Errorf("kin name: %w", err)
	}
	contentEnd := 33 + contentLength
	sectionOffset := contentEnd
	if len(payload) == 294 {
		sectionOffset = 290
	}
	if contentEnd > len(payload) || sectionOffset+4 > len(payload) {
		return CreateKinRequest{}, fmt.Errorf("kin create content/section exceeds payload length %d", len(payload))
	}
	content, err := decodeLegacyGBKSlot(payload[33:contentEnd])
	if err != nil {
		return CreateKinRequest{}, fmt.Errorf("kin declaration: %w", err)
	}
	request := CreateKinRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]),
		Status: binary.BigEndian.Uint32(payload[8:12]), Reserve: binary.BigEndian.Uint16(payload[12:14]),
		Name: name, Declaration: content, Section: binary.BigEndian.Uint32(payload[sectionOffset : sectionOffset+4]),
	}
	if err := validateKinUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return CreateKinRequest{}, err
	}
	return request, nil
}

func DecodeLocalFetchKinBaseRequest(packet []byte) (FetchKinBaseRequest, error) {
	decoded, payload, err := decodeKinPacket(packet, FetchKinBaseCommand, 16, 16)
	if err != nil {
		return FetchKinBaseRequest{}, err
	}
	request := FetchKinBaseRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]), KinIndex: binary.BigEndian.Uint32(payload[8:12]), IsMember: binary.BigEndian.Uint32(payload[12:16])}
	if err := validateKinUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return FetchKinBaseRequest{}, err
	}
	return request, nil
}

func DecodeLocalFetchKinMembersRequest(packet []byte) (FetchKinMembersRequest, error) {
	decoded, payload, err := decodeKinPacket(packet, FetchKinMemberListCommand, 16, 16)
	if err != nil {
		return FetchKinMembersRequest{}, err
	}
	request := FetchKinMembersRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]), KinIndex: binary.BigEndian.Uint32(payload[8:12]), ListUpdate: binary.BigEndian.Uint32(payload[12:16])}
	if err := validateKinUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return FetchKinMembersRequest{}, err
	}
	return request, nil
}

func DecodeLocalKinOperationRequest(packet []byte) (KinOperationRequest, error) {
	decoded, payload, err := decodeKinPacket(packet, OperateKinCommand, 24, 24)
	if err != nil {
		return KinOperationRequest{}, err
	}
	request := KinOperationRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]), KinIndex: binary.BigEndian.Uint32(payload[8:12]), DstUIN: binary.BigEndian.Uint32(payload[12:16]), Para: binary.BigEndian.Uint32(payload[16:20]), Reserve: binary.BigEndian.Uint32(payload[20:24])}
	if err := validateKinUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return KinOperationRequest{}, err
	}
	return request, nil
}

func DecodeLocalKinServerOperationAnswer(packet []byte) (KinServerOperationAnswer, error) {
	decoded, payload, err := decodeKinPacket(packet, ServerOperateKinCommand, 24, 24+758+2+KinReasonSlotSize)
	if err != nil {
		return KinServerOperationAnswer{}, err
	}
	answer := KinServerOperationAnswer{
		InviterUIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]),
		KinIndex: binary.BigEndian.Uint32(payload[8:12]), ResponderUIN: binary.BigEndian.Uint32(payload[12:16]),
		Para: binary.BigEndian.Uint32(payload[16:20]), Reserve: binary.BigEndian.Uint32(payload[20:24]),
	}
	if answer.InviterUIN == 0 || answer.KinIndex == 0 || answer.ResponderUIN == 0 || answer.InviterUIN == answer.ResponderUIN {
		return KinServerOperationAnswer{}, fmt.Errorf("kin invitation answer has invalid identities")
	}
	if answer.ResponderUIN != decoded.EnvelopeUIN {
		return KinServerOperationAnswer{}, fmt.Errorf("kin invitation responder UIN %d does not match envelope UIN %d", answer.ResponderUIN, decoded.EnvelopeUIN)
	}
	if answer.Reserve != 0 || answer.Para != KinOperationAccepted && answer.Para != KinOperationRejected {
		return KinServerOperationAnswer{}, fmt.Errorf("kin invitation answer para=%d reserve=%d is invalid", answer.Para, answer.Reserve)
	}
	return answer, nil
}

func DecodeLocalKinChatRequest(packet []byte) (KinChatRequest, error) {
	decoded, payload, err := decodeKinPacket(packet, KinChatCommand, 16, 16+KinChatSlotSize)
	if err != nil {
		return KinChatRequest{}, err
	}
	length := int(binary.BigEndian.Uint32(payload[12:16]))
	if length <= 0 || length > KinChatSlotSize || 16+length > len(payload) {
		return KinChatRequest{}, fmt.Errorf("kin chat length %d is outside 1..%d", length, KinChatSlotSize)
	}
	message, err := decodeLegacyGBKSlot(payload[16 : 16+length])
	if err != nil {
		return KinChatRequest{}, err
	}
	request := KinChatRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]), KinIndex: binary.BigEndian.Uint32(payload[8:12]), Message: message}
	if err := validateKinUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return KinChatRequest{}, err
	}
	return request, nil
}

func DecodeLocalKickKinMemberRequest(packet []byte) (KinTargetRequest, error) {
	decoded, payload, err := decodeKinPacket(packet, KickKinMemberCommand, 16, 16)
	if err != nil {
		return KinTargetRequest{}, err
	}
	request := KinTargetRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]), DstUIN: binary.BigEndian.Uint32(payload[8:12]), KinIndex: binary.BigEndian.Uint32(payload[12:16])}
	if err := validateKinUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return KinTargetRequest{}, err
	}
	return request, nil
}

func DecodeLocalExitKinRequest(packet []byte) (KinSelfRequest, error) {
	return decodeKinSelfRequest(packet, ExitKinCommand, false)
}

func DecodeLocalDismissKinRequest(packet []byte) (KinSelfRequest, error) {
	return decodeKinSelfRequest(packet, DismissKinCommand, true)
}

func DecodeLocalKinAuthorityRequest(packet []byte) (KinAuthorityRequest, error) {
	decoded, payload, err := decodeKinPacket(packet, AssignKinAuthorityCommand, 20, 20)
	if err != nil {
		return KinAuthorityRequest{}, err
	}
	request := KinAuthorityRequest{KinIndex: binary.BigEndian.Uint32(payload[0:4]), UIN: binary.BigEndian.Uint32(payload[4:8]), Time: binary.BigEndian.Uint32(payload[8:12]), DstUIN: binary.BigEndian.Uint32(payload[12:16]), AuthorityID: binary.BigEndian.Uint32(payload[16:20])}
	if err := validateKinUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return KinAuthorityRequest{}, err
	}
	return request, nil
}

func DecodeLocalUpdateKinTitleRequest(packet []byte) (KinTextRequest, error) {
	return decodeKinTextRequest(packet, UpdateKinTitleCommand, KinTitleSlotSize, false)
}

func DecodeLocalSetKinAuthorityRequest(packet []byte) (KinTextRequest, error) {
	request, wire, err := decodeKinTextRequestBytes(packet, SetKinAuthorityCommand, KinTitleSlotSize, true)
	if err != nil {
		return KinTextRequest{}, err
	}
	request.Text, err = decodeKinAuthorityTitleObject(wire)
	if err != nil {
		return KinTextRequest{}, fmt.Errorf("decode kin authority titles: %w", err)
	}
	return request, nil
}

func DecodeLocalSetKinDeclarationRequest(packet []byte) (KinTextRequest, error) {
	return decodeAliasedKinTextRequest(packet, SetKinDeclarationCommand, LegacySetKinDeclarationCommand, KinDeclarationSlotSize, false)
}

func DecodeLocalSetKinNotificationRequest(packet []byte) (KinTextRequest, error) {
	return decodeAliasedKinTextRequest(packet, SetKinNotificationCommand, LegacySetKinNotificationCommand, KinDeclarationSlotSize, true)
}

func DecodeLocalSetKinBadgeRequest(packet []byte) (KinBadgeRequest, error) {
	decoded, payload, err := decodeKinPacket(packet, SetKinBadgeCommand, 20, 20)
	if err != nil {
		return KinBadgeRequest{}, err
	}
	request := KinBadgeRequest{KinIndex: binary.BigEndian.Uint32(payload[0:4]), UIN: binary.BigEndian.Uint32(payload[4:8]), Time: binary.BigEndian.Uint32(payload[8:12]), BadgeID: binary.BigEndian.Uint32(payload[12:16]), DefinedBadgeID: binary.BigEndian.Uint32(payload[16:20])}
	if err := validateKinUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return KinBadgeRequest{}, err
	}
	return request, nil
}

func DecodeLocalFetchKinTopRequest(packet []byte) (KinTopRequest, error) {
	decoded, payload, err := decodeKinPacket(packet, FetchKinTopCommand, 24, 24)
	if err != nil {
		return KinTopRequest{}, err
	}
	request := KinTopRequest{
		UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]),
		ClientOrderTime: binary.BigEndian.Uint32(payload[8:12]), KinIndex: binary.BigEndian.Uint32(payload[12:16]),
		HonorOrder: binary.BigEndian.Uint32(payload[16:20]), ActivePointOrder: binary.BigEndian.Uint32(payload[20:24]),
	}
	if err := validateKinUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return KinTopRequest{}, err
	}
	return request, nil
}

func BuildLocalCreateKinResponse(packet []byte, uin, kinIndex, errorNo uint32, reason string) ([]byte, error) {
	payload := binary.BigEndian.AppendUint32(nil, uin)
	payload = binary.BigEndian.AppendUint32(payload, kinIndex)
	payload = binary.BigEndian.AppendUint32(payload, errorNo)
	var err error
	payload, err = appendKinText(payload, reason, KinTitleSlotSize, 2)
	if err != nil {
		return nil, err
	}
	return buildKinResponse(packet, CreateKinCommand, payload)
}

func BuildLocalFetchKinBaseResponse(packet []byte, uin uint32, kin KinBase) ([]byte, error) {
	payload := binary.BigEndian.AppendUint32(nil, uin)
	var err error
	payload, err = kin.appendNetworkBinary(payload)
	if err != nil {
		return nil, err
	}
	return buildKinResponse(packet, FetchKinBaseCommand, payload)
}

func BuildLocalFetchKinMembersResponse(packet []byte, uin, listUpdate uint32, result uint16, members []KinMemberOld) ([]byte, error) {
	if len(members) > KinMemberMaximum {
		return nil, fmt.Errorf("kin member count %d exceeds %d", len(members), KinMemberMaximum)
	}
	payload := binary.BigEndian.AppendUint32(nil, uin)
	payload = binary.BigEndian.AppendUint32(payload, listUpdate)
	payload = binary.BigEndian.AppendUint16(payload, result)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(members)))
	for _, member := range members {
		var err error
		payload, err = member.appendNetworkBinary(payload)
		if err != nil {
			return nil, err
		}
	}
	for _, member := range members {
		payload = binary.BigEndian.AppendUint32(payload, member.Honor)
		payload = binary.BigEndian.AppendUint32(payload, member.ActivePoint)
	}
	return buildKinResponse(packet, FetchKinMemberListCommand, payload)
}

func BuildLocalKinOperationResponse(packet []byte, request KinOperationRequest, reason string) ([]byte, error) {
	payload := binary.BigEndian.AppendUint32(nil, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.Time)
	payload = binary.BigEndian.AppendUint32(payload, request.KinIndex)
	payload = binary.BigEndian.AppendUint32(payload, request.DstUIN)
	payload = binary.BigEndian.AppendUint32(payload, request.Para)
	payload = binary.BigEndian.AppendUint32(payload, request.Reserve)
	var err error
	payload, err = appendKinText(payload, reason, KinReasonSlotSize, 2)
	if err != nil {
		return nil, err
	}
	return buildKinResponse(packet, OperateKinCommand, payload)
}

// BuildLocalKinServerOperationRequest sends REQUEST_SERVEROPR_KIN
// (0x00BC/schema 0x177F) to the invited player. QQTSection caches the KinBase
// from this packet before displaying its native accept/reject dialog.
func BuildLocalKinServerOperationRequest(templatePacket []byte, request KinOperationRequest, kin KinBase) ([]byte, error) {
	payload, err := appendKinServerOperationPayload(nil, request, kin)
	if err != nil {
		return nil, err
	}
	return buildKinNotification(templatePacket, ServerOperateKinCommand, payload)
}

// BuildLocalKinServerOperationNotification sends the final
// NOTIFY_SERVERNOTIFYOPR_KIN (0x00BD/schema 0x1781). On acceptance the native
// handler installs KinIndex and the authoritative KinBase for the new member.
func BuildLocalKinServerOperationNotification(templatePacket []byte, request KinOperationRequest, kin KinBase) ([]byte, error) {
	payload, err := appendKinServerOperationPayload(nil, request, kin)
	if err != nil {
		return nil, err
	}
	return buildKinNotification(templatePacket, ServerOperateKinNotifyCommand, payload)
}

func appendKinServerOperationPayload(payload []byte, request KinOperationRequest, kin KinBase) ([]byte, error) {
	payload = binary.BigEndian.AppendUint32(payload, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.Time)
	payload = binary.BigEndian.AppendUint32(payload, request.KinIndex)
	payload = binary.BigEndian.AppendUint32(payload, request.DstUIN)
	payload = binary.BigEndian.AppendUint32(payload, request.Para)
	payload = binary.BigEndian.AppendUint32(payload, request.Reserve)
	return kin.appendNetworkBinary(payload)
}

func BuildLocalKinChatNotification(templatePacket []byte, request KinChatRequest) ([]byte, error) {
	message, err := encodeLegacyGBKText(request.Message, KinChatSlotSize)
	if err != nil {
		return nil, err
	}
	payload := binary.BigEndian.AppendUint32(nil, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.KinIndex)
	payload = binary.BigEndian.AppendUint32(payload, uint32(len(message)))
	payload = append(payload, message...)
	return buildKinNotification(templatePacket, KinChatNotifyCommand, payload)
}

// BuildLocalKinEventNotification encodes NOTIFY_KIN_EVENT (schema 0x178A).
// Its counted strings are compact on the wire; the client's schema decoder
// expands them into the fixed 262-byte native structure before dispatch.
func BuildLocalKinEventNotification(templatePacket []byte, event KinEventNotification) ([]byte, error) {
	payload := binary.BigEndian.AppendUint32(nil, event.EventType)
	payload = binary.BigEndian.AppendUint32(payload, event.KinIndex)
	payload = binary.BigEndian.AppendUint32(payload, event.UIN)
	var err error
	payload, err = appendKinText(payload, event.Nickname, KinNicknameSlotSize, 2)
	if err != nil {
		return nil, fmt.Errorf("kin event nickname: %w", err)
	}
	payload = binary.BigEndian.AppendUint32(payload, event.AttachUIN)
	payload, err = appendKinText(payload, event.AttachNickname, KinNicknameSlotSize, 2)
	if err != nil {
		return nil, fmt.Errorf("kin event attached nickname: %w", err)
	}
	payload, err = appendKinText(payload, event.Description, KinNotificationSlotSize, 2)
	if err != nil {
		return nil, fmt.Errorf("kin event description: %w", err)
	}
	return buildKinNotification(templatePacket, KinEventNotifyCommand, payload)
}

func BuildLocalKinTargetResponse(packet []byte, command uint16, result uint16, request KinTargetRequest, reason string) ([]byte, error) {
	if command != KickKinMemberCommand {
		return nil, fmt.Errorf("unsupported kin target response command 0x%04X", command)
	}
	payload := binary.BigEndian.AppendUint16(nil, result)
	payload = binary.BigEndian.AppendUint32(payload, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.DstUIN)
	payload = binary.BigEndian.AppendUint32(payload, request.KinIndex)
	var err error
	payload, err = appendKinText(payload, reason, KinReasonSlotSize, 2)
	if err != nil {
		return nil, err
	}
	return buildKinResponse(packet, command, payload)
}

func BuildLocalKinSelfResponse(packet []byte, command uint16, result uint16, request KinSelfRequest, reason string) ([]byte, error) {
	payload := binary.BigEndian.AppendUint16(nil, result)
	payload = binary.BigEndian.AppendUint32(payload, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.KinIndex)
	if command == DismissKinCommand {
		payload = binary.BigEndian.AppendUint32(payload, request.Value)
	}
	var err error
	payload, err = appendKinText(payload, reason, KinReasonSlotSize, 2)
	if err != nil {
		return nil, err
	}
	return buildKinResponse(packet, command, payload)
}

func BuildLocalKinAuthorityResponse(packet []byte, result uint16, request KinAuthorityRequest, reason string) ([]byte, error) {
	payload := binary.BigEndian.AppendUint16(nil, result)
	payload = binary.BigEndian.AppendUint32(payload, request.KinIndex)
	payload = binary.BigEndian.AppendUint32(payload, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.DstUIN)
	payload = binary.BigEndian.AppendUint32(payload, request.AuthorityID)
	var err error
	payload, err = appendKinText(payload, reason, KinReasonSlotSize, 2)
	if err != nil {
		return nil, err
	}
	return buildKinResponse(packet, AssignKinAuthorityCommand, payload)
}

func BuildLocalKinTextResponse(packet []byte, command uint16, result uint16, request KinTextRequest, reason string) ([]byte, error) {
	canonicalCommand, err := canonicalKinTextCommand(command)
	if err != nil {
		return nil, err
	}
	var payload []byte
	if canonicalCommand == UpdateKinTitleCommand {
		payload = binary.BigEndian.AppendUint32(payload, request.UIN)
		payload = binary.BigEndian.AppendUint16(payload, result)
	} else if canonicalCommand == SetKinAuthorityCommand {
		payload = binary.BigEndian.AppendUint16(payload, result)
		payload = binary.BigEndian.AppendUint32(payload, request.KinIndex)
	} else {
		payload = binary.BigEndian.AppendUint16(payload, result)
	}
	if canonicalCommand == SetKinDeclarationCommand {
		payload = binary.BigEndian.AppendUint32(payload, request.UIN)
		payload = binary.BigEndian.AppendUint32(payload, request.KinIndex)
	} else if canonicalCommand == SetKinNotificationCommand {
		payload = binary.BigEndian.AppendUint32(payload, request.KinIndex)
		payload = binary.BigEndian.AppendUint32(payload, request.UIN)
	} else if canonicalCommand != UpdateKinTitleCommand && canonicalCommand != SetKinAuthorityCommand {
		return nil, fmt.Errorf("unsupported canonical kin text response command 0x%04X", canonicalCommand)
	}
	maximum := KinTitleSlotSize
	if canonicalCommand != UpdateKinTitleCommand && canonicalCommand != SetKinAuthorityCommand {
		maximum = KinDeclarationSlotSize
	}
	if canonicalCommand == SetKinAuthorityCommand {
		var title []byte
		title, err = encodeKinAuthorityTitleObject(request.Text)
		if err == nil {
			payload = binary.BigEndian.AppendUint16(payload, uint16(len(title)))
			payload = append(payload, title...)
		}
	} else {
		payload, err = appendKinText(payload, request.Text, maximum, 2)
	}
	if err != nil {
		return nil, err
	}
	if canonicalCommand != UpdateKinTitleCommand {
		payload, err = appendKinText(payload, reason, KinReasonSlotSize, 2)
		if err != nil {
			return nil, err
		}
	}
	if canonicalCommand != command {
		return buildKinResponseAs(packet, command, canonicalCommand, payload)
	}
	return buildKinResponse(packet, canonicalCommand, payload)
}

// BuildLocalKinSessionRestore seeds QQTSection's otherwise empty per-process
// family cache after login. RESPONSE_LOGIN has no KinIndex field; the native
// successful CREATE_KIN response is the non-chat state transition that sets
// the current KinIndex and immediately asks the server for the authoritative
// KinBase. An empty reason deliberately produces no create-success UI text.
func BuildLocalKinSessionRestore(loginPacket []byte, uin, kinIndex uint32) ([]byte, error) {
	if uin == 0 || kinIndex == 0 {
		return nil, fmt.Errorf("kin session restore requires non-zero UIN and kin index")
	}
	payload := binary.BigEndian.AppendUint32(nil, uin)
	payload = binary.BigEndian.AppendUint32(payload, kinIndex)
	payload = binary.BigEndian.AppendUint32(payload, 0)
	payload = binary.BigEndian.AppendUint16(payload, 0)
	return buildKinResponseAs(loginPacket, LoginCommand, CreateKinCommand, payload)
}

func BuildLocalKinBadgeResponse(packet []byte, result uint16, request KinBadgeRequest) ([]byte, error) {
	payload := binary.BigEndian.AppendUint16(nil, result)
	payload = binary.BigEndian.AppendUint32(payload, request.KinIndex)
	payload = binary.BigEndian.AppendUint32(payload, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.BadgeID)
	payload = binary.BigEndian.AppendUint32(payload, request.DefinedBadgeID)
	return buildKinResponse(packet, SetKinBadgeCommand, payload)
}

// BuildLocalFetchKinTopResponse follows the original TopKinInfo layout: the
// first twenty KinOrderInfo records are honor rank and the next twenty are
// activity rank. Both arrays are schema-fixed; unused records are encoded as
// zero values rather than omitted.
func BuildLocalFetchKinTopResponse(packet []byte, result uint16, orderTime, uin uint32, honor, activity []KinOrderInfo) ([]byte, error) {
	if len(honor) > 20 || len(activity) > 20 {
		return nil, fmt.Errorf("kin ranking sizes honor=%d activity=%d exceed 20", len(honor), len(activity))
	}
	payload := binary.BigEndian.AppendUint16(nil, result)
	payload = binary.BigEndian.AppendUint32(payload, orderTime)
	payload = binary.BigEndian.AppendUint32(payload, uin)
	payload = binary.BigEndian.AppendUint32(payload, orderTime)
	for index := 0; index < 20; index++ {
		entry := KinOrderInfo{}
		if index < len(honor) {
			entry = honor[index]
		}
		var err error
		payload, err = entry.appendNetworkBinary(payload)
		if err != nil {
			return nil, err
		}
	}
	for index := 0; index < 20; index++ {
		entry := KinOrderInfo{}
		if index < len(activity) {
			entry = activity[index]
		}
		var err error
		payload, err = entry.appendNetworkBinary(payload)
		if err != nil {
			return nil, err
		}
	}
	// No same-group slice is authored until the client grouping semantics are
	// proven. The two zero counts are authoritative and therefore no trailing
	// KinOrderInfo records follow.
	payload = binary.BigEndian.AppendUint16(payload, 0)
	payload = binary.BigEndian.AppendUint16(payload, 0)
	return buildKinResponse(packet, FetchKinTopCommand, payload)
}

func (kin KinBase) appendNetworkBinary(dst []byte) ([]byte, error) {
	name, err := encodeLegacyGBKText(kin.Name, KinNameSlotSize)
	if err != nil {
		return nil, fmt.Errorf("kin name: %w", err)
	}
	declaration, err := encodeLegacyGBKText(kin.Declaration, KinDeclarationSlotSize)
	if err != nil {
		return nil, fmt.Errorf("kin declaration: %w", err)
	}
	title, err := encodeKinAuthorityTitleObject(kin.Title)
	if err != nil {
		return nil, fmt.Errorf("kin title: %w", err)
	}
	notification, err := encodeLegacyGBKText(kin.Notification, KinNotificationSlotSize)
	if err != nil {
		return nil, fmt.Errorf("kin notification: %w", err)
	}
	// QQTSection compares a proposed announcement with the cached one using
	// strncmp(new, cached, cachedLength). A decoded length of zero therefore
	// makes every first announcement look unchanged and mode 14 sends no
	// request at all. The original fixed C string contract permits a counted
	// NUL for an empty value: it still renders as empty, while preserving a
	// one-byte comparison sentinel in the native family cache.
	if len(notification) == 0 {
		notification = []byte{0}
	}
	dst = binary.BigEndian.AppendUint32(dst, kin.Index)
	dst = binary.BigEndian.AppendUint32(dst, kin.OwnerUIN)
	dst = binary.BigEndian.AppendUint32(dst, kin.CreatedUnix)
	dst = binary.BigEndian.AppendUint32(dst, kin.DismissUnix)
	dst = binary.BigEndian.AppendUint32(dst, kin.Status)
	dst = binary.BigEndian.AppendUint32(dst, kin.Grade)
	dst = append(dst, kin.FlagID[:]...)
	dst = binary.BigEndian.AppendUint16(dst, kin.MemberCount)
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(declaration)))
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(title)))
	nameSlot := make([]byte, KinNameSlotSize)
	copy(nameSlot, name)
	dst = append(dst, nameSlot...)
	dst = append(dst, declaration...)
	dst = append(dst, title...)
	dst = binary.BigEndian.AppendUint32(dst, kin.Section)
	dst = binary.BigEndian.AppendUint32(dst, kin.BaseUpdate)
	dst = binary.BigEndian.AppendUint32(dst, kin.ListUpdate)
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(notification)))
	dst = append(dst, notification...)
	dst = binary.BigEndian.AppendUint32(dst, kin.Honor)
	dst = binary.BigEndian.AppendUint32(dst, kin.ActivePoint)
	dst = binary.BigEndian.AppendUint32(dst, kin.LastHonor)
	dst = binary.BigEndian.AppendUint32(dst, kin.LastActivePoint)
	dst = binary.BigEndian.AppendUint32(dst, kin.LastHonorOrder)
	dst = binary.BigEndian.AppendUint32(dst, kin.LastActivePointOrder)
	dst = binary.BigEndian.AppendUint32(dst, kin.BeforeHonorOrder)
	dst = binary.BigEndian.AppendUint32(dst, kin.BeforeActivePointOrder)
	return dst, nil
}

func (member KinMemberOld) appendNetworkBinary(dst []byte) ([]byte, error) {
	nickname, err := encodeLegacyGBKText(member.Nickname, KinNicknameSlotSize)
	if err != nil {
		return nil, fmt.Errorf("kin member nickname: %w", err)
	}
	dst = binary.BigEndian.AppendUint32(dst, member.UIN)
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(nickname)))
	dst = append(dst, nickname...)
	dst = binary.BigEndian.AppendUint32(dst, member.JoinedUnix)
	dst = binary.BigEndian.AppendUint32(dst, member.Status)
	dst = binary.BigEndian.AppendUint32(dst, member.StatusTime)
	dst = binary.BigEndian.AppendUint32(dst, member.Grade)
	dst = binary.BigEndian.AppendUint32(dst, member.OnlineTime)
	dst = binary.BigEndian.AppendUint32(dst, member.LastLoginTime)
	return dst, nil
}

func (entry KinOrderInfo) appendNetworkBinary(dst []byte) ([]byte, error) {
	name, err := encodeLegacyGBKText(entry.Name, KinNameSlotSize)
	if err != nil {
		return nil, fmt.Errorf("kin ranking name: %w", err)
	}
	dst = binary.BigEndian.AppendUint32(dst, entry.KinIndex)
	dst = append(dst, byte(len(name)))
	dst = append(dst, name...)
	dst = binary.BigEndian.AppendUint32(dst, entry.Value)
	dst = binary.BigEndian.AppendUint16(dst, entry.Order)
	dst = binary.BigEndian.AppendUint16(dst, entry.BeforeOrder)
	return dst, nil
}

func decodeKinSelfRequest(packet []byte, command uint16, hasValue bool) (KinSelfRequest, error) {
	want := 12
	if hasValue {
		want = 16
	}
	decoded, payload, err := decodeKinPacket(packet, command, want, want)
	if err != nil {
		return KinSelfRequest{}, err
	}
	request := KinSelfRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), Time: binary.BigEndian.Uint32(payload[4:8]), KinIndex: binary.BigEndian.Uint32(payload[8:12])}
	if hasValue {
		request.Value = binary.BigEndian.Uint32(payload[12:16])
	}
	if err := validateKinUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return KinSelfRequest{}, err
	}
	return request, nil
}

func decodeKinTextRequest(packet []byte, command uint16, maximum int, indexFirst bool) (KinTextRequest, error) {
	request, wire, err := decodeKinTextRequestBytes(packet, command, maximum, indexFirst)
	if err != nil {
		return KinTextRequest{}, err
	}
	request.Text, err = decodeLegacyGBKSlot(wire)
	if err != nil {
		return KinTextRequest{}, err
	}
	return request, nil
}

func decodeKinTextRequestBytes(packet []byte, command uint16, maximum int, indexFirst bool) (KinTextRequest, []byte, error) {
	decoded, payload, err := decodeKinPacket(packet, command, 14, 14+maximum)
	if err != nil {
		return KinTextRequest{}, nil, err
	}
	request := KinTextRequest{Time: binary.BigEndian.Uint32(payload[4:8])}
	if indexFirst {
		request.KinIndex = binary.BigEndian.Uint32(payload[0:4])
		request.UIN = binary.BigEndian.Uint32(payload[4:8])
		request.Time = binary.BigEndian.Uint32(payload[8:12])
	} else {
		request.UIN = binary.BigEndian.Uint32(payload[0:4])
		request.Time = binary.BigEndian.Uint32(payload[4:8])
		request.KinIndex = binary.BigEndian.Uint32(payload[8:12])
	}
	length := int(binary.BigEndian.Uint16(payload[12:14]))
	if length > maximum || 14+length > len(payload) {
		return KinTextRequest{}, nil, fmt.Errorf("kin text length %d exceeds %d or payload", length, maximum)
	}
	if err := validateKinUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return KinTextRequest{}, nil, err
	}
	return request, payload[14 : 14+length], nil
}

func decodeAliasedKinTextRequest(packet []byte, command, legacyCommand uint16, maximum int, indexFirst bool) (KinTextRequest, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return KinTextRequest{}, err
	}
	if decoded.Command != command && decoded.Command != legacyCommand {
		return KinTextRequest{}, fmt.Errorf("kin command 0x%04X, want 0x%04X or legacy 0x%04X", decoded.Command, command, legacyCommand)
	}
	return decodeKinTextRequest(packet, decoded.Command, maximum, indexFirst)
}

func canonicalKinTextCommand(command uint16) (uint16, error) {
	switch command {
	case UpdateKinTitleCommand, SetKinAuthorityCommand, SetKinDeclarationCommand, SetKinNotificationCommand:
		return command, nil
	case LegacySetKinDeclarationCommand:
		return SetKinDeclarationCommand, nil
	case LegacySetKinNotificationCommand:
		return SetKinNotificationCommand, nil
	default:
		return 0, fmt.Errorf("unsupported kin text response command 0x%04X", command)
	}
}

func decodeKinPacket(packet []byte, command uint16, minimumPayloadSize, maximumPayloadSize int) (localPacket, []byte, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return localPacket{}, nil, err
	}
	if decoded.Command != command {
		return localPacket{}, nil, fmt.Errorf("kin command 0x%04X, want 0x%04X", decoded.Command, command)
	}
	payload := decoded.Plaintext[localInnerHeaderSize:]
	if len(payload) < minimumPayloadSize || len(payload) > maximumPayloadSize {
		return localPacket{}, nil, fmt.Errorf("kin command 0x%04X payload length %d is outside %d..%d", command, len(payload), minimumPayloadSize, maximumPayloadSize)
	}
	return decoded, payload, nil
}

func appendKinText(dst []byte, text string, maximum, lengthBytes int) ([]byte, error) {
	encoded, err := encodeLegacyGBKText(text, maximum)
	if err != nil {
		return nil, err
	}
	if lengthBytes == 2 {
		dst = binary.BigEndian.AppendUint16(dst, uint16(len(encoded)))
	} else {
		dst = binary.BigEndian.AppendUint32(dst, uint32(len(encoded)))
	}
	return append(dst, encoded...), nil
}

var kinAuthorityTitleGrades = [...]byte{12, 10, 8, 6, 4}

// KinBase.Title is an ASCII-framed GBK object embedded inside an otherwise
// GBK protocol structure. QQTSection's FUN_10011f37 uses "%d " for the count,
// grade and byte length. Each title is formatted with "%s "; the advertised
// length therefore includes the trailing ASCII space. FUN_10011eee consumes
// that full span but copies length-1 bytes into the position cache, which is
// how the separator is removed without truncating the final GBK character.
func encodeKinAuthorityTitleObject(logical string) ([]byte, error) {
	if logical == "" {
		return nil, nil
	}
	trimmed := strings.TrimSuffix(logical, "\x07")
	titles := strings.Split(trimmed, "\x07")
	if len(titles) != len(kinAuthorityTitleGrades) {
		return nil, fmt.Errorf("kin authority title contains %d positions, want %d", len(titles), len(kinAuthorityTitleGrades))
	}
	wire := make([]byte, 0, KinTitleSlotSize)
	wire = fmt.Appendf(wire, "%d ", len(titles))
	for index, title := range titles {
		title = strings.TrimSpace(title)
		if title == "" {
			return nil, fmt.Errorf("kin authority title position %d is empty", index)
		}
		encoded, err := encodeLegacyGBKText(title, 10)
		if err != nil {
			return nil, fmt.Errorf("kin authority title position %d: %w", index, err)
		}
		// The native parser requires the separator to be part of the string
		// span: it copies length-1 bytes and then advances by the full length.
		wire = fmt.Appendf(wire, "%d %d ", kinAuthorityTitleGrades[index], len(encoded)+1)
		wire = append(wire, encoded...)
		wire = append(wire, ' ')
	}
	if len(wire) > KinTitleSlotSize {
		return nil, fmt.Errorf("kin authority title object uses %d bytes, maximum is %d", len(wire), KinTitleSlotSize)
	}
	return wire, nil
}

func decodeKinAuthorityTitleObject(wire []byte) (string, error) {
	cursor := 0
	readNumber := func(label string) (int, error) {
		if cursor >= len(wire) {
			return 0, fmt.Errorf("kin authority title %s is missing", label)
		}
		start := cursor
		for cursor < len(wire) && wire[cursor] >= '0' && wire[cursor] <= '9' {
			cursor++
		}
		if cursor == start || cursor >= len(wire) || wire[cursor] != ' ' {
			return 0, fmt.Errorf("kin authority title %s is not an ASCII integer", label)
		}
		value, err := strconv.Atoi(string(wire[start:cursor]))
		if err != nil {
			return 0, fmt.Errorf("kin authority title %s: %w", label, err)
		}
		cursor++
		return value, nil
	}
	count, err := readNumber("count")
	if err != nil || count != len(kinAuthorityTitleGrades) {
		return "", fmt.Errorf("kin authority title count is invalid")
	}
	titles := make([]string, 0, len(kinAuthorityTitleGrades))
	for index, grade := range kinAuthorityTitleGrades {
		wireGrade, gradeErr := readNumber(fmt.Sprintf("grade %d", index))
		if gradeErr != nil || wireGrade != int(grade) {
			return "", fmt.Errorf("kin authority title grade %d is invalid", index)
		}
		length, lengthErr := readNumber(fmt.Sprintf("position %d length", index))
		if lengthErr != nil || length < 2 || length > 11 || cursor+length > len(wire) || wire[cursor+length-1] != ' ' {
			return "", fmt.Errorf("kin authority title position %d length is invalid", index)
		}
		title, err := decodeLegacyGBKSlot(wire[cursor : cursor+length-1])
		if err != nil || strings.TrimSpace(title) == "" {
			return "", fmt.Errorf("kin authority title position %d text is invalid", index)
		}
		titles = append(titles, title)
		cursor += length
	}
	if cursor != len(wire) {
		return "", fmt.Errorf("kin authority title has %d trailing bytes", len(wire)-cursor)
	}
	return strings.Join(titles, "\x07") + "\x07", nil
}

func buildKinResponse(packet []byte, command uint16, payload []byte) ([]byte, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(packet, decoded, command, payload, nil)
}

func buildKinResponseAs(packet []byte, requestCommand, responseCommand uint16, payload []byte) ([]byte, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return nil, err
	}
	if decoded.Command != requestCommand {
		return nil, fmt.Errorf("local game command 0x%04X, want 0x%04X", decoded.Command, requestCommand)
	}
	return buildLocalMessageFromRequest(packet, decoded, responseCommand, payload, nil)
}

func buildKinNotification(packet []byte, command uint16, payload []byte) ([]byte, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(packet, decoded, command, payload, localMessageRoute{Route: localPlayerProfileRoute, SectionID: localPlayerProfileSectionID}, nil)
}

func validateKinUIN(uin, envelopeUIN uint32) error {
	if uin == 0 || uin != envelopeUIN {
		return fmt.Errorf("kin UIN %d does not match envelope UIN %d", uin, envelopeUIN)
	}
	return nil
}

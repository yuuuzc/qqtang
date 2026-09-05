package game

import (
	"encoding/binary"
	"fmt"
)

const (
	RequestSparkSchema              uint32 = 0x0BEA
	ResponseSparkSchema             uint32 = 0x0BEB
	NotifySparkSchema               uint32 = 0x0BEC
	RequestAnswerSparkSchema        uint32 = 0x0BED
	ResponseAnswerSparkSchema       uint32 = 0x0BEE
	RequestMarriageInfoSchema       uint32 = 0x0BEF
	ResponseMarriageInfoSchema      uint32 = 0x0BF0
	RequestModifyLoveWordSchema     uint32 = 0x0BF1
	ResponseModifyLoveWordSchema    uint32 = 0x0BF2
	RequestDivorceSchema            uint32 = 0x0BF3
	ResponseDivorceSchema           uint32 = 0x0BF4
	RequestStartWeddingSchema       uint32 = 0x0BF5
	ResponseStartWeddingSchema      uint32 = 0x0BF6
	NotifyWeddingConfirmSchema      uint32 = 0x0BF7
	RequestAnswerWeddingSchema      uint32 = 0x0BF8
	ResponseAnswerWeddingSchema     uint32 = 0x0BF9
	RequestChangeWeddingModeSchema  uint32 = 0x0BFA
	ResponseChangeWeddingModeSchema uint32 = 0x0BFB
	NotifyChangeWeddingModeSchema   uint32 = 0x0BFC

	MarriageTextSlotSize     = 200
	MarriageNicknameSlotSize = 20

	MarriageResultSuccess  uint16 = 0
	MarriageResultRejected uint16 = 1
	MarriageResultFailed   uint16 = 2
)

type SparkRequest struct {
	UIN       uint32
	TargetUIN uint32
	Word      string
}

type AnswerSparkRequest struct {
	// REQUEST_ANSWER_SPARK uses historical field names which are reversed
	// from the action: TargetUin is the responding client, while Uin is the
	// original proposer. Name the domain roles explicitly to avoid swapping
	// the authenticated actor and proposal owner again.
	ResponderUIN uint32
	ProposerUIN  uint32
	ResultID     uint16
}

type MarriageInfoRequest struct {
	UIN       uint32
	TargetUIN uint32
}

type MarriageInfo struct {
	TargetUIN       uint32
	MarriageUIN     uint32
	MarriageLoyalty uint32
	MarriageLevel   uint16
	LevelValue      uint32
	MarriageAge     uint32
	LoveWord        string
	Nickname        string
	SpouseNickname  string
	RingID          uint32
}

type ModifyLoveWordRequest struct {
	UIN       uint32
	SpouseUIN uint32
	LoveWord  string
}

type MarriagePairRequest struct {
	UIN       uint32
	TargetUIN uint32
}

type StartWeddingRequest struct {
	UIN           uint32
	TargetUIN     uint32
	WeddingModeID uint16
}

type AnswerWeddingRequest struct {
	UIN       uint32
	TargetUIN uint32
	ResultID  uint16
}

type ChangeWeddingModeRequest struct {
	UIN           uint32
	WeddingModeID uint16
}

func DecodeLocalSparkRequest(packet []byte) (SparkRequest, error) {
	decoded, payload, err := decodeMarriagePacket(packet, RequestSparkCommand, 9, 209)
	if err != nil {
		return SparkRequest{}, err
	}
	length := int(payload[8])
	if length > MarriageTextSlotSize || 9+length > len(payload) {
		return SparkRequest{}, fmt.Errorf("spark word length %d exceeds payload %d", length, len(payload))
	}
	word, err := decodeLegacyGBKSlot(payload[9 : 9+length])
	if err != nil {
		return SparkRequest{}, err
	}
	request := SparkRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), TargetUIN: binary.BigEndian.Uint32(payload[4:8]), Word: word}
	if err := validateMarriageUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return SparkRequest{}, err
	}
	if request.TargetUIN == 0 || request.TargetUIN == request.UIN {
		return SparkRequest{}, fmt.Errorf("spark target UIN %d is invalid", request.TargetUIN)
	}
	return request, nil
}

func DecodeLocalAnswerSparkRequest(packet []byte) (AnswerSparkRequest, error) {
	decoded, payload, err := decodeMarriagePacket(packet, AnswerSparkCommand, 10, 10)
	if err != nil {
		return AnswerSparkRequest{}, err
	}
	request := AnswerSparkRequest{ResponderUIN: binary.BigEndian.Uint32(payload[0:4]), ProposerUIN: binary.BigEndian.Uint32(payload[4:8]), ResultID: binary.BigEndian.Uint16(payload[8:10])}
	if err := validateMarriageUIN(request.ResponderUIN, decoded.EnvelopeUIN); err != nil {
		return AnswerSparkRequest{}, err
	}
	if request.ProposerUIN == 0 || request.ProposerUIN == request.ResponderUIN || request.ResultID > MarriageResultRejected {
		return AnswerSparkRequest{}, fmt.Errorf("answer-spark fields are invalid")
	}
	return request, nil
}

func DecodeLocalMarriageInfoRequest(packet []byte) (MarriageInfoRequest, error) {
	decoded, payload, err := decodeMarriagePacket(packet, MarriageInfoCommand, 8, 8)
	if err != nil {
		return MarriageInfoRequest{}, err
	}
	request := MarriageInfoRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), TargetUIN: binary.BigEndian.Uint32(payload[4:8])}
	if err := validateMarriageUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return MarriageInfoRequest{}, err
	}
	if request.TargetUIN == 0 {
		request.TargetUIN = request.UIN
	}
	return request, nil
}

func DecodeLocalModifyLoveWordRequest(packet []byte) (ModifyLoveWordRequest, error) {
	decoded, payload, err := decodeMarriagePacket(packet, ModifyLoveWordCommand, 10, 210)
	if err != nil {
		return ModifyLoveWordRequest{}, err
	}
	length := int(binary.BigEndian.Uint16(payload[8:10]))
	if length > MarriageTextSlotSize || 10+length > len(payload) {
		return ModifyLoveWordRequest{}, fmt.Errorf("love-word length %d exceeds payload %d", length, len(payload))
	}
	word, err := decodeLegacyGBKSlot(payload[10 : 10+length])
	if err != nil {
		return ModifyLoveWordRequest{}, err
	}
	request := ModifyLoveWordRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), SpouseUIN: binary.BigEndian.Uint32(payload[4:8]), LoveWord: word}
	if err := validateMarriageUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return ModifyLoveWordRequest{}, err
	}
	return request, nil
}

func DecodeLocalDivorceRequest(packet []byte) (MarriagePairRequest, error) {
	return decodeMarriagePairRequest(packet, DivorceCommand)
}

func DecodeLocalStartWeddingRequest(packet []byte) (StartWeddingRequest, error) {
	decoded, payload, err := decodeMarriagePacket(packet, StartWeddingCommand, 10, 10)
	if err != nil {
		return StartWeddingRequest{}, err
	}
	request := StartWeddingRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), TargetUIN: binary.BigEndian.Uint32(payload[4:8]), WeddingModeID: binary.BigEndian.Uint16(payload[8:10])}
	if err := validateMarriageUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return StartWeddingRequest{}, err
	}
	if request.TargetUIN == 0 || request.WeddingModeID > 1 {
		return StartWeddingRequest{}, fmt.Errorf("start-wedding fields are invalid")
	}
	return request, nil
}

func DecodeLocalAnswerWeddingRequest(packet []byte) (AnswerWeddingRequest, error) {
	decoded, payload, err := decodeMarriagePacket(packet, AnswerWeddingCommand, 10, 10)
	if err != nil {
		return AnswerWeddingRequest{}, err
	}
	request := AnswerWeddingRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), TargetUIN: binary.BigEndian.Uint32(payload[4:8]), ResultID: binary.BigEndian.Uint16(payload[8:10])}
	if err := validateMarriageUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return AnswerWeddingRequest{}, err
	}
	if request.TargetUIN == 0 || request.ResultID > MarriageResultRejected {
		return AnswerWeddingRequest{}, fmt.Errorf("answer-wedding fields are invalid")
	}
	return request, nil
}

func DecodeLocalChangeWeddingModeRequest(packet []byte) (ChangeWeddingModeRequest, error) {
	decoded, payload, err := decodeMarriagePacket(packet, ChangeWeddingModeCommand, 6, 6)
	if err != nil {
		return ChangeWeddingModeRequest{}, err
	}
	request := ChangeWeddingModeRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), WeddingModeID: binary.BigEndian.Uint16(payload[4:6])}
	if err := validateMarriageUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return ChangeWeddingModeRequest{}, err
	}
	if request.WeddingModeID > 1 {
		return ChangeWeddingModeRequest{}, fmt.Errorf("wedding mode %d is outside 0..1", request.WeddingModeID)
	}
	return request, nil
}

func BuildLocalSparkResponse(packet []byte, result uint16, request SparkRequest, word string) ([]byte, error) {
	payload := binary.BigEndian.AppendUint16(nil, result)
	payload = binary.BigEndian.AppendUint32(payload, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.TargetUIN)
	var err error
	payload, err = appendMarriageText8(payload, word)
	if err != nil {
		return nil, err
	}
	return buildMarriageResponse(packet, RequestSparkCommand, payload)
}

func BuildLocalSparkNotification(template []byte, request SparkRequest, nickname string) ([]byte, error) {
	payload := binary.BigEndian.AppendUint32(nil, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.TargetUIN)
	var err error
	payload, err = appendMarriageNickname(payload, nickname)
	if err != nil {
		return nil, err
	}
	payload, err = appendMarriageText8(payload, request.Word)
	if err != nil {
		return nil, err
	}
	return buildMarriageNotification(template, NotifySparkCommand, payload)
}

func BuildLocalAnswerSparkResponse(packet []byte, result uint16, request AnswerSparkRequest, word string) ([]byte, error) {
	payload, err := marshalAnswerSparkPayload(result, request, word)
	if err != nil {
		return nil, err
	}
	return buildMarriageResponse(packet, AnswerSparkCommand, payload)
}

func BuildLocalAnswerSparkNotification(template []byte, result uint16, request AnswerSparkRequest, word string) ([]byte, error) {
	payload, err := marshalAnswerSparkPayload(result, request, word)
	if err != nil {
		return nil, err
	}
	return buildMarriageNotification(template, AnswerSparkCommand, payload)
}

func marshalAnswerSparkPayload(result uint16, request AnswerSparkRequest, word string) ([]byte, error) {
	payload := binary.BigEndian.AppendUint16(nil, result)
	// RESPONSE_ANSWER_SPARK restores the conventional Uin/TargetUin order:
	// original proposer first, responder second.
	payload = binary.BigEndian.AppendUint32(payload, request.ProposerUIN)
	payload = binary.BigEndian.AppendUint32(payload, request.ResponderUIN)
	var err error
	payload, err = appendMarriageText8(payload, word)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func BuildLocalMarriageInfoResponse(packet []byte, result uint16, info MarriageInfo) ([]byte, error) {
	payload := binary.BigEndian.AppendUint16(nil, result)
	payload = binary.BigEndian.AppendUint32(payload, info.TargetUIN)
	payload = binary.BigEndian.AppendUint32(payload, info.MarriageUIN)
	payload = binary.BigEndian.AppendUint32(payload, info.MarriageLoyalty)
	payload = binary.BigEndian.AppendUint16(payload, info.MarriageLevel)
	payload = binary.BigEndian.AppendUint32(payload, info.LevelValue)
	payload = binary.BigEndian.AppendUint32(payload, info.MarriageAge)
	word, err := encodeLegacyGBKText(info.LoveWord, MarriageTextSlotSize)
	if err != nil {
		return nil, err
	}
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(word)))
	payload = append(payload, word...)
	payload, err = appendMarriageNickname(payload, info.Nickname)
	if err != nil {
		return nil, err
	}
	payload, err = appendMarriageNickname(payload, info.SpouseNickname)
	if err != nil {
		return nil, err
	}
	payload = binary.BigEndian.AppendUint32(payload, info.RingID)
	return buildMarriageResponse(packet, MarriageInfoCommand, payload)
}

func BuildLocalModifyLoveWordResponse(packet []byte, result uint16, request ModifyLoveWordRequest) ([]byte, error) {
	payload := binary.BigEndian.AppendUint32(nil, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.SpouseUIN)
	payload = binary.BigEndian.AppendUint16(payload, result)
	return buildMarriageResponse(packet, ModifyLoveWordCommand, payload)
}

func BuildLocalDivorceResponse(packet []byte, result uint16, request MarriagePairRequest) ([]byte, error) {
	payload := marshalDivorcePayload(result, request)
	return buildMarriageResponse(packet, DivorceCommand, payload)
}

func BuildLocalDivorceNotification(template []byte, result uint16, request MarriagePairRequest) ([]byte, error) {
	return buildMarriageNotification(template, DivorceCommand, marshalDivorcePayload(result, request))
}

func marshalDivorcePayload(result uint16, request MarriagePairRequest) []byte {
	payload := binary.BigEndian.AppendUint32(nil, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.TargetUIN)
	payload = binary.BigEndian.AppendUint16(payload, result)
	return payload
}

func BuildLocalStartWeddingResponse(packet []byte, result uint16, request StartWeddingRequest, nickname, targetNickname, word string) ([]byte, error) {
	payload := binary.BigEndian.AppendUint32(nil, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.TargetUIN)
	var err error
	payload, err = appendMarriageNickname(payload, nickname)
	if err != nil {
		return nil, err
	}
	payload, err = appendMarriageNickname(payload, targetNickname)
	if err != nil {
		return nil, err
	}
	payload = binary.BigEndian.AppendUint16(payload, result)
	payload, err = appendMarriageText8(payload, word)
	if err != nil {
		return nil, err
	}
	payload = binary.BigEndian.AppendUint16(payload, request.WeddingModeID)
	return buildMarriageResponse(packet, StartWeddingCommand, payload)
}

func BuildLocalWeddingConfirmNotification(template []byte, request StartWeddingRequest, nickname, targetNickname, word string) ([]byte, error) {
	payload := binary.BigEndian.AppendUint32(nil, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.TargetUIN)
	var err error
	payload, err = appendMarriageNickname(payload, nickname)
	if err != nil {
		return nil, err
	}
	payload, err = appendMarriageNickname(payload, targetNickname)
	if err != nil {
		return nil, err
	}
	payload, err = appendMarriageText8(payload, word)
	if err != nil {
		return nil, err
	}
	return buildMarriageNotification(template, WeddingConfirmNotifyCommand, payload)
}

func BuildLocalAnswerWeddingResponse(packet []byte, result uint16, request AnswerWeddingRequest, nickname, targetNickname, word string) ([]byte, error) {
	payload, err := marshalAnswerWeddingPayload(result, request, nickname, targetNickname, word)
	if err != nil {
		return nil, err
	}
	return buildMarriageResponse(packet, AnswerWeddingCommand, payload)
}

func BuildLocalAnswerWeddingNotification(template []byte, result uint16, request AnswerWeddingRequest, nickname, targetNickname, word string) ([]byte, error) {
	payload, err := marshalAnswerWeddingPayload(result, request, nickname, targetNickname, word)
	if err != nil {
		return nil, err
	}
	return buildMarriageNotification(template, AnswerWeddingCommand, payload)
}

func marshalAnswerWeddingPayload(result uint16, request AnswerWeddingRequest, nickname, targetNickname, word string) ([]byte, error) {
	payload := binary.BigEndian.AppendUint32(nil, request.UIN)
	payload = binary.BigEndian.AppendUint32(payload, request.TargetUIN)
	var err error
	payload, err = appendMarriageNickname(payload, nickname)
	if err != nil {
		return nil, err
	}
	payload, err = appendMarriageNickname(payload, targetNickname)
	if err != nil {
		return nil, err
	}
	payload = binary.BigEndian.AppendUint16(payload, result)
	payload, err = appendMarriageText8(payload, word)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func BuildLocalChangeWeddingModeResponse(packet []byte, result uint16, request ChangeWeddingModeRequest, word string) ([]byte, error) {
	payload := binary.BigEndian.AppendUint32(nil, request.UIN)
	payload = binary.BigEndian.AppendUint16(payload, result)
	var err error
	payload, err = appendMarriageText8(payload, word)
	if err != nil {
		return nil, err
	}
	return buildMarriageResponse(packet, ChangeWeddingModeCommand, payload)
}

func BuildLocalChangeWeddingModeNotification(template []byte, uin uint32, mode uint16) ([]byte, error) {
	payload := binary.BigEndian.AppendUint32(nil, uin)
	payload = binary.BigEndian.AppendUint16(payload, mode)
	return buildMarriageNotification(template, ChangeWeddingModeNotifyCommand, payload)
}

func decodeMarriagePairRequest(packet []byte, command uint16) (MarriagePairRequest, error) {
	decoded, payload, err := decodeMarriagePacket(packet, command, 8, 8)
	if err != nil {
		return MarriagePairRequest{}, err
	}
	request := MarriagePairRequest{UIN: binary.BigEndian.Uint32(payload[0:4]), TargetUIN: binary.BigEndian.Uint32(payload[4:8])}
	if err := validateMarriageUIN(request.UIN, decoded.EnvelopeUIN); err != nil {
		return MarriagePairRequest{}, err
	}
	if request.TargetUIN == 0 || request.TargetUIN == request.UIN {
		return MarriagePairRequest{}, fmt.Errorf("marriage target UIN %d is invalid", request.TargetUIN)
	}
	return request, nil
}

func decodeMarriagePacket(packet []byte, command uint16, minimumPayloadSize, maximumPayloadSize int) (localPacket, []byte, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return localPacket{}, nil, err
	}
	if decoded.Command != command {
		return localPacket{}, nil, fmt.Errorf("marriage command 0x%04X, want 0x%04X", decoded.Command, command)
	}
	payload := decoded.Plaintext[localInnerHeaderSize:]
	if len(payload) < minimumPayloadSize || len(payload) > maximumPayloadSize {
		return localPacket{}, nil, fmt.Errorf("marriage command 0x%04X payload length %d is outside %d..%d", command, len(payload), minimumPayloadSize, maximumPayloadSize)
	}
	return decoded, payload, nil
}

func validateMarriageUIN(uin, envelopeUIN uint32) error {
	if uin == 0 || uin != envelopeUIN {
		return fmt.Errorf("marriage UIN %d does not match envelope UIN %d", uin, envelopeUIN)
	}
	return nil
}

func appendMarriageText8(dst []byte, text string) ([]byte, error) {
	encoded, err := encodeLegacyGBKText(text, MarriageTextSlotSize)
	if err != nil {
		return nil, err
	}
	dst = append(dst, byte(len(encoded)))
	return append(dst, encoded...), nil
}

func appendMarriageNickname(dst []byte, nickname string) ([]byte, error) {
	encoded, err := encodeLegacyGBKText(nickname, MarriageNicknameSlotSize)
	if err != nil {
		return nil, err
	}
	start := len(dst)
	dst = append(dst, make([]byte, MarriageNicknameSlotSize)...)
	copy(dst[start:], encoded)
	return dst, nil
}

func buildMarriageResponse(packet []byte, command uint16, payload []byte) ([]byte, error) {
	decoded, err := decodeLocalPacket(packet)
	if err != nil {
		return nil, err
	}
	return buildLocalResponse(packet, decoded, command, payload, nil)
}

func buildMarriageNotification(template []byte, command uint16, payload []byte) ([]byte, error) {
	decoded, err := decodeLocalPacket(template)
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(template, decoded, command, payload,
		localMessageRoute{Route: localPlayerProfileRoute, SectionID: localPlayerProfileSectionID}, nil)
}

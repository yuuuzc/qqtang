package game

import (
	"encoding/binary"
	"testing"
)

func TestMarriageProposalAndAnswerCodecs(t *testing.T) {
	word := []byte{0xBC, 0xDE, 0xB8, 0xF8, 0xCE, 0xD2, 0xB0, 0xC9} // 嫁给我吧
	payload := binary.BigEndian.AppendUint32(nil, 1_000_001)
	payload = binary.BigEndian.AppendUint32(payload, 1_000_002)
	payload = append(payload, byte(len(word)))
	payload = append(payload, word...)
	packet := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(RequestSparkCommand), payload...))
	request, err := DecodeLocalSparkRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.TargetUIN != 1_000_002 || request.Word != "嫁给我吧" {
		t.Fatalf("spark request = %+v", request)
	}
	notification, err := BuildLocalSparkNotification(packet, request, "糖一")
	if err != nil {
		t.Fatal(err)
	}
	inspect, err := InspectLocalPacket(notification)
	if err != nil {
		t.Fatal(err)
	}
	if inspect.Command != NotifySparkCommand || inspect.InnerSequence != 0 || binary.BigEndian.Uint32(inspect.Payload[4:8]) != 1_000_002 {
		t.Fatalf("spark notification = %+v", inspect)
	}

	answerPayload := binary.BigEndian.AppendUint32(nil, 1_000_002)
	answerPayload = binary.BigEndian.AppendUint32(answerPayload, 1_000_001)
	answerPayload = binary.BigEndian.AppendUint16(answerPayload, MarriageResultSuccess)
	answerPacket := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(AnswerSparkCommand), answerPayload...))
	// The responder (first field) owns this authenticated envelope. The
	// second field is the original proposer.
	binary.BigEndian.PutUint32(answerPacket[12:16], 1_000_002)
	answer, err := DecodeLocalAnswerSparkRequest(answerPacket)
	if err != nil {
		t.Fatal(err)
	}
	if answer.ResponderUIN != 1_000_002 || answer.ProposerUIN != 1_000_001 || answer.ResultID != MarriageResultSuccess {
		t.Fatalf("answer request = %+v", answer)
	}
	response, err := BuildLocalAnswerSparkResponse(answerPacket, MarriageResultSuccess, answer, "求婚成功")
	if err != nil {
		t.Fatal(err)
	}
	responseInspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(responseInspection.Payload[2:6]) != 1_000_001 || binary.BigEndian.Uint32(responseInspection.Payload[6:10]) != 1_000_002 {
		t.Fatalf("answer response UIN order = %x", responseInspection.Payload[:10])
	}
}

func TestMarriageInfoAndWeddingCodecsFollowRecoveredLayouts(t *testing.T) {
	requestPayload := binary.BigEndian.AppendUint32(nil, 1_000_001)
	requestPayload = binary.BigEndian.AppendUint32(requestPayload, 1_000_001)
	packet := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(MarriageInfoCommand), requestPayload...))
	response, err := BuildLocalMarriageInfoResponse(packet, MarriageResultSuccess, MarriageInfo{
		TargetUIN: 1_000_001, MarriageUIN: 1_000_002, MarriageLoyalty: 8,
		MarriageLevel: 2, LevelValue: 100, MarriageAge: 3, LoveWord: "永远在一起",
		Nickname: "糖一", SpouseNickname: "糖二", RingID: 9021,
	})
	if err != nil {
		t.Fatal(err)
	}
	inspect, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspect.Command != MarriageInfoCommand || binary.BigEndian.Uint32(inspect.Payload[6:10]) != 1_000_002 || binary.BigEndian.Uint16(inspect.Payload[14:16]) != 2 {
		t.Fatalf("marriage info response = %+v", inspect)
	}
	wordLength := int(binary.BigEndian.Uint16(inspect.Payload[24:26]))
	if len(inspect.Payload) != 26+wordLength+20+20+4 {
		t.Fatalf("marriage info compact payload length = %d, word=%d", len(inspect.Payload), wordLength)
	}

	weddingPayload := binary.BigEndian.AppendUint32(nil, 1_000_001)
	weddingPayload = binary.BigEndian.AppendUint32(weddingPayload, 1_000_002)
	weddingPayload = binary.BigEndian.AppendUint16(weddingPayload, 1)
	weddingPacket := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(StartWeddingCommand), weddingPayload...))
	wedding, err := DecodeLocalStartWeddingRequest(weddingPacket)
	if err != nil || wedding.WeddingModeID != 1 {
		t.Fatalf("start wedding = %+v, %v", wedding, err)
	}
	confirm, err := BuildLocalWeddingConfirmNotification(weddingPacket, wedding, "糖一", "糖二", "请确认婚礼")
	if err != nil {
		t.Fatal(err)
	}
	confirmInspect, err := InspectLocalPacket(confirm)
	if err != nil {
		t.Fatal(err)
	}
	if confirmInspect.Command != WeddingConfirmNotifyCommand || len(confirmInspect.Payload) < 49 {
		t.Fatalf("wedding confirmation = %+v", confirmInspect)
	}
}

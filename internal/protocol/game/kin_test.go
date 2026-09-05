package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestKinCreateAndBaseCodecsUseCountedLegacyText(t *testing.T) {
	declaration := []byte{0xCE, 0xD2, 0xB0, 0xAE, 0xCE, 0xD2, 0xBC, 0xD2} // 我爱我家
	payload := make([]byte, 33+len(declaration)+4)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint32(payload[4:8], 123)
	binary.BigEndian.PutUint16(payload[14:16], uint16(len(declaration)))
	copy(payload[16:33], []byte{0xCC, 0xC7, 0xBC, 0xD2, 0xD7, 0xE5}) // 糖家族
	copy(payload[33:], declaration)
	binary.BigEndian.PutUint32(payload[33+len(declaration):], 7)
	packet := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(CreateKinCommand), payload...))
	request, err := DecodeLocalCreateKinRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.Name != "糖家族" || request.Declaration != "我爱我家" || request.Section != 7 {
		t.Fatalf("create kin request = %+v", request)
	}

	baseResponse, err := BuildLocalFetchKinBaseResponse(
		makeLocalPacketForTest(t, append(buildInnerHeaderForTest(FetchKinBaseCommand), make([]byte, 16)...)),
		1_000_001,
		KinBase{Index: 9, OwnerUIN: 1_000_001, CreatedUnix: 100, Grade: 1, MemberCount: 2, Name: "糖家族", Declaration: "我爱我家", Notification: "欢迎", BaseUpdate: 1, ListUpdate: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(baseResponse)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != FetchKinBaseCommand || binary.BigEndian.Uint32(inspection.Payload[0:4]) != 1_000_001 || binary.BigEndian.Uint32(inspection.Payload[4:8]) != 9 {
		t.Fatalf("kin base response = %+v", inspection)
	}
	if len(inspection.Payload) >= 4+758 {
		t.Fatalf("kin base response used maximum decoded image instead of compact wire data: %d", len(inspection.Payload))
	}
}

func TestKinBaseEmptyNotificationKeepsNativeComparisonSentinel(t *testing.T) {
	baseResponse, err := BuildLocalFetchKinBaseResponse(
		makeLocalPacketForTest(t, append(buildInnerHeaderForTest(FetchKinBaseCommand), make([]byte, 16)...)),
		1_000_001,
		KinBase{Index: 9, OwnerUIN: 1_000_001, Name: "糖家族"},
	)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(baseResponse)
	if err != nil {
		t.Fatal(err)
	}
	// Four response UIN bytes, 38 fixed KinBase bytes, the fixed 17-byte
	// family-name slot, no declaration/title bytes, then three uint32 fields.
	notificationLengthOffset := 4 + 38 + KinNameSlotSize + 12
	if got := binary.BigEndian.Uint16(inspection.Payload[notificationLengthOffset : notificationLengthOffset+2]); got != 1 {
		t.Fatalf("empty notification wire length = %d, want 1", got)
	}
	if got := inspection.Payload[notificationLengthOffset+2]; got != 0 {
		t.Fatalf("empty notification sentinel = 0x%02X, want NUL", got)
	}
}

func TestKinMemberAndChatNotificationsAreCompact(t *testing.T) {
	requestPacket := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(FetchKinMemberListCommand), make([]byte, 16)...))
	response, err := BuildLocalFetchKinMembersResponse(requestPacket, 1_000_001, 3, 0, []KinMemberOld{{UIN: 1_000_001, Nickname: "糖一", Status: 2, Honor: 7, ActivePoint: 8}})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.BigEndian.Uint16(inspection.Payload[10:12]); got != 1 {
		t.Fatalf("member count = %d", got)
	}
	if got := binary.BigEndian.Uint32(inspection.Payload[len(inspection.Payload)-8 : len(inspection.Payload)-4]); got != 7 {
		t.Fatalf("member honor = %d", got)
	}

	template := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(KinChatCommand), make([]byte, 16)...))
	notification, err := BuildLocalKinChatNotification(template, KinChatRequest{UIN: 1_000_001, KinIndex: 9, Message: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := InspectLocalPacket(notification)
	if err != nil {
		t.Fatal(err)
	}
	if chat.Command != KinChatNotifyCommand || chat.Route != localPlayerProfileRoute || chat.InnerSequence != 0 || binary.BigEndian.Uint32(chat.Payload[8:12]) != 4 {
		t.Fatalf("kin chat notification = %+v", chat)
	}
}

func TestKinAnnouncementEventNotificationUsesNativeEventNine(t *testing.T) {
	template := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(SetKinNotificationCommand), make([]byte, 14)...))
	packet, err := BuildLocalKinEventNotification(template, KinEventNotification{
		EventType:   KinEventNotificationUpdateAnnouncement,
		KinIndex:    9,
		UIN:         1_000_001,
		Nickname:    "糖一",
		Description: "测试公告",
	})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != KinEventNotifyCommand || inspection.Route != localPlayerProfileRoute {
		t.Fatalf("kin event envelope = %+v", inspection)
	}
	if got := binary.BigEndian.Uint32(inspection.Payload[0:4]); got != KinEventNotificationUpdateAnnouncement {
		t.Fatalf("kin event type = %d", got)
	}
	if got := binary.BigEndian.Uint32(inspection.Payload[4:8]); got != 9 {
		t.Fatalf("kin event index = %d", got)
	}
	nameLength := int(binary.BigEndian.Uint16(inspection.Payload[12:14]))
	attachOffset := 14 + nameLength
	descriptionLengthOffset := attachOffset + 4 + 2
	descriptionLength := int(binary.BigEndian.Uint16(inspection.Payload[descriptionLengthOffset : descriptionLengthOffset+2]))
	if descriptionLength == 0 || descriptionLengthOffset+2+descriptionLength != len(inspection.Payload) {
		t.Fatalf("kin event description layout length=%d payload=%x", descriptionLength, inspection.Payload)
	}
}

func TestKinAuthorityTitleAndTopRankingCodecs(t *testing.T) {
	logicalTitle := "族长\x07长老\x07堂主\x07护法\x07弟子\x07"
	title := []byte{
		'5', ' ',
		'1', '2', ' ', '5', ' ', 0xD7, 0xE5, 0xB3, 0xA4, ' ',
		'1', '0', ' ', '5', ' ', 0xB3, 0xA4, 0xC0, 0xCF, ' ',
		'8', ' ', '5', ' ', 0xCC, 0xC3, 0xD6, 0xF7, ' ',
		'6', ' ', '5', ' ', 0xBB, 0xA4, 0xB7, 0xA8, ' ',
		'4', ' ', '5', ' ', 0xB5, 0xDC, 0xD7, 0xD3, ' ',
	}
	payload := make([]byte, 14+len(title))
	binary.BigEndian.PutUint32(payload[0:4], 9)
	binary.BigEndian.PutUint32(payload[4:8], 1_000_001)
	binary.BigEndian.PutUint32(payload[8:12], 123)
	binary.BigEndian.PutUint16(payload[12:14], uint16(len(title)))
	copy(payload[14:], title)
	requestPacket := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(SetKinAuthorityCommand), payload...))
	request, err := DecodeLocalSetKinAuthorityRequest(requestPacket)
	if err != nil || request.KinIndex != 9 || request.UIN != 1_000_001 || request.Text != logicalTitle {
		t.Fatalf("set kin authority title = %+v, %v", request, err)
	}
	response, err := BuildLocalKinTextResponse(requestPacket, SetKinAuthorityCommand, 0, request, "")
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != SetKinAuthorityCommand || binary.BigEndian.Uint32(inspection.Payload[2:6]) != 9 ||
		binary.BigEndian.Uint16(inspection.Payload[6:8]) != uint16(len(title)) || !bytes.Equal(inspection.Payload[8:8+len(title)], title) {
		t.Fatalf("set kin authority response = %+v", inspection)
	}
	baseResponse, err := BuildLocalFetchKinBaseResponse(
		makeLocalPacketForTest(t, append(buildInnerHeaderForTest(FetchKinBaseCommand), make([]byte, 16)...)),
		1_000_001, KinBase{Index: 9, OwnerUIN: 1_000_001, Name: "糖家族", Title: logicalTitle},
	)
	if err != nil {
		t.Fatal(err)
	}
	base, err := InspectLocalPacket(baseResponse)
	if err != nil {
		t.Fatal(err)
	}
	titleLengthOffset := 4 + 36
	titleOffset := 4 + 38 + KinNameSlotSize
	if binary.BigEndian.Uint16(base.Payload[titleLengthOffset:titleLengthOffset+2]) != uint16(len(title)) ||
		!bytes.Equal(base.Payload[titleOffset:titleOffset+len(title)], title) {
		t.Fatalf("kin base title object = %x", base.Payload[titleOffset:titleOffset+len(title)])
	}

	topRequestPayload := make([]byte, 24)
	binary.BigEndian.PutUint32(topRequestPayload[0:4], 1_000_001)
	topPacket := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(FetchKinTopCommand), topRequestPayload...))
	topResponse, err := BuildLocalFetchKinTopResponse(topPacket, 0, 456, 1_000_001,
		[]KinOrderInfo{{KinIndex: 9, Name: "糖家族", Value: 100, Order: 1, BeforeOrder: 1}},
		[]KinOrderInfo{{KinIndex: 9, Name: "糖家族", Value: 200, Order: 1, BeforeOrder: 1}})
	if err != nil {
		t.Fatal(err)
	}
	top, err := InspectLocalPacket(topResponse)
	if err != nil {
		t.Fatal(err)
	}
	if top.Command != FetchKinTopCommand || binary.BigEndian.Uint32(top.Payload[2:6]) != 456 || binary.BigEndian.Uint32(top.Payload[6:10]) != 1_000_001 {
		t.Fatalf("kin top response = %+v", top)
	}
	// Fixed twenty-entry honor and activity arrays must remain present even
	// when only one actual family exists. Compact names keep the payload below
	// the schema's 1278-byte decoded maximum.
	if len(top.Payload) < 10+4+40*13+4 || len(top.Payload) >= 1278 {
		t.Fatalf("kin top compact payload length = %d", len(top.Payload))
	}
}

func TestKinDeclarationResponsesUseNativeServerCommands(t *testing.T) {
	request := KinTextRequest{UIN: 1_000_001, Time: 123, KinIndex: 9, Text: "欢迎回家"}
	for _, test := range []struct {
		requestCommand  uint16
		responseCommand uint16
		indexFirst      bool
	}{
		{SetKinDeclarationCommand, SetKinDeclarationResponseCommand, false},
		{SetKinNotificationCommand, SetKinNotificationResponseCommand, true},
	} {
		text := []byte{0xBB, 0xB6, 0xD3, 0xAD, 0xBB, 0xD8, 0xBC, 0xD2}
		payload := make([]byte, 14+len(text))
		if test.indexFirst {
			binary.BigEndian.PutUint32(payload[0:4], request.KinIndex)
			binary.BigEndian.PutUint32(payload[4:8], request.UIN)
			binary.BigEndian.PutUint32(payload[8:12], request.Time)
		} else {
			binary.BigEndian.PutUint32(payload[0:4], request.UIN)
			binary.BigEndian.PutUint32(payload[4:8], request.Time)
			binary.BigEndian.PutUint32(payload[8:12], request.KinIndex)
		}
		binary.BigEndian.PutUint16(payload[12:14], uint16(len(text)))
		copy(payload[14:], text)
		packet := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(test.requestCommand), payload...))
		response, err := BuildLocalKinTextResponse(packet, test.requestCommand, 0, request, "")
		if err != nil {
			t.Fatal(err)
		}
		inspection, err := InspectLocalPacket(response)
		if err != nil {
			t.Fatal(err)
		}
		if inspection.Command != test.responseCommand {
			t.Fatalf("request 0x%04X response command = 0x%04X, want 0x%04X", test.requestCommand, inspection.Command, test.responseCommand)
		}
	}
}

func TestKinNotificationUsesNativeBidirectionalCommandAndAcceptsLegacyAlias(t *testing.T) {
	text := []byte{0xB2, 0xE2, 0xCA, 0xD4, 0xB9, 0xAB, 0xB8, 0xE6}
	for _, command := range []uint16{SetKinNotificationCommand, LegacySetKinNotificationCommand} {
		payload := make([]byte, 14+len(text))
		binary.BigEndian.PutUint32(payload[0:4], 9)
		binary.BigEndian.PutUint32(payload[4:8], 1_000_001)
		binary.BigEndian.PutUint32(payload[8:12], 123)
		binary.BigEndian.PutUint16(payload[12:14], uint16(len(text)))
		copy(payload[14:], text)
		packet := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(command), payload...))
		request, err := DecodeLocalSetKinNotificationRequest(packet)
		if err != nil {
			t.Fatalf("decode command 0x%04X: %v", command, err)
		}
		response, err := BuildLocalKinTextResponse(packet, command, 0, request, "")
		if err != nil {
			t.Fatalf("response command 0x%04X: %v", command, err)
		}
		inspection, err := InspectLocalPacket(response)
		if err != nil {
			t.Fatal(err)
		}
		if inspection.Command != SetKinNotificationCommand {
			t.Fatalf("request command 0x%04X response = 0x%04X, want native 0x%04X", command, inspection.Command, SetKinNotificationCommand)
		}
	}
}

func TestKinSessionRestoreUsesSilentNativeCreateSuccess(t *testing.T) {
	login := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(LoginCommand), make([]byte, 16)...))
	packet, err := BuildLocalKinSessionRestore(login, 1_000_001, 9)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != CreateKinCommand || binary.BigEndian.Uint32(inspection.Payload[0:4]) != 1_000_001 || binary.BigEndian.Uint32(inspection.Payload[4:8]) != 9 || binary.BigEndian.Uint32(inspection.Payload[8:12]) != 0 || binary.BigEndian.Uint16(inspection.Payload[12:14]) != 0 {
		t.Fatalf("kin session restore = %+v payload=%X", inspection, inspection.Payload)
	}
}

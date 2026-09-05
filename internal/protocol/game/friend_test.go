package game

import (
	"encoding/binary"
	"testing"
)

func TestFriendRequestCodecsAndDirectedResultLayouts(t *testing.T) {
	listPayload := binary.BigEndian.AppendUint32(nil, 1_000_001)
	listPayload = binary.BigEndian.AppendUint32(listPayload, 77)
	listPacket := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(FriendListCommand), listPayload...))
	listRequest, err := DecodeLocalFriendListRequest(listPacket)
	if err != nil {
		t.Fatal(err)
	}
	listResponse, err := BuildLocalFriendListResponse(listPacket, FriendResultSuccess, listRequest, []uint32{1_000_002, 1_000_003})
	if err != nil {
		t.Fatal(err)
	}
	listInspect, err := InspectLocalPacket(listResponse)
	if err != nil {
		t.Fatal(err)
	}
	if listInspect.Command != FriendListCommand || len(listInspect.Payload) != FriendListPayloadSize || binary.BigEndian.Uint16(listInspect.Payload[8:10]) != 2 {
		t.Fatalf("friend list response=%+v payload=%x", listInspect, listInspect.Payload)
	}
	if binary.BigEndian.Uint32(listInspect.Payload[10:14]) != 1_000_002 || binary.BigEndian.Uint32(listInspect.Payload[14:18]) != 1_000_003 || binary.BigEndian.Uint32(listInspect.Payload[18:22]) != 0 {
		t.Fatalf("friend list fixed slots=%x", listInspect.Payload[10:22])
	}

	word := []byte{0xC4, 0xE3, 0xBA, 0xC3} // 你好
	addPayload := binary.BigEndian.AppendUint32(nil, 1_000_001)
	addPayload = binary.BigEndian.AppendUint32(addPayload, 88)
	addPayload = binary.BigEndian.AppendUint32(addPayload, 1_000_002)
	addPayload = binary.BigEndian.AppendUint16(addPayload, uint16(len(word)))
	addPayload = append(addPayload, word...)
	addPacket := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(AddFriendCommand), addPayload...))
	add, err := DecodeLocalAddFriendRequest(addPacket)
	if err != nil || add.Word != "你好" || add.TargetUIN != 1_000_002 {
		t.Fatalf("add request=%+v err=%v", add, err)
	}
	resultPacket, err := BuildLocalFriendAnswerResult(addPacket, FriendResultRejected, add.UIN, add.Time, add.TargetUIN, false)
	if err != nil {
		t.Fatal(err)
	}
	resultInspect, _ := InspectLocalPacket(resultPacket)
	if resultInspect.Command != AnswerFriendCommand || len(resultInspect.Payload) != 14 || binary.BigEndian.Uint16(resultInspect.Payload[:2]) != FriendResultRejected {
		t.Fatalf("answer result=%+v payload=%x", resultInspect, resultInspect.Payload)
	}
}

func TestAddFriendRequestAcceptsZeroPaddedMaximumSlot(t *testing.T) {
	word := []byte("hello")
	payload := binary.BigEndian.AppendUint32(nil, 1_000_001)
	payload = binary.BigEndian.AppendUint32(payload, 88)
	payload = binary.BigEndian.AppendUint32(payload, 1_000_002)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(word)))
	payload = append(payload, word...)
	payload = append(payload, make([]byte, FriendRequestWordMaximum-len(word))...)
	packet := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(AddFriendCommand), payload...))
	request, err := DecodeLocalAddFriendRequest(packet)
	if err != nil || request.Word != "hello" {
		t.Fatalf("padded add request=%+v err=%v", request, err)
	}
}

func TestAnswerFriendRequestAuthenticatesResponderField(t *testing.T) {
	payload := binary.BigEndian.AppendUint16(nil, FriendResultSuccess)
	payload = binary.BigEndian.AppendUint32(payload, 1_000_002) // original requester
	payload = binary.BigEndian.AppendUint32(payload, 99)
	payload = binary.BigEndian.AppendUint32(payload, 1_000_001) // authenticated responder
	packet := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(AnswerFriendCommand), payload...))
	request, err := DecodeLocalAnswerFriendRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.RequesterUIN != 1_000_002 || request.ResponderUIN != 1_000_001 {
		t.Fatalf("answer friend request = %+v", request)
	}
}

func TestFriendUpdateUsesFixedThirtyItemStorage(t *testing.T) {
	update := FriendUpdate{
		UIN: 1_000_001, FriendUIN: 1_000_002, Point: 1234,
		ExtItemIDs: []uint32{19, 244, 12087}, Online: true,
		Nickname: "糖二", Gender: 1, Identity: uint32(IdentityPurpleDiamond), ExtPoint: 5200,
	}
	payload, err := update.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != FriendUpdatePayloadSize || payload[12] != 3 || payload[133] != 1 || binary.BigEndian.Uint32(payload[159:163]) != 5200 {
		t.Fatalf("friend update len=%d count=%d online=%d tail=%x", len(payload), payload[12], payload[133], payload[159:])
	}
	if binary.BigEndian.Uint32(payload[13:17]) != 19 || binary.BigEndian.Uint32(payload[21:25]) != 12087 || binary.BigEndian.Uint32(payload[25:29]) != 0 {
		t.Fatalf("friend external item slots=%x", payload[13:29])
	}

	templatePayload := binary.BigEndian.AppendUint32(nil, 1_000_001)
	templatePayload = binary.BigEndian.AppendUint32(templatePayload, 1)
	template := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(FriendListCommand), templatePayload...))
	notification, err := BuildLocalFriendUpdateNotification(template, update)
	if err != nil {
		t.Fatal(err)
	}
	inspect, err := InspectLocalPacket(notification)
	if err != nil {
		t.Fatal(err)
	}
	if inspect.Command != FriendUpdateNotifyCommand || inspect.Route != localPlayerProfileRoute || inspect.InnerSequence != 0 || len(inspect.Payload) != FriendUpdatePayloadSize {
		t.Fatalf("friend notification=%+v", inspect)
	}
}

func TestFriendListNotificationRestoresRosterBeforeEntryUpdates(t *testing.T) {
	templatePayload := binary.BigEndian.AppendUint32(nil, 1_000_001)
	templatePayload = binary.BigEndian.AppendUint32(templatePayload, 1)
	template := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(LoginCommand), templatePayload...))
	notification, err := BuildLocalFriendListNotification(template, 1_000_001, []uint32{1_000_002})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(notification)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != FriendListCommand || inspection.Route != localPlayerProfileRoute ||
		inspection.InnerSequence != 0 || len(inspection.Payload) != FriendListPayloadSize ||
		binary.BigEndian.Uint32(inspection.Payload[2:6]) != 1_000_001 ||
		binary.BigEndian.Uint16(inspection.Payload[8:10]) != 1 ||
		binary.BigEndian.Uint32(inspection.Payload[10:14]) != 1_000_002 {
		t.Fatalf("friend roster notification=%+v payload=%x", inspection, inspection.Payload)
	}
}

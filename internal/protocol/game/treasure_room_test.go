package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestTreasureRoomResponseRoundTrip(t *testing.T) {
	want := TreasureRoomResponse{
		MapID: TreasureRoomMapID,
		Items: []ItemInfo{
			NewPermanentItemInfo(453, 1),
			NewPermanentItemInfo(30067, 4),
		},
	}
	payload, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseTreasureRoomResponse(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got.ResultID != want.ResultID || got.MapID != want.MapID || len(got.Items) != len(want.Items) {
		t.Fatalf("treasure response = %+v, want %+v", got, want)
	}
	for index := range want.Items {
		if got.Items[index] != want.Items[index] {
			t.Fatalf("treasure item[%d] = %+v, want %+v", index, got.Items[index], want.Items[index])
		}
	}
}

func TestLocalTreasureCommandsPreserveRequestRoute(t *testing.T) {
	requestPayload := binary.BigEndian.AppendUint32(nil, 1_000_001)
	requestPayload = binary.BigEndian.AppendUint32(requestPayload, 321)
	requestPlaintext := make([]byte, localInnerHeaderSize, localInnerHeaderSize+len(requestPayload))
	binary.BigEndian.PutUint16(requestPlaintext[0:2], EnterTreasureCommand)
	binary.BigEndian.PutUint16(requestPlaintext[8:10], 13)
	binary.BigEndian.PutUint16(requestPlaintext[10:12], 0xffff)
	binary.BigEndian.PutUint16(requestPlaintext[12:14], 1)
	requestPacket := makeLocalPacketForTest(t, append(requestPlaintext, requestPayload...))
	response, err := BuildLocalTreasureRoomResponseWithReader(requestPacket, TreasureRoomResponse{MapID: TreasureRoomMapID}, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Command != EnterTreasureCommand || decoded.Route != 13 || decoded.SectionID != 1 {
		t.Fatalf("treasure response route = command:0x%04X route:%d section:%d", decoded.Command, decoded.Route, decoded.SectionID)
	}
	parsed, err := ParseTreasureRoomResponse(decoded.Payload)
	if err != nil || parsed.MapID != TreasureRoomMapID {
		t.Fatalf("treasure response = %+v err:%v", parsed, err)
	}

	itemPayload := binary.BigEndian.AppendUint32(nil, 1_000_001)
	itemPayload = binary.BigEndian.AppendUint32(itemPayload, 322)
	itemPayload = binary.BigEndian.AppendUint32(itemPayload, 453)
	itemPlaintext := append([]byte(nil), requestPlaintext...)
	binary.BigEndian.PutUint16(itemPlaintext[0:2], GetTreasureItemCommand)
	itemPacket := makeLocalPacketForTest(t, append(itemPlaintext, itemPayload...))
	itemResponse, err := BuildLocalTreasureItemResponseWithReader(itemPacket, TreasureItemResponse{}, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	decodedItem, err := InspectLocalPacket(itemResponse)
	if err != nil {
		t.Fatal(err)
	}
	if decodedItem.Command != GetTreasureItemCommand || len(decodedItem.Plaintext) != localInnerHeaderSize+2 {
		t.Fatalf("treasure item response = command:0x%04X plaintext:%d", decodedItem.Command, len(decodedItem.Plaintext))
	}
}

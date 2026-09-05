package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestCombineForgeUsesCompactCountedWireFields(t *testing.T) {
	const (
		uin              = 1_000_001
		medicalKitItemID = 20_050
	)
	materials := []uint32{30_057, 30_045, 30_008}
	payload := make([]byte, 0, combineForgeRequestFixedSize+4*len(materials))
	payload = binary.BigEndian.AppendUint32(payload, uin)
	payload = binary.BigEndian.AppendUint32(payload, 0x2a5228bd)
	payload = append(payload, 0)
	payload = binary.BigEndian.AppendUint32(payload, medicalKitItemID)
	payload = binary.BigEndian.AppendUint32(payload, 0)
	payload = binary.BigEndian.AppendUint16(payload, uint16(len(materials)))
	for _, itemID := range materials {
		payload = binary.BigEndian.AppendUint32(payload, itemID)
	}
	plaintext := append(buildInnerHeaderForTest(CombineForgeCommand), payload...)
	requestPacket := makeLocalPacketForTest(t, plaintext)

	request, err := DecodeLocalCombineForgeRequest(requestPacket)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != uin || request.ItemID != medicalKitItemID || request.FromItemID != 0 || !uint32SlicesEqual(request.MaterialIDs, materials) {
		t.Fatalf("decoded combine request = %+v", request)
	}

	responsePacket, err := BuildLocalCombineForgeResponseWithReader(requestPacket, CombineForgeResponse{
		Type: request.Type, ItemID: request.ItemID, FromItemID: request.FromItemID,
		MaterialIDs: request.MaterialIDs, Reason: "合成成功",
	}, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(responsePacket)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != CombineForgeCommand {
		t.Fatalf("response command = 0x%04X", inspection.Command)
	}
	reason, err := encodeLegacyGBKText("合成成功", craftReasonMaximum)
	if err != nil {
		t.Fatal(err)
	}
	wantLength := combineForgeResponseFixedSize + 4*len(materials) + len(reason)
	if len(inspection.Payload) != wantLength {
		t.Fatalf("response payload length = %d, want compact length %d", len(inspection.Payload), wantLength)
	}
	if got := binary.BigEndian.Uint16(inspection.Payload[11:13]); got != uint16(len(materials)) {
		t.Fatalf("response material count = %d", got)
	}
	reasonOffset := 13 + 4*len(materials)
	if got := int(binary.BigEndian.Uint16(inspection.Payload[reasonOffset : reasonOffset+2])); got != len(reason) {
		t.Fatalf("response reason length = %d, want %d", got, len(reason))
	}
}

func TestForgeResponseCarriesGBKSuccessTextAndIndependentEffectColor(t *testing.T) {
	const uin = 1_000_001
	payload := make([]byte, 0, forgeRequestSize)
	payload = binary.BigEndian.AppendUint32(payload, uin)
	payload = binary.BigEndian.AppendUint32(payload, 0x12345678)
	payload = append(payload, 1)
	payload = binary.BigEndian.AppendUint32(payload, 300)
	payload = binary.BigEndian.AppendUint32(payload, 20057)
	request := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(ForgeCommand), payload...))

	packet, err := BuildLocalForgeResponseWithReader(request, ForgeResponse{
		Type: 1, ItemID: 300, MaterialID: 20057, Effect: 5, Color: 3, Reason: "锻造成功",
	}, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	want, err := encodeLegacyGBKText("锻造成功", craftReasonMaximum-1)
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, 0)
	if inspection.Command != ForgeCommand || len(inspection.Payload) != forgeResponseHeaderSize+len(want) {
		t.Fatalf("forge response command/size = 0x%04X/%d", inspection.Command, len(inspection.Payload))
	}
	if inspection.Payload[11] != 5 || inspection.Payload[12] != 3 {
		t.Fatalf("forge effect/color = %d/%d", inspection.Payload[11], inspection.Payload[12])
	}
	length := int(binary.BigEndian.Uint16(inspection.Payload[13:15]))
	if length != len(want) || !bytes.Equal(inspection.Payload[15:15+length], want) {
		t.Fatalf("forge reason bytes = % X, want % X", inspection.Payload[15:15+length], want)
	}
	if want[length-1] != 0 {
		t.Fatal("forge reason is not NUL-terminated inside its counted field")
	}
	if len(inspection.Payload) != 15+length {
		t.Fatalf("forge response contains %d bytes after counted reason", len(inspection.Payload)-(15+length))
	}
}

func uint32SlicesEqual(left, right []uint32) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestItemStatusChangeCapturedQuickSlotVector(t *testing.T) {
	// Captured 2026-08-07 14:58:35 UTC after placing large stamina potion
	// 20043 in the first local shortcut slot.
	payload := []byte{
		0x00, 0x0f, 0x42, 0x41, 0x05, 0xbd, 0x71, 0x6d, 0x00, 0x01,
		0x00, 0x00, 0x4e, 0x4b, 0x01, 0x00,
	}
	plaintext := make([]byte, localInnerHeaderSize)
	binary.BigEndian.PutUint16(plaintext[0:2], ItemStatusChangeCommand)
	packet := makeLocalPacketForTest(t, append(plaintext, payload...))

	request, err := DecodeLocalItemStatusChangeRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.ClientTime != 0x05BD716D || len(request.Items) != 1 {
		t.Fatalf("decoded item-status-change request = %+v", request)
	}
	if got := request.Items[0]; got != (ItemStatusChange{ItemID: LargeStaminaPotionItemID, NewStatus: 1}) {
		t.Fatalf("decoded item change = %+v", got)
	}

	response, err := BuildLocalItemStatusChangeSuccessWithReader(packet, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != ItemStatusChangeCommand || !bytes.Equal(inspection.Payload, []byte{0, 0}) {
		t.Fatalf("item-status-change response = 0x%04X/%x", inspection.Command, inspection.Payload)
	}
}

func TestItemStatusChangeRejectsCountLengthMismatch(t *testing.T) {
	payload := make([]byte, itemStatusChangeHeaderSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint16(payload[8:10], 1)
	plaintext := make([]byte, localInnerHeaderSize)
	binary.BigEndian.PutUint16(plaintext[0:2], ItemStatusChangeCommand)
	if _, err := DecodeLocalItemStatusChangeRequest(makeLocalPacketForTest(t, append(plaintext, payload...))); err == nil {
		t.Fatal("expected missing ITEM_CHANGE_INFO to be rejected")
	}
}

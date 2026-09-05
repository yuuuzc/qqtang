package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBreakEggRequestAndResponseContract(t *testing.T) {
	if BreakEggRequestCommand != 0x00FE || BreakEggResponseCommand != 0x00FE {
		t.Fatalf("break-egg transport commands = request 0x%04X response 0x%04X", BreakEggRequestCommand, BreakEggResponseCommand)
	}
	payload := make([]byte, breakEggRequestSize)
	binary.BigEndian.PutUint32(payload[0:4], 1_000_001)
	binary.BigEndian.PutUint32(payload[4:8], 1234)
	binary.BigEndian.PutUint32(payload[8:12], 9013)
	binary.BigEndian.PutUint32(payload[12:16], 9003)
	plaintext := append(buildInnerHeaderForTest(BreakEggRequestCommand), payload...)
	packet := makeLocalPacketForTest(t, plaintext)

	request, err := DecodeLocalBreakEggRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1_000_001 || request.ClientTime != 1234 || request.HammerID != 9013 || request.EggID != 9003 {
		t.Fatalf("decoded request = %+v", request)
	}
	response, err := BuildLocalBreakEggResponseWithReader(packet, BreakEggResponse{
		ShowItemID: 468, AttachInfo: "获得少林金刚脚",
	}, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != BreakEggResponseCommand || binary.BigEndian.Uint16(inspection.Payload[0:2]) != 0 || binary.BigEndian.Uint32(inspection.Payload[2:6]) != 468 {
		t.Fatalf("response inspection = %+v payload=%x", inspection, inspection.Payload)
	}
	if length := int(inspection.Payload[6]); length == 0 || len(inspection.Payload) != 7+length {
		t.Fatalf("counted attach info payload = %x", inspection.Payload)
	}
}

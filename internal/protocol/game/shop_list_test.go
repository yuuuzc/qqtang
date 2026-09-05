package game

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestLocalShopListCurrentResponse(t *testing.T) {
	payload := make([]byte, 20)
	binary.BigEndian.PutUint32(payload[0:4], 1000001)
	binary.BigEndian.PutUint32(payload[4:8], 0x0F836A01)
	binary.BigEndian.PutUint32(payload[8:12], 0x004F5D31)
	binary.BigEndian.PutUint32(payload[12:16], 0x06B40CC0)
	requestPacket := makeLocalPacketForTest(t, append(buildInnerHeaderForTest(ShopListCommand), payload...))

	request, err := DecodeLocalShopListRequest(requestPacket)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1000001 || request.ClientVersion != 0x004F5D31 || request.ShopListVersion != 0 {
		t.Fatalf("decoded request = %+v", request)
	}

	response, err := BuildLocalShopListCurrentWithReader(requestPacket, 848, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != ShopListCommand || len(inspection.Payload) != 19 {
		t.Fatalf("response command/payload = 0x%04X/%x", inspection.Command, inspection.Payload)
	}
	if got := binary.BigEndian.Uint16(inspection.Payload[2:4]); got != ShopListDownloadCurrent {
		t.Fatalf("download result = %d", got)
	}
	if got := binary.BigEndian.Uint32(inspection.Payload[4:8]); got != 848 {
		t.Fatalf("latest version = %d", got)
	}
}

func buildInnerHeaderForTest(command uint16) []byte {
	header := make([]byte, localInnerHeaderSize)
	binary.BigEndian.PutUint16(header[0:2], command)
	binary.BigEndian.PutUint16(header[6:8], 1)
	binary.BigEndian.PutUint16(header[8:10], 1)
	binary.BigEndian.PutUint16(header[10:12], 0xFFFF)
	binary.BigEndian.PutUint16(header[12:14], 1)
	return header
}

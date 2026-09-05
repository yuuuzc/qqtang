package directory

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"net/netip"
	"testing"
)

func TestBuildLocalHallNative(t *testing.T) {
	config := DefaultLocalHallNativeConfig()
	buffer, err := BuildLocalHallNative(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(buffer) != HallNativeSize {
		t.Fatalf("length = 0x%X, want 0x%X", len(buffer), HallNativeSize)
	}
	if got := binary.LittleEndian.Uint32(buffer[2:6]); got != 0x004F5D31 {
		t.Fatalf("version = 0x%08X", got)
	}
	if buffer[0x0A] != 1 || buffer[0x523] != 1 || buffer[0x1064] != 1 {
		t.Fatalf("counts location=%d server=%d channel=%d", buffer[0x0A], buffer[0x523], buffer[0x1064])
	}
	if got := binary.LittleEndian.Uint32(buffer[0x528:0x52C]); got != 0x0100007F {
		t.Fatalf("server IP integer = 0x%08X", got)
	}
	if got := buffer[0x528:0x52C]; !bytes.Equal(got, []byte{127, 0, 0, 1}) {
		t.Fatalf("server IP native bytes = % X", got)
	}
	if got := buffer[0x0E : 0x0E+8]; !bytes.Equal(got, []byte("\xB6\xAB\xB2\xBF\xB5\xE7\xD0\xC5")) {
		t.Fatalf("official location name = % X", got)
	}
	if got := buffer[0x1065 : 0x1065+8]; !bytes.Equal(got, []byte("\xD7\xD4\xD3\xC9\xC6\xB5\xB5\xC0")) {
		t.Fatalf("official channel name = % X", got)
	}
	section := buffer[0x1065+0x1A:]
	if got := section[1:8]; !bytes.Equal(got, []byte("\xD7\xD4\xD3\xC9\x31\xC7\xF8")) {
		t.Fatalf("official section name = % X", got)
	}
	if got := binary.LittleEndian.Uint16(section[0x20:0x22]); got != 100 {
		t.Fatalf("current players = %d", got)
	}
	if got := binary.LittleEndian.Uint16(section[0x2A:0x2C]); got != 0x32E9 {
		t.Fatalf("location ID = 0x%04X", got)
	}
}

func TestBuildLocalHallNativeRejectsOversizedName(t *testing.T) {
	config := DefaultLocalHallNativeConfig()
	config.SectionName = "123456789012345678901234"
	if _, err := BuildLocalHallNative(config); err == nil {
		t.Fatal("expected oversized section name error")
	}
}

func TestBuildLocalHallAdvertisesDistinctShopServer(t *testing.T) {
	config := DefaultLocalHallNativeConfig()
	config.AdditionalServers = []LocalHallServerConfig{{
		ServerID: 3, ServerIP: netip.MustParseAddr("127.0.0.1"),
		ServerPort: 18002, ServerUDPPort: 18002,
	}}
	native, err := BuildLocalHallNative(config)
	if err != nil {
		t.Fatal(err)
	}
	if native[0x523] != 2 {
		t.Fatalf("native server count = %d", native[0x523])
	}
	second := native[0x530:0x53C]
	if id := binary.LittleEndian.Uint32(second[:4]); id != 3 {
		t.Fatalf("second native server ID = %d", id)
	}
	if port := binary.LittleEndian.Uint16(second[8:10]); port != 18002 {
		t.Fatalf("second native server port = %d", port)
	}

	payload, err := BuildLocalHallPayload(config)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != LocalHallPayloadSize+hallServerRecordSize || payload[0x1A] != 2 {
		t.Fatalf("two-server payload length/count = %d/%d", len(payload), payload[0x1A])
	}
	if id := binary.BigEndian.Uint32(payload[0x27:0x2B]); id != 3 {
		t.Fatalf("second payload server ID = %d", id)
	}
	if port := binary.BigEndian.Uint16(payload[0x2F:0x31]); port != 18002 {
		t.Fatalf("second payload server port = %d", port)
	}
}

func TestBuildLocalHallRejectsDuplicateServerID(t *testing.T) {
	config := DefaultLocalHallNativeConfig()
	config.AdditionalServers = []LocalHallServerConfig{{
		ServerID: config.ServerID, ServerIP: config.ServerIP,
		ServerPort: 18002, ServerUDPPort: 18002,
	}}
	if _, err := BuildLocalHallPayload(config); err == nil {
		t.Fatal("expected duplicate server ID error")
	}
}

func TestBuildLocalHallPayloadMatchesClientEncoderVector(t *testing.T) {
	payload, err := BuildLocalHallPayload(DefaultLocalHallNativeConfig())
	if err != nil {
		t.Fatal(err)
	}
	want, err := hex.DecodeString(
		"0000004f5d31000000010132e908b6ab" +
			"b2bfb5e7d0c500000000010000000101" +
			"00007f4650465001d7d4d3c9c6b5b5c0" +
			"00000000000000000000000000000001" +
			"000107d7d4d3c931c7f8000100000001" +
			"01f40064000000000000000032e90000" +
			"0000000000000000000000",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != LocalHallPayloadSize {
		t.Fatalf("payload length = %d, want %d", len(payload), LocalHallPayloadSize)
	}
	if !bytes.Equal(payload, want) {
		t.Fatalf("payload mismatch\n got: %x\nwant: %x", payload, want)
	}
	if got := payload[0x1F:0x23]; !bytes.Equal(got, []byte{1, 0, 0, 127}) {
		t.Fatalf("wire IP bytes = % X", got)
	}
}

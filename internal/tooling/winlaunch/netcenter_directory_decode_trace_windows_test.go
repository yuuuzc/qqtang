package winlaunch

import (
	"testing"

	"qqtang/internal/protocol/directory"
)

func TestDecodeDirectoryNativeCapture(t *testing.T) {
	native, err := directory.BuildLocalHallNative(directory.DefaultLocalHallNativeConfig())
	if err != nil {
		t.Fatal(err)
	}
	var capture NetCenterDirectoryDecodeCapture
	decodeDirectoryNativeCapture(
		&capture,
		native[:directoryNativePrefixLength],
		native[directoryNativeChannelOffset:directoryNativeChannelOffset+directoryNativeChannelLength],
	)
	if capture.Version != 0x004F5D31 || capture.LocationName != "\xB6\xAB\xB2\xBF\xB5\xE7\xD0\xC5" {
		t.Fatalf("version=0x%X location=%q", capture.Version, capture.LocationName)
	}
	if capture.ServerIP != "127.0.0.1" || capture.ServerPort != 18000 {
		t.Fatalf("server=%s:%d", capture.ServerIP, capture.ServerPort)
	}
	if capture.ChannelName != "\xD7\xD4\xD3\xC9\xC6\xB5\xB5\xC0" || capture.SectionName != "\xD7\xD4\xD3\xC9\x31\xC7\xF8" {
		t.Fatalf("channel=%q section=%q", capture.ChannelName, capture.SectionName)
	}
	if capture.MaxPlayers != 500 || capture.CurrentPlayers != 100 {
		t.Fatalf("players=%d/%d", capture.CurrentPlayers, capture.MaxPlayers)
	}
}

func TestBuildNetCenterDirectoryDecodeTraceStub(t *testing.T) {
	stub := buildNetCenterDirectoryDecodeTraceStub(0x10000000, 0x10000400, 0x20004664)
	if len(stub) < 80 || stub[0] != 0x9C || stub[1] != 0x60 {
		t.Fatalf("unexpected stub prefix/length: len=%d bytes=%X", len(stub), stub[:2])
	}
	if got := stub[len(stub)-5]; got != 0xE9 {
		t.Fatalf("final jump opcode = 0x%02X", got)
	}
}

package game

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestDecodeLocalConfigFileRequestFromShopEntry(t *testing.T) {
	packet, err := hex.DecodeString("0000008a0000000002ffffff000f42410120202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f9b2bbf783f6fba79c99695e83b292ceba21ff302086b93c6f0ed0c2dbb68e93de0c66d9a636dcca575750c281b52b80de4147c6722bf86f29a6857e47fb9e96fca8838023b1f2c97c6e0aacb1160d067f571bb8dc993b6ee")
	if err != nil {
		t.Fatal(err)
	}
	request, err := DecodeLocalConfigFileRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.UIN != 1000001 || len(request.Files) != 2 {
		t.Fatalf("decoded request = UIN %d, %d files", request.UIN, len(request.Files))
	}
	if request.Files[0].FileID != 5 || request.Files[0].FileVersion != 110 {
		t.Fatalf("first report = %+v", request.Files[0])
	}
	if request.Files[1].FileID != 7 || request.Files[1].FileVersion != 108 {
		t.Fatalf("second report = %+v", request.Files[1])
	}
}

func TestBuildLocalConfigFilesCurrent(t *testing.T) {
	packet, err := hex.DecodeString("00000072000000000300ffff000f42410120202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3fde71b5fc08b596f4b8841bcca8f0233e9647e2a8ebe500607b6b614c81c338a0b9461008986f2d3e8f7fa9260570130c9d2b1edf60d741449e38eb6b8bee1163")
	if err != nil {
		t.Fatal(err)
	}
	response, err := BuildLocalConfigFilesCurrentWithReader(packet, bytes.NewReader(make([]byte, 64)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(response)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != GetConfigFileCommand || inspection.Route != 2 || inspection.SectionID != 1 {
		t.Fatalf("response routing = command 0x%04X route %d section %d", inspection.Command, inspection.Route, inspection.SectionID)
	}
	if !bytes.Equal(inspection.Payload, []byte{0, 0, 0, 0}) {
		t.Fatalf("response payload = %x", inspection.Payload)
	}
}

package game

import (
	"bytes"
	"encoding/hex"
	"testing"

	"qqtang/internal/protocol/directory"
	"qqtang/internal/protocol/qqtea"
)

func TestDecodeLocalTransferUDPOKRequestCapture(t *testing.T) {
	packet, err := hex.DecodeString("00000062000000000e36ffff000f42420120202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3fa73522bc33062bb414ebd178f92369691d98fd1ad80bca49c341fb7a40cf14d46c34c00eac75de80a4ae614ae75be7d4")
	if err != nil {
		t.Fatal(err)
	}
	request, err := DecodeLocalTransferUDPOKRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.SourceUIN != 1_000_002 || request.ClientTime != 0x1AF6D415 || request.DestinationPlayerID != 1 || request.DestinationUIN != 1_000_001 || !bytes.Equal(request.Info, []byte{0}) {
		t.Fatalf("decoded request = %+v", request)
	}
}

func TestDecodeLocalTransferUDPOKRequestTransportDescriptorCapture(t *testing.T) {
	packet, err := hex.DecodeString("00000062000000000c84ffff000f42410120202122232425262728292a2b2c2d2e2f303132333435363738393a3b3c3d3e3f1d4dfc265749da8ff9a766b09764b156ad2610aaaab30b06f7dc9d095d4b02eb3ed725eba1e713e7318df8887252eda1")
	if err != nil {
		t.Fatal(err)
	}
	request, err := DecodeLocalTransferUDPOKRequest(packet)
	if err != nil {
		t.Fatal(err)
	}
	wantInfo := []byte{1, 0, 0x0F, 0x42, 0x42, 0, 3}
	if request.SourceUIN != 1_000_001 || request.ClientTime != 0x1AE6B31E || request.DestinationPlayerID != 2 || request.DestinationUIN != 1_000_002 || !bytes.Equal(request.Info, wantInfo) {
		t.Fatalf("decoded request = %+v", request)
	}
}

func TestBuildLocalTransferUDPOKNotificationForRecipient(t *testing.T) {
	request := makeLocalPacketForTest(t, append([]byte{
		0x00, 0x88, 0, 0, 0, 0, 0, 1, 0, 4, 0xFF, 0xFF, 0xFF, 0xFF,
	}, []byte{0, 0}...))
	// Rebind the test template to the intended recipient UIN.
	request[12], request[13], request[14], request[15] = 0, 0x0F, 0x42, 0x41
	plaintext, err := qqtea.Decrypt(request[localEncryptedOffset:], directory.LocalKey)
	if err != nil {
		t.Fatal(err)
	}
	_ = plaintext

	// makeLocalPacketForTest encrypts before the outer UIN rewrite, which is
	// valid because the encrypted routing header contains no duplicate UIN.
	packet, err := BuildLocalTransferUDPOKNotificationForRecipient(request, TransferUDPOKNotification{
		RecipientUIN: 1_000_001, ClientTime: 0x1AF6D415,
		SourcePlayerID: 1, SourceUIN: 1_000_002, Info: []byte{0},
	})
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != NotifyUDPOKCommand || inspection.Route != transferUDPOKRoute || inspection.SectionID != transferUDPOKSectionID || inspection.RouteSequence != 0 || inspection.InnerSequence != 0 {
		t.Fatalf("notification route = %+v", inspection)
	}
	want := "000f42411af6d4150001000f4242000100"
	if got := hex.EncodeToString(inspection.Payload); got != want {
		t.Fatalf("notification payload = %s, want %s", got, want)
	}
}

func TestTransferUDPOKTransportAddressInfoRequiresCompletePair(t *testing.T) {
	_, err := (TransferUDPOKNotification{
		RecipientUIN: 1_000_001, ClientTime: 1,
		SourcePlayerID: 2, SourceUIN: 1_000_002, Info: []byte{1, 127},
	}).MarshalNetworkBinary()
	if err == nil {
		t.Fatal("short non-zero transport-address info was accepted")
	}
}

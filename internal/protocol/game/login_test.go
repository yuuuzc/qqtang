package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"qqtang/internal/protocol/directory"
	"qqtang/internal/protocol/qqtea"
)

const capturedLoginPlaintext = "00640000000000060002ffff0001000f42410d2ba0984c6f63616c506c617965720000000000000000000000ffff00010201000017004f5d31000400030000867ea5a3d7026df3e283a60295c3a75100060000376454b1af95f57331d7c2ec5d99353c000400003a438747b25da7e3c34a7e71d51985c3000900002a7adc640f80e7e5f1631a472d2aaa67000000000000000a"

func TestBuildLocalLoginPayloadVector(t *testing.T) {
	config := DefaultLocalLoginConfig()
	config.Items = []ItemInfo{NewPermanentItemInfo(SinglePlayerAdventureCardItemID, 1)}
	payload, err := BuildLocalLoginPayload(1_000_001, config)
	if err != nil {
		t.Fatal(err)
	}
	want, err := hex.DecodeString(
		"00000001000f424100000000000110" +
			"101112131415161718191a1b1c1d1e1f" +
			"00000000000000000000000000000000" +
			"00000001" +
			"0000000000000000000000010000000000000000" +
			"05f5e0ff00010100000000" +
			"00000000000000000000000000000000" +
			"00010063000000010000000000000000ffffffff" +
			"0000000000000000000000000000000000000000000000",
	)
	if err != nil {
		t.Fatal(err)
	}
	wantSize := LoginResponseFixedPayloadSize + ItemInfoBinarySize
	if len(payload) != wantSize {
		t.Fatalf("payload length = %d, want %d", len(payload), wantSize)
	}
	if !bytes.Equal(payload, want) {
		t.Fatalf("payload mismatch\n got: %x\nwant: %x", payload, want)
	}
	decoded, err := ParseLoginResponseNetwork(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.UIN != 1_000_001 || decoded.PlayerID != 1 || len(decoded.Items) != 1 {
		t.Fatalf("unexpected typed login response: %+v", decoded)
	}
	roundTrip, err := decoded.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(roundTrip, payload) {
		t.Fatalf("login response round trip mismatch\n got: %x\nwant: %x", roundTrip, payload)
	}
}

func TestDefaultLocalLoginConfigMarksTutorialCompleteWithoutHardcodedInventory(t *testing.T) {
	payload, err := BuildLocalLoginPayload(1_000_001, DefaultLocalLoginConfig())
	if err != nil {
		t.Fatal(err)
	}
	response, err := ParseLoginResponseNetwork(payload)
	if err != nil {
		t.Fatal(err)
	}
	if response.GameInfo.EqualNum != 1 {
		t.Fatalf("neutral completed-game marker = %d, want 1", response.GameInfo.EqualNum)
	}
	if len(response.Items) != 0 {
		t.Fatalf("default inventory must come from config or SQLite, got %+v", response.Items)
	}
}

func TestLoginResponseCarriesPatternPoints(t *testing.T) {
	config := DefaultLocalLoginConfig()
	config.Patterns = []PatternPoint{{GameMode: 2, PatternPoint: 30, PatternLevel: 3, LevelValue: 100}}
	payload, err := BuildLocalLoginPayload(1_000_001, config)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(payload), LoginResponseFixedPayloadSize+patternPointNetworkSize; got != want {
		t.Fatalf("payload length = %d, want %d", got, want)
	}
	decoded, err := ParseLoginResponseNetwork(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Patterns) != 1 || decoded.Patterns[0] != config.Patterns[0] {
		t.Fatalf("pattern points round trip = %+v, want %+v", decoded.Patterns, config.Patterns)
	}
}

func TestBuildLocalLoginPayloadRejectsInvalidInventory(t *testing.T) {
	config := DefaultLocalLoginConfig()
	config.Items = []ItemInfo{NewPermanentItemInfo(SinglePlayerAdventureCardItemID, 1)}
	config.Items[0].NumOfItem = 0
	if _, err := BuildLocalLoginPayload(1_000_001, config); err == nil {
		t.Fatal("expected zero item quantity to be rejected")
	}
	config = DefaultLocalLoginConfig()
	config.Items = []ItemInfo{NewPermanentItemInfo(SinglePlayerAdventureCardItemID, 1)}
	config.Items[0].AvailPeriod = 0
	if _, err := BuildLocalLoginPayload(1_000_001, config); err == nil {
		t.Fatal("expected zero item available period to be rejected")
	}
	config = DefaultLocalLoginConfig()
	config.Items = make([]ItemInfo, MaxItemInfoCount+1)
	if _, err := BuildLocalLoginPayload(1_000_001, config); err == nil {
		t.Fatal("expected excessive item count to be rejected")
	}
}

func TestParseLoginResponseRejectsTruncatedAndTrailingData(t *testing.T) {
	payload, err := BuildLocalLoginPayload(1_000_001, DefaultLocalLoginConfig())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseLoginResponseNetwork(payload[:len(payload)-1]); err == nil {
		t.Fatal("expected truncated login response to fail")
	}
	if _, err := ParseLoginResponseNetwork(append(payload, 0)); err == nil {
		t.Fatal("expected trailing login response data to fail")
	}
}

func TestDecodeAndBuildLocalLoginSuccess(t *testing.T) {
	plaintext, err := hex.DecodeString(capturedLoginPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := qqtea.EncryptWithReader(plaintext, directory.LocalKey, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	request := make([]byte, localEnvelopeSize+localSTSize+len(ciphertext))
	binary.BigEndian.PutUint32(request[0:4], uint32(len(request)))
	binary.BigEndian.PutUint16(request[8:10], 5)
	binary.BigEndian.PutUint16(request[10:12], 0xffff)
	binary.BigEndian.PutUint32(request[12:16], 1_000_001)
	request[16], request[17] = 1, localSTSize
	copy(request[18:50], directory.LocalST)
	copy(request[50:], ciphertext)

	decoded, err := DecodeLocalLoginRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.UIN != 1_000_001 || decoded.InnerSequence != 6 || decoded.RouteSequence != 5 || decoded.SectionID != 1 {
		t.Fatalf("unexpected decoded request: %+v", decoded)
	}
	if decoded.Nickname != "LocalPlayer" || decoded.Gender != 0 || decoded.IconID != 0 || decoded.RoleID != 23 {
		t.Fatalf("unexpected login player fields: %+v", decoded)
	}
	if decoded.ClientVersion != 0x004F5D31 || decoded.ClientType != 0 || decoded.CSVersion != 10 || len(decoded.ConfigFiles) != 4 {
		t.Fatalf("unexpected login compatibility fields: %+v", decoded)
	}
	if decoded.ConfigFiles[0].FileID != 3 || decoded.ConfigFiles[0].Version != 0 || decoded.ConfigFiles[3].FileID != 9 {
		t.Fatalf("unexpected login config files: %+v", decoded.ConfigFiles)
	}
	response, err := BuildLocalLoginSuccessWithReader(request, DefaultLocalLoginConfig(), bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(response[0:4]) != uint32(len(response)) {
		t.Fatalf("response declared length mismatch: %x", response[:4])
	}
	if !bytes.Equal(response[4:50], request[4:50]) {
		t.Fatal("response did not preserve the observed request envelope")
	}
	responsePlaintext, err := qqtea.Decrypt(response[50:], directory.LocalKey)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(responsePlaintext[0:2]) != LoginCommand {
		t.Fatalf("response command = 0x%04X", binary.BigEndian.Uint16(responsePlaintext[0:2]))
	}
	if !bytes.Equal(responsePlaintext[2:14], plaintext[2:14]) {
		t.Fatal("response did not preserve the inner request route header")
	}
	if got := binary.BigEndian.Uint32(responsePlaintext[18:22]); got != 1_000_001 {
		t.Fatalf("response payload UIN = %d", got)
	}
}

func TestDecodeLocalLoginRequestRejectsWrongCommand(t *testing.T) {
	plaintext, _ := hex.DecodeString(capturedLoginPlaintext)
	binary.BigEndian.PutUint16(plaintext[0:2], 0x0066)
	ciphertext, err := qqtea.EncryptWithReader(plaintext, directory.LocalKey, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, localEncryptedOffset+len(ciphertext))
	binary.BigEndian.PutUint32(packet[0:4], uint32(len(packet)))
	binary.BigEndian.PutUint32(packet[12:16], 1_000_001)
	packet[16], packet[17] = 1, localSTSize
	copy(packet[18:50], directory.LocalST)
	copy(packet[50:], ciphertext)
	if _, err := DecodeLocalLoginRequest(packet); err == nil {
		t.Fatal("expected wrong command to be rejected")
	}
}

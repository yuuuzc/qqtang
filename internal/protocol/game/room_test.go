package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"

	"qqtang/internal/protocol/directory"
	"qqtang/internal/protocol/qqtea"
)

const capturedRoomListPlaintext = "00660000000000120002ffff0001000f42410d44eaa802000100080000"

func makeLocalPacketForTest(t *testing.T, plaintext []byte) []byte {
	t.Helper()
	ciphertext, err := qqtea.EncryptWithReader(plaintext, directory.LocalKey, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	packet := make([]byte, localEncryptedOffset+len(ciphertext))
	binary.BigEndian.PutUint32(packet[0:4], uint32(len(packet)))
	binary.BigEndian.PutUint32(packet[4:8], 0x0479)
	binary.BigEndian.PutUint16(packet[8:10], 0x0433)
	binary.BigEndian.PutUint16(packet[10:12], 0xffff)
	binary.BigEndian.PutUint32(packet[12:16], 1_000_001)
	packet[16], packet[17] = 1, localSTSize
	copy(packet[localEnvelopeSize:localEncryptedOffset], directory.LocalST)
	copy(packet[localEncryptedOffset:], ciphertext)
	return packet
}

func TestBuildEmptyRoomListPayloadNativeVector(t *testing.T) {
	got := BuildEmptyRoomListPayload()
	want, err := osReadTestVector("0000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("room-list payload = %x, want %x", got, want)
	}
}

func TestBuildEmptyRoomListResponse(t *testing.T) {
	plaintext, err := hex.DecodeString(capturedRoomListPlaintext)
	if err != nil {
		t.Fatal(err)
	}
	request := makeLocalPacketForTest(t, plaintext)
	response, err := BuildEmptyRoomListResponseWithReader(request, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if len(response) != 82 {
		t.Fatalf("response length = %d, want 82", len(response))
	}
	if binary.BigEndian.Uint32(response[0:4]) != uint32(len(response)) {
		t.Fatalf("response declared length mismatch: %x", response[:4])
	}
	if !bytes.Equal(response[4:localEncryptedOffset], request[4:localEncryptedOffset]) {
		t.Fatal("response did not preserve the observed request envelope")
	}
	responsePlaintext, err := qqtea.Decrypt(response[localEncryptedOffset:], directory.LocalKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(responsePlaintext) != localInnerHeaderSize+roomListFixedSize {
		t.Fatalf("response plaintext length = %d", len(responsePlaintext))
	}
	if !bytes.Equal(responsePlaintext[:localInnerHeaderSize], plaintext[:localInnerHeaderSize]) {
		t.Fatalf("response route header changed\n got: %x\nwant: %x", responsePlaintext[:localInnerHeaderSize], plaintext[:localInnerHeaderSize])
	}
	wantPayload, _ := hex.DecodeString("0000020000000001")
	if !bytes.Equal(responsePlaintext[localInnerHeaderSize:], wantPayload) {
		t.Fatalf("response payload = %x", responsePlaintext[localInnerHeaderSize:])
	}
}

func TestRoomListAndPushEncodeVariableGBKRoomInfo(t *testing.T) {
	population, err := PackRoomPopulation(1, 4)
	if err != nil {
		t.Fatal(err)
	}
	room := RoomListEntry{Name: "糖果", RoomID: 9, RoomFlag: RoomListFlagPreparing, MapID: 1601, NumOfPlayer: population, GameType: 2, ContinueID: 1}
	payload, err := BuildRoomListPayload([]RoomListEntry{room})
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(payload[3:5]) != 1 || payload[5] != 5 {
		t.Fatalf("room-list count/name length = %x", payload[:6])
	}
	if payload[10] != 0 {
		t.Fatalf("ROOM_INFO name terminator = 0x%02x, want 0", payload[10])
	}
	// NameLen + 4 GBK name bytes + NUL + RoomID place RoomFlag at payload[13].
	if payload[13] != byte(RoomListFlagPreparing) {
		t.Fatalf("preparing room flag = %d, want %d", payload[13], RoomListFlagPreparing)
	}
	push, err := BuildRoomPushPayload([]RoomListEntry{room})
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(push[:2]) != 1 || !bytes.Equal(push[2:], payload[5:len(payload)-3]) {
		t.Fatalf("room push does not reuse ROOM_INFO: list=%x push=%x", payload, push)
	}
}

func TestRoomInfoNameLengthIncludesNULTerminator(t *testing.T) {
	room := RoomListEntry{Name: "TRE", RoomID: 0x1234, RoomFlag: RoomListFlagPreparing}
	encoded, err := room.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []byte{4, 'T', 'R', 'E', 0, 0x12, 0x34, byte(RoomListFlagPreparing)}
	if !bytes.Equal(encoded[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("ROOM_INFO prefix = %x, want %x", encoded[:len(wantPrefix)], wantPrefix)
	}
}

func TestRoomListFlagNativeProjectionBits(t *testing.T) {
	if RoomListFlagInMatch != 0 || RoomListFlagPreparing != 1 || RoomListFlagPassword != 2 || RoomListFlagFreeRule != 4 || RoomListFlagVIP != 8 {
		t.Fatalf("ROOM_INFO native flags changed: in-match=%d preparing=%d password=%d free=%d vip=%d",
			RoomListFlagInMatch, RoomListFlagPreparing, RoomListFlagPassword, RoomListFlagFreeRule, RoomListFlagVIP)
	}
	if !(RoomListFlagPreparing | RoomListFlagPassword | RoomListFlagFreeRule | RoomListFlagVIP).Valid() {
		t.Fatal("native ROOM_INFO flag combination was rejected")
	}
	if RoomListFlag(1 << 4).Valid() {
		t.Fatal("unknown ROOM_INFO bit 4 was accepted")
	}
}

func TestRoomInfoNameReservesNULTerminatorInsideTwentyByteSlot(t *testing.T) {
	if _, err := (RoomListEntry{Name: "1234567890123456789", RoomID: 1}).MarshalNetworkBinary(); err != nil {
		t.Fatalf("19-byte room name rejected: %v", err)
	}
	if _, err := (RoomListEntry{Name: "12345678901234567890", RoomID: 1}).MarshalNetworkBinary(); err == nil {
		t.Fatal("20-byte visible room name left no space for the required NUL")
	}
}

func TestRoomPopulationNibblePacking(t *testing.T) {
	packed, err := PackRoomPopulation(1, 4)
	if err != nil {
		t.Fatal(err)
	}
	current, capacity := UnpackRoomPopulation(packed)
	if packed != 0x14 || current != 1 || capacity != 4 {
		t.Fatalf("population packed=%02x current=%d capacity=%d", packed, current, capacity)
	}
	if _, err := PackRoomPopulation(5, 4); err == nil {
		t.Fatal("accepted population above room capacity")
	}
}

func TestRoomListResponseEchoesRequestCursorAndFilterMode(t *testing.T) {
	plaintext, _ := hex.DecodeString(capturedRoomListPlaintext)
	response, err := BuildRoomListResponseWithReader(
		makeLocalPacketForTest(t, plaintext),
		[]RoomListEntry{{Name: "糖果", RoomID: 1, GameType: 2}},
		bytes.NewReader(make([]byte, 64)),
	)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := qqtea.Decrypt(response[localEncryptedOffset:], directory.LocalKey)
	if err != nil {
		t.Fatal(err)
	}
	body := decoded[localInnerHeaderSize:]
	if body[2] != byte(RoomListOperationRefresh) || binary.BigEndian.Uint16(body[3:5]) != 1 {
		t.Fatalf("response operation/count = %x", body[:5])
	}
	if got := binary.BigEndian.Uint16(body[len(body)-2:]); got != 1 {
		t.Fatalf("response StartRoomID = %d, want 1", got)
	}
}

func TestBuildEmptyRoomListResponseRejectsWrongCommand(t *testing.T) {
	plaintext, _ := hex.DecodeString(capturedRoomListPlaintext)
	binary.BigEndian.PutUint16(plaintext[0:2], LoginCommand)
	if _, err := BuildEmptyRoomListResponse(makeLocalPacketForTest(t, plaintext)); err == nil {
		t.Fatal("expected wrong command to be rejected")
	}
}

func osReadTestVector(value string) ([]byte, error) {
	return hex.DecodeString(value)
}

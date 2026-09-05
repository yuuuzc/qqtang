package game

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

const capturedRoomMovePacket = "00000057000000000352ffff000f42420105006501000109e8fb533c3f1872ede2c5d0ac197a425e456e1475bc4176a9bebcaadc1d0f9210a04d2654627143f968dc52d05c1b45da7d1b563e0add8a18c804326dac6492"
const capturedRoomBombPacket = "0000004f000000000355ffff000f424201050065010001a93b90a3b2363f65cefc17f1c0479d562d6deabcfd896837a3e37cc325978c3e86c72f021c04d404289375f09bd90e1dd24586b62d97fd9f"
const capturedMatchFastPacket = "0000006f00000000002fffff000f424201050065010001a344f986201c5e6430cc1cc532ea796592ae7dce8b9c22989699ca97a1aa22d4a1d334ed2e4e315e7159500a9d85454c592365b10a1f3fc8c847bc33c63b603c8e8a47550a5a83ee737d8e5493d7e9bf42f467805e61f4c2"

func decodeRoomFastVector(t *testing.T, encoded string) []byte {
	t.Helper()
	packet, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return packet
}

func TestInspectCapturedRoomMovePacket(t *testing.T) {
	packet := decodeRoomFastVector(t, capturedRoomMovePacket)
	inspection, err := InspectRoomFastPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.EnvelopeUIN != 1_000_002 || inspection.RouteSequence != 0x0352 || inspection.InnerSequence != 0x0352 || len(inspection.Events) != 1 {
		t.Fatalf("inspection = %+v", inspection)
	}
	if !bytes.Equal(uint16SliceBytes(inspection.RecipientPlayerIDs), []byte{0, 1}) {
		t.Fatalf("recipient player IDs = %v", inspection.RecipientPlayerIDs)
	}
	event := inspection.Events[0]
	if event.DataID != RoomPlayerMoveInfoEvent || event.Sequence != 39 {
		t.Fatalf("move event = %+v", event)
	}
	move, err := DecodeRoomPlayerMoveInfo(event.Data)
	if err != nil {
		t.Fatal(err)
	}
	if move.SeatIndex != 1 || move.PosX != 236 || move.PosY != 283 || move.Direction != 0 {
		t.Fatalf("move = %+v", move)
	}
}

func TestCapturedRoomMoveMarshalsExactType2RoomMessageData(t *testing.T) {
	packet := decodeRoomFastVector(t, capturedRoomMovePacket)
	inspection, err := InspectRoomFastPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Events) != 1 {
		t.Fatalf("event count = %d", len(inspection.Events))
	}
	payload, err := inspection.Events[0].MarshalRoomMessageData()
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) != roomMessageDataHeaderSize+17 || len(inspection.BatchData) != len(payload)+11 {
		t.Fatalf("ROOM_MSG_DATA length = %d, TCP batch length = %d", len(payload), len(inspection.BatchData))
	}
	decoded, err := ParseRoomMessageData(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Time != inspection.Events[0].Time || decoded.DataID != RoomPlayerMoveInfoEvent || decoded.Sequence != inspection.Events[0].Sequence || !bytes.Equal(decoded.Data, inspection.Events[0].Data) {
		t.Fatalf("decoded ROOM_MSG_DATA = %+v", decoded)
	}
}

func TestBuildRoomFastRelayRebindsOnlyRecipientEnvelope(t *testing.T) {
	packet := decodeRoomFastVector(t, capturedRoomMovePacket)
	relay, err := BuildRoomFastRelayPacketForRecipient(packet, 1_000_001)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectRoomFastPacket(relay)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.EnvelopeUIN != 1_000_001 || inspection.RouteSequence != 0x0352 || len(inspection.RecipientPlayerIDs) != 1 || inspection.RecipientPlayerIDs[0] != 1 {
		t.Fatalf("relay inspection = %+v", inspection)
	}
	if !bytes.Equal(relay[:12], packet[:12]) || !bytes.Equal(relay[16:], packet[16:]) {
		t.Fatal("relay changed data outside the outer recipient UIN")
	}
}

func TestLooksLikeRoomFastPacketDoesNotClaimOrdinarySessionTicket(t *testing.T) {
	template, _ := makeDeathEventPacket(t)
	if LooksLikeRoomFastPacket(template) {
		t.Fatal("ordinary 32-byte session-ticket packet was classified as room fast")
	}
}

func uint16SliceBytes(values []uint16) []byte {
	data := make([]byte, 2*len(values))
	for index, value := range values {
		binary.BigEndian.PutUint16(data[index*2:], value)
	}
	return data
}

func TestInspectCapturedRoomBombPacket(t *testing.T) {
	packet := decodeRoomFastVector(t, capturedRoomBombPacket)
	inspection, err := InspectRoomFastPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Events) != 1 || inspection.Events[0].DataID != RoomPlayerPutBombEvent || inspection.Events[0].Sequence != 42 {
		t.Fatalf("inspection = %+v", inspection)
	}
	bomb, err := DecodeRoomPlayerPutBomb(inspection.Events[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	if bomb.SeatIndex != 1 || bomb.PosX != 308 || bomb.PosY != 315 {
		t.Fatalf("bomb = %+v", bomb)
	}
}

func TestInspectCapturedMatchFastPacket(t *testing.T) {
	packet := decodeRoomFastVector(t, capturedMatchFastPacket)
	inspection, err := InspectRoomFastPacket(packet)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.EnvelopeUIN != 1_000_002 || inspection.RouteSequence != 0x002f || inspection.PayloadKind != RoomFastPayloadGameplay || len(inspection.GameplayPackages) != 1 {
		t.Fatalf("inspection = %+v", inspection)
	}
	gameplay := inspection.GameplayPackages[0]
	if gameplay.PlayerID != 2 || gameplay.Time != 0x6BC9 || gameplay.GameID != 1 || len(gameplay.MessageIndexes) != 1 || gameplay.MessageIndexes[0] != 0xB0 || len(gameplay.Messages) != 1 {
		t.Fatalf("gameplay package = %+v", gameplay)
	}
	event := gameplay.Messages[0]
	if event.DataID != 0x0FA3 || event.Time != 0x6BC9 || event.Sequence != 0x4B0004CD || len(event.Data) != 15 {
		t.Fatalf("gameplay event = %+v", event)
	}
}

func TestInspectRoomFastPacketRejectsTruncatedCompactEvent(t *testing.T) {
	packet := decodeRoomFastVector(t, capturedRoomMovePacket)
	packet = packet[:len(packet)-1]
	binary.BigEndian.PutUint32(packet[:4], uint32(len(packet)))
	if _, err := InspectRoomFastPacket(packet); err == nil {
		t.Fatal("truncated ciphertext should be rejected")
	}
}

func TestRewriteGameplayPeerSchemasTranslatesUseBombOnly(t *testing.T) {
	body := make([]byte, PlayerUseBombBodySize)
	binary.BigEndian.PutUint16(body[0:2], 30001)
	binary.BigEndian.PutUint32(body[6:10], 21)
	gameplay := GameplayDataPackage{
		PlayerID: 30001, Time: 10, GameID: 20, MessageIndexes: []uint32{1},
		Messages: []BattleMessageData{
			{Time: 11, DataID: uint32(PlayerUseBomb), Sequence: 12, Data: body},
		},
	}
	compact, err := gameplay.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	batch := []byte{1}
	batch = binary.BigEndian.AppendUint32(batch, 100)
	batch = binary.BigEndian.AppendUint32(batch, 0)
	batch = binary.BigEndian.AppendUint16(batch, uint16(len(compact)))
	batch = append(batch, compact...)

	rewritten, changed, err := RewriteGameplayPeerSchemas(batch)
	if err != nil || !changed {
		t.Fatalf("rewrite changed/error = %v/%v", changed, err)
	}
	got, err := ParseGameplayDataPackage(rewritten[11:])
	if err != nil {
		t.Fatal(err)
	}
	if got.Messages[0].DataID != uint32(NotifyPlayerUseBomb) {
		t.Fatalf("rewritten schema = 0x%04X", got.Messages[0].DataID)
	}
}

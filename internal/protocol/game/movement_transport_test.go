package game

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestGameplayUpstreamPackageMatchesNativeQQTEncoder(t *testing.T) {
	want := mustDecodeMovementHex(t, "000111223344010102030400000fa2000205060708aabb090a0b0c0d0e0f10")
	packet := GameplayUpstreamPackage{
		PlayerID: 1,
		Time:     0x11223344,
		Messages: []BattleMessageData{{
			Time: 0x01020304, DataID: 0x0FA2, Sequence: 0x05060708,
			Data: []byte{0xAA, 0xBB}, GameTime: 0x090A0B0C, Flag: 0x0D0E0F10,
		}},
	}
	assertMovementPacketRoundTrip(t, packet, want, ParseGameplayUpstreamPackage)
}

func TestGameplayDataPackageMatchesCapturedReliablePeerPayload(t *testing.T) {
	want := mustDecodeMovementHex(t, "000200006bc90000000101000000b000006bc900000fa3000f4b0004cd000200006bc9f20c7fff0412000400ffff006500000000")
	packet, err := ParseGameplayDataPackage(want)
	if err != nil {
		t.Fatal(err)
	}
	if packet.PlayerID != 2 || packet.Time != 0x6BC9 || packet.GameID != 1 || len(packet.MessageIndexes) != 1 || packet.MessageIndexes[0] != 0xB0 || len(packet.Messages) != 1 {
		t.Fatalf("decoded 0x0FBD = %+v", packet)
	}
	message := packet.Messages[0]
	if message.DataID != 0x0FA3 || message.Time != 0x6BC9 || message.Sequence != 0x4B0004CD || len(message.Data) != 15 || message.GameTime != 0xFFFF0065 || message.Flag != 0 {
		t.Fatalf("decoded QQT_MSG_DATA = %+v", message)
	}
	encoded, err := packet.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("0x0FBD round trip = %x, want %x", encoded, want)
	}
}

func TestGameplayDownstreamMovesMatchNativeQQTEncoder(t *testing.T) {
	want := mustDecodeMovementHex(t, "123456789abc0101020304112233445a0110fe7f8000")
	packet := GameplayDownstreamPackage{
		GameID: 0x1234, PlayerID: 0x5678, FirstIndex: 0x9ABC,
		AbsoluteMoves: []PlayerMoveCompressed{{
			Time: 0x01020304, PosX: 0x1122, PosY: 0x3344, DirState: 0x5A,
		}},
		RevisionMoves: []PlayerMoveRevision{{
			DeltaTime: 0x10, DeltaPosX: -2, DeltaPosY: 127, DirState: 0x80,
		}},
	}
	assertMovementPacketRoundTrip(t, packet, want, ParseGameplayDownstreamPackage)
}

func TestGameplayDownstreamMessageMatchesNativeQQTEncoder(t *testing.T) {
	want := mustDecodeMovementHex(t, "123456789abc0000010102030400000fa2000105060708cc090a0b0c0d0e0f10")
	packet := GameplayDownstreamPackage{
		GameID: 0x1234, PlayerID: 0x5678, FirstIndex: 0x9ABC,
		Messages: []BattleMessageData{{
			Time: 0x01020304, DataID: 0x0FA2, Sequence: 0x05060708,
			Data: []byte{0xCC}, GameTime: 0x090A0B0C, Flag: 0x0D0E0F10,
		}},
	}
	assertMovementPacketRoundTrip(t, packet, want, ParseGameplayDownstreamPackage)
}

func TestGameplayMovementCodecsRejectBoundsAndTruncation(t *testing.T) {
	tooManyMoves := GameplayDownstreamPackage{AbsoluteMoves: make([]PlayerMoveCompressed, maxDownstreamAbsoluteMoves+1)}
	if _, err := tooManyMoves.MarshalNetworkBinary(); err == nil {
		t.Fatal("too many absolute moves unexpectedly encoded")
	}
	tooMuchData := GameplayUpstreamPackage{Messages: []BattleMessageData{{Data: make([]byte, maxBattleMessageDataLength+1)}}}
	if _, err := tooMuchData.MarshalNetworkBinary(); err == nil {
		t.Fatal("oversized battle message unexpectedly encoded")
	}
	valid, err := (GameplayDownstreamPackage{}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	for length := range len(valid) {
		if _, err := ParseGameplayDownstreamPackage(valid[:length]); err == nil {
			t.Fatalf("truncated 0x10E7 length %d unexpectedly decoded", length)
		}
	}
	malformed := append([]byte(nil), valid...)
	malformed[6] = maxDownstreamAbsoluteMoves + 1
	if _, err := ParseGameplayDownstreamPackage(malformed); err == nil {
		t.Fatal("oversized absolute move count unexpectedly decoded")
	}
}

type movementBinaryMarshaler interface {
	MarshalNetworkBinary() ([]byte, error)
}

func assertMovementPacketRoundTrip[T movementBinaryMarshaler](t *testing.T, packet T, want []byte, parse func([]byte) (T, error)) {
	t.Helper()
	encoded, err := packet.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("encoded = %x, want %x", encoded, want)
	}
	decoded, err := parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	encodedAgain, err := decoded.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encodedAgain, want) {
		t.Fatalf("round trip = %x, want %x", encodedAgain, want)
	}
}

func mustDecodeMovementHex(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

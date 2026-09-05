package game

import (
	"bytes"
	"testing"
)

func TestPlayerMoveMatchesNativeQQTEncoderParallelArrays(t *testing.T) {
	want := mustDecodeMovementHex(t, "0001020102030400020003110111111102110311041105110611071108a1a2220122222202220322042205220622072208b1b2")
	movement := PlayerMove{
		PlayerID: 1,
		SeqReply: 0x01020304,
		Entries: []PlayerMoveEntry{
			{PlayerID: 2, Move: PlayerMoveSequence{
				Sequence: 0x1101, TimeStamp: 0x11111102,
				CurrentPosX: 0x1103, CurrentPosY: 0x1104,
				CornerPosX: 0x1105, CornerPosY: 0x1106,
				EndPosX: 0x1107, EndPosY: 0x1108,
				WalkAndDirection: 0xA1, Speed: 0xA2,
			}},
			{PlayerID: 3, Move: PlayerMoveSequence{
				Sequence: 0x2201, TimeStamp: 0x22222202,
				CurrentPosX: 0x2203, CurrentPosY: 0x2204,
				CornerPosX: 0x2205, CornerPosY: 0x2206,
				EndPosX: 0x2207, EndPosY: 0x2208,
				WalkAndDirection: 0xB1, Speed: 0xB2,
			}},
		},
	}
	encoded, err := movement.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, want) {
		t.Fatalf("PLAYER_MOVE = %x, want %x", encoded, want)
	}
	decoded, err := ParsePlayerMove(encoded)
	if err != nil {
		t.Fatal(err)
	}
	encodedAgain, err := decoded.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encodedAgain, want) {
		t.Fatalf("PLAYER_MOVE round trip = %x, want %x", encodedAgain, want)
	}
}

func TestPlayerMoveRejectsWrongCountLength(t *testing.T) {
	payload := mustDecodeMovementHex(t, "0001020102030400020003")
	if _, err := ParsePlayerMove(payload); err == nil {
		t.Fatal("truncated PLAYER_MOVE unexpectedly decoded")
	}
	tooMany := PlayerMove{Entries: make([]PlayerMoveEntry, maxPlayerMoveSequences+1)}
	if _, err := tooMany.MarshalNetworkBinary(); err == nil {
		t.Fatal("oversized PLAYER_MOVE unexpectedly encoded")
	}
}

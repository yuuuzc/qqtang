package game

import (
	"encoding/hex"
	"testing"
)

func TestDecodeQQTPPPGameplayBatchMovementVector(t *testing.T) {
	ciphertext, err := hex.DecodeString("55a8ae5212d5e6c1530f2b44a83022fc69454c0dfbc6eba4de36228d7b7cfe652329225fa074dfcf660dae2289252571a6865030142e7099dc56623c8006353307c1914f8dab84772f3dec0c697e35dc62895e75d8cf2702")
	if err != nil {
		t.Fatal(err)
	}
	batch, err := DecodeQQTPPPGameplayBatch(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if batch.GameID != 16 || batch.FirstIndex != 0 {
		t.Fatalf("batch envelope = game %d first index %d", batch.GameID, batch.FirstIndex)
	}
	if len(batch.Entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(batch.Entries))
	}
	entry := batch.Entries[0]
	if entry.Index != 1 {
		t.Fatalf("entry index = %d, want 1", entry.Index)
	}
	if entry.Package.PlayerID != 2 || entry.Package.GameID != 16 || len(entry.Package.Messages) != 1 {
		t.Fatalf("gameplay package = %+v", entry.Package)
	}
	if entry.Package.Messages[0].DataID != uint32(PlayerMoveSchema) {
		t.Fatalf("message schema = 0x%04X, want 0x%04X", entry.Package.Messages[0].DataID, PlayerMoveSchema)
	}
	if !GameplayBatchIsMovementOnly(batch) {
		t.Fatal("known PLAYER_MOVE vector was not classified as movement-only")
	}

	mixed := batch
	mixed.Entries[0].Package.Messages[0].DataID = 0x0FA3
	if GameplayBatchIsMovementOnly(mixed) {
		t.Fatal("non-movement schema was classified as movement-only")
	}
}

func TestDecodeQQTPPPGameplayBatchRejectsInvalidCiphertext(t *testing.T) {
	if _, err := DecodeQQTPPPGameplayBatch(make([]byte, 16)); err == nil {
		t.Fatal("invalid QQ-TEA ciphertext was accepted")
	}
}

func TestEncodeQQTPPPGameplayBatchRoundTrip(t *testing.T) {
	moveBody, err := (PlayerMove{
		PlayerID: 7,
		Entries: []PlayerMoveEntry{{PlayerID: 7, Move: PlayerMoveSequence{
			Sequence: 3, TimeStamp: 100, CurrentPosX: 40, CurrentPosY: 80,
			CornerPosX: 40, CornerPosY: 80, EndPosX: 40, EndPosY: 80,
			WalkAndDirection: 0x10, Speed: 5,
		}}},
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	want := GameplayBatch{GameID: 9, Entries: []GameplayBatchEntry{{
		Index: 1, Package: GameplayDataPackage{
			PlayerID: 7, Time: 100, GameID: 9, MessageIndexes: []uint32{1},
			Messages: []BattleMessageData{{Time: 100, DataID: uint32(PlayerMoveSchema), Sequence: 3, Data: moveBody, GameTime: 100, Flag: 0}},
		},
	}}}
	encoded, err := EncodeQQTPPPGameplayBatch(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeQQTPPPGameplayBatch(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.GameID != want.GameID || len(got.Entries) != 1 || got.Entries[0].Index != want.Entries[0].Index || got.Entries[0].Package.PlayerID != 7 || len(got.Entries[0].Package.Messages) != 1 {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestDecodeQQTPPPGameplayBatchNativeTwoEntryVector(t *testing.T) {
	ciphertext, err := hex.DecodeString("983db1d7c14a209e9e1501ccdf7de14d92026b0c2eba2ee2347ce03034c2b0443e2e2236f91f3db0e18bb36b82a6707a269742e6cbe4f8bcee6d4e50620148f9af078bf6f041808f0a0e27d58ee63f75889df6751c83ec3c48d208b272992ac6c671a61ae431d920349ae447f0719895f738c92013e86bee7b843713d85c8f27fa546dc5b6ec5e2311796e997f7a0b6344a279cb7b74a04a9333c50405eaa419")
	if err != nil {
		t.Fatal(err)
	}
	batch, err := DecodeQQTPPPGameplayBatch(ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if batch.GameID != 1 || batch.FirstIndex != 0 || len(batch.Entries) != 2 {
		t.Fatalf("batch = %+v", batch)
	}
	for index, entry := range batch.Entries {
		wantIndex := uint16(index + 1)
		if entry.Index != wantIndex || entry.Package.GameID != 1 || entry.Package.PlayerID != 1 {
			t.Fatalf("entry %d = %+v", index, entry)
		}
	}
	if !GameplayBatchIsMovementOnly(batch) {
		t.Fatal("native two-entry PLAYER_MOVE vector was not classified as movement-only")
	}
}

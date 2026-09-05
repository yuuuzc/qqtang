package probe

import (
	"testing"

	"qqtang/internal/protocol/game"
)

func TestRemovedRoomLobbyPushUsesNativeEmptyCardTombstone(t *testing.T) {
	entry, err := new(Server).roomLobbyPushEntry(1, 9, true)
	if err != nil {
		t.Fatal(err)
	}
	if entry.RoomID != 9 || entry.RoomFlag != game.RoomListFlagPreparing || entry.NumOfPlayer != 0 {
		t.Fatalf("removed ROOM_INFO = %+v, want room 9 with preparing bit and zero population", entry)
	}
	encoded, err := entry.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	// NameLen includes the NUL, followed by RoomID, RoomFlag, MapID and the
	// packed population. The native projection turns waiting+current(0) into
	// iRoomFlag=0 and removes the cached card.
	if len(encoded) < 8 || encoded[4] != byte(game.RoomListFlagPreparing) || encoded[7] != 0 {
		t.Fatalf("removed ROOM_INFO wire bytes = %x", encoded)
	}
}

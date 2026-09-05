package lobby

import (
	"testing"
	"time"
)

func TestHubSeparatesSectionsAndSupportsPeriodicSnapshots(t *testing.T) {
	hub := NewHub()
	one, err := hub.Section(1)
	if err != nil {
		t.Fatal(err)
	}
	two, err := hub.Section(2)
	if err != nil {
		t.Fatal(err)
	}
	if err := one.UpsertPlayer(Presence{UIN: 1_000_001, PlayerID: 1, SectionID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := one.UpsertRoom(RoomSummary{RoomID: 7, OwnerID: 1, GameType: 2, Players: 1, MaxPlayers: 4, Phase: RoomPhasePreparing}); err != nil {
		t.Fatal(err)
	}
	message, err := one.AppendChat(1_000_001, "local", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := one.Snapshot(0)
	if snapshot.SectionID != 1 || len(snapshot.Players) != 1 || len(snapshot.Rooms) != 1 || len(snapshot.Chat) != 1 {
		t.Fatalf("section one snapshot = %+v", snapshot)
	}
	if len(one.Snapshot(message.Sequence).Chat) != 0 {
		t.Fatal("incremental chat snapshot repeated an acknowledged message")
	}
	if snapshot := two.Snapshot(0); len(snapshot.Players) != 0 || len(snapshot.Rooms) != 0 {
		t.Fatalf("section two leaked section one state: %+v", snapshot)
	}
}

package probe

import (
	"testing"

	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
)

func TestCurrentRoomOwnerAfterNonOwnerDepartureKeepsExistingOwner(t *testing.T) {
	server := &Server{}
	one := game.DefaultPlayerProfile()
	two := game.DefaultPlayerProfile()
	two.PlayerID = 2
	if err := server.worldState().EnterLobby(1_000_001, one); err != nil {
		t.Fatal(err)
	}
	if err := server.worldState().EnterLobby(1_000_002, two); err != nil {
		t.Fatal(err)
	}
	created, err := server.worldState().CreateRoom(1_000_001, one, roomstate.MatchSettings{Map: roomstate.RandomMapSelection(), GameType: roomstate.GameTypeAdventure}, roomstate.Properties{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().JoinRoom(1_000_002, two, created.RoomID, two.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	departure, err := server.worldState().LeaveRoom(1_000_002, roomstate.LeaveVoluntary)
	if err != nil {
		t.Fatal(err)
	}
	if departure.NewOwnerID != 0 {
		t.Fatalf("domain transition owner = %d, want unchanged marker 0", departure.NewOwnerID)
	}
	ownerID, err := server.currentRoomOwnerAfterDeparture(created.RoomID, departure)
	if err != nil || ownerID != one.PlayerID {
		t.Fatalf("wire current owner = %d, err=%v, want %d", ownerID, err, one.PlayerID)
	}
}

func TestCurrentRoomOwnerAfterOwnerDepartureUsesMigratedOwner(t *testing.T) {
	server := &Server{}
	one := game.DefaultPlayerProfile()
	two := game.DefaultPlayerProfile()
	two.PlayerID = 2
	if err := server.worldState().EnterLobby(1_000_001, one); err != nil {
		t.Fatal(err)
	}
	if err := server.worldState().EnterLobby(1_000_002, two); err != nil {
		t.Fatal(err)
	}
	created, err := server.worldState().CreateRoom(1_000_001, one, roomstate.MatchSettings{Map: roomstate.RandomMapSelection(), GameType: roomstate.GameTypeAdventure}, roomstate.Properties{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = server.worldState().JoinRoom(1_000_002, two, created.RoomID, two.GameInfo.RoleID, 1); err != nil {
		t.Fatal(err)
	}
	departure, err := server.worldState().LeaveRoom(1_000_001, roomstate.LeaveVoluntary)
	if err != nil {
		t.Fatal(err)
	}
	ownerID, err := server.currentRoomOwnerAfterDeparture(created.RoomID, departure)
	if err != nil || ownerID != two.PlayerID {
		t.Fatalf("wire migrated owner = %d, err=%v, want %d", ownerID, err, two.PlayerID)
	}
}

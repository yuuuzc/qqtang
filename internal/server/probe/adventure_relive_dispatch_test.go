package probe

import (
	"testing"

	"qqtang/internal/game/match"
	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
)

func TestAdventureReliveNotificationUpdatesAdventureBattle(t *testing.T) {
	server, owner, peer := newRoomPeerUDPTestServer(t)
	state, err := server.sessionRoom(owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.SetReady(peer.Profile.PlayerID, true); err != nil {
		t.Fatal(err)
	}
	const gameID uint32 = 77
	if _, err = server.worldState().StartMatchWithID(owner.UIN, gameID); err != nil {
		t.Fatal(err)
	}
	battle, err := match.NewAdventureBattle(gameID, 1601, owner.Profile.PlayerID, []match.AdventureParticipant{
		{PlayerID: owner.Profile.PlayerID, TeamID: 1},
		{PlayerID: peer.Profile.PlayerID, TeamID: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = battle.RecordDeath(peer.Profile.PlayerID); err != nil {
		t.Fatal(err)
	}
	server.battles = map[uint32]*match.AdventureBattle{gameID: battle}
	server.projectRoomMatch(owner.RoomID, gameID, 1601)

	body, err := (game.PlayerReliveEvent{
		PlayerID: peer.Profile.PlayerID, ClientTime: 1_000, Row: 6, Col: 30,
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := game.MarshalGameEventPayload(owner.UIN, 1, game.NotifyPlayerRelive, body)
	if err != nil {
		t.Fatal(err)
	}
	packet := testLocalRoutedPacketWithPayload(
		t, game.GameEventRequestCommand, 3, 0xffff, owner.Profile.PlayerID, owner.UIN, payload,
	)
	result := server.handleGameEventMessage(
		ListenerConfig{Response: ResponseConfig{QQTGameEventRelay: true}}, owner,
		"test", "local", "remote", packet,
	)
	if !result.handled || len(result.response) == 0 || result.result != "qqt_game_event_ack_player_relive_2" {
		t.Fatalf("adventure relive dispatch = %+v", result)
	}
	transition, err := battle.PrepareNextStage(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(transition.Survivors) != 2 || len(transition.SettledOut) != 0 {
		t.Fatalf("stage transition after relive = %+v", transition)
	}
	if category, err := state.Snapshot().Settings.GameType.Category(); err != nil || category != roomstate.CategoryAdventure {
		t.Fatalf("test room category = %d/%v", category, err)
	}
}

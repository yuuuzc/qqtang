package probe

import (
	"testing"

	"qqtang/internal/protocol/game"
)

func TestRoomRoleRefreshNotificationsContainOnlyExistingRemotePlayers(t *testing.T) {
	response := game.EnterRoomResponseOld{Players: []game.EnterRoomPlayer{
		{Player: game.PlayerInfoInRoomOld{UIN: 1_000_001, PlayerID: 1, GameInfo: game.GameInfo{RoleID: 7}}},
		{Player: game.PlayerInfoInRoomOld{UIN: 1_000_003, PlayerID: 3, GameInfo: game.GameInfo{RoleID: 23}}},
		{Player: game.PlayerInfoInRoomOld{UIN: 1_000_002, PlayerID: 2, GameInfo: game.GameInfo{RoleID: 22}}},
	}}

	got := roomRoleRefreshNotifications(response, 2)
	if len(got) != 2 {
		t.Fatalf("role refresh count = %d, want 2: %+v", len(got), got)
	}
	if got[0].PlayerUIN != 1_000_001 || got[0].PlayerID != 1 || got[0].NewRoleID != 7 {
		t.Fatalf("first role refresh = %+v", got[0])
	}
	if got[1].PlayerUIN != 1_000_003 || got[1].PlayerID != 3 || got[1].NewRoleID != 23 {
		t.Fatalf("second role refresh = %+v", got[1])
	}
}

func TestRoomRoleRefreshNotificationsSkipIncompleteRecords(t *testing.T) {
	response := game.EnterRoomResponseOld{Players: []game.EnterRoomPlayer{
		{Player: game.PlayerInfoInRoomOld{UIN: 0, PlayerID: 1, GameInfo: game.GameInfo{RoleID: 7}}},
		{Player: game.PlayerInfoInRoomOld{UIN: 1_000_001, PlayerID: 0, GameInfo: game.GameInfo{RoleID: 7}}},
		{Player: game.PlayerInfoInRoomOld{UIN: 1_000_001, PlayerID: 1, GameInfo: game.GameInfo{RoleID: 0}}},
	}}
	if got := roomRoleRefreshNotifications(response, 2); len(got) != 0 {
		t.Fatalf("incomplete role refreshes = %+v", got)
	}
}

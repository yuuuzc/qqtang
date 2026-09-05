package probe

import (
	"fmt"

	"qqtang/internal/protocol/game"
)

// roomRoleRefreshNotifications projects only the already-present remote
// members. RESPONSE_ENTER_ROOM_OLD carries their RoleID values, but the 5.2
// client does not always rebuild a remote avatar from that first snapshot.
// Replaying the standard NOTIFY_CHANGE_ROLE path after the room UI is mounted
// primes the native avatar cache without changing authoritative room state.
func roomRoleRefreshNotifications(response game.EnterRoomResponseOld, localPlayerID uint16) []game.ChangeRoleNotification {
	notifications := make([]game.ChangeRoleNotification, 0, len(response.Players))
	for _, entry := range response.Players {
		player := entry.Player
		if player.PlayerID == 0 || player.PlayerID == localPlayerID || player.UIN == 0 || player.GameInfo.RoleID == 0 {
			continue
		}
		notifications = append(notifications, game.ChangeRoleNotification{
			PlayerUIN: player.UIN,
			PlayerID:  player.PlayerID,
			NewRoleID: player.GameInfo.RoleID,
		})
	}
	return notifications
}

// sendRoomRoleRefresh is deliberately transport-only: it neither mutates the
// profile nor rebroadcasts to other room members. The room-entry response has
// already been written when this runs, so TCP ordering is the only sequencing
// contract. Do not add a timer unless a controlled trace proves one is needed.
func (server *Server) sendRoomRoleRefresh(session *connectionSession, recipientTemplate []byte, roomID uint16, notifications []game.ChangeRoleNotification) {
	if server == nil || session == nil || session.connection == nil || roomID == 0 || len(notifications) == 0 || len(recipientTemplate) == 0 {
		return
	}
	for _, notification := range notifications {
		packet, err := game.BuildLocalChangeRoleNotificationForRecipient(recipientTemplate, roomID, notification)
		if err != nil {
			server.log(logEvent{
				Level: "error", Event: "room_role_refresh_build_failed", ConnectionID: session.connectionID,
				AccountID: fmt.Sprint(session.UIN), RoomID: fmt.Sprint(roomID), Result: fmt.Sprintf("player_%d_role_%d", notification.PlayerID, notification.NewRoleID), ErrorContext: err.Error(),
			})
			return
		}
		if !server.writeTCP(session.connection, session.connectionID, session.localAddress, session.remoteAddress, packet,
			fmt.Sprintf("qqt_enter_room_role_refresh_player_%d_role_%d", notification.PlayerID, notification.NewRoleID)) {
			return
		}
	}
}

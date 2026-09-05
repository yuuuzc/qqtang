package probe

import (
	"context"
	"fmt"

	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
)

// handleKickOffPlayerMessage owns the preparation-room kick transaction. The
// room state is changed before the success response is exposed, while client
// notifications are delivered afterwards from each recipient's own packet
// template.
func (server *Server) handleKickOffPlayerMessage(config ListenerConfig, actor *connectionSession, connectionID string, data []byte) protocolMessageResult {
	if !config.Response.QQTRoomList || actor == nil {
		return protocolMessageResult{}
	}
	request, err := game.DecodeLocalKickOffPlayerRequest(data)
	if err != nil {
		return protocolMessageResult{}
	}
	fail := func(reason string) protocolMessageResult {
		response, responseErr := game.BuildLocalKickOffPlayerResponse(data, game.KickOffPlayerFailed)
		if responseErr != nil {
			server.log(logEvent{Level: "error", Event: "room_kick_response_failed", ConnectionID: connectionID, AccountID: fmt.Sprint(actor.UIN), ErrorContext: responseErr.Error()})
			return protocolMessageResult{handled: true, result: "qqt_room_kick_response_failed"}
		}
		server.log(logEvent{Level: "info", Event: "room_kick_rejected", ConnectionID: connectionID, AccountID: fmt.Sprint(actor.UIN), RoomID: fmt.Sprint(actor.RoomID), Result: reason})
		return protocolMessageResult{handled: true, response: response, result: "qqt_room_kick_rejected_" + reason}
	}
	if request.UIN != actor.UIN || actor.RoomID == 0 || request.PlayerUIN == actor.UIN || request.PlayerID == actor.Profile.PlayerID {
		return fail("invalid_target")
	}
	state, stateErr := server.sessionRoom(actor)
	if stateErr != nil {
		return fail("room_unavailable")
	}
	snapshot := state.Snapshot()
	if snapshot.Phase != roomstate.PhasePreparing || snapshot.OwnerID != actor.Profile.PlayerID {
		return fail("not_preparing_owner")
	}

	target := server.liveRoomSessionForPlayer(actor.RoomID, request.PlayerID)
	if target == nil {
		return fail("target_offline")
	}
	if target.liveRoomID.Load() != uint32(actor.RoomID) || target.liveUIN.Load() != request.PlayerUIN || target.routingPlayerID() != request.PlayerID {
		return fail("target_mismatch")
	}
	targetProfile := server.config.playerProfileForUIN(request.PlayerUIN)
	if server.playerStore != nil {
		targetProfile, err = server.playerStore.Load(context.Background(), request.PlayerUIN)
		if err != nil {
			return fail("target_profile")
		}
	} else if target.mu.TryLock() {
		targetProfile = clonePlayerProfile(target.Profile)
		target.mu.Unlock()
	} else {
		return fail("target_busy")
	}
	if server.profileHasKickProtection(targetProfile) {
		return fail("protected")
	}
	targetTemplate := target.packetTemplate()
	response, buildErr := game.BuildLocalKickOffPlayerResponse(data, game.KickOffPlayerSuccess)
	if buildErr != nil {
		return fail("response_build")
	}
	leaveNotification := game.LeaveRoomNotification{
		PlayerID: request.PlayerID, NewRoomOwnerID: actor.Profile.PlayerID,
		NewArbitratorID: 0, MapID: selectedRoomMapID(snapshot),
	}
	followUp, buildErr := game.BuildLocalLeaveRoomNotification(data, actor.RoomID, leaveNotification)
	if buildErr != nil {
		return fail("leave_notification_build")
	}
	var targetNotification []byte
	if len(targetTemplate) != 0 {
		targetNotification, buildErr = game.BuildLocalKickOffRoomNotification(targetTemplate, actor.RoomID, game.KickOffRoomNotification{
			RoomID: actor.RoomID, PlayerUIN: request.PlayerUIN,
		})
		if buildErr != nil {
			return fail("target_notification_build")
		}
	}
	departure, leaveErr := server.worldState().LeaveRoom(request.PlayerUIN, roomstate.LeaveKicked)
	if leaveErr != nil {
		initialErr := leaveErr
		departure, leaveErr = server.worldState().ReconcileRoomDeparture(request.PlayerUIN, actor.RoomID, request.PlayerID, roomstate.LeaveKicked)
		if leaveErr != nil {
			server.log(logEvent{
				Level: "error", Event: "room_kick_state_reconcile_failed", ConnectionID: connectionID,
				AccountID: fmt.Sprint(actor.UIN), RoomID: fmt.Sprint(actor.RoomID), Result: "state_rejected",
				ErrorContext: fmt.Sprintf("leave: %v; reconcile: %v", initialErr, leaveErr),
			})
			return fail("state_rejected")
		}
	}
	// World is authoritative. Retire the recipient from routing immediately and
	// let its own connection actor consume the local projection without a
	// cross-session lock edge.
	target.liveRoomID.Store(0)
	target.applyOrEnqueueProjection(sessionProjection{kind: sessionProjectionLeaveRoom, roomID: actor.RoomID})
	roomID := actor.RoomID
	actorUIN := actor.UIN
	targetUIN := request.PlayerUIN
	postResponse := func() {
		if len(targetNotification) != 0 {
			server.writeTCP(target.connection, target.connectionID, target.localAddress, target.remoteAddress, targetNotification, "qqt_room_kicked")
		}
		server.broadcastRoomNotification(roomID, actorUIN, "qqt_room_kick_peer_leave", func(recipient []byte) ([]byte, error) {
			return game.BuildLocalLeaveRoomNotification(recipient, roomID, leaveNotification)
		})
	}
	server.log(logEvent{Level: "info", Event: "room_player_kicked", ConnectionID: connectionID, AccountID: fmt.Sprint(actor.UIN), RoomID: fmt.Sprint(roomID), Result: fmt.Sprintf("player_%d_uin_%d_reason_%d", departure.Member.PlayerID, targetUIN, departure.Reason)})
	return protocolMessageResult{
		handled: true, response: response, result: "qqt_room_kick_success",
		followUp: followUp, followUpResult: "qqt_room_kick_owner_leave_notify", postResponse: postResponse,
	}
}

func selectedRoomMapID(snapshot roomstate.Snapshot) uint16 {
	if snapshot.Settings.Map.Kind == roomstate.MapSelectionFixed && snapshot.Settings.Map.MapID <= 0xFFFF {
		return uint16(snapshot.Settings.Map.MapID)
	}
	return 0
}

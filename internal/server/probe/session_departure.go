package probe

import (
	"fmt"
	"math"

	"qqtang/internal/game/match"
	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
)

func (server *Server) leaveSessionRoom(session *connectionSession, reason roomstate.LeaveReason) (roomstate.Departure, error) {
	if session == nil || session.RoomID == 0 {
		return roomstate.Departure{}, fmt.Errorf("session is not in a room")
	}
	worldUIN := sessionWorldUIN(session)
	departure, err := server.worldState().LeaveRoom(worldUIN, reason)
	if err != nil {
		initialErr := err
		departure, err = server.worldState().ReconcileRoomDeparture(worldUIN, session.RoomID, session.Profile.PlayerID, reason)
		if err != nil {
			return roomstate.Departure{}, fmt.Errorf("leave room: %v; reconcile: %w", initialErr, err)
		}
		server.log(logEvent{
			Level: "warn", Event: "room_departure_reconciled", ConnectionID: session.connectionID,
			AccountID: fmt.Sprint(session.UIN), RoomID: fmt.Sprint(session.RoomID),
			Result: "stale_world_membership_removed", ErrorContext: initialErr.Error(),
		})
	}
	session.setRoomID(0)
	session.worldUIN = 0
	return departure, nil
}

// currentRoomOwnerAfterDeparture translates domain transition state into the
// NOTIFY_LEAVE_ROOM field expected by the native client. room.Departure keeps
// NewOwnerID zero when a non-owner leaves, while the wire field represents the
// current owner after every departure. Sending that zero cleared the surviving
// owner's local host flag and changed its Start button into Ready.
func (server *Server) currentRoomOwnerAfterDeparture(roomID uint16, departure roomstate.Departure) (uint16, error) {
	if departure.Empty {
		return 0, nil
	}
	if departure.NewOwnerID != 0 {
		return departure.NewOwnerID, nil
	}
	state, active := server.worldState().Room(roomID)
	if !active || state == nil {
		return 0, fmt.Errorf("non-empty room %d disappeared while projecting its owner", roomID)
	}
	ownerID := state.Snapshot().OwnerID
	if ownerID == 0 {
		return 0, fmt.Errorf("non-empty room %d has no current owner", roomID)
	}
	return ownerID, nil
}

// handleActiveAdventureExitToLobby models explicit navigation away from a
// match, such as leaving an active game or logging out after settlement.
func (server *Server) handleActiveAdventureExitToLobby(session *connectionSession, connectionID string) error {
	gameID, roomID := session.CurrentGameID, session.RoomID
	var leaveMapID uint16
	if state, err := server.sessionRoom(session); err == nil {
		selected := state.Snapshot().Settings.Map
		if selected.Kind == roomstate.MapSelectionFixed && selected.MapID <= math.MaxUint16 {
			leaveMapID = uint16(selected.MapID)
		}
	}
	category, categoryErr := server.activeSessionMatchCategory(session)
	wasActiveMatch := gameID != 0 && categoryErr == nil && (category == roomstate.CategoryAdventure || category == roomstate.CategoryCompetitive)
	var newArbitratorID uint16
	var adventureTransition adventureDepartureTransition
	var competitiveTransition competitiveDepartureTransition
	competitiveDeparture := gameID != 0 && categoryErr == nil && category == roomstate.CategoryCompetitive
	transition := "active_exit"
	if gameID != 0 {
		if competitiveDeparture {
			competitiveTransition = server.handleCompetitiveDeparture(session, connectionID)
			newArbitratorID = competitiveTransition.ArbitratorPlayerID
		} else {
			adventureTransition = server.handleAdventureDeparture(session, connectionID, match.AdventureDepartureLeftMatch)
			newArbitratorID = adventureTransition.ArbitratorPlayerID
		}
	}
	departure, err := server.leaveSessionRoom(session, roomstate.LeaveVoluntary)
	if err != nil {
		return err
	}
	// GAME_OVER updates the active scene, but it does not mutate the native
	// room model retained underneath that scene. The ordinary room-departure
	// push updates that model and Client+0x1A2615 translates it into exactly one
	// in-scene 0x10EC player cleanup. Authority is rebound separately below.
	departureProjected := false
	if !departure.Empty && departure.Member.PlayerID != 0 {
		currentOwnerID, ownerErr := server.currentRoomOwnerAfterDeparture(roomID, departure)
		if ownerErr != nil {
			server.log(logEvent{
				Level: "error", Event: "active_leave_owner_projection_failed", ConnectionID: connectionID,
				AccountID: fmt.Sprint(session.UIN), RoomID: fmt.Sprint(roomID), Result: "notification_skipped",
				ErrorContext: ownerErr.Error(),
			})
		} else {
			notification := game.LeaveRoomNotification{
				PlayerID: departure.Member.PlayerID, NewRoomOwnerID: currentOwnerID,
				NewArbitratorID: newArbitratorID, MapID: leaveMapID,
			}
			server.broadcastRoomNotification(roomID, session.UIN, "qqt_active_leave_room_peer_notify", func(recipient []byte) ([]byte, error) {
				return game.BuildLocalLeaveRoomNotification(recipient, roomID, notification)
			})
			departureProjected = true
		}
	}
	// A departure-caused settlement is intentionally finalized only after the
	// native room model consumed NOTIFY_LEAVE_ROOM.  Otherwise GAME_OVER can
	// retain the exited avatar as a ghost room member.
	if gameID != 0 {
		if competitiveDeparture {
			server.finishCompetitiveDeparture(session, connectionID, roomID, gameID, competitiveTransition)
		} else {
			server.finishAdventureDeparture(session, connectionID, roomID, gameID, adventureTransition)
		}
	}
	if wasActiveMatch && departureProjected && newArbitratorID != 0 {
		server.broadcastActiveArbitratorChange(roomID, gameID, session.UIN, newArbitratorID)
	}
	session.CurrentGameID = 0
	session.CurrentStageGameID = 0
	session.CurrentMapID = 0
	server.log(logEvent{
		Level: "info", Event: "match_player_returned_to_lobby", ConnectionID: connectionID,
		RoomID: fmt.Sprint(roomID), Result: fmt.Sprintf("game_%d_%s", gameID, transition),
	})
	return nil
}

func (server *Server) handleConnectionDeparture(session *connectionSession, connectionID string) {
	// QQTShop owns independent catalog and session TCP objects. Exiting the
	// shop closes those objects, not the authenticated hall connection. Keep
	// this guard first so no future navigation/room cleanup can accidentally
	// turn an auxiliary close into an account disconnect.
	if session.auxiliary {
		return
	}
	if server.handoffAccountAuthNavigation(session) {
		return
	}
	if session.continuation && session.liveUIN.Load() == 0 {
		return
	}
	if server.promoteSessionContinuation(session, connectionID) {
		return
	}
	roomID := session.RoomID
	actorUIN := session.UIN
	sectionID := session.Profile.SectionID
	gameID := session.CurrentGameID
	category, categoryErr := server.activeSessionMatchCategory(session)
	wasActiveMatch := gameID != 0 && categoryErr == nil && (category == roomstate.CategoryAdventure || category == roomstate.CategoryCompetitive)
	var leaveMapID uint16
	if roomID != 0 {
		if state, err := server.sessionRoom(session); err == nil {
			snapshot := state.Snapshot()
			if snapshot.Settings.Map.Kind == roomstate.MapSelectionFixed && snapshot.Settings.Map.MapID <= math.MaxUint16 {
				leaveMapID = uint16(snapshot.Settings.Map.MapID)
			}
		}
	}
	var departure roomstate.Departure
	var leaveErr error
	var newArbitratorID uint16
	var adventureTransition adventureDepartureTransition
	var competitiveTransition competitiveDepartureTransition
	competitiveDeparture := gameID != 0 && categoryErr == nil && category == roomstate.CategoryCompetitive
	if roomID != 0 {
		if competitiveDeparture {
			competitiveTransition = server.handleCompetitiveDeparture(session, connectionID)
			newArbitratorID = competitiveTransition.ArbitratorPlayerID
		} else {
			adventureTransition = server.handleAdventureDeparture(session, connectionID, match.AdventureDepartureDisconnected)
			newArbitratorID = adventureTransition.ArbitratorPlayerID
		}
		departure, leaveErr = server.leaveSessionRoom(session, roomstate.LeaveDisconnected)
	}
	if leaveErr != nil {
		server.log(logEvent{
			Level: "error", Event: "disconnect_room_departure_failed", ConnectionID: connectionID,
			AccountID: fmt.Sprint(actorUIN), RoomID: fmt.Sprint(roomID), Result: "world_state_retained",
			ErrorContext: leaveErr.Error(),
		})
	}
	if leaveErr == nil && roomID != 0 {
		server.broadcastRoomLobbyMutationExcept(sectionID, roomID, departure.Empty, actorUIN, "qqt_room_lobby_push_disconnect")
	}
	// NOTIFY_LEAVE_ROOM removes the departed player from the room model and its
	// Client+0x1A2615 path performs one in-scene 0x10EC cleanup. It does not
	// change the battle arbitrator: Client+0x202B03 handles that through 0x0FB1.
	departureProjected := false
	if leaveErr == nil && !departure.Empty && departure.Member.PlayerID != 0 {
		currentOwnerID, ownerErr := server.currentRoomOwnerAfterDeparture(roomID, departure)
		if ownerErr != nil {
			server.log(logEvent{Level: "error", Event: "disconnect_leave_owner_projection_failed", ConnectionID: connectionID, AccountID: fmt.Sprint(actorUIN), RoomID: fmt.Sprint(roomID), Result: "notification_skipped", ErrorContext: ownerErr.Error()})
			currentOwnerID = 0
		}
		notification := game.LeaveRoomNotification{
			PlayerID: departure.Member.PlayerID, NewRoomOwnerID: currentOwnerID,
			NewArbitratorID: newArbitratorID, MapID: leaveMapID,
		}
		if currentOwnerID != 0 {
			server.broadcastRoomNotification(roomID, actorUIN, "qqt_disconnect_leave_room_peer_notify", func(recipient []byte) ([]byte, error) {
				return game.BuildLocalLeaveRoomNotification(recipient, roomID, notification)
			})
			departureProjected = true
		}
	}
	if leaveErr == nil && gameID != 0 {
		if competitiveDeparture {
			server.finishCompetitiveDeparture(session, connectionID, roomID, gameID, competitiveTransition)
		} else {
			server.finishAdventureDeparture(session, connectionID, roomID, gameID, adventureTransition)
		}
	}
	if wasActiveMatch && departureProjected && newArbitratorID != 0 {
		server.broadcastActiveArbitratorChange(roomID, gameID, actorUIN, newArbitratorID)
	}
	session.CurrentGameID = 0
	session.CurrentStageGameID = 0
	session.CurrentMapID = 0
	if session.UIN != 0 {
		server.notifyFriendOwnersStatus(session.UIN, false, nil)
		server.worldState().LeaveLobby(session.UIN, session.Profile.SectionID)
	}
}

func (server *Server) broadcastActiveArbitratorChange(roomID uint16, gameID uint32, actorUIN uint32, arbitratorID uint16) {
	if roomID == 0 || gameID == 0 || arbitratorID == 0 {
		return
	}
	sequence := server.nextGameDataSequence()
	server.broadcastRoomNotification(roomID, actorUIN, "qqt_active_arbitrator_change_peer_notify", func(recipientPacket []byte) ([]byte, error) {
		return game.BuildLocalArbitratorChangeNotify(recipientPacket, roomID, sequence, arbitratorID)
	})
}

// logoutSession completes the global account transition before the legacy
// client tears down its current game-server socket. This differs from a room
// leave: REQUEST_LOGOUT is routed through the player-profile subsystem and the
// same client may immediately connect again to the selected lobby section.
func (server *Server) logoutSession(session *connectionSession, connectionID string) error {
	if session == nil || session.UIN == 0 {
		return fmt.Errorf("cannot logout an inactive session")
	}
	uin := session.UIN
	if session.RoomID != 0 {
		if session.CurrentGameID != 0 {
			if err := server.handleActiveAdventureExitToLobby(session, connectionID); err != nil {
				return err
			}
		} else if _, err := server.leaveSessionRoom(session, roomstate.LeaveVoluntary); err != nil {
			return err
		}
	}
	if session.Profile.SectionID != 0 {
		server.notifyFriendOwnersStatus(uin, false, nil)
		server.worldState().LeaveLobby(uin, session.Profile.SectionID)
	}
	session.CurrentGameID = 0
	session.CurrentStageGameID = 0
	session.CurrentMapID = 0
	session.setRoomID(0)
	// Returning to the district list is an authenticated account-navigation
	// state, not a full password sign-out. Keep a host-bound, one-time grant
	// with no room/match snapshot so the user may spend time choosing another
	// district. Do not retain the short transport-reconnect snapshot here:
	// leaving a district is a clean topology boundary and must never restore a
	// stale room or match when another district is selected later.
	server.grantAccountNavigation(session.remoteAddress, uin)
	server.forgetAuthenticatedSession(session.remoteAddress, uin)
	session.setUIN(0)
	server.log(logEvent{
		Level: "info", Event: "player_logged_out", ConnectionID: connectionID,
		AccountID: fmt.Sprint(uin), Result: "district_navigation_grant_retained",
	})
	return nil
}

func (server *Server) removeSettledOutRoomMembers(roomID uint16, settlements []match.AdventureSettlement) error {
	for _, settlement := range settlements {
		if _, err := server.worldState().RemoveMember(roomID, settlement.Participant.PlayerID, roomstate.LeaveEliminatedBetweenStages); err != nil {
			return err
		}
	}
	return nil
}

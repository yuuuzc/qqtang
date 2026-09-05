package probe

import (
	"fmt"

	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
)

// handleRoomMessage adapts room commands to the protocol-independent room
// state. Match events and lobby queries are owned by separate handlers.
func (server *Server) handleRoomMessage(config ListenerConfig, session *connectionSession, id string, data []byte) protocolMessageResult {
	inspection, err := game.InspectLocalPacket(data)
	if err != nil {
		return protocolMessageResult{}
	}
	switch inspection.Command {
	case game.ChangeTeamCommand, game.ChangeRoleCommand,
		game.ReadyCommand, game.CancelReadyCommand,
		game.SetSeatStatusCommand, game.ChangeRoomTypeCommand,
		game.ModifyRoomInfoCommand, game.ModifyRoomCommand,
		game.KickOffPlayerCommand, game.LeaveRoomCommand, game.HelloCommand,
		game.BackgroundListCommand, game.UseBackgroundCommand, game.UseBackgroundNotifyCommand:
	default:
		return protocolMessageResult{}
	}
	if inspection.Command == game.KickOffPlayerCommand {
		return server.handleKickOffPlayerMessage(config, session, id, data)
	}
	if inspection.Command == game.BackgroundListCommand || inspection.Command == game.UseBackgroundCommand || inspection.Command == game.UseBackgroundNotifyCommand {
		return server.handleChatBackgroundMessage(session, id, data)
	}
	var response []byte
	result := ""
	var followUp []byte
	followUpResult := "qqt_room_followup"
	var postResponse func()

	if len(response) == 0 && config.Response.gameBeginEnabled() {
		if selection, selectionErr := game.DecodeLocalChangeTeamRequest(data); selectionErr == nil {
			response, selectionErr = game.BuildLocalChangeTeamSuccess(data)
			if selectionErr == nil {
				selectionErr = server.updateSessionRoomTeam(session, selection.TeamID)
			}
			if selectionErr == nil {
				notification := game.ChangeTeamNotification{
					PlayerUIN: session.UIN, PlayerID: session.Profile.PlayerID, NewTeamID: selection.TeamID,
				}
				followUp, selectionErr = game.BuildLocalChangeTeamNotification(data, session.RoomID, notification)
				followUpResult = fmt.Sprintf("qqt_room_team_notify_%d", selection.TeamID)
				roomID := session.RoomID
				postResponse = func() {
					server.broadcastRoomNotification(roomID, session.UIN, followUpResult+"_peer", func(recipient []byte) ([]byte, error) {
						return game.BuildLocalChangeTeamNotificationForRecipient(recipient, roomID, notification)
					})
				}
			}
			if selectionErr != nil {
				response = nil
				followUp = nil
				server.log(logEvent{Level: "warn", Event: "room_team_selection_rejected", ConnectionID: id, MessageID: fmt.Sprintf("0x%04X", game.ChangeTeamCommand), Result: "rejected", ErrorContext: selectionErr.Error()})
			} else {
				result = fmt.Sprintf("qqt_room_team_selected_%d", selection.TeamID)
			}
		}
	}
	if len(response) == 0 && config.Response.gameBeginEnabled() {
		if selection, selectionErr := game.DecodeLocalChangeRoleRequest(data); selectionErr == nil {
			response, selectionErr = game.BuildLocalChangeRoleSuccess(data)
			resolvedRoleID := selection.RoleID
			if selectionErr == nil {
				resolvedRoleID, selectionErr = server.updateSessionRoomRole(session, selection.RoleID)
			}
			if selectionErr == nil {
				notification := game.ChangeRoleNotification{
					PlayerUIN: session.UIN, PlayerID: session.Profile.PlayerID, NewRoleID: resolvedRoleID,
				}
				followUp, selectionErr = game.BuildLocalChangeRoleNotification(data, session.RoomID, notification)
				followUpResult = fmt.Sprintf("qqt_room_role_notify_%d", resolvedRoleID)
				roomID := session.RoomID
				postResponse = func() {
					server.broadcastRoomNotification(roomID, session.UIN, followUpResult+"_peer", func(recipient []byte) ([]byte, error) {
						return game.BuildLocalChangeRoleNotificationForRecipient(recipient, roomID, notification)
					})
				}
			}
			if selectionErr != nil {
				response = nil
				followUp = nil
				server.log(logEvent{Level: "warn", Event: "room_role_selection_rejected", ConnectionID: id, MessageID: fmt.Sprintf("0x%04X", game.ChangeRoleCommand), Result: "rejected", ErrorContext: selectionErr.Error()})
			} else {
				result = fmt.Sprintf("qqt_room_role_selected_%d_resolved_%d", selection.RoleID, resolvedRoleID)
			}
		}
	}
	if len(response) == 0 && config.Response.QQTRoomList {
		if readyRequest, readyErr := game.DecodeLocalReadyRequest(data); readyErr == nil {
			roomID := session.RoomID
			response, readyErr = game.BuildLocalReadySuccess(data)
			if readyErr == nil {
				readyErr = server.updateSessionRoomReady(session, readyRequest.Ready)
			}
			if readyErr == nil {
				notification := game.ReadyStateNotification{
					PlayerUIN: session.UIN, PlayerID: session.Profile.PlayerID, Ready: readyRequest.Ready,
				}
				followUp, readyErr = game.BuildLocalReadyStateNotification(data, roomID, notification)
				readyWord := "cancelled"
				if readyRequest.Ready {
					readyWord = "ready"
				}
				followUpResult = fmt.Sprintf("qqt_room_player_%d_%s_notify", session.Profile.PlayerID, readyWord)
				postResponse = func() {
					server.broadcastRoomNotification(roomID, session.UIN, followUpResult+"_peer", func(recipient []byte) ([]byte, error) {
						return game.BuildLocalReadyStateNotification(recipient, roomID, notification)
					})
				}
			}
			if readyErr != nil {
				response = nil
				followUp = nil
				server.log(logEvent{Level: "warn", Event: "room_ready_change_rejected", ConnectionID: id, MessageID: fmt.Sprintf("0x%04X", game.ReadyCommand), Result: "rejected", ErrorContext: readyErr.Error()})
			} else {
				result = fmt.Sprintf("qqt_room_player_%d_ready_%t", session.Profile.PlayerID, readyRequest.Ready)
			}
		}
	}
	if len(response) == 0 && config.Response.gameBeginEnabled() {
		if seatRequest, seatErr := game.DecodeLocalSetSeatStatusRequest(data); seatErr == nil {
			roomID := session.RoomID
			response, seatErr = game.BuildLocalSetSeatStatusSuccess(data)
			if seatErr == nil {
				seatErr = server.updateSessionRoomSeatStatus(session, seatRequest.SeatID, seatRequest.Locked())
			}
			if seatErr == nil {
				followUp, seatErr = game.BuildLocalSetSeatStatusNotification(data, roomID, seatRequest.SeatID, seatRequest.NewStatus)
				followUpResult = fmt.Sprintf("qqt_room_seat_%d_status_%d_notify", seatRequest.SeatID, seatRequest.NewStatus)
				postResponse = func() {
					server.broadcastRoomNotification(roomID, session.UIN, followUpResult+"_peer", func(recipient []byte) ([]byte, error) {
						return game.BuildLocalSetSeatStatusNotification(recipient, roomID, seatRequest.SeatID, seatRequest.NewStatus)
					})
					server.broadcastRoomLobbyMutation(session.Profile.SectionID, roomID, false, "qqt_room_lobby_push_seat")
				}
			}
			if seatErr != nil {
				response = nil
				followUp = nil
				server.log(logEvent{Level: "warn", Event: "room_seat_status_rejected", ConnectionID: id, MessageID: fmt.Sprintf("0x%04X", game.SetSeatStatusCommand), Result: "rejected", ErrorContext: seatErr.Error()})
			} else {
				result = fmt.Sprintf("qqt_room_seat_%d_status_%d", seatRequest.SeatID, seatRequest.NewStatus)
			}
		}
	}
	if len(response) == 0 && config.Response.gameBeginEnabled() {
		if selection, selectionErr := game.DecodeLocalChangeRoomTypeRequest(data); selectionErr == nil {
			roomID := session.RoomID
			response, selectionErr = game.BuildLocalChangeRoomTypeSuccess(data)
			if selectionErr == nil {
				selectionErr = server.updateSessionRoomGameType(session, selection.GameType, selection.ContinueID)
			}
			if selectionErr != nil {
				response = nil
				server.log(logEvent{Level: "warn", Event: "room_type_change_rejected", ConnectionID: id, MessageID: fmt.Sprintf("0x%04X", game.ChangeRoomTypeCommand), Result: "rejected", ErrorContext: selectionErr.Error()})
			} else {
				result = fmt.Sprintf("qqt_room_type_changed_%d", selection.GameType)
				postResponse = func() {
					server.broadcastRoomNotification(roomID, session.UIN, result+"_peer", func(recipient []byte) ([]byte, error) {
						return game.BuildLocalChangeRoomTypeNotification(recipient, roomID, selection.GameType, selection.ContinueID)
					})
					server.broadcastRoomLobbyMutation(session.Profile.SectionID, roomID, false, "qqt_room_lobby_push_type")
				}
			}
		}
	}
	if len(response) == 0 && config.Response.gameBeginEnabled() {
		if inspection, inspectErr := game.InspectLocalPacket(data); inspectErr == nil && inspection.Command == game.ModifyRoomInfoCommand {
			selection, selectionErr := game.DecodeLocalModifyRoomInfoRequest(data)
			// Captured first-stage selections carry ContinueID=0 even when the
			// installed Continue.ini sequence ID is non-zero. MapID is the
			// authoritative room selection here; continuation IDs are validated
			// only by REQUEST_GAME_NEXTMAP between stages.
			if selectionErr == nil {
				selectionErr = server.validateSessionRoomMapSelection(session, uint32(selection.MapID))
			}
			if selectionErr != nil {
				contextText := selectionErr.Error()
				server.log(logEvent{Level: "warn", Event: "room_map_selection_rejected", ConnectionID: id, MessageID: fmt.Sprintf("0x%04X", inspection.Command), Result: "rejected", ErrorContext: contextText})
			} else {
				response, selectionErr = game.BuildLocalModifyRoomInfoSuccess(data)
				if selectionErr == nil {
					if selection.MapID == 0 {
						selectionErr = server.updateSessionRoomRandomMap(session, selection.GameType, selection.ContinueID)
					} else {
						selectionErr = server.updateSessionRoomSettings(session, uint32(selection.MapID), selection.GameType, selection.ContinueID)
					}
				}
				if selectionErr != nil {
					response = nil
					server.log(logEvent{Level: "warn", Event: "room_map_selection_rejected", ConnectionID: id, MessageID: fmt.Sprintf("0x%04X", inspection.Command), Result: "rejected", ErrorContext: selectionErr.Error()})
				} else {
					roomID := session.RoomID
					result = fmt.Sprintf("qqt_room_map_selected_%d", selection.MapID)
					postResponse = func() {
						server.broadcastRoomNotification(roomID, session.UIN, result+"_peer", func(recipient []byte) ([]byte, error) {
							return game.BuildLocalModifyRoomInfoNotification(recipient, roomID, selection.MapID, selection.GameType, selection.ContinueID)
						})
						server.broadcastRoomLobbyMutation(session.Profile.SectionID, roomID, false, "qqt_room_lobby_push_map")
					}
				}
			}
		}
	}
	if len(response) == 0 && config.Response.QQTRoomList {
		if modifyRequest, modifyErr := game.DecodeLocalModifyRoomRequest(data); modifyErr == nil {
			roomID := session.RoomID
			modifiedRoom, updateErr := server.updateSessionRoomProperties(session, modifyRequest)
			modifyErr = updateErr
			if modifyErr == nil {
				response, modifyErr = game.BuildLocalModifyRoomSuccess(data)
			}
			if modifyErr != nil {
				response = nil
				server.log(logEvent{Level: "warn", Event: "room_modify_rejected", ConnectionID: id, MessageID: fmt.Sprintf("0x%04X", game.ModifyRoomCommand), Result: "rejected", ErrorContext: modifyErr.Error()})
			} else {
				result = fmt.Sprintf("qqt_room_modified_flags_0x%X", uint32(modifyRequest.Flags))
				notificationFlag := modifiedRoom.Properties.Flag
				notificationPassword := modifiedRoom.Properties.Password
				postResponse = func() {
					server.broadcastRoomNotification(roomID, session.UIN, result+"_peer", func(recipient []byte) ([]byte, error) {
						return game.BuildLocalModifyRoomNotificationForRecipient(recipient, roomID, modifyRequest.Flags, modifyRequest.RoomName, notificationFlag, notificationPassword)
					})
					server.broadcastRoomLobbyMutation(session.Profile.SectionID, roomID, false, "qqt_room_lobby_push_properties")
				}
			}
		}
	}
	if len(response) == 0 && config.Response.QQTRoomList {
		if leaveRequest, leaveErr := game.DecodeLocalLeaveRoomRequest(data); leaveErr == nil {
			roomID := session.RoomID
			// Between-stage elimination removes the member immediately for the
			// surviving room, but the native eliminated client sends its ordinary
			// leave request only after consuming GAME_OVER. Acknowledge that real
			// request idempotently from its saved room correlation.
			if roomID == 0 && session.detachedRoomID != 0 {
				roomID = session.detachedRoomID
				response, leaveErr = game.BuildLocalLeaveRoomSuccess(data)
				if leaveErr == nil {
					session.detachedRoomID = 0
					result = fmt.Sprintf("qqt_confirm_detached_room_exit_%d", roomID)
				}
				return protocolMessageResult{handled: leaveErr == nil, response: response, result: result}
			}
			wasActiveMatch := session.CurrentGameID != 0
			var leaveMapID uint16
			if state, stateErr := server.sessionRoom(session); stateErr == nil {
				snapshot := state.Snapshot()
				selected := snapshot.Settings.Map
				if selected.Kind == roomstate.MapSelectionFixed && selected.MapID <= 0xFFFF {
					leaveMapID = uint16(selected.MapID)
				}
			}
			var departure roomstate.Departure
			response, leaveErr = game.BuildLocalLeaveRoomSuccess(data)
			if leaveErr == nil && wasActiveMatch {
				leaveErr = server.handleActiveAdventureExitToLobby(session, id)
			} else if leaveErr == nil {
				departure, leaveErr = server.leaveSessionRoom(session, roomstate.LeaveVoluntary)
			}
			if leaveErr != nil {
				response = nil
				server.log(logEvent{Level: "warn", Event: "room_leave_rejected", ConnectionID: id, AccountID: fmt.Sprint(leaveRequest.UIN), RoomID: fmt.Sprint(roomID), MessageID: fmt.Sprintf("0x%04X", game.LeaveRoomCommand), Result: "rejected", ErrorContext: leaveErr.Error()})
			} else if wasActiveMatch {
				result = fmt.Sprintf("qqt_leave_active_room_%d", roomID)
				sectionID := session.Profile.SectionID
				_, roomStillActive := server.worldState().Room(roomID)
				postResponse = appendPostResponse(postResponse, func() {
					server.broadcastRoomLobbyMutation(sectionID, roomID, !roomStillActive, "qqt_room_lobby_push_active_leave")
				})
			} else {
				currentOwnerID, ownerErr := server.currentRoomOwnerAfterDeparture(roomID, departure)
				if ownerErr != nil {
					response = nil
					server.log(logEvent{Level: "error", Event: "room_leave_owner_projection_failed", ConnectionID: id, AccountID: fmt.Sprint(leaveRequest.UIN), RoomID: fmt.Sprint(roomID), Result: "rejected", ErrorContext: ownerErr.Error()})
				} else {
					result = fmt.Sprintf("qqt_leave_room_%d_empty_%t_owner_%d", roomID, departure.Empty, currentOwnerID)
				}
				if ownerErr == nil && !departure.Empty && departure.Member.PlayerID != 0 {
					notification := game.LeaveRoomNotification{
						PlayerID: departure.Member.PlayerID, NewRoomOwnerID: currentOwnerID,
						NewArbitratorID: 0, MapID: leaveMapID,
					}
					postResponse = func() {
						server.broadcastRoomNotification(roomID, session.UIN, "qqt_leave_room_peer_notify", func(recipient []byte) ([]byte, error) {
							return game.BuildLocalLeaveRoomNotification(recipient, roomID, notification)
						})
					}
				}
				if ownerErr == nil {
					sectionID := session.Profile.SectionID
					postResponse = appendPostResponse(postResponse, func() {
						server.broadcastRoomLobbyMutation(sectionID, roomID, departure.Empty, "qqt_room_lobby_push_leave")
					})
				}
			}
		}
	}
	if len(response) == 0 && config.Response.QQTRoomList {
		if helloRequest, helloErr := game.DecodeLocalHelloRequest(data); helloErr == nil {
			var rooms []game.RoomListEntry
			rooms, helloErr = server.roomListEntries(session)
			if helloErr == nil {
				response, helloErr = game.BuildLocalHelloSuccess(data)
			}
			if helloErr == nil {
				followUp, helloErr = game.BuildLocalRoomPush(data, session.Profile.SectionID, rooms)
			}
			if helloErr != nil {
				response = nil
				followUp = nil
				server.log(logEvent{Level: "error", Event: "hello_room_push_failed", ConnectionID: id, AccountID: fmt.Sprint(helloRequest.UIN), Result: "rejected", ErrorContext: helloErr.Error()})
			} else {
				result = "qqt_hello_success"
				followUpResult = fmt.Sprintf("qqt_room_push_%d", len(rooms))
			}
		}
	}

	handled := len(response) != 0 || result != "" || len(followUp) != 0 || postResponse != nil
	return protocolMessageResult{
		handled: handled, response: response, result: result,
		followUp: followUp, followUpResult: followUpResult, postResponse: postResponse,
	}
}

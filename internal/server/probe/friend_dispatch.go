package probe

import (
	"context"
	"errors"
	"fmt"

	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

func (server *Server) handleFriendMessage(session *connectionSession, connectionID string, data []byte) protocolMessageResult {
	inspection, err := game.InspectLocalPacket(data)
	if err != nil || inspection.Command < game.FriendListCommand || inspection.Command > game.AnswerFriendCommand {
		return protocolMessageResult{}
	}
	if inspection.Command == game.FriendUpdateNotifyCommand {
		return server.rejectedFriend(connectionID, inspection.Command, fmt.Errorf("client originated server-only friend update"))
	}
	if server.playerStore == nil || session == nil || session.UIN == 0 {
		return server.rejectedFriend(connectionID, inspection.Command, fmt.Errorf("authenticated player store is unavailable"))
	}
	if inspection.EnvelopeUIN != session.UIN {
		return server.rejectedFriend(connectionID, inspection.Command, fmt.Errorf("friend envelope UIN %d does not match authenticated session UIN %d", inspection.EnvelopeUIN, session.UIN))
	}

	switch inspection.Command {
	case game.FriendListCommand:
		request, decodeErr := game.DecodeLocalFriendListRequest(data)
		if decodeErr != nil {
			return server.rejectedFriend(connectionID, inspection.Command, decodeErr)
		}
		friends, operationErr := server.playerStore.ListFriends(context.Background(), request.UIN)
		resultID := game.FriendResultSuccess
		if operationErr != nil {
			resultID = game.FriendResultRejected
			friends = nil
		}
		response, buildErr := game.BuildLocalFriendListResponse(data, resultID, request, friends)
		if buildErr != nil {
			return server.rejectedFriend(connectionID, inspection.Command, buildErr)
		}
		if operationErr != nil {
			return server.friendOperationResult(connectionID, inspection.Command, request.UIN, "list", response, operationErr)
		}
		return protocolMessageResult{
			handled: true, response: response, result: "qqt_friend_list",
			postResponse: func() {
				for _, friendUIN := range friends {
					update, projectionErr := server.friendUpdateProjection(request.UIN, friendUIN, session)
					if projectionErr != nil {
						server.log(logEvent{Level: "warn", Event: "friend_update_projection_failed", ConnectionID: connectionID, AccountID: fmt.Sprint(request.UIN), Result: fmt.Sprint(friendUIN), ErrorContext: projectionErr.Error()})
						continue
					}
					server.sendUINNotification(request.UIN, "qqt_friend_list_entry", func(template []byte) ([]byte, error) {
						return game.BuildLocalFriendUpdateNotification(template, update)
					})
				}
			},
		}

	case game.AddFriendCommand:
		request, decodeErr := game.DecodeLocalAddFriendRequest(data)
		if decodeErr != nil {
			return server.rejectedFriend(connectionID, inspection.Command, decodeErr)
		}
		if server.primarySessionForUIN(request.TargetUIN) == nil {
			response, buildErr := game.BuildLocalFriendAnswerResult(data, game.FriendResultRejected, request.UIN, request.Time, request.TargetUIN, false)
			if buildErr != nil {
				return server.rejectedFriend(connectionID, inspection.Command, buildErr)
			}
			return protocolMessageResult{handled: true, response: response, result: "qqt_friend_add_target_offline"}
		}
		_, operationErr := server.playerStore.RequestFriend(context.Background(), request.UIN, request.TargetUIN, request.Word)
		if operationErr != nil {
			response, buildErr := game.BuildLocalFriendAnswerResult(data, game.FriendResultRejected, request.UIN, request.Time, request.TargetUIN, false)
			if buildErr != nil {
				return server.rejectedFriend(connectionID, inspection.Command, buildErr)
			}
			return server.friendOperationResult(connectionID, inspection.Command, request.UIN, "add", response, operationErr)
		}
		return protocolMessageResult{
			handled: true, result: "qqt_friend_add_requested",
			postResponse: func() {
				server.broadcastDirectNotification(request.UIN, request.TargetUIN, "qqt_friend_add_peer", func(template []byte) ([]byte, error) {
					return game.BuildLocalAddFriendRequestNotification(template, request)
				})
			},
		}

	case game.RemoveFriendCommand:
		request, decodeErr := game.DecodeLocalRemoveFriendRequest(data)
		if decodeErr != nil {
			return server.rejectedFriend(connectionID, inspection.Command, decodeErr)
		}
		operationErr := server.playerStore.RemoveFriend(context.Background(), request.UIN, request.TargetUIN)
		resultID := game.FriendResultSuccess
		if operationErr != nil {
			resultID = game.FriendResultRejected
		}
		response, buildErr := game.BuildLocalRemoveFriendResponse(data, resultID, request)
		if buildErr != nil {
			return server.rejectedFriend(connectionID, inspection.Command, buildErr)
		}
		if operationErr != nil {
			return server.friendOperationResult(connectionID, inspection.Command, request.UIN, "remove", response, operationErr)
		}
		return protocolMessageResult{handled: true, response: response, result: "qqt_friend_removed"}

	case game.AnswerFriendCommand:
		request, decodeErr := game.DecodeLocalAnswerFriendRequest(data)
		if decodeErr != nil {
			return server.rejectedFriend(connectionID, inspection.Command, decodeErr)
		}
		accepted := request.ResultID == game.FriendResultSuccess
		changedOwners, operationErr := server.playerStore.AnswerFriendRequest(context.Background(), request.ResponderUIN, request.RequesterUIN, accepted)
		resultID := request.ResultID
		if operationErr != nil {
			resultID = game.FriendResultRejected
		}
		response, buildErr := game.BuildLocalFriendAnswerResult(data, resultID, request.RequesterUIN, request.Time, request.ResponderUIN, false)
		if buildErr != nil {
			return server.rejectedFriend(connectionID, inspection.Command, buildErr)
		}
		if operationErr != nil {
			return server.friendOperationResult(connectionID, inspection.Command, request.ResponderUIN, "answer", response, operationErr)
		}
		return protocolMessageResult{
			handled: true, response: response, result: "qqt_friend_answered",
			postResponse: func() {
				server.broadcastDirectNotification(request.ResponderUIN, request.RequesterUIN, "qqt_friend_answer_peer", func(template []byte) ([]byte, error) {
					return game.BuildLocalFriendAnswerResult(template, resultID, request.RequesterUIN, request.Time, request.ResponderUIN, true)
				})
				if !accepted {
					return
				}
				for _, ownerUIN := range changedOwners {
					friendUIN := request.RequesterUIN
					if ownerUIN == request.RequesterUIN {
						friendUIN = request.ResponderUIN
					}
					update, projectionErr := server.friendUpdateProjection(ownerUIN, friendUIN, session)
					if projectionErr != nil {
						server.log(logEvent{Level: "warn", Event: "friend_accept_projection_failed", ConnectionID: connectionID, AccountID: fmt.Sprint(ownerUIN), Result: fmt.Sprint(friendUIN), ErrorContext: projectionErr.Error()})
						continue
					}
					server.sendUINNotification(ownerUIN, "qqt_friend_accept_update", func(template []byte) ([]byte, error) {
						return game.BuildLocalFriendUpdateNotification(template, update)
					})
				}
			},
		}
	default:
		return protocolMessageResult{}
	}
}

func (server *Server) primarySessionForUIN(uin uint32) *connectionSession {
	if uin == 0 {
		return nil
	}
	server.liveMu.RLock()
	defer server.liveMu.RUnlock()
	for _, candidate := range server.liveSessions {
		if candidate.liveUIN.Load() == uin {
			return candidate
		}
	}
	return nil
}

func (server *Server) friendUpdateProjection(ownerUIN, friendUIN uint32, locked *connectionSession) (game.FriendUpdate, error) {
	if server.playerStore == nil {
		return game.FriendUpdate{}, fmt.Errorf("player store is unavailable")
	}
	profile, err := server.playerStore.Load(context.Background(), friendUIN)
	if err != nil {
		return game.FriendUpdate{}, fmt.Errorf("load friend profile %d: %w", friendUIN, err)
	}
	online := false
	activeRoleID := profile.GameInfo.RoleID
	live := server.primarySessionForUIN(friendUIN)
	if live != nil {
		online = true
		activeRoleID = live.routingRoleID()
		switch {
		case live == locked:
			profile = clonePlayerProfile(live.Profile)
			activeRoleID = live.selectedRoleID()
		case live.mu.TryLock():
			profile = clonePlayerProfile(live.Profile)
			activeRoleID = live.selectedRoleID()
			live.mu.Unlock()
		}
	}
	if profile.Nickname == "" {
		profile.Nickname = server.config.playerProfileForUIN(friendUIN).Nickname
	}
	extItemIDs := make([]uint32, 0)
	if online && activeRoleID != 0 {
		profile.GameInfo.RoleID = activeRoleID
		projected, assignments, projectionErr := server.projectProfileEquipment(context.Background(), friendUIN, profile)
		if projectionErr != nil {
			return game.FriendUpdate{}, projectionErr
		}
		profile = projected
		if server.equipmentCatalog != nil {
			extItemIDs = server.equipmentCatalog.ItemIDsForRole(assignments, activeRoleID)
			if len(extItemIDs) > game.FriendExternalItemMaximum {
				extItemIDs = extItemIDs[:game.FriendExternalItemMaximum]
			}
		}
	}
	return game.FriendUpdate{
		UIN: ownerUIN, FriendUIN: friendUIN, Point: profile.GameInfo.Point,
		ExtItemIDs: extItemIDs, Online: online, Nickname: profile.Nickname,
		Gender: profile.Gender, Identity: profile.Identity, ExtPoint: profile.GameInfo.ExtPoint,
	}, nil
}

func (server *Server) friendSessionRestorePackets(template []byte, ownerUIN uint32, locked *connectionSession, connectionID string) []tcpFollowUpPacket {
	if server.playerStore == nil || ownerUIN == 0 {
		return nil
	}
	friends, err := server.playerStore.ListFriends(context.Background(), ownerUIN)
	if err != nil {
		server.log(logEvent{Level: "warn", Event: "friend_session_restore_list_failed", ConnectionID: connectionID, AccountID: fmt.Sprint(ownerUIN), Result: "restore_skipped", ErrorContext: err.Error()})
		return nil
	}
	// RESPONSE_FRIENDS owns the client's durable UIN roster.  Its 0x00A5
	// handler only updates an entry already present in that roster, so replay
	// the list first even when it is empty (which also clears stale cache).
	roster, buildErr := game.BuildLocalFriendListNotification(template, ownerUIN, friends)
	if buildErr != nil {
		server.log(logEvent{Level: "warn", Event: "friend_session_restore_roster_build_failed", ConnectionID: connectionID, AccountID: fmt.Sprint(ownerUIN), Result: "restore_skipped", ErrorContext: buildErr.Error()})
		return nil
	}
	packets := make([]tcpFollowUpPacket, 0, len(friends)+1)
	packets = append(packets, tcpFollowUpPacket{data: roster, result: "qqt_friend_session_restore_roster"})
	for _, friendUIN := range friends {
		update, projectionErr := server.friendUpdateProjection(ownerUIN, friendUIN, locked)
		if projectionErr != nil {
			server.log(logEvent{Level: "warn", Event: "friend_session_restore_projection_failed", ConnectionID: connectionID, AccountID: fmt.Sprint(ownerUIN), Result: fmt.Sprint(friendUIN), ErrorContext: projectionErr.Error()})
			continue
		}
		notification, buildErr := game.BuildLocalFriendUpdateNotification(template, update)
		if buildErr != nil {
			server.log(logEvent{Level: "warn", Event: "friend_session_restore_build_failed", ConnectionID: connectionID, AccountID: fmt.Sprint(ownerUIN), Result: fmt.Sprint(friendUIN), ErrorContext: buildErr.Error()})
			continue
		}
		packets = append(packets, tcpFollowUpPacket{data: notification, result: fmt.Sprintf("qqt_friend_session_restore_%d", friendUIN)})
	}
	return packets
}

func (server *Server) notifyFriendOwnersStatus(friendUIN uint32, online bool, locked *connectionSession) {
	if server.playerStore == nil || friendUIN == 0 {
		return
	}
	owners, err := server.playerStore.ListFriendOwners(context.Background(), friendUIN)
	if err != nil {
		server.log(logEvent{Level: "warn", Event: "friend_status_owners_failed", AccountID: fmt.Sprint(friendUIN), Result: "status_skipped", ErrorContext: err.Error()})
		return
	}
	for _, ownerUIN := range owners {
		update, projectionErr := server.friendUpdateProjection(ownerUIN, friendUIN, locked)
		if projectionErr != nil {
			server.log(logEvent{Level: "warn", Event: "friend_status_projection_failed", AccountID: fmt.Sprint(ownerUIN), Result: fmt.Sprint(friendUIN), ErrorContext: projectionErr.Error()})
			continue
		}
		update.Online = online
		if !online {
			update.ExtItemIDs = nil
		}
		server.sendUINNotification(ownerUIN, "qqt_friend_status_update", func(template []byte) ([]byte, error) {
			return game.BuildLocalFriendUpdateNotification(template, update)
		})
	}
}

func (server *Server) rejectedFriend(connectionID string, command uint16, err error) protocolMessageResult {
	server.log(logEvent{Level: "warn", Event: "friend_rejected", ConnectionID: connectionID, MessageID: fmt.Sprintf("0x%04X", command), Result: "rejected", ErrorContext: err.Error()})
	return protocolMessageResult{handled: true}
}

func (server *Server) friendOperationResult(connectionID string, command uint16, uin uint32, operation string, response []byte, err error) protocolMessageResult {
	reason := "operation_failed"
	switch {
	case errors.Is(err, persistence.ErrFriendAlreadyExists):
		reason = "already_friend"
	case errors.Is(err, persistence.ErrFriendLimitReached):
		reason = "friend_limit"
	case errors.Is(err, persistence.ErrFriendRequestMissing):
		reason = "request_missing"
	case errors.Is(err, persistence.ErrFriendNotFound):
		reason = "not_found"
	}
	server.log(logEvent{Level: "warn", Event: "friend_operation_rejected", ConnectionID: connectionID, AccountID: fmt.Sprint(uin), MessageID: fmt.Sprintf("0x%04X", command), Result: operation + "_" + reason, ErrorContext: err.Error()})
	return protocolMessageResult{handled: true, response: response, result: "qqt_friend_" + operation + "_rejected"}
}

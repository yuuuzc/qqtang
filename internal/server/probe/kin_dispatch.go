package probe

import (
	"context"
	"errors"
	"fmt"
	"time"

	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

func (server *Server) handleKinMessage(session *connectionSession, connectionID string, data []byte) protocolMessageResult {
	inspection, err := game.InspectLocalPacket(data)
	lateKinText := inspection.Command == game.SetKinDeclarationCommand || inspection.Command == game.SetKinNotificationCommand
	if err != nil || (!lateKinText && (inspection.Command < game.CreateKinCommand || inspection.Command > game.FetchKinTopCommand)) {
		return protocolMessageResult{}
	}
	if server.playerStore == nil || session == nil || session.UIN == 0 {
		return server.rejectedKin(connectionID, inspection.Command, fmt.Errorf("authenticated player store is unavailable"))
	}

	switch inspection.Command {
	case game.CreateKinCommand:
		request, decodeErr := game.DecodeLocalCreateKinRequest(data)
		if decodeErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, decodeErr)
		}
		kin, operationErr := server.playerStore.CreateKin(context.Background(), request.UIN, request.Name, request.Declaration, request.Status, request.Section)
		resultID, reason := kinResult(operationErr)
		response, responseErr := game.BuildLocalCreateKinResponse(data, request.UIN, kin.Index, uint32(resultID), reason)
		if responseErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, responseErr)
		}
		if operationErr != nil {
			server.logKinOperation(connectionID, request.UIN, inspection.Command, "create_rejected", operationErr)
			return protocolMessageResult{handled: true, response: response, result: "qqt_kin_create_rejected"}
		}
		server.refreshLiveKinProfiles(session, request.UIN)
		return protocolMessageResult{handled: true, response: response, result: fmt.Sprintf("qqt_kin_created_%d", kin.Index)}

	case game.FetchKinBaseCommand:
		request, decodeErr := game.DecodeLocalFetchKinBaseRequest(data)
		if decodeErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, decodeErr)
		}
		kin, loadErr := server.playerStore.LoadKin(context.Background(), request.KinIndex)
		if loadErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, loadErr)
		}
		members, loadErr := server.playerStore.ListKinMembers(context.Background(), request.KinIndex)
		if loadErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, loadErr)
		}
		base := protocolKinBase(kin, len(members))
		response, responseErr := game.BuildLocalFetchKinBaseResponse(data, request.UIN, base)
		if responseErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, responseErr)
		}
		return protocolMessageResult{handled: true, response: response, result: fmt.Sprintf("qqt_kin_base_%d", kin.Index)}

	case game.FetchKinMemberListCommand:
		request, decodeErr := game.DecodeLocalFetchKinMembersRequest(data)
		if decodeErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, decodeErr)
		}
		kin, loadErr := server.playerStore.LoadKin(context.Background(), request.KinIndex)
		if loadErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, loadErr)
		}
		members, loadErr := server.playerStore.ListKinMembers(context.Background(), request.KinIndex)
		if loadErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, loadErr)
		}
		response, responseErr := game.BuildLocalFetchKinMembersResponse(data, request.UIN, kin.ListUpdate, 0, protocolKinMembers(members, server.livePrimaryUINs()))
		if responseErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, responseErr)
		}
		return protocolMessageResult{handled: true, response: response, result: fmt.Sprintf("qqt_kin_members_%d", len(members))}

	case game.KinChatCommand:
		request, decodeErr := game.DecodeLocalKinChatRequest(data)
		if decodeErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, decodeErr)
		}
		if session.Profile.KinIndex == 0 || session.Profile.KinIndex != request.KinIndex {
			return server.rejectedKin(connectionID, inspection.Command, persistence.ErrKinNotMember)
		}
		return protocolMessageResult{
			handled: true, result: "qqt_kin_chat", postResponse: func() {
				// REQUEST_SEND_KINMSG has no separate response schema. The sender
				// therefore needs the same content-bearing notification as every
				// other online kin member; excluding the actor makes a one-member
				// kin appear to discard chat entirely.
				server.sendUINNotification(request.UIN, "qqt_kin_chat_self", func(template []byte) ([]byte, error) {
					return game.BuildLocalKinChatNotification(template, request)
				})
				server.broadcastKinNotification(request.KinIndex, request.UIN, "qqt_kin_chat_peer", func(template []byte) ([]byte, error) {
					return game.BuildLocalKinChatNotification(template, request)
				})
			},
		}

	case game.KickKinMemberCommand:
		request, decodeErr := game.DecodeLocalKickKinMemberRequest(data)
		if decodeErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, decodeErr)
		}
		members, _ := server.playerStore.ListKinMembers(context.Background(), request.KinIndex)
		targetNickname := fmt.Sprint(request.DstUIN)
		for _, member := range members {
			if member.UIN == request.DstUIN {
				targetNickname = member.Nickname
				break
			}
		}
		operationErr := server.playerStore.KickKinMember(context.Background(), request.UIN, request.DstUIN, request.KinIndex)
		resultID, reason := kinResult(operationErr)
		response, responseErr := game.BuildLocalKinTargetResponse(data, inspection.Command, resultID, request, reason)
		if responseErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, responseErr)
		}
		result := protocolMessageResult{handled: true, response: response, result: kinDispatchResult("kick", operationErr)}
		if operationErr == nil {
			server.refreshLiveKinProfiles(session, request.DstUIN)
			event := game.KinEventNotification{
				EventType: game.KinEventNotificationMemberRemoved, KinIndex: request.KinIndex,
				UIN: request.UIN, Nickname: session.Profile.Nickname,
				AttachUIN: request.DstUIN, AttachNickname: targetNickname,
				Description: fmt.Sprintf("%s已被移出家族", targetNickname),
			}
			result.followUp, responseErr = game.BuildLocalKinEventNotification(data, event)
			if responseErr != nil {
				return server.rejectedKin(connectionID, inspection.Command, responseErr)
			}
			result.followUpResult = "qqt_kin_kick_owner_refresh"
			result.postResponse = func() {
				server.sendUINNotification(request.DstUIN, "qqt_kin_kick_target_refresh", func(template []byte) ([]byte, error) {
					return game.BuildLocalKinEventNotification(template, event)
				})
				server.broadcastKinNotification(request.KinIndex, request.UIN, "qqt_kin_kick_peer_refresh", func(template []byte) ([]byte, error) {
					return game.BuildLocalKinEventNotification(template, event)
				})
			}
		}
		return result

	case game.ExitKinCommand:
		request, decodeErr := game.DecodeLocalExitKinRequest(data)
		if decodeErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, decodeErr)
		}
		operationErr := server.playerStore.LeaveKin(context.Background(), request.UIN, request.KinIndex)
		resultID, reason := kinResult(operationErr)
		response, responseErr := game.BuildLocalKinSelfResponse(data, inspection.Command, resultID, request, reason)
		if responseErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, responseErr)
		}
		if operationErr == nil {
			server.refreshLiveKinProfiles(session, request.UIN)
		}
		return protocolMessageResult{handled: true, response: response, result: kinDispatchResult("exit", operationErr)}

	case game.DismissKinCommand:
		request, decodeErr := game.DecodeLocalDismissKinRequest(data)
		if decodeErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, decodeErr)
		}
		members, _ := server.playerStore.ListKinMembers(context.Background(), request.KinIndex)
		operationErr := server.playerStore.DismissKin(context.Background(), request.UIN, request.KinIndex)
		resultID, reason := kinResult(operationErr)
		response, responseErr := game.BuildLocalKinSelfResponse(data, inspection.Command, resultID, request, reason)
		if responseErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, responseErr)
		}
		result := protocolMessageResult{handled: true, response: response, result: kinDispatchResult("dismiss", operationErr)}
		if operationErr == nil {
			for _, member := range members {
				server.refreshLiveKinProfiles(session, member.UIN)
			}
			event := game.KinEventNotification{
				EventType: game.KinEventNotificationFamilyDismissed, KinIndex: request.KinIndex,
				UIN: request.UIN, Nickname: session.Profile.Nickname, Description: "家族已经解散",
			}
			result.followUp, responseErr = game.BuildLocalKinEventNotification(data, event)
			if responseErr != nil {
				return server.rejectedKin(connectionID, inspection.Command, responseErr)
			}
			result.followUpResult = "qqt_kin_dismiss_owner_refresh"
			result.postResponse = func() {
				for _, member := range members {
					if member.UIN == request.UIN {
						continue
					}
					server.sendUINNotification(member.UIN, "qqt_kin_dismiss_member_refresh", func(template []byte) ([]byte, error) {
						return game.BuildLocalKinEventNotification(template, event)
					})
				}
			}
		}
		return result

	case game.AssignKinAuthorityCommand:
		request, decodeErr := game.DecodeLocalKinAuthorityRequest(data)
		if decodeErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, decodeErr)
		}
		operationErr := server.playerStore.SetKinMemberAuthority(context.Background(), request.UIN, request.DstUIN, request.KinIndex, request.AuthorityID)
		resultID, reason := kinResult(operationErr)
		response, responseErr := game.BuildLocalKinAuthorityResponse(data, resultID, request, reason)
		if responseErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, responseErr)
		}
		return protocolMessageResult{handled: true, response: response, result: kinDispatchResult("authority", operationErr)}

	case game.UpdateKinTitleCommand, game.SetKinAuthorityCommand,
		game.LegacySetKinDeclarationCommand, game.SetKinDeclarationCommand,
		game.LegacySetKinNotificationCommand, game.SetKinNotificationCommand:
		return server.handleKinTextMessage(session, connectionID, data, inspection.Command)

	case game.SetKinBadgeCommand:
		request, decodeErr := game.DecodeLocalSetKinBadgeRequest(data)
		if decodeErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, decodeErr)
		}
		// REQUEST_SETKINBEDGE names the selected built-in badge first and
		// the optional custom-badge index second. KINFLAGID stores those as
		// Index/FlagID, so their wire order is intentionally reversed here.
		flag := game.NewKinFlagID(request.DefinedBadgeID, request.BadgeID)
		_, operationErr := server.playerStore.SetKinFlag(context.Background(), request.UIN, request.KinIndex, flag)
		resultID, _ := kinResult(operationErr)
		response, responseErr := game.BuildLocalKinBadgeResponse(data, resultID, request)
		if responseErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, responseErr)
		}
		if operationErr == nil {
			members, _ := server.playerStore.ListKinMembers(context.Background(), request.KinIndex)
			for _, member := range members {
				server.refreshLiveKinProfiles(session, member.UIN)
			}
		}
		return protocolMessageResult{handled: true, response: response, result: kinDispatchResult("badge", operationErr)}

	case game.FetchKinTopCommand:
		request, decodeErr := game.DecodeLocalFetchKinTopRequest(data)
		if decodeErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, decodeErr)
		}
		honor, rankingErr := server.playerStore.ListKinRanking(context.Background(), false, 20)
		if rankingErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, rankingErr)
		}
		activity, rankingErr := server.playerStore.ListKinRanking(context.Background(), true, 20)
		if rankingErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, rankingErr)
		}
		response, responseErr := game.BuildLocalFetchKinTopResponse(data, 0, uint32(time.Now().Unix()), request.UIN, protocolKinRanking(honor), protocolKinRanking(activity))
		if responseErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, responseErr)
		}
		return protocolMessageResult{handled: true, response: response, result: fmt.Sprintf("qqt_kin_top_honor_%d_activity_%d", len(honor), len(activity))}

	case game.OperateKinCommand:
		request, decodeErr := game.DecodeLocalKinOperationRequest(data)
		if decodeErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, decodeErr)
		}
		var (
			kin          persistence.Kin
			members      []persistence.KinMember
			operationErr error
		)
		if request.Para != game.KinOperationAccepted || request.Reserve != 0 || request.DstUIN == 0 || request.DstUIN == request.UIN {
			operationErr = fmt.Errorf("unsupported kin invitation para=%d reserve=%d", request.Para, request.Reserve)
		} else if _, online := server.livePrimaryUINs()[request.DstUIN]; !online {
			operationErr = fmt.Errorf("invited player is offline")
		} else {
			kin, operationErr = server.playerStore.InviteToKin(context.Background(), request.UIN, request.DstUIN, request.KinIndex)
			if operationErr == nil {
				// The invitation is already committed. Member count is only a
				// presentation field in the target's KinBase and must not turn a
				// successful transaction into a rejected response.
				members, _ = server.playerStore.ListKinMembers(context.Background(), request.KinIndex)
			}
		}
		responseRequest := request
		reason := ""
		if operationErr != nil {
			responseRequest.Para = game.KinOperationRejected
			_, reason = kinResult(operationErr)
		}
		response, responseErr := game.BuildLocalKinOperationResponse(data, responseRequest, reason)
		if responseErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, responseErr)
		}
		result := protocolMessageResult{handled: true, response: response, result: kinDispatchResult("invite", operationErr)}
		if operationErr == nil {
			base := protocolKinBase(kin, len(members))
			result.postResponse = func() {
				server.sendUINNotification(request.DstUIN, "qqt_kin_invitation_target", func(template []byte) ([]byte, error) {
					return game.BuildLocalKinServerOperationRequest(template, request, base)
				})
			}
		}
		return result

	case game.ServerOperateKinCommand:
		answer, decodeErr := game.DecodeLocalKinServerOperationAnswer(data)
		if decodeErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, decodeErr)
		}
		accepted := answer.Para == game.KinOperationAccepted
		kin, operationErr := server.playerStore.AnswerKinInvitation(context.Background(), answer.ResponderUIN, answer.InviterUIN, answer.KinIndex, accepted)
		members := []persistence.KinMember(nil)
		if operationErr == nil {
			// AnswerKinInvitation is atomic and has already committed. Keep a
			// later display-only member count failure out of the protocol result.
			members, _ = server.playerStore.ListKinMembers(context.Background(), answer.KinIndex)
		}
		final := game.KinOperationRequest{
			UIN: answer.InviterUIN, Time: answer.Time, KinIndex: answer.KinIndex,
			DstUIN: answer.ResponderUIN, Para: answer.Para, Reserve: 0,
		}
		if operationErr != nil {
			final.Para = game.KinOperationRejected
		}
		base := protocolKinBase(kin, len(members))
		followUp, buildErr := game.BuildLocalKinServerOperationNotification(data, final, base)
		if buildErr != nil {
			return server.rejectedKin(connectionID, inspection.Command, buildErr)
		}
		result := protocolMessageResult{
			handled: true, result: kinDispatchResult("invitation_answer", operationErr),
			followUp: followUp, followUpResult: "qqt_kin_invitation_answer_notify",
		}
		if operationErr == nil && accepted {
			server.refreshLiveKinProfiles(session, answer.ResponderUIN)
			event := game.KinEventNotification{
				EventType: game.KinEventNotificationMemberJoined, KinIndex: answer.KinIndex,
				UIN: answer.ResponderUIN, Nickname: session.Profile.Nickname,
				Description: fmt.Sprintf("%s加入了家族", session.Profile.Nickname),
			}
			result.postResponse = func() {
				server.broadcastKinNotification(answer.KinIndex, answer.ResponderUIN, "qqt_kin_invitation_join_peer", func(template []byte) ([]byte, error) {
					return game.BuildLocalKinEventNotification(template, event)
				})
			}
		}
		return result

	default:
		return protocolMessageResult{}
	}
}

func (server *Server) handleKinTextMessage(session *connectionSession, connectionID string, data []byte, command uint16) protocolMessageResult {
	var (
		request game.KinTextRequest
		err     error
	)
	switch command {
	case game.UpdateKinTitleCommand:
		request, err = game.DecodeLocalUpdateKinTitleRequest(data)
	case game.SetKinAuthorityCommand:
		request, err = game.DecodeLocalSetKinAuthorityRequest(data)
	case game.LegacySetKinDeclarationCommand, game.SetKinDeclarationCommand:
		request, err = game.DecodeLocalSetKinDeclarationRequest(data)
	case game.LegacySetKinNotificationCommand, game.SetKinNotificationCommand:
		request, err = game.DecodeLocalSetKinNotificationRequest(data)
	}
	if err != nil {
		return server.rejectedKin(connectionID, command, err)
	}
	var operationErr error
	switch command {
	case game.UpdateKinTitleCommand, game.SetKinAuthorityCommand:
		_, operationErr = server.playerStore.SetKinTitle(context.Background(), request.UIN, request.KinIndex, request.Text)
	case game.LegacySetKinDeclarationCommand, game.SetKinDeclarationCommand:
		_, operationErr = server.playerStore.SetKinDeclaration(context.Background(), request.UIN, request.KinIndex, request.Text)
	case game.LegacySetKinNotificationCommand, game.SetKinNotificationCommand:
		_, operationErr = server.playerStore.SetKinNotification(context.Background(), request.UIN, request.KinIndex, request.Text)
	}
	resultID, reason := kinResult(operationErr)
	response, responseErr := game.BuildLocalKinTextResponse(data, command, resultID, request, reason)
	if responseErr != nil {
		return server.rejectedKin(connectionID, command, responseErr)
	}
	result := protocolMessageResult{handled: true, response: response, result: kinDispatchResult("text", operationErr)}
	if (command == game.LegacySetKinNotificationCommand || command == game.SetKinNotificationCommand) && operationErr == nil {
		event := game.KinEventNotification{
			EventType:   game.KinEventNotificationUpdateAnnouncement,
			KinIndex:    request.KinIndex,
			UIN:         request.UIN,
			Nickname:    session.Profile.Nickname,
			Description: request.Text,
		}
		followUp, eventErr := game.BuildLocalKinEventNotification(data, event)
		if eventErr != nil {
			return server.rejectedKin(connectionID, command, eventErr)
		}
		result.followUp = followUp
		result.followUpResult = "qqt_kin_notification_self_refresh"
		result.postResponse = func() {
			server.broadcastKinNotification(request.KinIndex, request.UIN, "qqt_kin_notification_peer_refresh", func(template []byte) ([]byte, error) {
				return game.BuildLocalKinEventNotification(template, event)
			})
		}
	}
	return result
}

func protocolKinBase(kin persistence.Kin, memberCount int) game.KinBase {
	if memberCount > game.KinMemberMaximum {
		memberCount = game.KinMemberMaximum
	}
	return game.KinBase{
		Index: kin.Index, OwnerUIN: kin.OwnerUIN, CreatedUnix: kin.CreatedUnix,
		Status: kin.Status, Grade: kin.Grade, FlagID: kin.FlagID, MemberCount: uint16(memberCount),
		Name: kin.Name, Declaration: kin.Declaration, Title: kin.Title, Section: kin.Section,
		BaseUpdate: kin.BaseUpdate, ListUpdate: kin.ListUpdate, Notification: kin.Notification,
		Honor: kin.Honor, ActivePoint: kin.ActivePoint, LastHonor: kin.LastHonor, LastActivePoint: kin.LastActivePoint,
	}
}

func protocolKinMembers(members []persistence.KinMember, online map[uint32]struct{}) []game.KinMemberOld {
	result := make([]game.KinMemberOld, 0, len(members))
	for _, member := range members {
		// QQTSection builds its mode-3 (online family members) cache from bit 0,
		// but GetKinPositionByUin/GetKinMemberHonor read the authority grade from
		// bits 24..27 of the same cached status word.  The standalone Grade field
		// is retained for the other member views; it is not what the permission
		// and position APIs consult.
		status := member.Status &^ uint32(1) &^ uint32(0x0f000000)
		status |= (member.AuthorityID & 0x0f) << 24
		if _, present := online[member.UIN]; present {
			status |= 1
		}
		result = append(result, game.KinMemberOld{
			UIN: member.UIN, Nickname: member.Nickname, JoinedUnix: member.JoinedUnix,
			Status: status, StatusTime: member.StatusTime, Grade: member.AuthorityID,
			OnlineTime: member.OnlineTime, LastLoginTime: member.LastLoginTime,
			Honor: member.Honor, ActivePoint: member.ActivePoint,
		})
	}
	return result
}

func protocolKinRanking(entries []persistence.KinRankingEntry) []game.KinOrderInfo {
	result := make([]game.KinOrderInfo, 0, len(entries))
	for _, entry := range entries {
		result = append(result, game.KinOrderInfo{
			KinIndex: entry.Index, Name: entry.Name, Value: entry.Value,
			Order: entry.Order, BeforeOrder: entry.BeforeOrder,
		})
	}
	return result
}

func kinResult(err error) (uint16, string) {
	if err == nil {
		return 0, ""
	}
	switch {
	case errors.Is(err, persistence.ErrKinAlreadyMember):
		return 2, "已经加入其他家族"
	case errors.Is(err, persistence.ErrKinNameOwned):
		return 3, "家族名称已存在"
	case errors.Is(err, persistence.ErrKinPermissionDenied):
		return 4, "没有家族管理权限"
	case errors.Is(err, persistence.ErrKinNotMember):
		return 5, "不是该家族成员"
	case errors.Is(err, persistence.ErrKinNotFound):
		return 6, "家族不存在"
	case errors.Is(err, persistence.ErrKinCreationItemMissing):
		return 7, "需要100份盟约书或1份超级盟约书"
	case errors.Is(err, persistence.ErrKinInvitationMissing):
		return 8, "家族邀请已经失效"
	case errors.Is(err, persistence.ErrKinMemberLimitReached):
		return 9, "家族人数已满"
	default:
		return 1, "家族操作失败"
	}
}

func kinDispatchResult(operation string, err error) string {
	if err != nil {
		return "qqt_kin_" + operation + "_rejected"
	}
	return "qqt_kin_" + operation + "_success"
}

func (server *Server) rejectedKin(connectionID string, command uint16, err error) protocolMessageResult {
	server.logKinOperation(connectionID, 0, command, "rejected", err)
	return protocolMessageResult{handled: true}
}

func (server *Server) logKinOperation(connectionID string, uin uint32, command uint16, result string, err error) {
	event := logEvent{Level: "warn", Event: "kin_operation_rejected", ConnectionID: connectionID, MessageID: fmt.Sprintf("0x%04X", command), Result: result}
	if uin != 0 {
		event.AccountID = fmt.Sprint(uin)
	}
	if err != nil {
		event.ErrorContext = err.Error()
	}
	server.log(event)
}

func (server *Server) refreshLiveKinProfiles(lockedSession *connectionSession, uins ...uint32) {
	server.refreshLiveProfiles(lockedSession, uins...)
}

func (server *Server) refreshLiveProfiles(lockedSession *connectionSession, uins ...uint32) {
	if server.playerStore == nil || len(uins) == 0 {
		return
	}
	wanted := make(map[uint32]struct{}, len(uins))
	for _, uin := range uins {
		if uin != 0 {
			wanted[uin] = struct{}{}
		}
	}
	type liveProfileTarget struct {
		session *connectionSession
		uin     uint32
	}
	server.liveMu.RLock()
	sessions := make([]liveProfileTarget, 0, len(wanted))
	for _, candidate := range server.liveSessions {
		uin := candidate.liveUIN.Load()
		if _, ok := wanted[uin]; ok {
			sessions = append(sessions, liveProfileTarget{session: candidate, uin: uin})
		}
	}
	server.liveMu.RUnlock()
	for _, target := range sessions {
		candidate := target.session
		profile, err := server.playerStore.Load(context.Background(), target.uin)
		if err != nil {
			continue
		}
		var lobbyProfile game.PlayerProfile
		if candidate == lockedSession {
			candidate.replaceProfile(profile)
			lobbyProfile = clonePlayerProfile(candidate.Profile)
		} else {
			if candidate.liveUIN.Load() == target.uin {
				cloned := clonePlayerProfile(profile)
				candidate.pendingProfile.Store(&cloned)
				lobbyProfile = cloned
			}
		}
		if lobbyProfile.PlayerID != 0 {
			_ = server.worldState().EnterLobby(target.uin, lobbyProfile)
		}
	}
}

func (server *Server) broadcastKinNotification(kinIndex, actorUIN uint32, result string, build func([]byte) ([]byte, error)) {
	if kinIndex == 0 || build == nil {
		return
	}
	server.liveMu.RLock()
	candidates := make([]*connectionSession, 0)
	for _, candidate := range server.liveSessions {
		if candidate.liveUIN.Load() != 0 && candidate.liveUIN.Load() != actorUIN {
			candidates = append(candidates, candidate)
		}
	}
	server.liveMu.RUnlock()
	recipients := make([]*connectionSession, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.liveUIN.Load() != 0 && candidate.liveUIN.Load() != actorUIN && candidate.routingKinIndex() == kinIndex {
			recipients = append(recipients, candidate)
		}
	}
	server.sendChatNotifications(recipients, result, build)
}

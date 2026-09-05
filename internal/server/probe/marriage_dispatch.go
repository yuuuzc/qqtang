package probe

import (
	"context"
	"errors"
	"fmt"
	"time"

	"qqtang/internal/protocol/game"
	"qqtang/internal/server/persistence"
)

func (server *Server) handleMarriageMessage(session *connectionSession, connectionID string, data []byte) protocolMessageResult {
	inspection, err := game.InspectLocalPacket(data)
	if err != nil || inspection.Command < game.RequestSparkCommand || inspection.Command > game.ChangeWeddingModeNotifyCommand {
		return protocolMessageResult{}
	}
	if inspection.Command == game.NotifySparkCommand || inspection.Command == game.WeddingConfirmNotifyCommand || inspection.Command == game.ChangeWeddingModeNotifyCommand {
		return server.rejectedMarriage(connectionID, inspection.Command, fmt.Errorf("client originated server-only marriage notification"))
	}
	if server.playerStore == nil || session == nil || session.UIN == 0 {
		return server.rejectedMarriage(connectionID, inspection.Command, fmt.Errorf("authenticated player store is unavailable"))
	}

	switch inspection.Command {
	case game.RequestSparkCommand:
		request, decodeErr := game.DecodeLocalSparkRequest(data)
		if decodeErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, decodeErr)
		}
		if _, _, online := server.liveMarriagePlayer(session, request.TargetUIN); !online {
			response, buildErr := game.BuildLocalSparkResponse(data, game.MarriageResultFailed, request, "对方当前不在线")
			if buildErr != nil {
				return server.rejectedMarriage(connectionID, inspection.Command, buildErr)
			}
			return protocolMessageResult{handled: true, response: response, result: "qqt_marriage_spark_target_offline"}
		}
		proposal, operationErr := server.playerStore.CreateMarriageProposal(context.Background(), request.UIN, request.TargetUIN, request.Word)
		resultID, reason := marriageResult(operationErr)
		if operationErr == nil {
			// Client 5.2 always opens RESPONSE_SPARK.ResultWord as a result
			// window. An empty success word therefore produces the observed
			// blank dialog even though the target received the proposal.
			reason = "求婚请求已发送"
		}
		response, buildErr := game.BuildLocalSparkResponse(data, resultID, request, reason)
		if buildErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, buildErr)
		}
		if operationErr != nil {
			return server.marriageOperationResult(connectionID, inspection.Command, request.UIN, "spark", response, operationErr)
		}
		return protocolMessageResult{
			handled: true, response: response, result: "qqt_marriage_spark",
			postResponse: func() {
				server.broadcastDirectNotification(request.UIN, request.TargetUIN, "qqt_marriage_spark_peer", func(template []byte) ([]byte, error) {
					return game.BuildLocalSparkNotification(template, request, proposal.ProposerNickname)
				})
			},
		}

	case game.AnswerSparkCommand:
		request, decodeErr := game.DecodeLocalAnswerSparkRequest(data)
		if decodeErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, decodeErr)
		}
		accepted := request.ResultID == game.MarriageResultSuccess
		_, operationErr := server.playerStore.AnswerMarriageProposal(context.Background(), request.ResponderUIN, request.ProposerUIN, accepted)
		resultID, reason := marriageResult(operationErr)
		if operationErr == nil {
			resultID = request.ResultID
			if accepted {
				reason = "求婚成功"
			} else {
				reason = "对方拒绝了求婚"
			}
		}
		response, buildErr := game.BuildLocalAnswerSparkResponse(data, resultID, request, reason)
		if buildErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, buildErr)
		}
		if operationErr != nil {
			return server.marriageOperationResult(connectionID, inspection.Command, request.ResponderUIN, "answer_spark", response, operationErr)
		}
		if accepted {
			server.refreshLiveProfiles(session, request.ResponderUIN, request.ProposerUIN)
		}
		return protocolMessageResult{
			handled: true, response: response, result: "qqt_marriage_answer_spark",
			postResponse: func() {
				server.broadcastDirectNotification(request.ResponderUIN, request.ProposerUIN, "qqt_marriage_answer_spark_peer", func(template []byte) ([]byte, error) {
					return game.BuildLocalAnswerSparkNotification(template, resultID, request, reason)
				})
			},
		}

	case game.MarriageInfoCommand:
		request, decodeErr := game.DecodeLocalMarriageInfoRequest(data)
		if decodeErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, decodeErr)
		}
		marriage, loadErr := server.playerStore.SynchronizeMarriageRing(context.Background(), request.TargetUIN)
		resultID, _ := marriageResult(loadErr)
		info := game.MarriageInfo{TargetUIN: request.TargetUIN, MarriageLevel: 1, LevelValue: 1}
		if loadErr == nil {
			age := uint32(0)
			now := uint32(time.Now().Unix())
			if now > marriage.MarriedUnix {
				age = (now - marriage.MarriedUnix) / uint32((24*time.Hour)/time.Second)
			}
			info = game.MarriageInfo{
				TargetUIN: marriage.UIN, MarriageUIN: marriage.SpouseUIN,
				MarriageLoyalty: marriage.Loyalty, MarriageLevel: marriage.Level,
				LevelValue: marriage.LevelValue, MarriageAge: age, LoveWord: marriage.LoveWord,
				Nickname: marriage.Nickname, SpouseNickname: marriage.SpouseNickname, RingID: marriage.RingID,
			}
		}
		response, buildErr := game.BuildLocalMarriageInfoResponse(data, resultID, info)
		if buildErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, buildErr)
		}
		return protocolMessageResult{handled: true, response: response, result: "qqt_marriage_info"}

	case game.ModifyLoveWordCommand:
		request, decodeErr := game.DecodeLocalModifyLoveWordRequest(data)
		if decodeErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, decodeErr)
		}
		_, operationErr := server.playerStore.UpdateMarriageLoveWord(context.Background(), request.UIN, request.SpouseUIN, request.LoveWord)
		resultID, _ := marriageResult(operationErr)
		response, buildErr := game.BuildLocalModifyLoveWordResponse(data, resultID, request)
		if buildErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, buildErr)
		}
		if operationErr != nil {
			return server.marriageOperationResult(connectionID, inspection.Command, request.UIN, "love_word", response, operationErr)
		}
		return protocolMessageResult{handled: true, response: response, result: "qqt_marriage_love_word"}

	case game.DivorceCommand:
		request, decodeErr := game.DecodeLocalDivorceRequest(data)
		if decodeErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, decodeErr)
		}
		operationErr := server.playerStore.Divorce(context.Background(), request.UIN, request.TargetUIN)
		resultID, _ := marriageResult(operationErr)
		response, buildErr := game.BuildLocalDivorceResponse(data, resultID, request)
		if buildErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, buildErr)
		}
		if operationErr != nil {
			return server.marriageOperationResult(connectionID, inspection.Command, request.UIN, "divorce", response, operationErr)
		}
		server.refreshLiveProfiles(session, request.UIN, request.TargetUIN)
		return protocolMessageResult{
			handled: true, response: response, result: "qqt_marriage_divorce",
			postResponse: func() {
				server.broadcastDirectNotification(request.UIN, request.TargetUIN, "qqt_marriage_divorce_peer", func(template []byte) ([]byte, error) {
					return game.BuildLocalDivorceNotification(template, resultID, request)
				})
			},
		}

	case game.StartWeddingCommand:
		request, decodeErr := game.DecodeLocalStartWeddingRequest(data)
		if decodeErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, decodeErr)
		}
		marriage, operationErr := server.playerStore.LoadMarriage(context.Background(), request.UIN)
		if operationErr == nil && marriage.SpouseUIN != request.TargetUIN {
			operationErr = persistence.ErrMarriageNotFound
		}
		targetPlayerID, targetRoomID, online := server.liveMarriagePlayer(session, request.TargetUIN)
		if operationErr == nil && (!online || targetRoomID == 0 || targetRoomID != session.RoomID) {
			operationErr = fmt.Errorf("spouse is not present in the wedding room")
		}
		if operationErr == nil {
			_, operationErr = server.worldState().StartWedding(sessionWorldUIN(session), targetPlayerID, request.WeddingModeID)
		}
		resultID, reason := marriageResult(operationErr)
		if operationErr == nil {
			reason = "请确认婚礼"
		}
		response, buildErr := game.BuildLocalStartWeddingResponse(data, resultID, request, marriage.Nickname, marriage.SpouseNickname, reason)
		if buildErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, buildErr)
		}
		if operationErr != nil {
			return server.marriageOperationResult(connectionID, inspection.Command, request.UIN, "start_wedding", response, operationErr)
		}
		return protocolMessageResult{
			handled: true, response: response, result: "qqt_marriage_wedding_started",
			postResponse: func() {
				server.broadcastDirectNotification(request.UIN, request.TargetUIN, "qqt_marriage_wedding_confirm", func(template []byte) ([]byte, error) {
					return game.BuildLocalWeddingConfirmNotification(template, request, marriage.Nickname, marriage.SpouseNickname, "请确认婚礼")
				})
			},
		}

	case game.AnswerWeddingCommand:
		request, decodeErr := game.DecodeLocalAnswerWeddingRequest(data)
		if decodeErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, decodeErr)
		}
		marriage, operationErr := server.playerStore.LoadMarriage(context.Background(), request.UIN)
		if operationErr == nil && marriage.SpouseUIN != request.TargetUIN {
			operationErr = persistence.ErrMarriageNotFound
		}
		targetPlayerID, targetRoomID, online := server.liveMarriagePlayer(session, request.TargetUIN)
		if operationErr == nil && (!online || targetRoomID == 0 || targetRoomID != session.RoomID) {
			operationErr = fmt.Errorf("spouse is not present in the wedding room")
		}
		accepted := request.ResultID == game.MarriageResultSuccess
		if operationErr == nil {
			_, operationErr = server.worldState().AnswerWedding(sessionWorldUIN(session), targetPlayerID, accepted)
		}
		resultID, reason := marriageResult(operationErr)
		if operationErr == nil {
			resultID = request.ResultID
			if accepted {
				reason = "婚礼开始"
			} else {
				reason = "对方拒绝了婚礼"
			}
		}
		response, buildErr := game.BuildLocalAnswerWeddingResponse(data, resultID, request, marriage.Nickname, marriage.SpouseNickname, reason)
		if buildErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, buildErr)
		}
		if operationErr != nil {
			return server.marriageOperationResult(connectionID, inspection.Command, request.UIN, "answer_wedding", response, operationErr)
		}
		roomID := session.RoomID
		return protocolMessageResult{
			handled: true, response: response, result: "qqt_marriage_wedding_answered",
			postResponse: func() {
				server.broadcastRoomNotification(roomID, request.UIN, "qqt_marriage_wedding_answer_peer", func(template []byte) ([]byte, error) {
					return game.BuildLocalAnswerWeddingNotification(template, resultID, request, marriage.Nickname, marriage.SpouseNickname, reason)
				})
			},
		}

	case game.ChangeWeddingModeCommand:
		request, decodeErr := game.DecodeLocalChangeWeddingModeRequest(data)
		if decodeErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, decodeErr)
		}
		_, operationErr := server.worldState().SetWeddingMode(sessionWorldUIN(session), request.WeddingModeID)
		resultID, reason := marriageResult(operationErr)
		response, buildErr := game.BuildLocalChangeWeddingModeResponse(data, resultID, request, reason)
		if buildErr != nil {
			return server.rejectedMarriage(connectionID, inspection.Command, buildErr)
		}
		if operationErr != nil {
			return server.marriageOperationResult(connectionID, inspection.Command, request.UIN, "change_wedding_mode", response, operationErr)
		}
		roomID := session.RoomID
		return protocolMessageResult{
			handled: true, response: response, result: "qqt_marriage_wedding_mode",
			postResponse: func() {
				server.broadcastRoomNotification(roomID, request.UIN, "qqt_marriage_wedding_mode_peer", func(template []byte) ([]byte, error) {
					return game.BuildLocalChangeWeddingModeNotification(template, request.UIN, request.WeddingModeID)
				})
			},
		}
	default:
		return protocolMessageResult{}
	}
}

func (server *Server) liveMarriagePlayer(locked *connectionSession, uin uint32) (playerID, roomID uint16, found bool) {
	if uin == 0 {
		return 0, 0, false
	}
	server.liveMu.RLock()
	var candidate *connectionSession
	for _, active := range server.liveSessions {
		if active.liveUIN.Load() != uin {
			continue
		}
		candidate = active
		break
	}
	server.liveMu.RUnlock()
	if candidate == nil {
		return 0, 0, false
	}
	if candidate == locked {
		return candidate.Profile.PlayerID, uint16(candidate.liveRoomID.Load()), !candidate.auxiliary
	}
	// liveUIN/livePlayerID/liveRoomID are the routing projection.  Locking the
	// other participant while the caller's dispatcher owns locked.mu lets two
	// simultaneous proposals deadlock in opposite order.
	if candidate.liveUIN.Load() != uin || candidate.auxiliary {
		return 0, 0, false
	}
	return uint16(candidate.livePlayerID.Load()), uint16(candidate.liveRoomID.Load()), true
}

func marriageResult(err error) (uint16, string) {
	if err == nil {
		return game.MarriageResultSuccess, ""
	}
	switch {
	case errors.Is(err, persistence.ErrMarriageProposalItemMissing):
		return game.MarriageResultFailed, "缺少求婚必备道具水晶之恋"
	case errors.Is(err, persistence.ErrMarriageAlreadyExists):
		return game.MarriageResultFailed, "双方中已有结婚关系"
	case errors.Is(err, persistence.ErrMarriageProposalBusy):
		return game.MarriageResultFailed, "双方中已有待处理的求婚"
	case errors.Is(err, persistence.ErrMarriageProposalMissing):
		return game.MarriageResultFailed, "求婚请求已失效"
	case errors.Is(err, persistence.ErrMarriageNotFound):
		return game.MarriageResultFailed, "没有找到对应的婚姻关系"
	default:
		return game.MarriageResultFailed, "操作失败"
	}
}

func (server *Server) rejectedMarriage(connectionID string, command uint16, err error) protocolMessageResult {
	server.log(logEvent{Level: "warn", Event: "marriage_rejected", ConnectionID: connectionID, MessageID: fmt.Sprintf("0x%04X", command), Result: "rejected", ErrorContext: err.Error()})
	return protocolMessageResult{handled: true}
}

func (server *Server) marriageOperationResult(connectionID string, command uint16, uin uint32, operation string, response []byte, err error) protocolMessageResult {
	server.log(logEvent{Level: "warn", Event: "marriage_operation_rejected", ConnectionID: connectionID, AccountID: fmt.Sprint(uin), MessageID: fmt.Sprintf("0x%04X", command), Result: operation, ErrorContext: err.Error()})
	return protocolMessageResult{handled: true, response: response, result: "qqt_marriage_" + operation + "_rejected"}
}

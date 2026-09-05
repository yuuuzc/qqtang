package probe

import (
	roomstate "qqtang/internal/game/room"
	"qqtang/internal/protocol/game"
)

// matchGameOverDispatchResult is the application outcome of a legacy
// NOTIFY_GAME_OVER event. Packet delivery remains in tcp_sequence.go; this
// type only describes what must be sent and committed after validation.
type matchGameOverDispatchResult struct {
	response                     []byte
	result                       string
	followUp                     []byte
	followUpResult               string
	completeAdventureAfterSend   bool
	completeCompetitiveAfterSend bool
	adventureSettlement          *adventureSettlementCommit
	adventureRoomSettlement      *adventureRoomSettlementCommit
	competitiveRoomSettlement    *competitiveRoomSettlementCommit
	handledWithoutResponse       bool
}

func (server *Server) handleMatchGameOver(session *connectionSession, data []byte, event game.GameEvent) (matchGameOverDispatchResult, error) {
	var output matchGameOverDispatchResult
	gameOver, err := game.ParseGameOverData(event.Body)
	if err != nil {
		return output, err
	}
	if session.CurrentGameID == 0 {
		// The client can emit its own 0x0FBB after consuming the death
		// fan-out while the server's authoritative fallback is already in
		// flight. The completed game has no remaining recipients.
		output.result = "qqt_game_event_late_client_game_over_ignored"
		output.handledWithoutResponse = true
		return output, nil
	}

	rewritten := false
	category, err := server.activeSessionMatchCategory(session)
	if err != nil {
		return output, err
	}
	if category == roomstate.CategoryCompetitive {
		battle, battleErr := server.competitiveBattle(session.CurrentGameID)
		if battleErr != nil {
			return output, battleErr
		}
		if err = validateCompetitiveGameOver(gameOver, battle); err != nil {
			return output, err
		}
		changed, normalizeErr := normalizeCompetitiveTeamOutcome(&gameOver, battle)
		if normalizeErr != nil {
			return output, normalizeErr
		}
		rewritten = changed
		rewritten = normalizeCompetitiveNoWinnerDraw(&gameOver, battle) || rewritten
		if err = server.validateCompetitiveGameOverSource(session, battle); err != nil {
			return output, err
		}
		if err = applyCompetitiveBattleStatistics(&gameOver, battle); err != nil {
			return output, err
		}
		addCompetitiveOutcomeBasePoints(&gameOver)
		rewritten = true
		output.competitiveRoomSettlement = &competitiveRoomSettlementCommit{GameOver: gameOver, Battle: battle}
		output.completeCompetitiveAfterSend = true
	} else {
		// Captured legacy clients can construct GAME_OVER with the default
		// mode byte 0 even in a confirmed adventure match. The active
		// server-side category is authoritative.
		gameOver.GameMode = game.SettlementGameModeAdventure
		rewritten = true
		battle, battleErr := server.adventureBattle(session.CurrentGameID)
		if battleErr != nil {
			return output, battleErr
		}
		changed, normalizeErr := normalizeAdventureTeamOutcome(&gameOver, battle)
		if normalizeErr != nil {
			return output, normalizeErr
		}
		rewritten = rewritten || changed
		applyAdventureOutcomeCourageMultiplier(&gameOver)
		rewritten = true
		settlement, settlementErr := adventureSettlementForPlayer(gameOver, session.Profile.PlayerID)
		if settlementErr != nil {
			return output, settlementErr
		}
		if err = attachAdventureCollectedItems(battle, session.Profile.PlayerID, &settlement); err != nil {
			return output, err
		}
		output.adventureSettlement = &settlement
		output.adventureRoomSettlement = &adventureRoomSettlementCommit{GameOver: gameOver, Battle: battle}
	}

	if rewritten {
		output.response, event, err = game.BuildLocalGameOverRelay(data, gameOver)
	} else {
		output.response, event, err = game.BuildLocalGameEventRelay(data)
	}
	if err != nil {
		return output, err
	}
	if rewritten {
		output.followUp, event, err = game.BuildLocalGameOverNotification(data, session.RoomID, server.nextGameDataSequence(), gameOver)
	} else {
		output.followUp, event, err = game.BuildLocalGameEventNotification(data, session.RoomID, server.nextGameDataSequence())
	}
	if err != nil {
		return output, err
	}
	output.result = "qqt_game_event_ack_client_game_over"
	output.followUpResult = "qqt_game_event_notify_client_game_over"
	output.completeAdventureAfterSend = output.competitiveRoomSettlement == nil
	return output, nil
}

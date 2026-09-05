package probe

import (
	"fmt"
	"time"

	"qqtang/internal/game/match"
	"qqtang/internal/protocol/game"
)

type adventureDeathDispatchResult struct {
	response             []byte
	result               string
	followUp             []byte
	followUpResult       string
	startFollowUp        []byte
	startFollowUpResult  string
	startFollowUpDelay   time.Duration
	completeAfterSend    bool
	settlement           *adventureSettlementCommit
	roomSettlement       *adventureRoomSettlementCommit
	finalVictorySchedule *adventureFinalVictorySchedule
}

func (server *Server) handleAdventureDeath(session *connectionSession, data []byte) (adventureDeathDispatchResult, error) {
	var result adventureDeathDispatchResult
	response, event, err := game.BuildLocalGameEventRelay(data)
	if err != nil {
		return result, err
	}
	result.response = response
	result.followUp, event, err = game.BuildLocalGameEventNotification(data, session.RoomID, server.nextGameDataSequence())
	if err != nil {
		return adventureDeathDispatchResult{}, err
	}
	death, err := game.ParsePlayerDeathEvent(event)
	if err != nil {
		return adventureDeathDispatchResult{}, err
	}
	battle, err := server.adventureBattle(session.CurrentGameID)
	if err != nil {
		return adventureDeathDispatchResult{}, err
	}

	// Adventure NPCs use the same NOTIFY_PLAYER_DIE schema with object IDs
	// outside the room roster. The body has no proven killer ID, so NPC courage
	// is awarded cooperatively to the stage participant set.
	if !battle.HasParticipant(death.PlayerID) {
		return server.handleAdventureNPCDeath(session, data, death, battle, result)
	}
	return server.handleAdventurePlayerDeath(session, data, death, battle, result)
}

func (server *Server) handleAdventureNPCDeath(session *connectionSession, data []byte, death game.PlayerDeathEvent, battle *match.AdventureBattle, result adventureDeathDispatchResult) (adventureDeathDispatchResult, error) {
	result.result = fmt.Sprintf("qqt_game_event_ack_npc_death_%d", death.PlayerID)
	result.followUpResult = fmt.Sprintf("qqt_game_event_notify_npc_death_%d", death.PlayerID)
	stage, exists := server.adventureStageRule(session.CurrentMapID)
	if !exists {
		return adventureDeathDispatchResult{}, fmt.Errorf("adventure map %d has no stage rules", session.CurrentMapID)
	}
	group, exists := stage.NPCDropGroup(death.PlayerID)
	if !exists {
		return adventureDeathDispatchResult{}, fmt.Errorf("adventure map %d has no NPC %d combat rule", session.CurrentMapID, death.PlayerID)
	}
	courage := group.Courage()
	progress, err := battle.RecordNPCDeath(courage)
	if err != nil {
		return adventureDeathDispatchResult{}, err
	}
	if progress.Credited {
		result.result = fmt.Sprintf("%s_credited_player_%d_courage_%d", result.result, progress.CreditedPlayerID, courage)
	} else if progress.Recorded {
		result.result = fmt.Sprintf("%s_shared_courage_%d", result.result, courage)
	}
	if !progress.NewlyCompleted {
		return result, nil
	}
	if _, hasNextMap := server.mapCatalog.Next(session.CurrentMapID); hasNextMap {
		result.result = fmt.Sprintf("%s_stage_cleared_%d_of_%d_waiting_for_doorway", result.result, progress.DeathOrdinal, progress.ExpectedDeaths)
		return result, nil
	}
	pending, err := battle.BeginFinalStageVictory()
	if err != nil {
		return adventureDeathDispatchResult{}, err
	}
	if pending.NewlyConcluded {
		result.result = fmt.Sprintf("%s_final_stage_cleared_%d_of_%d_loot_grace_%s", result.result, progress.DeathOrdinal, progress.ExpectedDeaths, adventureFinalLootGrace)
		result.finalVictorySchedule = &adventureFinalVictorySchedule{
			gameID: session.CurrentGameID, roomID: session.RoomID,
			clientTime: death.ClientTime, battle: battle,
		}
	}
	return result, nil
}

func (server *Server) handleAdventurePlayerDeath(session *connectionSession, data []byte, death game.PlayerDeathEvent, battle *match.AdventureBattle, result adventureDeathDispatchResult) (adventureDeathDispatchResult, error) {
	result.result = fmt.Sprintf("qqt_game_event_ack_player_death_%d", death.PlayerID)
	result.followUpResult = fmt.Sprintf("qqt_game_event_notify_player_death_%d", death.PlayerID)
	resolution, err := battle.RecordDeath(death.PlayerID)
	if err != nil {
		return adventureDeathDispatchResult{}, err
	}
	if resolution.FinalVictoryPending && resolution.AllPlayersDead {
		return server.finalizeAdventurePendingVictory(session, data, death.ClientTime, battle, result)
	}
	if !resolution.NewlyConcluded {
		return result, nil
	}

	results := make([]game.GameResultData, 0, len(resolution.Participants))
	for _, participant := range resolution.Participants {
		statistics, statisticsErr := battle.PlayerStatistics(participant.PlayerID)
		if statisticsErr != nil {
			return adventureDeathDispatchResult{}, statisticsErr
		}
		results = append(results, game.GameResultData{
			PlayerID: participant.PlayerID,
			Result:   game.GameResultLoss,
			Fields: (game.AdventureResultStatistics{
				KillCount: statistics.KillCount, KillScore: statistics.KillScore,
				RescueCount: statistics.RescueCount, RescueScore: statistics.RescueScore,
				RewardCount: statistics.RewardCount, RewardScore: statistics.RewardScore,
			}).GameResultFields(),
		})
	}
	gameOver := game.GameOverData{Time: death.ClientTime, Results: results, GameMode: game.SettlementGameModeAdventure}
	applyAdventureOutcomeCourageMultiplier(&gameOver)
	result.startFollowUp, err = game.BuildLocalGameOverNotifyFromDeath(data, session.RoomID, server.nextGameDataSequence(), gameOver)
	if err != nil {
		return adventureDeathDispatchResult{}, err
	}
	result.startFollowUpResult = "qqt_adventure_game_over_all_players_eliminated"
	result.startFollowUpDelay = 750 * time.Millisecond
	result.completeAfterSend = true
	return attachAdventureDeathSettlement(session, battle, gameOver, result)
}

func (server *Server) finalizeAdventurePendingVictory(session *connectionSession, data []byte, clientTime uint32, battle *match.AdventureBattle, result adventureDeathDispatchResult) (adventureDeathDispatchResult, error) {
	finalResolution, err := battle.FinalizePendingVictory()
	if err != nil || !finalResolution.NewlyConcluded {
		return result, err
	}
	gameOver, err := adventureFinalGameOverData(battle, finalResolution, clientTime)
	if err != nil {
		return adventureDeathDispatchResult{}, err
	}
	result.startFollowUp, err = game.BuildLocalGameOverNotifyFromEvent(data, session.RoomID, server.nextGameDataSequence(), gameOver)
	if err != nil {
		return adventureDeathDispatchResult{}, err
	}
	result.result += "_final_victory_all_players_dead"
	result.startFollowUpResult = "qqt_adventure_game_over_final_victory_all_players_dead"
	result.startFollowUpDelay = 50 * time.Millisecond
	result.completeAfterSend = true
	return attachAdventureDeathSettlement(session, battle, gameOver, result)
}

func attachAdventureDeathSettlement(session *connectionSession, battle *match.AdventureBattle, gameOver game.GameOverData, result adventureDeathDispatchResult) (adventureDeathDispatchResult, error) {
	settlement, err := adventureSettlementForPlayer(gameOver, session.Profile.PlayerID)
	if err != nil {
		return adventureDeathDispatchResult{}, err
	}
	if err = attachAdventureCollectedItems(battle, session.Profile.PlayerID, &settlement); err != nil {
		return adventureDeathDispatchResult{}, err
	}
	result.settlement = &settlement
	result.roomSettlement = &adventureRoomSettlementCommit{GameOver: gameOver, Battle: battle}
	return result, nil
}

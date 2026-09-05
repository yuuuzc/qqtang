package probe

import (
	"fmt"
	"sort"

	"qqtang/internal/game/match"
	"qqtang/internal/protocol/game"
)

type adventureSettlementCommit struct {
	Result                   game.GameResultCode
	AdventurePoints          uint32
	OutcomeMultiplierPercent uint32
	CollectedItems           map[uint32]uint32
}

type adventureRoomSettlementCommit struct {
	GameOver game.GameOverData
	Battle   *match.AdventureBattle
}

type competitiveSettlementCommit struct {
	Result            game.GameResultCode
	Points            uint32
	OutcomeBasePoints uint32
	MoneyReward       uint32
	CollectedItems    map[uint32]uint32
}

type competitiveRoomSettlementCommit struct {
	GameOver game.GameOverData
	Battle   *match.CompetitiveBattle
}

const (
	competitiveWinBasePoints           uint32 = 50
	competitiveLossBasePoints          uint32 = 10
	competitiveDrawBasePoints          uint32 = 20
	competitivePlayerActionPoints      uint32 = 10
	adventureWinMultiplierNumerator    uint64 = 3
	adventureWinMultiplierDenominator  uint64 = 2
	adventureLossMultiplierNumerator   uint64 = 7
	adventureLossMultiplierDenominator uint64 = 10
)

func competitiveActionScore(count uint32) uint32 {
	if count > ^uint32(0)/competitivePlayerActionPoints {
		return ^uint32(0)
	}
	return count * competitivePlayerActionPoints
}

func applyCompetitivePlayerStatistics(result *game.GameResultData, statistics match.CompetitivePlayerStatistics) {
	if result == nil {
		return
	}
	for len(result.Fields) < game.GameResultKnownFieldCount {
		result.Fields = append(result.Fields, game.GameResultField{})
	}
	killScore := competitiveActionScore(statistics.KillCount)
	rescueScore := competitiveActionScore(statistics.RescueCount)
	result.Fields[game.GameResultKillFieldIndex] = game.GameResultField{Count: statistics.KillCount, Score: killScore}
	result.Fields[game.GameResultRescueFieldIndex] = game.GameResultField{Count: statistics.RescueCount, Score: rescueScore}
	result.Point = saturatingAddUint32(result.Point, killScore)
	result.Point = saturatingAddUint32(result.Point, rescueScore)
}

func applyCompetitiveBattleStatistics(data *game.GameOverData, battle *match.CompetitiveBattle) error {
	if data == nil || battle == nil {
		return fmt.Errorf("competitive statistics require game-over data and battle")
	}
	for index := range data.Results {
		statistics, err := battle.PlayerStatistics(data.Results[index].PlayerID)
		if err != nil {
			return err
		}
		applyCompetitivePlayerStatistics(&data.Results[index], statistics)
	}
	return nil
}

func adventureOutcomeMultiplier(result game.GameResultCode) (numerator, denominator uint64) {
	switch result {
	case game.GameResultWin:
		return adventureWinMultiplierNumerator, adventureWinMultiplierDenominator
	case game.GameResultLoss:
		return adventureLossMultiplierNumerator, adventureLossMultiplierDenominator
	default:
		return 1, 1
	}
}

func adventureOutcomeMultiplierPercent(result game.GameResultCode) uint32 {
	numerator, denominator := adventureOutcomeMultiplier(result)
	return uint32(numerator * 100 / denominator)
}

func competitiveSettlementForPlayer(data game.GameOverData, playerID uint16) (competitiveSettlementCommit, error) {
	if data.GameMode != game.SettlementGameModeCompetitive {
		return competitiveSettlementCommit{}, fmt.Errorf("competitive settlement has game mode %d", data.GameMode)
	}
	var found *game.GameResultData
	for index := range data.Results {
		if data.Results[index].PlayerID != playerID {
			continue
		}
		if found != nil {
			return competitiveSettlementCommit{}, fmt.Errorf("competitive GAME_OVER has duplicate result for player %d", playerID)
		}
		found = &data.Results[index]
	}
	if found == nil {
		return competitiveSettlementCommit{}, fmt.Errorf("competitive GAME_OVER has no result for player %d", playerID)
	}
	switch found.Result {
	case game.GameResultLoss, game.GameResultDraw, game.GameResultWin:
	default:
		return competitiveSettlementCommit{}, fmt.Errorf("competitive GAME_OVER player %d has unsupported result %d", playerID, found.Result)
	}
	return competitiveSettlementCommit{
		Result: found.Result, Points: found.Point, OutcomeBasePoints: competitiveOutcomeBasePoints(found.Result),
	}, nil
}

func competitiveOutcomeBasePoints(result game.GameResultCode) uint32 {
	switch result {
	case game.GameResultWin:
		return competitiveWinBasePoints
	case game.GameResultDraw:
		return competitiveDrawBasePoints
	case game.GameResultLoss:
		return competitiveLossBasePoints
	default:
		return 0
	}
}

func addOutcomeBaseRewardScore(result *game.GameResultData, score uint32) {
	if result == nil || score == 0 {
		return
	}
	for len(result.Fields) < game.GameResultKnownFieldCount {
		result.Fields = append(result.Fields, game.GameResultField{})
	}
	result.Fields[game.GameResultRewardFieldIndex].Score = saturatingAddUint32(
		result.Fields[game.GameResultRewardFieldIndex].Score,
		score,
	)
}

// addCompetitiveOutcomeBasePoints adds the server-owned participation award
// to the native competitive score and mirrors it into the settlement UI's
// reward-score row. Competitive persistence consumes Point only, so the UI
// projection does not award the same points twice. Callers apply this exactly
// once at the authority boundary.
func addCompetitiveOutcomeBasePoints(data *game.GameOverData) {
	if data == nil {
		return
	}
	for index := range data.Results {
		base := competitiveOutcomeBasePoints(data.Results[index].Result)
		data.Results[index].Point = saturatingAddUint32(
			data.Results[index].Point,
			base,
		)
		addOutcomeBaseRewardScore(&data.Results[index], base)
	}
}

// applyAdventureOutcomeCourageMultiplier scales the total earned courage at
// the authority boundary: victory adds 50% and failure deducts 30%. Counts
// remain unchanged. Integer remainders are distributed across existing score
// fields so their sum exactly matches the rounded total multiplier.
func applyAdventureOutcomeCourageMultiplier(data *game.GameOverData) {
	if data == nil {
		return
	}
	for index := range data.Results {
		result := &data.Results[index]
		numerator, denominator := adventureOutcomeMultiplier(result.Result)
		if denominator == 0 || len(result.Fields) == 0 {
			continue
		}
		type remainderField struct {
			index     int
			remainder uint64
		}
		remainders := make([]remainderField, 0, len(result.Fields))
		var total, allocated uint64
		for fieldIndex := range result.Fields {
			score := uint64(result.Fields[fieldIndex].Score)
			total += score
			scaled := score * numerator
			base := scaled / denominator
			if base > uint64(^uint32(0)) {
				base = uint64(^uint32(0))
			}
			result.Fields[fieldIndex].Score = uint32(base)
			allocated += base
			remainders = append(remainders, remainderField{index: fieldIndex, remainder: scaled % denominator})
		}
		target := (total*numerator + denominator/2) / denominator
		maximum := uint64(len(result.Fields)) * uint64(^uint32(0))
		if target > maximum {
			target = maximum
		}
		sort.SliceStable(remainders, func(i, j int) bool { return remainders[i].remainder > remainders[j].remainder })
		for _, candidate := range remainders {
			if allocated >= target {
				break
			}
			if result.Fields[candidate.index].Score == ^uint32(0) {
				continue
			}
			result.Fields[candidate.index].Score++
			allocated++
		}
	}
}

func validateCompetitiveGameOver(data game.GameOverData, battle *match.CompetitiveBattle) error {
	if battle == nil {
		return fmt.Errorf("competitive battle is nil")
	}
	participants := battle.Participants()
	if len(data.Results) != len(participants) {
		return fmt.Errorf("competitive GAME_OVER has %d results for %d participants", len(data.Results), len(participants))
	}
	for _, participant := range participants {
		if _, err := competitiveSettlementForPlayer(data, participant.PlayerID); err != nil {
			return err
		}
	}
	return nil
}

// normalizeCompetitiveNoWinnerDraw applies the ordinary opposed-team timeout
// rule. A native rule may emit a result table with no winner when its clock
// expires; in that case every participant drew, regardless of which neutral
// animation code an old rule object placed in individual rows. Cooperative
// Boss defeat is intentionally excluded because no winner there means loss.
func normalizeCompetitiveNoWinnerDraw(data *game.GameOverData, battle *match.CompetitiveBattle) bool {
	if data == nil || battle == nil || battle.IsCooperative() {
		return false
	}
	for _, result := range data.Results {
		if result.Result == game.GameResultWin {
			return false
		}
	}
	changed := false
	for index := range data.Results {
		if data.Results[index].Result != game.GameResultDraw {
			data.Results[index].Result = game.GameResultDraw
			changed = true
		}
	}
	return changed
}

// normalizeCompetitiveTeamOutcome separates a participant's eliminated/alive
// state from the final team outcome. Native rule objects can report an already
// eliminated teammate as a loss even when another member of the same colour
// wins the round. The server-owned room teams are authoritative: once exactly
// one team has a winning row, every participant on that team wins.
// Cooperative Boss rounds use the same rule with one effective team.
func normalizeCompetitiveTeamOutcome(data *game.GameOverData, battle *match.CompetitiveBattle) (bool, error) {
	if data == nil || battle == nil {
		return false, nil
	}
	participants := battle.Participants()
	teamByPlayer := make(map[uint16]byte, len(participants))
	for _, participant := range participants {
		teamByPlayer[participant.PlayerID] = participant.TeamID
	}
	winnerTeams := make(map[byte]struct{})
	for _, result := range data.Results {
		teamID, exists := teamByPlayer[result.PlayerID]
		if !exists {
			return false, fmt.Errorf("competitive GAME_OVER contains unknown player %d", result.PlayerID)
		}
		if result.Result == game.GameResultWin {
			winnerTeams[teamID] = struct{}{}
		}
	}
	if len(winnerTeams) == 0 {
		return false, nil
	}
	if len(winnerTeams) != 1 {
		return false, fmt.Errorf("competitive GAME_OVER names %d winning teams", len(winnerTeams))
	}
	var winnerTeam byte
	for teamID := range winnerTeams {
		winnerTeam = teamID
	}
	changed := false
	for index := range data.Results {
		outcome := game.GameResultLoss
		if teamByPlayer[data.Results[index].PlayerID] == winnerTeam {
			outcome = game.GameResultWin
		}
		if data.Results[index].Result != outcome {
			data.Results[index].Result = outcome
			changed = true
		}
	}
	return changed, nil
}

// normalizeAdventureTeamOutcome applies the cooperative final-stage rule to a
// client-originated GAME_OVER. Only participants still carried by the current
// stage are changed: a player settled out before a stage transition keeps that
// earlier loss, while a teammate who died during the final stage shares the
// surviving team's victory.
func normalizeAdventureTeamOutcome(data *game.GameOverData, battle *match.AdventureBattle) (bool, error) {
	if data == nil || battle == nil {
		return false, nil
	}
	participants := battle.Participants()
	active := make(map[uint16]struct{}, len(participants))
	for _, participant := range participants {
		active[participant.PlayerID] = struct{}{}
	}
	results := make(map[uint16]int, len(data.Results))
	teamWon := false
	for index, result := range data.Results {
		if _, exists := active[result.PlayerID]; !exists {
			continue
		}
		if _, duplicate := results[result.PlayerID]; duplicate {
			return false, fmt.Errorf("adventure GAME_OVER has duplicate result for player %d", result.PlayerID)
		}
		results[result.PlayerID] = index
		teamWon = teamWon || result.Result == game.GameResultWin
	}
	for playerID := range active {
		if _, exists := results[playerID]; !exists {
			return false, fmt.Errorf("adventure GAME_OVER has no result for active player %d", playerID)
		}
	}
	if !teamWon {
		return false, nil
	}
	changed := false
	for playerID, index := range results {
		if _, exists := active[playerID]; exists && data.Results[index].Result != game.GameResultWin {
			data.Results[index].Result = game.GameResultWin
			changed = true
		}
	}
	return changed, nil
}

func adventureSettlementForPlayer(data game.GameOverData, playerID uint16) (adventureSettlementCommit, error) {
	if data.GameMode != game.SettlementGameModeAdventure {
		return adventureSettlementCommit{}, fmt.Errorf("adventure settlement has game mode %d", data.GameMode)
	}
	var found *game.GameResultData
	for index := range data.Results {
		if data.Results[index].PlayerID != playerID {
			continue
		}
		if found != nil {
			return adventureSettlementCommit{}, fmt.Errorf("NOTIFY_GAME_OVER has duplicate result for player %d", playerID)
		}
		found = &data.Results[index]
	}
	if found == nil {
		return adventureSettlementCommit{}, fmt.Errorf("NOTIFY_GAME_OVER has no result for player %d", playerID)
	}
	switch found.Result {
	case game.GameResultLoss, game.GameResultWin:
	default:
		return adventureSettlementCommit{}, fmt.Errorf("NOTIFY_GAME_OVER player %d has unsupported result %d", playerID, found.Result)
	}
	return adventureSettlementCommit{
		Result: found.Result, AdventurePoints: found.TotalFieldScore(),
		OutcomeMultiplierPercent: adventureOutcomeMultiplierPercent(found.Result),
	}, nil
}

func attachAdventureCollectedItems(battle *match.AdventureBattle, playerID uint16, settlement *adventureSettlementCommit) error {
	if settlement == nil {
		return fmt.Errorf("adventure settlement is nil")
	}
	items, err := battle.CollectedItems(playerID)
	if err != nil {
		return err
	}
	settlement.CollectedItems = items
	return nil
}

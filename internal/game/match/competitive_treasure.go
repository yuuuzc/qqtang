package match

import (
	"fmt"

	"qqtang/internal/clientdata/sceneelement"
)

// These are scene-element IDs, not account inventory IDs. Client.exe's native
// rule-5 object constructs exactly these three objects and computes the carried
// score as count(150) + 2*count(151) + 3*count(152).
const (
	TreasureGemValueOne   uint32 = uint32(sceneelement.TreasureGemOne)
	TreasureGemValueTwo   uint32 = uint32(sceneelement.TreasureGemTwo)
	TreasureGemValueThree uint32 = uint32(sceneelement.TreasureGemThree)
	// The original mode scales its team victory target with team size.
	TreasureTargetPerTeamMember uint32 = 22
)

type treasureGemCounts [3]uint32

type treasurePickupKey struct {
	PlayerID   uint16
	ClientTime uint32
	ItemID     uint32
	PosX       uint16
	PosY       uint16
}

type competitiveTreasureState struct {
	held         map[uint16]treasureGemCounts
	seen         map[treasurePickupKey]struct{}
	targetByTeam map[byte]uint32
}

func newCompetitiveTreasureState(participants []CompetitiveParticipant) *competitiveTreasureState {
	state := &competitiveTreasureState{
		held: make(map[uint16]treasureGemCounts), seen: make(map[treasurePickupKey]struct{}),
		targetByTeam: make(map[byte]uint32),
	}
	for _, participant := range participants {
		state.targetByTeam[participant.TeamID] += TreasureTargetPerTeamMember
	}
	return state
}

func treasureGemIndex(itemID uint32) (int, bool) {
	switch itemID {
	case TreasureGemValueOne:
		return 0, true
	case TreasureGemValueTwo:
		return 1, true
	case TreasureGemValueThree:
		return 2, true
	default:
		return 0, false
	}
}

func IsTreasureGem(itemID uint32) bool {
	_, ok := treasureGemIndex(itemID)
	return ok
}

func (state *competitiveTreasureState) pickup(key treasurePickupKey) (bool, error) {
	index, ok := treasureGemIndex(key.ItemID)
	if !ok {
		return false, fmt.Errorf("scene item %d is not a treasure gem", key.ItemID)
	}
	if _, duplicate := state.seen[key]; duplicate {
		return false, nil
	}
	counts := state.held[key.PlayerID]
	if counts[index] == ^uint32(0) {
		return false, fmt.Errorf("treasure gem counter overflow for player %d item %d", key.PlayerID, key.ItemID)
	}
	counts[index]++
	state.held[key.PlayerID] = counts
	state.seen[key] = struct{}{}
	return true, nil
}

func (state *competitiveTreasureState) scatter(playerID uint16, _ []uint32) error {
	// The native rule object owns the carried slot and emits the authoritative
	// death-drop list. Pickups and the death notification can arrive through
	// different transports, so the server mirror is not guaranteed to have
	// observed every preceding pickup. Requiring an exact mirror match rejected
	// valid deaths and prevented the reliable drop notification from reaching
	// peers. A death boundary only needs to clear the mirrored carried score;
	// the client-provided list remains responsible for scene placement.
	delete(state.held, playerID)
	return nil
}

func (state *competitiveTreasureState) value(playerID uint16) uint32 {
	counts := state.held[playerID]
	return counts[0] + counts[1]*2 + counts[2]*3
}

func (state *competitiveTreasureState) teamValue(teamID byte, participants []CompetitiveParticipant) uint32 {
	var total uint32
	for _, participant := range participants {
		if participant.TeamID == teamID {
			total += state.value(participant.PlayerID)
		}
	}
	return total
}

// RecordTreasurePickup validates one common REQUEST_GET_ITEM boundary and
// closes the round when the collecting team reaches 22 points per member.
// Native rule 5 still owns scene placement; the server mirrors only the
// carried counters exposed by pickup/death packets.
func (battle *CompetitiveBattle) RecordTreasurePickup(playerID uint16, clientTime uint32, itemID uint32, posX, posY uint16) (CompetitiveResolution, bool, error) {
	if battle == nil {
		return CompetitiveResolution{}, false, fmt.Errorf("competitive battle is nil")
	}
	battle.mu.Lock()
	defer battle.mu.Unlock()
	if battle.treasure == nil {
		return CompetitiveResolution{}, false, fmt.Errorf("competitive battle does not use the treasure objective")
	}
	participant, ok := battle.participantByID[playerID]
	if !ok {
		return CompetitiveResolution{}, false, fmt.Errorf("treasure pickup player %d is not a participant", playerID)
	}
	if battle.concluded {
		return battle.resolution(false), false, nil
	}
	if battle.departed[playerID] {
		return battle.resolution(false), false, fmt.Errorf("treasure pickup player %d has departed", playerID)
	}
	recorded, err := battle.treasure.pickup(treasurePickupKey{
		PlayerID: playerID, ClientTime: clientTime, ItemID: itemID, PosX: posX, PosY: posY,
	})
	if err != nil || !recorded {
		return battle.resolution(false), recorded, err
	}
	if battle.treasure.teamValue(participant.TeamID, battle.participants) >= battle.treasure.targetByTeam[participant.TeamID] {
		battle.concluded = true
		battle.winnerTeamID = participant.TeamID
		return battle.resolution(true), true, nil
	}
	return battle.resolution(false), true, nil
}

// RecordTreasureScatter consumes NOTIFY_PLAYER_DIE or
// NOTIFY_PLAYER_BE_KILLED as the authoritative native carried-slot reset.
// itemIDs are retained in the boundary for diagnostics/scene relay, but the
// server deliberately does not require them to equal its transport-dependent
// pickup mirror.
func (battle *CompetitiveBattle) RecordTreasureScatter(playerID uint16, itemIDs []uint32) error {
	if battle == nil {
		return fmt.Errorf("competitive battle is nil")
	}
	battle.mu.Lock()
	defer battle.mu.Unlock()
	if battle.treasure == nil {
		return nil
	}
	if _, ok := battle.participantByID[playerID]; !ok {
		return fmt.Errorf("treasure scatter player %d is not a participant", playerID)
	}
	return battle.treasure.scatter(playerID, itemIDs)
}

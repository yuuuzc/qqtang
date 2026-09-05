package match

import "fmt"

type competitiveBunEventKey struct {
	pickup           bool
	playerID         uint16
	clientTime       uint32
	posX, posY       uint16
	bunID, bunTeamID byte
}

// competitiveBunState mirrors only the conserved state exposed by the
// native 0x0FB7/0x0FB9 notifications. Positions and collision ownership stay
// in the elected client rule object; the server tracks which colour bun is at
// a base, loose in the scene, or carried by exactly one participant.
type competitiveBunState struct {
	initialByTeam map[byte]uint16
	storedByBase  map[byte]map[byte]uint16
	looseByTeam   map[byte]uint16
	carriedBy     map[uint16]byte
	seen          map[competitiveBunEventKey]struct{}
}

func newCompetitiveBunState(participants []CompetitiveParticipant) *competitiveBunState {
	state := &competitiveBunState{
		initialByTeam: make(map[byte]uint16), storedByBase: make(map[byte]map[byte]uint16),
		looseByTeam: make(map[byte]uint16), carriedBy: make(map[uint16]byte),
		seen: make(map[competitiveBunEventKey]struct{}),
	}
	// Native rule 3 starts one home bun per player. This matches the observed
	// 1v1 layout (one differently coloured bun in each house) and preserves
	// equal-team room scaling without inventing map-specific quantities.
	for _, participant := range participants {
		state.initialByTeam[participant.TeamID]++
	}
	for teamID, count := range state.initialByTeam {
		state.storedByBase[teamID] = map[byte]uint16{teamID: count}
	}
	return state
}

func (state *competitiveBunState) totalAtBase(teamID byte) uint32 {
	var total uint32
	for _, count := range state.storedByBase[teamID] {
		total += uint32(count)
	}
	return total
}

func (state *competitiveBunState) timeoutWinner() byte {
	var winner byte
	var highest uint32
	tied := false
	for teamID := range state.initialByTeam {
		total := state.totalAtBase(teamID)
		switch {
		case winner == 0 || total > highest:
			winner, highest, tied = teamID, total, false
		case total == highest:
			tied = true
		}
	}
	if tied {
		return 0
	}
	return winner
}

func (state *competitiveBunState) hasCapturedEveryOpponent(teamID byte) bool {
	for originTeam, initial := range state.initialByTeam {
		if originTeam == teamID {
			continue
		}
		if state.storedByBase[teamID][originTeam] < initial {
			return false
		}
	}
	return true
}

func (battle *CompetitiveBattle) dropCarriedBunLocked(playerID uint16) {
	if battle.buns == nil {
		return
	}
	if bunTeamID, carried := battle.buns.carriedBy[playerID]; carried {
		delete(battle.buns.carriedBy, playerID)
		battle.buns.looseByTeam[bunTeamID]++
	}
}

// RecordBunAction consumes only an arbitrator-generated NOTIFY event. It
// returns newEvent=false for an exact transport retry so callers can ACK it
// without broadcasting or applying it twice.
func (battle *CompetitiveBattle) RecordBunAction(pickup bool, playerID uint16, clientTime uint32, posX, posY uint16, bunID, bunTeamID byte) (CompetitiveResolution, bool, error) {
	if battle == nil {
		return CompetitiveResolution{}, false, fmt.Errorf("competitive battle is nil")
	}
	battle.mu.Lock()
	defer battle.mu.Unlock()
	if battle.buns == nil {
		return CompetitiveResolution{}, false, fmt.Errorf("competitive battle does not use the bun objective")
	}
	participant, ok := battle.participantByID[playerID]
	if !ok {
		return CompetitiveResolution{}, false, fmt.Errorf("bun player %d is not a participant", playerID)
	}
	if _, ok = battle.buns.initialByTeam[bunTeamID]; !ok {
		return CompetitiveResolution{}, false, fmt.Errorf("bun team %d is not in this battle", bunTeamID)
	}
	key := competitiveBunEventKey{pickup: pickup, playerID: playerID, clientTime: clientTime, posX: posX, posY: posY, bunID: bunID, bunTeamID: bunTeamID}
	if _, duplicate := battle.buns.seen[key]; duplicate {
		return battle.resolution(false), false, nil
	}
	if battle.concluded {
		return battle.resolution(false), false, nil
	}
	if battle.departed[playerID] {
		return CompetitiveResolution{}, false, fmt.Errorf("departed bun player %d cannot act", playerID)
	}

	if pickup {
		if carriedTeam, carrying := battle.buns.carriedBy[playerID]; carrying {
			// Rule 3 has one native carry slot, but the player may drop its
			// current bun without a separate server message. The arbitrator only
			// emits this pickup notification after observing an empty slot, so it
			// is authoritative proof that the mirrored bun became loose first.
			delete(battle.buns.carriedBy, playerID)
			battle.buns.looseByTeam[carriedTeam]++
		}
		switch bunID {
		case 0: // bun still stored in its colour's home base
			// A preceding fast-only pickup/drop may be absent from the service
			// mirror. Reconcile the known count without rejecting the elected
			// native rule's collision result.
			if battle.buns.storedByBase[bunTeamID][bunTeamID] > 0 {
				battle.buns.storedByBase[bunTeamID][bunTeamID]--
			}
		case 1: // loose bun created by death/drop
			if battle.buns.looseByTeam[bunTeamID] > 0 {
				battle.buns.looseByTeam[bunTeamID]--
			}
		default:
			return CompetitiveResolution{}, false, fmt.Errorf("bun pickup ID %d is outside native rule-3 forms 0..1", bunID)
		}
		battle.buns.carriedBy[playerID] = bunTeamID
		battle.buns.seen[key] = struct{}{}
		return battle.resolution(false), true, nil
	}

	// The reliable deposit can arrive after a fast-only pickup that the
	// service did not observe. The arbitrator already validated the native
	// one-slot carry state, so reconcile rather than inventing causal order.
	delete(battle.buns.carriedBy, playerID)
	battle.buns.storedByBase[participant.TeamID][bunTeamID]++
	battle.buns.seen[key] = struct{}{}
	if participant.TeamID != bunTeamID {
		battle.objectives[playerID] = saturatingIncrement(battle.objectives[playerID], 1)
	}
	if battle.buns.hasCapturedEveryOpponent(participant.TeamID) {
		battle.concluded = true
		battle.winnerTeamID = participant.TeamID
		return battle.resolution(true), true, nil
	}
	return battle.resolution(false), true, nil
}

// RecordDeparture permanently removes a participant from the active round.
// Unlike timed death, departure has no respawn boundary; when only one
// opposed team remains it receives a complete settlement immediately.
func (battle *CompetitiveBattle) RecordDeparture(playerID uint16) (CompetitiveResolution, error) {
	if battle == nil {
		return CompetitiveResolution{}, fmt.Errorf("competitive battle is nil")
	}
	battle.mu.Lock()
	defer battle.mu.Unlock()
	participant, ok := battle.participantByID[playerID]
	if !ok {
		return CompetitiveResolution{}, fmt.Errorf("competitive departure player %d is not a participant", playerID)
	}
	if battle.concluded || battle.departed[playerID] {
		return battle.resolution(false), nil
	}
	battle.departed[playerID] = true
	battle.alive[playerID] = false
	battle.trapped[playerID] = false
	battle.pendingDeath[playerID] = false
	battle.dropCarriedBunLocked(playerID)
	battle.dropCarriedSculptureLocked(playerID)

	remainingTeams := make(map[byte]struct{})
	remainingParticipants := make([]CompetitiveParticipant, 0, len(battle.participants)-1)
	for id, member := range battle.participantByID {
		if !battle.departed[id] {
			remainingTeams[member.TeamID] = struct{}{}
			remainingParticipants = append(remainingParticipants, member)
		}
	}
	// Native shared-scene ownership belongs to a connected room participant,
	// not to an avatar's temporary alive state. Timed-respawn rules therefore
	// keep a connected defeated player eligible. If the departing player owned
	// the rule object, SelectCompetitiveArbitrator deterministically elects the
	// lowest remaining PlayerID.
	if len(remainingParticipants) == 0 {
		battle.arbitratorID = 0
	} else if battle.arbitratorID == playerID {
		selected, selectErr := SelectCompetitiveArbitrator(battle.arbitratorID, remainingParticipants)
		if selectErr != nil {
			// A live server battle cannot retain a native shared-scene owner after
			// its last connected human leaves. Virtual actors may still remain in
			// the deterministic engine, but there is no client to observe that
			// headless continuation; close this live match without ever assigning
			// the arbitrator role to AI.
			battle.arbitratorID = 0
			battle.concluded = true
			if len(remainingTeams) == 1 {
				for teamID := range remainingTeams {
					battle.winnerTeamID = teamID
				}
			} else {
				battle.timedOutDraw = true
			}
			return battle.resolution(true), nil
		}
		battle.arbitratorID = selected
	}
	if battle.teamTopology == CompetitiveTeamsCooperative {
		// A cooperative/Boss round is lost as soon as no connected avatar is
		// alive.  Counting merely connected teams here left an already-dead
		// teammate holding the battle open after the final living player left;
		// the client had no further death event it could send, so settlement
		// never arrived.  Departed avatars are marked dead above, while timed
		// respawn participants remain alive only after their normal revive path.
		if len(battle.livingTeams()) != 0 {
			return battle.resolution(false), nil
		}
		battle.concluded = true
		battle.arbitratorID = 0
		return battle.resolution(true), nil
	}
	if battle.objective == CompetitiveObjectiveTankBase {
		// A connected team with an intact base remains eligible for native timed
		// respawn even when every avatar is temporarily dead. A team with no
		// connected member, or a destroyed base plus no living member, is out.
		return battle.maybeConcludeTankLocked(), nil
	}
	if len(remainingTeams) > 1 {
		return battle.resolution(false), nil
	}
	battle.concluded = true
	battle.arbitratorID = 0
	if len(remainingTeams) == 0 {
		battle.timedOutDraw = true
		return battle.resolution(true), nil
	}
	for teamID := range remainingTeams {
		battle.winnerTeamID = teamID
	}
	_ = participant
	return battle.resolution(true), nil
}

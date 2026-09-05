package match

import "fmt"

const (
	competitiveSculpturePickupID       byte = 2
	competitiveSculptureDepositID      byte = 0
	competitiveSculptureFirstMaterial  byte = 12
	competitiveSculptureLastMaterial   byte = 16
	competitiveSculptureRequiredPieces byte = 4
)

type competitiveSculptureEventKey struct {
	pickup       bool
	playerID     uint16
	clientTime   uint32
	posX, posY   uint16
	bunID        byte
	materialType byte
}

// competitiveSculptureState mirrors the conserved subset of native rule 6.
// The arbitrator-owned client remains responsible for scene collisions and
// the sculpture animation; the server only validates one carried fragment per
// player and the four accepted deposits that complete a team's sculpture.
// A validated arbitrator notification is the authoritative observation of
// that client-owned slot: it may reconcile stale server state left by an
// intervening native drop/re-pick path, but never creates a second slot.
type competitiveSculptureState struct {
	progressByTeam map[byte]byte
	carriedBy      map[uint16]byte
	seen           map[competitiveSculptureEventKey]struct{}
}

func newCompetitiveSculptureState(participants []CompetitiveParticipant) *competitiveSculptureState {
	state := &competitiveSculptureState{
		progressByTeam: make(map[byte]byte),
		carriedBy:      make(map[uint16]byte),
		seen:           make(map[competitiveSculptureEventKey]struct{}),
	}
	for _, participant := range participants {
		state.progressByTeam[participant.TeamID] = 0
	}
	return state
}

func (state *competitiveSculptureState) timeoutWinner() byte {
	var winner byte
	var highest byte
	tied := false
	for teamID, progress := range state.progressByTeam {
		switch {
		case winner == 0 || progress > highest:
			winner, highest, tied = teamID, progress, false
		case progress == highest:
			tied = true
		}
	}
	if tied {
		return 0
	}
	return winner
}

func (battle *CompetitiveBattle) dropCarriedSculptureLocked(playerID uint16) {
	if battle.sculptures != nil {
		delete(battle.sculptures.carriedBy, playerID)
	}
}

func validSculptureMaterial(materialType byte) bool {
	return materialType >= competitiveSculptureFirstMaterial && materialType <= competitiveSculptureLastMaterial
}

// RecordSculptureAction consumes an arbitrator-generated 0x0FB7/0x0FB9
// notification. Rule 6 uses BunID 2 for a scene-fragment pickup and BunID 0
// for a same-team sculpture deposit; BunTeamID carries material type 12..16.
// Exact transport retries are acknowledged without being applied twice.
func (battle *CompetitiveBattle) RecordSculptureAction(pickup bool, playerID uint16, clientTime uint32, posX, posY uint16, bunID, materialType byte) (CompetitiveResolution, bool, error) {
	if battle == nil {
		return CompetitiveResolution{}, false, fmt.Errorf("competitive battle is nil")
	}
	battle.mu.Lock()
	defer battle.mu.Unlock()
	if battle.sculptures == nil {
		return CompetitiveResolution{}, false, fmt.Errorf("competitive battle does not use the sculpture objective")
	}
	participant, ok := battle.participantByID[playerID]
	if !ok {
		return CompetitiveResolution{}, false, fmt.Errorf("sculpture player %d is not a participant", playerID)
	}
	if !validSculptureMaterial(materialType) {
		return CompetitiveResolution{}, false, fmt.Errorf("sculpture material type %d is outside native range %d..%d", materialType, competitiveSculptureFirstMaterial, competitiveSculptureLastMaterial)
	}
	key := competitiveSculptureEventKey{
		pickup: pickup, playerID: playerID, clientTime: clientTime,
		posX: posX, posY: posY, bunID: bunID, materialType: materialType,
	}
	if _, duplicate := battle.sculptures.seen[key]; duplicate {
		return battle.resolution(false), false, nil
	}
	if battle.concluded {
		return battle.resolution(false), false, nil
	}
	if battle.departed[playerID] {
		return CompetitiveResolution{}, false, fmt.Errorf("departed sculpture player %d cannot act", playerID)
	}

	if pickup {
		if bunID != competitiveSculpturePickupID {
			return CompetitiveResolution{}, false, fmt.Errorf("sculpture pickup ID %d, want native rule-6 form %d", bunID, competitiveSculpturePickupID)
		}
		// The native arbitrator only emits NOTIFY_GET after checking the
		// player's single carry slot is empty. If our mirror still contains an
		// older value, this notification proves a native drop/re-pick boundary
		// occurred that has no separate server message. Replace the one slot;
		// do not reject the event and desynchronise the other clients.
		battle.sculptures.carriedBy[playerID] = materialType
		battle.sculptures.seen[key] = struct{}{}
		return battle.resolution(false), true, nil
	}

	if bunID != competitiveSculptureDepositID {
		return CompetitiveResolution{}, false, fmt.Errorf("sculpture deposit ID %d, want native rule-6 form %d", bunID, competitiveSculptureDepositID)
	}
	// The arbitrator validates its native carry slot before producing this
	// notification. Its preceding pickup can be fast-only and absent from the
	// server mirror; the deposit itself is authoritative proof of the carried
	// fragment. Missing or differing mirrored state must not block the relay.
	progress := battle.sculptures.progressByTeam[participant.TeamID]
	if progress >= competitiveSculptureRequiredPieces {
		return CompetitiveResolution{}, false, fmt.Errorf("team %d sculpture is already complete", participant.TeamID)
	}
	delete(battle.sculptures.carriedBy, playerID)
	progress++
	battle.sculptures.progressByTeam[participant.TeamID] = progress
	battle.sculptures.seen[key] = struct{}{}
	battle.objectives[playerID] = saturatingIncrement(battle.objectives[playerID], 1)
	if progress == competitiveSculptureRequiredPieces {
		battle.concluded = true
		battle.winnerTeamID = participant.TeamID
		return battle.resolution(true), true, nil
	}
	return battle.resolution(false), true, nil
}

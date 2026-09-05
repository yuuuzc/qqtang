package probe

import (
	"context"

	"qqtang/internal/game/mapdata"
	"qqtang/internal/game/match"
	"qqtang/internal/protocol/game"
)

const (
	lightningCardItemID = uint16(199)
	bezierSwordItemID   = uint16(467)
	shaolinKickItemID   = uint16(468)
)

// passiveFunctionItemEffects is intentionally ownership based.  These three
// shop entries are passive function items, not wearable role equipment.  The
// collection cabinet is the player's authoritative off switch: status 2 keeps
// ownership but removes the item from active inventory use.
func passiveFunctionItemEffects(profile game.PlayerProfile) byte {
	var effects byte
	for _, item := range profile.Inventory {
		// Active() checks only quantity and availability; it does not mean
		// equipped/status 1. Status 2 remains the explicit cabinet off switch.
		if !item.Active() || item.ItemStatus == game.ItemStatusCollected {
			continue
		}
		switch item.ItemID {
		case lightningCardItemID:
			effects |= game.PassiveEffectLightning
		case bezierSwordItemID:
			effects |= game.PassiveEffectSwordArc
		case shaolinKickItemID:
			effects |= game.PassiveEffectKickImpact
		}
	}
	return effects
}

func passiveFunctionRule(rule mapdata.CompetitiveRuleKind) (byte, byte, bool) {
	switch rule {
	case mapdata.CompetitiveRuleMachine:
		return game.PassiveFunctionRuleMachine, game.PassiveEffectLightning, true
	case mapdata.CompetitiveRuleKickBomb:
		return game.PassiveFunctionRuleKickBomb, game.PassiveEffectSwordArc | game.PassiveEffectKickImpact, true
	default:
		return 0, 0, false
	}
}

// competitivePassiveFunctionProjections snapshots passive item ownership at
// match start. It deliberately does not mutate the inventory or synthesize an
// equipped platform slot: the client adapter caches only this match-local
// player-to-mask table.
func (server *Server) competitivePassiveFunctionProjections(actor *connectionSession, participants []match.CompetitiveParticipant, rule mapdata.CompetitiveRuleKind) []game.PassiveFunctionProjection {
	wireRule, allowed, ok := passiveFunctionRule(rule)
	if !ok {
		return nil
	}
	projections := make([]game.PassiveFunctionProjection, 0, len(participants))
	for index, participant := range participants {
		profile, _ := server.competitiveParticipantProfile(actor, participant.PlayerID)
		projections = append(projections, game.PassiveFunctionProjection{
			Rule: wireRule, PlayerID: participant.PlayerID,
			Effects: passiveFunctionItemEffects(profile) & allowed,
			Reset:   index == 0,
		})
	}
	return projections
}

func (server *Server) competitiveParticipantProfile(actor *connectionSession, playerID uint16) (game.PlayerProfile, bool) {
	if actor != nil && actor.Profile.PlayerID == playerID {
		return clonePlayerProfile(actor.Profile), true
	}
	if actor == nil {
		return game.PlayerProfile{}, false
	}
	peer := server.liveRoomSessionForPlayer(actor.RoomID, playerID)
	if peer == nil {
		return game.PlayerProfile{}, false
	}
	if peer.mu.TryLock() {
		profile := clonePlayerProfile(peer.Profile)
		peer.mu.Unlock()
		return profile, true
	}
	if server.playerStore == nil {
		return game.PlayerProfile{}, false
	}
	profile, err := server.playerStore.Load(context.Background(), peer.liveUIN.Load())
	if err != nil {
		return game.PlayerProfile{}, false
	}
	return profile, true
}

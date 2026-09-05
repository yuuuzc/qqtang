package probe

import (
	"fmt"

	"qqtang/internal/game/mapdata"
	"qqtang/internal/game/match"
	roomstate "qqtang/internal/game/room"
)

func competitivePlayerLifecycle(lifecycle mapdata.CompetitivePlayerLifecycle) (match.CompetitivePlayerLifecycle, error) {
	switch lifecycle {
	case mapdata.CompetitiveLifecyclePermanentElimination:
		return match.CompetitivePlayerPermanentElimination, nil
	case mapdata.CompetitiveLifecycleTimedRespawn:
		return match.CompetitivePlayerTimedRespawn, nil
	case mapdata.CompetitiveLifecycleNativeDurability:
		return match.CompetitivePlayerNativeDurability, nil
	default:
		return 0, fmt.Errorf("unknown competitive player lifecycle %q", lifecycle)
	}
}

func competitiveObjective(objective mapdata.CompetitiveObjective) match.CompetitiveObjectiveKind {
	switch objective {
	case mapdata.CompetitiveObjectiveBun:
		return match.CompetitiveObjectiveBun
	case mapdata.CompetitiveObjectiveWrestle:
		return match.CompetitiveObjectiveWrestle
	case mapdata.CompetitiveObjectiveSculpture:
		return match.CompetitiveObjectiveSculpture
	case mapdata.CompetitiveObjectiveTreasure:
		return match.CompetitiveObjectiveTreasure
	case mapdata.CompetitiveObjectiveTankBase:
		return match.CompetitiveObjectiveTankBase
	}
	return match.CompetitiveObjectiveNone
}

// requireAvatarRecoveryAuthority preserves the original ownership boundary
// for 0x10E0: only the elected native arbitrator scans avatar expiry. The
// target keeps its stable participant/Boss identity while its render object
// is destroyed and restored.
func (server *Server) requireAvatarRecoveryAuthority(session *connectionSession, targetID uint16) error {
	if targetID == 0 {
		return fmt.Errorf("avatar recovery requires a non-zero target")
	}
	if err := server.requireMatchArbitrator(session, "avatar-recovery notify"); err != nil {
		return err
	}
	category, err := server.activeSessionMatchCategory(session)
	if err != nil {
		return err
	}
	switch category {
	case roomstate.CategoryAdventure:
		battle, battleErr := server.adventureBattle(session.CurrentGameID)
		if battleErr != nil {
			return battleErr
		}
		if !battle.HasParticipant(targetID) {
			return fmt.Errorf("avatar-recovery target %d is not an adventure participant", targetID)
		}
	case roomstate.CategoryCompetitive:
		battle, battleErr := server.competitiveBattle(session.CurrentGameID)
		if battleErr != nil {
			return battleErr
		}
		if !battle.HasParticipant(targetID) && !battle.AuthorizesBossProxy(session.Profile.PlayerID, targetID) {
			return fmt.Errorf("avatar-recovery target %d is neither a participant nor an active Boss", targetID)
		}
	default:
		return fmt.Errorf("avatar recovery requires an adventure or competitive match")
	}
	return nil
}

// requireMatchArbitrator protects client-generated shared scene tables. Both
// adventure and competitive rounds elect exactly one native authority; all
// other peers consume the resulting notification but must not create a
// second copy of the same items or drops.
func (server *Server) requireMatchArbitrator(session *connectionSession, operation string) error {
	if session == nil || session.Profile.PlayerID == 0 || session.CurrentGameID == 0 {
		return fmt.Errorf("%s requires an identified active match", operation)
	}
	category, err := server.activeSessionMatchCategory(session)
	if err != nil {
		return err
	}
	switch category {
	case roomstate.CategoryAdventure:
		battle, battleErr := server.adventureBattle(session.CurrentGameID)
		if battleErr != nil {
			return battleErr
		}
		if !battle.IsArbitrator(session.Profile.PlayerID) {
			return fmt.Errorf("%s player %d is not the adventure match arbitrator", operation, session.Profile.PlayerID)
		}
	case roomstate.CategoryCompetitive:
		battle, battleErr := server.competitiveBattle(session.CurrentGameID)
		if battleErr != nil {
			return battleErr
		}
		if !battle.IsArbitrator(session.Profile.PlayerID) {
			return fmt.Errorf("%s player %d is not the competitive match arbitrator", operation, session.Profile.PlayerID)
		}
	default:
		return fmt.Errorf("%s requires an adventure or competitive match", operation)
	}
	return nil
}

// requireMatchParticipantActor validates the legacy native ownership model.
// A participant may upload its own action directly; alternatively, the one
// elected scene arbitrator may forward that action after receiving it over
// QQTPPP.  The body actor remains the affected participant and must never be
// rewritten to the arbitrator's identity.
func (server *Server) requireMatchParticipantActor(session *connectionSession, actorID uint16, operation string) (bool, error) {
	if session == nil || actorID == 0 || session.Profile.PlayerID == 0 || session.CurrentGameID == 0 {
		return false, fmt.Errorf("%s requires an identified active match participant", operation)
	}
	category, err := server.activeSessionMatchCategory(session)
	if err != nil {
		return false, err
	}
	participant := false
	arbitrator := false
	switch category {
	case roomstate.CategoryAdventure:
		battle, battleErr := server.adventureBattle(session.CurrentGameID)
		if battleErr != nil {
			return false, battleErr
		}
		participant = battle.HasParticipant(actorID)
		arbitrator = battle.IsArbitrator(session.Profile.PlayerID)
	case roomstate.CategoryCompetitive:
		battle, battleErr := server.competitiveBattle(session.CurrentGameID)
		if battleErr != nil {
			return false, battleErr
		}
		participant = battle.HasParticipant(actorID)
		arbitrator = battle.IsArbitrator(session.Profile.PlayerID)
	default:
		return false, fmt.Errorf("%s requires an adventure or competitive match", operation)
	}
	if !participant {
		return false, fmt.Errorf("%s actor %d is not an active match participant", operation, actorID)
	}
	if actorID == session.Profile.PlayerID {
		return false, nil
	}
	if !arbitrator {
		return false, fmt.Errorf("%s actor %d is not owned by sender %d or the current match arbitrator", operation, actorID, session.Profile.PlayerID)
	}
	return true, nil
}

// requireNativeNPCEventAuthority validates the shared 0x1169/0x116A producer.
// Ordinary native rule NPCs are owned by the elected arbitrator. A named Boss
// overlay is narrower: the object must be registered, and a skill event must
// use one of the skill IDs advertised in that object's BOSS_INFO.
func (server *Server) requireNativeNPCEventAuthority(session *connectionSession, objectID, skillID uint16, operation string) error {
	if objectID == 0 {
		return fmt.Errorf("%s requires a non-zero NPC object ID", operation)
	}
	category, err := server.activeSessionMatchCategory(session)
	if err != nil {
		return err
	}
	if category != roomstate.CategoryCompetitive {
		return server.requireMatchArbitrator(session, operation)
	}
	battle, err := server.competitiveBattle(session.CurrentGameID)
	if err != nil {
		return err
	}
	if battle.BossID() == "" {
		if !battle.IsArbitrator(session.Profile.PlayerID) {
			return fmt.Errorf("%s player %d is not the competitive match arbitrator", operation, session.Profile.PlayerID)
		}
		return nil
	}
	if skillID != 0 {
		if !battle.AuthorizesBossSkillTarget(session.Profile.PlayerID, objectID, skillID) {
			return fmt.Errorf("%s object/target %d skill %d is not authorized by one unambiguous active BOSS_INFO", operation, objectID, skillID)
		}
		return nil
	}
	if !battle.AuthorizesBossProxy(session.Profile.PlayerID, objectID) {
		return fmt.Errorf("%s Boss %d is not an active arbitrator-owned entity", operation, objectID)
	}
	return nil
}

// requireCompetitiveRule is the common guard for every native special-rule
// event. It prevents adjacent schemas from being accepted on a different map
// merely because their wire body parses successfully.
func (server *Server) requireCompetitiveRule(session *connectionSession, kind mapdata.CompetitiveRuleKind, playerID uint16) (*match.CompetitiveBattle, error) {
	category, err := server.activeSessionMatchCategory(session)
	if err != nil {
		return nil, err
	}
	if category != roomstate.CategoryCompetitive {
		return nil, fmt.Errorf("%s event requires a competitive match", kind)
	}
	if server.mapCatalog == nil {
		return nil, fmt.Errorf("%s event requires the client map catalog", kind)
	}
	selected, ok := server.mapCatalog.CompetitiveMap(session.CurrentMapID)
	if !ok || selected.Rule != kind {
		return nil, fmt.Errorf("%s event is invalid on competitive map %d", kind, session.CurrentMapID)
	}
	battle, err := server.competitiveBattle(session.CurrentGameID)
	if err != nil {
		return nil, err
	}
	if playerID != 0 && !battle.HasParticipant(playerID) {
		return nil, fmt.Errorf("%s event player %d is not a battle participant", kind, playerID)
	}
	return battle, nil
}

// authorizeCompetitiveActor keeps gameplay identity independent from the
// actor's current rendering form. A transformed player remains a registered
// participant, while a transformed Boss remains the registered Boss object.
// The only cross-session exception is the native scene arbitrator uploading
// an action on behalf of one living Boss advertised by CREATE_NPC_BOSS.
func authorizeCompetitiveActor(session *connectionSession, battle *match.CompetitiveBattle, actorID uint16, operation string) (bool, error) {
	if session == nil || battle == nil || actorID == 0 {
		return false, fmt.Errorf("%s requires an identified competitive actor", operation)
	}
	if actorID == session.Profile.PlayerID {
		if !battle.HasParticipant(actorID) {
			return false, fmt.Errorf("%s actor %d is not a battle participant", operation, actorID)
		}
		return false, nil
	}
	if battle.AuthorizesBossProxy(session.Profile.PlayerID, actorID) {
		return true, nil
	}
	if battle.AuthorizesNativeNPCProxy(session.Profile.PlayerID, actorID) {
		return true, nil
	}
	return false, fmt.Errorf("%s actor %d is neither authenticated player %d nor an active arbitrator-owned native entity", operation, actorID, session.Profile.PlayerID)
}

// requireCompetitiveObjectiveRule accepts the shared 0x0FB6..0x0FB9 scene
// objective family only on the two native rules that register it. Rule 3
// carries buns; rule 6 carries and places sculpture fragments. Neither path
// represents an account-inventory pickup.
func (server *Server) requireCompetitiveObjectiveRule(session *connectionSession, playerID uint16) (*match.CompetitiveBattle, mapdata.CompetitiveRuleKind, error) {
	if server.mapCatalog == nil || session == nil {
		return nil, "", fmt.Errorf("objective event requires the client map catalog and a session")
	}
	selected, ok := server.mapCatalog.CompetitiveMap(session.CurrentMapID)
	if !ok || (selected.Rule != mapdata.CompetitiveRuleBun && selected.Rule != mapdata.CompetitiveRuleSculpture) {
		return nil, "", fmt.Errorf("objective event is invalid on competitive map %d", session.CurrentMapID)
	}
	battle, err := server.requireCompetitiveRule(session, selected.Rule, playerID)
	if err != nil {
		return nil, "", err
	}
	return battle, selected.Rule, nil
}

// requireCompetitiveBombDispatch admits the native airborne-batch uplink only
// on rule 2 (kick-bomb). Client.exe installs the 0x10E1 producer from rule 2's
// initializer; ordinary rule-1 Boss overlays only have the receiver. Boss
// airborne scenes therefore require a separate server-authored scheduler and
// must not broaden this client uplink boundary.
func (server *Server) requireCompetitiveBombDispatch(session *connectionSession, playerID uint16) (*match.CompetitiveBattle, error) {
	if server.mapCatalog == nil || session == nil {
		return nil, fmt.Errorf("dispatch-bomb event requires the client map catalog and a session")
	}
	selected, ok := server.mapCatalog.CompetitiveMap(session.CurrentMapID)
	if !ok {
		return nil, fmt.Errorf("dispatch-bomb map %d is not in the client catalog", session.CurrentMapID)
	}
	if selected.Rule != mapdata.CompetitiveRuleKickBomb {
		return nil, fmt.Errorf("dispatch-bomb is invalid for map %d rule %s", selected.ID, selected.Rule)
	}
	return server.requireCompetitiveRule(session, mapdata.CompetitiveRuleKickBomb, playerID)
}

// requireCompetitiveKickableBombRule accepts the shared PLAYER_THROW_BOMB
// action only for native rules which actually create kickable bubbles. Rule 2
// receives them from its airborne-bomb producer; rule 8 receives them from
// the fixed corner launchers and carried bomb props. Keeping this separate
// from requireCompetitiveBombDispatch is important: the 0x10E1 airborne
// batch producer remains exclusive to rule 2.
func (server *Server) requireCompetitiveKickableBombRule(session *connectionSession, playerID uint16) (*match.CompetitiveBattle, mapdata.CompetitiveRuleKind, error) {
	if server.mapCatalog == nil || session == nil {
		return nil, "", fmt.Errorf("kickable-bomb event requires the client map catalog and a session")
	}
	selected, ok := server.mapCatalog.CompetitiveMap(session.CurrentMapID)
	if !ok || (selected.Rule != mapdata.CompetitiveRuleKickBomb && selected.Rule != mapdata.CompetitiveRuleBox) {
		return nil, "", fmt.Errorf("kickable-bomb event is invalid on competitive map %d", session.CurrentMapID)
	}
	battle, err := server.requireCompetitiveRule(session, selected.Rule, playerID)
	if err != nil {
		return nil, "", err
	}
	return battle, selected.Rule, nil
}

type competitiveHarmTargetKind byte

const (
	competitiveHarmParticipant competitiveHarmTargetKind = iota
	competitiveHarmBoss
	competitiveHarmNativeNPC
)

func (kind competitiveHarmTargetKind) String() string {
	switch kind {
	case competitiveHarmParticipant:
		return "participant"
	case competitiveHarmBoss:
		return "boss"
	case competitiveHarmNativeNPC:
		return "native_npc"
	default:
		return "unknown"
	}
}

type competitiveHarmRoute struct {
	Battle *match.CompetitiveBattle
	Rule   mapdata.CompetitiveRuleKind
	Kind   competitiveHarmTargetKind
}

// requireCompetitiveHarmTarget preserves the two distinct 0x10F4 consumers
// proven in Client.exe. Mechanical/box rules use participant IDs for native
// durability. Rule-1/2 Boss handling instead uses an explicitly advertised
// 30000..30200 object ID and only the elected arbitrator may report it. Tank
// uses a separate base-HP report and is deliberately excluded.
func (server *Server) requireCompetitiveHarmTarget(session *connectionSession, objectID uint16, lossHP uint16) (competitiveHarmRoute, error) {
	if server.mapCatalog == nil || session == nil {
		return competitiveHarmRoute{}, fmt.Errorf("entity-harmed event requires the client map catalog and a session")
	}
	battle, err := server.competitiveBattle(session.CurrentGameID)
	if err != nil {
		return competitiveHarmRoute{}, err
	}
	if battle.HasBossEntity(objectID) {
		if !battle.IsArbitrator(session.Profile.PlayerID) {
			return competitiveHarmRoute{}, fmt.Errorf("Boss-harmed source player %d is not the match arbitrator", session.Profile.PlayerID)
		}
		// The native Boss/controller paths use more than one LossHP encoding
		// (captured values include both 500 and zero). The service does not
		// calculate Boss HP here, so do not invent a scalar allowlist.
		selected, ok := server.mapCatalog.CompetitiveMap(session.CurrentMapID)
		if !ok || (selected.Rule != mapdata.CompetitiveRuleOrdinary && selected.Rule != mapdata.CompetitiveRuleKickBomb) {
			return competitiveHarmRoute{}, fmt.Errorf("Boss-harmed event is invalid on competitive map %d", session.CurrentMapID)
		}
		return competitiveHarmRoute{Battle: battle, Rule: selected.Rule, Kind: competitiveHarmBoss}, nil
	}
	selected, ok := server.mapCatalog.CompetitiveMap(session.CurrentMapID)
	if !ok || (selected.Rule != mapdata.CompetitiveRuleMachine && selected.Rule != mapdata.CompetitiveRuleBox) {
		return competitiveHarmRoute{}, fmt.Errorf("entity-harmed event is invalid on competitive map %d", session.CurrentMapID)
	}
	if battle.HasNativeNPCEntity(objectID) {
		if selected.Rule != mapdata.CompetitiveRuleBox || !battle.IsArbitrator(session.Profile.PlayerID) {
			return competitiveHarmRoute{}, fmt.Errorf("native NPC-harmed object %d is not owned by this rule arbitrator", objectID)
		}
		if lossHP != 0 {
			return competitiveHarmRoute{}, fmt.Errorf("native NPC-harmed loss %d, want rule-8 marker 0", lossHP)
		}
		return competitiveHarmRoute{Battle: battle, Rule: selected.Rule, Kind: competitiveHarmNativeNPC}, nil
	}
	battle, err = server.requireCompetitiveRule(session, selected.Rule, objectID)
	if err != nil {
		return competitiveHarmRoute{}, err
	}
	return competitiveHarmRoute{Battle: battle, Rule: selected.Rule, Kind: competitiveHarmParticipant}, nil
}

// validateCompetitiveGameOverSource enforces the conclusion authority from
// the active battle contract. Special rules and map overlays elect exactly one participant to
// create shared scene state and the final result table; accepting GAME_OVER
// from every peer would allow contradictory settlements to race each other.
func (server *Server) validateCompetitiveGameOverSource(session *connectionSession, battle *match.CompetitiveBattle) error {
	if session == nil || session.Profile.PlayerID == 0 {
		return fmt.Errorf("competitive GAME_OVER requires an identified player")
	}
	if battle == nil {
		return fmt.Errorf("competitive GAME_OVER requires an active battle")
	}
	if battle.RequiresArbitratorConclusion() && !battle.IsArbitrator(session.Profile.PlayerID) {
		return fmt.Errorf("competitive GAME_OVER player %d is not the match arbitrator", session.Profile.PlayerID)
	}
	return nil
}

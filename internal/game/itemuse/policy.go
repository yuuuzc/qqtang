// Package itemuse classifies item-bearing actions without coupling game rules
// to a particular QQTang packet. The legacy client has several unrelated
// instructions that all contain an ItemID; sharing an ID field does not make
// them one generic "use item" operation.
package itemuse

import (
	"fmt"

	"qqtang/internal/game/itemeffect"
)

// Path is the protocol/application path that originated an item action.
// These values intentionally model semantics rather than numeric message IDs.
type Path string

const (
	PathBattleWorldItem  Path = "battle_world_item"
	PathPreparedProp     Path = "prepared_inventory_prop"
	PathRoomInventory    Path = "room_inventory_item"
	PathPetOperation     Path = "pet_operation"
	PathCombine          Path = "combine"
	PathForge            Path = "forge"
	PathMatchRequirement Path = "match_start_requirement"
)

// Context is the authoritative server state in which an action is requested.
type Context string

const (
	ContextLobby       Context = "lobby"
	ContextRoom        Context = "room"
	ContextAdventure   Context = "adventure"
	ContextCompetitive Context = "competitive"
)

// Effect describes the state transition, independently of its wire path.
type Effect string

const (
	EffectWorldTemporary  Effect = "world_temporary"
	EffectPreparedGeneric Effect = "prepared_generic"
	EffectRestoreHealth   Effect = "restore_health"
	EffectReviveTeammate  Effect = "revive_teammate"
	EffectReviveSelf      Effect = "revive_self"
	EffectAdoptPet        Effect = "adopt_pet"
	EffectFeedPet         Effect = "feed_pet"
	EffectLearnPetSkill   Effect = "learn_pet_skill"
	EffectLearnRecipe     Effect = "learn_recipe"
	EffectCombine         Effect = "combine"
	EffectForge           Effect = "forge"
	EffectStartModifier   Effect = "start_modifier"
)

// Consumption defines when durable account inventory may be decremented.
type Consumption string

const (
	// ConsumeNone is required for items spawned inside a match. They are
	// transient battlefield objects and are not rows in player_inventory.
	ConsumeNone Consumption = "none"
	// ConsumeOnAccepted is used by a prepared account prop after membership,
	// ownership, slot and context validation all succeed.
	ConsumeOnAccepted Consumption = "on_accepted"
	// ConsumeAtomic means consumption must share the same repository
	// transaction as the durable mutation (adoption, learning, crafting).
	ConsumeAtomic Consumption = "atomic_with_effect"
	// ConsumeAtMatchStart is for tickets/modifiers checked by start rules. It
	// must not be charged merely because the player entered a room.
	ConsumeAtMatchStart Consumption = "at_match_start"
)

// Canonical client item IDs whose behavior is stated by the installed
// itemCFG/propdescrip data. Unknown effect magnitudes remain client-owned; the
// policy only identifies the effect family and consumption boundary.
const (
	FirstAidKitItemID       uint32 = 20036
	LargeStaminaPotionID    uint32 = 20043
	SmallStaminaPotionID    uint32 = 20044
	MediumStaminaPotionID   uint32 = 20045
	MedicalKitItemID        uint32 = 20050
	SmallSelfReviveCardID   uint32 = 20100
	LargeSelfReviveCardID   uint32 = 20101
	CompetitionTicketItemID uint32 = 2897
)

// Intent contains only facts already established by a protocol adapter or
// catalog. Kind is the itemcatalog classification; it may be empty in focused
// state-machine tests that do not load the client registry.
type Intent struct {
	Path    Path
	Context Context
	ItemID  uint32
	Kind    string
}

// Plan is the normalized action consumed by effect handlers.
type Plan struct {
	Intent
	Effect                  Effect
	Consumption             Consumption
	NeedsTargetConfirmation bool
}

// Resolve rejects path/category conflation before any inventory is mutated.
func Resolve(intent Intent) (Plan, error) {
	if intent.ItemID == 0 {
		return Plan{}, fmt.Errorf("item-use intent requires a non-zero item ID")
	}
	plan := Plan{Intent: intent}
	switch intent.Path {
	case PathBattleWorldItem:
		if intent.Context != ContextAdventure && intent.Context != ContextCompetitive {
			return Plan{}, fmt.Errorf("battlefield item %d cannot be used in %s", intent.ItemID, intent.Context)
		}
		plan.Effect = EffectWorldTemporary
		plan.Consumption = ConsumeNone
		return plan, nil

	case PathPreparedProp:
		var scope itemeffect.Scope
		switch intent.Context {
		case ContextAdventure:
			scope = itemeffect.ScopeAdventure
		case ContextCompetitive:
			scope = itemeffect.ScopeCompetitiveItem
		default:
			return Plan{}, fmt.Errorf("prepared inventory prop %d is not valid in %s", intent.ItemID, intent.Context)
		}
		if intent.Kind != "" && intent.Kind != "inventory-consumable" {
			return Plan{}, fmt.Errorf("prepared inventory prop %d has incompatible kind %q", intent.ItemID, intent.Kind)
		}
		if !itemeffect.AllowsPrepared(intent.ItemID, scope) {
			return Plan{}, fmt.Errorf("prepared inventory prop %d is not registered for %s", intent.ItemID, intent.Context)
		}
		plan.Consumption = ConsumeOnAccepted
		switch intent.ItemID {
		case FirstAidKitItemID, MedicalKitItemID:
			plan.Effect = EffectReviveTeammate
			plan.NeedsTargetConfirmation = true
		case SmallSelfReviveCardID, LargeSelfReviveCardID:
			plan.Effect = EffectReviveSelf
			plan.NeedsTargetConfirmation = true
		case SmallStaminaPotionID, MediumStaminaPotionID, LargeStaminaPotionID:
			plan.Effect = EffectRestoreHealth
		default:
			plan.Effect = EffectPreparedGeneric
		}
		return plan, nil

	case PathRoomInventory:
		if intent.Context != ContextLobby && intent.Context != ContextRoom {
			return Plan{}, fmt.Errorf("room inventory item %d cannot be used in %s", intent.ItemID, intent.Context)
		}
		if intent.Kind != "craft-recipe" {
			return Plan{}, fmt.Errorf("room inventory path has no confirmed handler for item %d kind %q", intent.ItemID, intent.Kind)
		}
		plan.Effect, plan.Consumption = EffectLearnRecipe, ConsumeAtomic
		return plan, nil

	case PathPetOperation:
		if intent.Context != ContextLobby && intent.Context != ContextRoom {
			return Plan{}, fmt.Errorf("pet item %d cannot be used in %s", intent.ItemID, intent.Context)
		}
		switch intent.Kind {
		case "pet-card":
			plan.Effect = EffectAdoptPet
		case "pet-food":
			plan.Effect = EffectFeedPet
		case "pet-skill-book":
			plan.Effect = EffectLearnPetSkill
		default:
			return Plan{}, fmt.Errorf("pet operation path has no handler for item %d kind %q", intent.ItemID, intent.Kind)
		}
		plan.Consumption = ConsumeAtomic
		return plan, nil

	case PathCombine:
		plan.Effect, plan.Consumption = EffectCombine, ConsumeAtomic
		return plan, nil
	case PathForge:
		plan.Effect, plan.Consumption = EffectForge, ConsumeAtomic
		return plan, nil
	case PathMatchRequirement:
		plan.Effect, plan.Consumption = EffectStartModifier, ConsumeAtMatchStart
		return plan, nil
	default:
		return Plan{}, fmt.Errorf("unknown item-use path %q", intent.Path)
	}
}

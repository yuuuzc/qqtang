// Package itemeffect contains the server-facing semantic registry for QQTang
// items whose description promises behavior beyond ordinary ownership or
// appearance.  It deliberately keeps inferred rules separate from protocol
// handlers: appearing in this catalog does not by itself authorize inventory
// consumption or a state mutation.
package itemeffect

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"qqtang/internal/game/itemcatalog"
	"qqtang/internal/protocol/game"
)

type Source string

const (
	SourceAccountItem Source = "account_item"
	SourceEquipment   Source = "equipped_item"
	SourcePetCard     Source = "pet_innate_talent"
	SourcePetSkill    Source = "pet_learned_skill"
)

type Trigger string

const (
	TriggerPreparedUse           Trigger = "prepared_use"
	TriggerPetAdoption           Trigger = "pet_adoption"
	TriggerKinCreation           Trigger = "kin_creation"
	TriggerRoomKick              Trigger = "room_kick"
	TriggerRoomMapSelection      Trigger = "room_map_selection"
	TriggerMatchStart            Trigger = "match_start"
	TriggerCompetitiveSettlement Trigger = "competitive_settlement"
	TriggerChatIdleReward        Trigger = "chat_idle_reward"
	TriggerInventoryOwnership    Trigger = "inventory_ownership"
	TriggerRoleLoadout           Trigger = "role_loadout"
	// TriggerNativeMapAction is evaluated by the original battlefield rule
	// while the matching action is performed.  Selecting a platform item in
	// the shop is durable input to that rule; it is not itself proof that the
	// effect fired and must not create a second persisted match-effect flag.
	TriggerNativeMapAction    Trigger = "native_map_action"
	TriggerSectionBroadcast   Trigger = "section_broadcast"
	TriggerBreakEgg           Trigger = "break_egg"
	TriggerLottery            Trigger = "lottery"
	TriggerMarriageProposal   Trigger = "marriage_proposal"
	TriggerMarriageInfo       Trigger = "marriage_info"
	TriggerRelationshipReward Trigger = "relationship_reward"
)

const (
	PetSlotExpansion10ItemID uint32 = 4334
	PetSlotExpansion20ItemID uint32 = 4335
)

type Scope string

const (
	ScopeAdventure       Scope = "adventure"
	ScopeCompetitiveItem Scope = "competitive_item_room"
	ScopeCompetitive     Scope = "competitive"
	ScopeChat            Scope = "chat_room"
)

type Kind string

const (
	KindPreparedClientEffect     Kind = "prepared_client_effect"
	KindKickProtection           Kind = "kick_protection"
	KindPointsPercent            Kind = "points_percent"
	KindPointsMultiplier         Kind = "points_multiplier"
	KindExperienceMultiplier     Kind = "experience_multiplier"
	KindPetTalent                Kind = "pet_talent"
	KindMatchRequirement         Kind = "match_requirement"
	KindMapAccessOverride        Kind = "map_access_override"
	KindAccountCapacity          Kind = "account_capacity"
	KindKinCreationQualification Kind = "kin_creation_qualification"
	KindRoleClientEffect         Kind = "role_client_effect"
	KindHiddenRoleChance         Kind = "hidden_role_chance"
	KindSectionBroadcast         Kind = "section_broadcast"
	KindLotteryTicket            Kind = "lottery_ticket"
	KindInventoryExchange        Kind = "inventory_exchange"
	KindMarriageProposal         Kind = "marriage_proposal"
	KindMarriageRingProjection   Kind = "marriage_ring_projection"
	KindIntimacyMultiplier       Kind = "intimacy_multiplier"
	KindIntimacyFlat             Kind = "intimacy_flat"
)

type Status string

const (
	StatusImplemented   Status = "implemented"
	StatusCatalogued    Status = "catalogued"
	StatusNeedsEvidence Status = "needs_protocol_evidence"
)

type Rule struct {
	Kind       Kind    `json:"kind"`
	Trigger    Trigger `json:"trigger"`
	Scopes     []Scope `json:"scopes,omitempty"`
	Target     string  `json:"target,omitempty"`
	Percent    uint16  `json:"percent,omitempty"`
	Amount     uint16  `json:"amount,omitempty"`
	Chance     uint16  `json:"chance_percent,omitempty"`
	Multiplier uint16  `json:"multiplier,omitempty"`
	Status     Status  `json:"status"`
	Evidence   string  `json:"evidence"`
}

type Definition struct {
	ItemID      uint32 `json:"item_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Source      Source `json:"source"`
	Rules       []Rule `json:"rules"`
}

// Catalog is an immutable-by-convention runtime index. Lookup returns copies
// so settlement and room policies cannot mutate bootstrap resources.
type Catalog struct {
	byItem map[uint32]Definition
}

func NewCatalog(definitions []Definition) *Catalog {
	catalog := &Catalog{byItem: make(map[uint32]Definition, len(definitions))}
	for _, definition := range definitions {
		copyDefinition := definition
		copyDefinition.Rules = append([]Rule(nil), definition.Rules...)
		catalog.byItem[definition.ItemID] = copyDefinition
	}
	return catalog
}

func (catalog *Catalog) Lookup(itemID uint32) (Definition, bool) {
	if catalog == nil {
		return Definition{}, false
	}
	definition, ok := catalog.byItem[itemID]
	if !ok {
		return Definition{}, false
	}
	definition.Rules = append([]Rule(nil), definition.Rules...)
	return definition, true
}

// PreparedScope is the exact mode contract stated by the installed v848
// itemCFG descriptions.  No-item and treasure rooms are intentionally absent.
// The client owns the moment-to-moment animation/stat effect after 0x1175;
// the server owns projection, validation, fan-out, and durable consumption.
func PreparedScope(itemID uint32) ([]Scope, bool) {
	if (itemID >= 20058 && itemID <= 20064) || itemID == 20008 || itemID == 20009 || itemID == 20020 || itemID == 20046 {
		return []Scope{ScopeAdventure, ScopeCompetitiveItem}, true
	}
	switch itemID {
	case 20003, 20006, 20007, 20010, 20011, 20012, 20013, 20014,
		20015, 20016, 20017, 20018, 20019, 20021, 20022, 20023,
		20024, 20025, 20026:
		return []Scope{ScopeCompetitiveItem}, true
	case 20027, 20028, 20030, 20032, 20033, 20034, 20035, 20036,
		20031, 20037, 20038, 20039, 20040, 20042, 20043, 20044, 20045,
		20047, 20048, 20050, 20100, 20101:
		return []Scope{ScopeAdventure}, true
	default:
		return nil, false
	}
}

func AllowsPrepared(itemID uint32, scope Scope) bool {
	scopes, ok := PreparedScope(itemID)
	if !ok {
		return false
	}
	for _, candidate := range scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

var (
	pointsPercentPattern   = regexp.MustCompile(`(?:所获|获得的)积分增加([0-9]+)%`)
	idleExperiencePattern  = regexp.MustCompile(`获得([0-9]+)倍经验`)
	petLightPattern        = regexp.MustCompile(`(糖币|声望|积分|勇气|材料|亲密)(之光|闪耀)([0-9]+)%`)
	petSkillPercentPattern = regexp.MustCompile(`增加所获得的(糖币|声望|积分|勇气|材料|亲密)([0-9]+)%`)
	petSkillChancePattern  = regexp.MustCompile(`有([0-9]+)%的概率使(?:得)?获得的(糖币|声望|积分|勇气|材料|亲密)翻倍`)
)

func parseUint16(value string) uint16 {
	parsed, _ := strconv.ParseUint(value, 10, 16)
	return uint16(parsed)
}

func Discover(entries []itemcatalog.Entry) []Definition {
	definitions := make([]Definition, 0)
	for _, entry := range entries {
		rules := discoverRules(entry)
		if len(rules) == 0 {
			continue
		}
		definitions = append(definitions, Definition{
			ItemID: entry.ID, Name: entry.Name, Description: entry.Description,
			Source: sourceForEntry(entry), Rules: rules,
		})
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ItemID < definitions[j].ItemID })
	return definitions
}

func discoverRules(entry itemcatalog.Entry) []Rule {
	var rules []Rule
	switch entry.ID {
	case 99:
		rules = append(rules, Rule{
			Kind: KindMatchRequirement, Trigger: TriggerMatchStart, Scopes: []Scope{ScopeAdventure},
			Target: "single_player_adventure", Status: StatusImplemented,
			Evidence: "installed itemCFG plus native client single-player room gate",
		})
	case game.SinglePlayerBossCardItemID:
		rules = append(rules, Rule{
			Kind: KindMatchRequirement, Trigger: TriggerMatchStart, Scopes: []Scope{ScopeCompetitive},
			Target: "single_player_boss", Status: StatusImplemented,
			Evidence: "local itemCFG/commodityCFG entry sharing item 99 artwork, static QQTSection room-gate patch, and authoritative server Boss-candidate validation",
		})
	case 2897:
		rules = append(rules, Rule{
			Kind: KindMatchRequirement, Trigger: TriggerMatchStart, Scopes: []Scope{ScopeCompetitive},
			Target: "treasure_entry_points", Status: StatusNeedsEvidence,
			Evidence: "installed itemCFG; exact charge schedule remains version-dependent",
		})
	case 199:
		rules = append(rules, Rule{
			Kind: KindRoleClientEffect, Trigger: TriggerNativeMapAction, Scopes: []Scope{ScopeCompetitive},
			Target: "bubble_throw_lightning", Status: StatusNeedsEvidence,
			Evidence: "installed itemCFG promises lightning while throwing a bubble; uiShop.py proves one durable role-bound platform selection, but the native battlefield map/action gate has not yet been recovered",
		})
	case 204:
		rules = append(rules, Rule{
			Kind: KindHiddenRoleChance, Trigger: TriggerMatchStart, Scopes: []Scope{ScopeCompetitive},
			Target: "random_role_bun_head", Chance: 50, Status: StatusImplemented,
			Evidence: "installed itemCFG plus server-owned competitive question-mark role resolution; the selected concrete role is carried by the ordinary GAME_BEGIN snapshot",
		})
	case 294:
		rules = append(rules, Rule{
			Kind: KindSectionBroadcast, Trigger: TriggerSectionBroadcast,
			Target: "small_bugle", Status: StatusImplemented,
			Evidence: "typed section-chat selector, per-message identity flag, atomic inventory debit, and section fan-out",
		})
	case 467:
		rules = append(rules, Rule{
			Kind: KindRoleClientEffect, Trigger: TriggerNativeMapAction, Scopes: []Scope{ScopeCompetitive},
			Target: "kick_bomb_02_action_effect", Status: StatusNeedsEvidence,
			Evidence: "installed platform resource 19 is a kick-bomb Boss reward and uiShop.py proves one durable role-bound platform selection; the exact native map/action effect remains unproven",
		})
	case 468:
		rules = append(rules, Rule{
			Kind: KindRoleClientEffect, Trigger: TriggerNativeMapAction, Scopes: []Scope{ScopeCompetitive},
			Target: "kick_bomb_01_action_effect", Status: StatusNeedsEvidence,
			Evidence: "installed platform resource 20 is a kick-bomb Boss reward and uiShop.py proves one durable role-bound platform selection; the exact native map/action effect remains unproven and no numeric force bonus is applied server-side",
		})
	case 4237, 4238:
		target := "bubble_cosmetic_lottery"
		if entry.ID == 4238 {
			target = "wing_cosmetic_lottery"
		}
		rules = append(rules, Rule{
			Kind: KindLotteryTicket, Trigger: TriggerLottery, Target: target,
			Status:   StatusNeedsEvidence,
			Evidence: "installed itemCFG identifies a lottery ticket; it is distinct from commodity 999 fruit-machine flow and its original reward protocol/pool is not proven",
		})
	case 9001, 9002, 9003, 9004, 9011, 9012, 9013:
		rules = append(rules, Rule{
			Kind: KindInventoryExchange, Trigger: TriggerBreakEgg,
			Target: "egg_and_hammer_reward", Status: StatusImplemented,
			Evidence: "typed REQUEST_BREAK_EGG/RESPONSE_BREAK_EGG, atomic egg+hammer debit and reward grant; reward policy is an editable local reconstruction",
		})
	case 9020:
		rules = append(rules, Rule{
			Kind: KindMarriageProposal, Trigger: TriggerMarriageProposal,
			Target: "proposal_qualification", Status: StatusImplemented,
			Evidence: "typed spark request/answer flow plus atomic proposal-item debit on acceptance",
		})
	case 9021, 9022, 9023, 9024, 9025, 9026:
		bonus := uint16(entry.ID - 9020)
		if entry.ID == 9026 {
			bonus = 10
		}
		rules = append(rules,
			Rule{
				Kind: KindMarriageRingProjection, Trigger: TriggerMarriageInfo,
				Target: "ring_asset", Status: StatusImplemented,
				Evidence: "marriage inventory synchronization projects item 9021..9026 directly to the client's official ring9021..ring9026 assets",
			},
			Rule{
				Kind: KindIntimacyFlat, Trigger: TriggerRelationshipReward,
				Target: "intimacy", Amount: bonus, Status: StatusNeedsEvidence,
				Evidence: "installed itemCFG states the flat intimacy bonus; the authoritative intimacy reward transaction is not yet proven",
			},
		)
	case 4061:
		rules = append(rules, Rule{
			Kind: KindIntimacyMultiplier, Trigger: TriggerRelationshipReward,
			Target: "intimacy", Multiplier: 2, Status: StatusNeedsEvidence,
			Evidence: "installed itemCFG promises double intimacy; the authoritative intimacy reward transaction is not yet proven",
		})
	case 1077:
		rules = append(rules, Rule{
			Kind: KindKinCreationQualification, Trigger: TriggerKinCreation,
			Target: "ordinary_alliance_book", Multiplier: 100, Status: StatusImplemented,
			Evidence: "installed itemCFG creation requirement plus atomic kin creation validation",
		})
	case 2235:
		rules = append(rules, Rule{
			Kind: KindKinCreationQualification, Trigger: TriggerKinCreation,
			Target: "super_alliance_book", Multiplier: 1, Status: StatusImplemented,
			Evidence: "installed itemCFG creation requirement plus atomic kin creation validation",
		})
	case 402, 403, 431, 433, 435, 436, 437, 453, 460, 464, 469:
		rules = append(rules, Rule{
			Kind: KindMatchRequirement, Trigger: TriggerMatchStart, Scopes: []Scope{ScopeCompetitive},
			Target: "boss_summon", Status: StatusImplemented,
			Evidence: "installed itemCFG joined to the typed competitive Boss activation catalog",
		})
	case PetSlotExpansion10ItemID, PetSlotExpansion20ItemID:
		rules = append(rules, Rule{
			Kind: KindAccountCapacity, Trigger: TriggerPetAdoption,
			Target: "pet_slots", Status: StatusImplemented,
			Evidence: "installed itemCFG capacity description plus PET_INFO 5+20 wire capacity",
		})
	}
	if scopes, ok := PreparedScope(entry.ID); ok {
		rules = append(rules, Rule{
			Kind: KindPreparedClientEffect, Trigger: TriggerPreparedUse, Scopes: scopes,
			Target: entry.Name, Status: StatusImplemented,
			Evidence: "installed itemCFG description plus REQUEST/NOTIFY_PREPARED_USE_PROP",
		})
	}
	if entry.ID == 100 {
		rules = append(rules, Rule{
			Kind: KindKickProtection, Trigger: TriggerRoomKick, Status: StatusImplemented,
			Evidence: "installed itemCFG plus typed REQUEST_KICKOFF_PLAYER room policy",
		})
	}
	if entry.ID == 121 {
		rules = append(rules, Rule{
			Kind: KindPointsMultiplier, Trigger: TriggerCompetitiveSettlement,
			Scopes: []Scope{ScopeCompetitive}, Target: "win_points", Multiplier: 2,
			Status:   StatusImplemented,
			Evidence: "installed itemCFG: winning reward points are doubled",
		})
		rules = append(rules, Rule{
			Kind: KindMapAccessOverride, Trigger: TriggerRoomMapSelection,
			Scopes: []Scope{ScopeCompetitive}, Target: "bypass_points_requirement",
			Status:   StatusImplemented,
			Evidence: "installed itemCFG plus v848 mapDesc.py required-points field",
		})
	}
	for _, match := range pointsPercentPattern.FindAllStringSubmatch(entry.Description, -1) {
		rules = append(rules, Rule{
			Kind: KindPointsPercent, Trigger: TriggerCompetitiveSettlement,
			Scopes: []Scope{ScopeCompetitive}, Target: "points", Percent: parseUint16(match[1]),
			Status: StatusImplemented, Evidence: "installed itemCFG percentage description",
		})
	}
	for _, match := range idleExperiencePattern.FindAllStringSubmatch(entry.Description, -1) {
		rules = append(rules, Rule{
			Kind: KindExperienceMultiplier, Trigger: TriggerChatIdleReward,
			Scopes: []Scope{ScopeChat}, Target: "competitive_experience", Multiplier: parseUint16(match[1]),
			Status: StatusNeedsEvidence, Evidence: "installed itemCFG description; idle reward protocol not yet proven",
		})
	}
	for _, match := range petLightPattern.FindAllStringSubmatch(entry.Description, -1) {
		kind := "deterministic_percent"
		chance := uint16(0)
		status := StatusCatalogued
		if petRewardTargetImplemented(match[1]) {
			status = StatusImplemented
		}
		if match[2] == "闪耀" {
			kind = "chance_double"
			chance = parseUint16(match[3])
		}
		rules = append(rules, Rule{
			Kind: KindPetTalent, Trigger: TriggerCompetitiveSettlement, Target: match[1] + ":" + kind,
			Percent: parseUint16(match[3]), Chance: chance, Status: status,
			Evidence: "installed pet-card talent description",
		})
	}
	for _, match := range petSkillPercentPattern.FindAllStringSubmatch(entry.Description, -1) {
		status := StatusCatalogued
		if petRewardTargetImplemented(match[1]) {
			status = StatusImplemented
		}
		rules = append(rules, Rule{
			Kind: KindPetTalent, Trigger: TriggerCompetitiveSettlement,
			Target: match[1] + ":deterministic_percent", Percent: parseUint16(match[2]),
			Status: status, Evidence: "installed learned pet-skill description",
		})
	}
	for _, match := range petSkillChancePattern.FindAllStringSubmatch(entry.Description, -1) {
		status := StatusCatalogued
		if petRewardTargetImplemented(match[2]) {
			status = StatusImplemented
		}
		rules = append(rules, Rule{
			Kind: KindPetTalent, Trigger: TriggerCompetitiveSettlement,
			Target: match[2] + ":chance_double", Chance: parseUint16(match[1]), Multiplier: 2,
			Status: status, Evidence: "installed learned pet-skill description",
		})
	}
	return rules
}

func petRewardTargetImplemented(target string) bool {
	switch target {
	case "积分", "糖币", "勇气", "材料":
		return true
	default:
		return false
	}
}

func sourceForEntry(entry itemcatalog.Entry) Source {
	if entry.ID == PetSlotExpansion10ItemID || entry.ID == PetSlotExpansion20ItemID {
		return SourceAccountItem
	}
	switch entry.Kind {
	case "pet-card":
		return SourcePetCard
	case "pet-skill-book":
		return SourcePetSkill
	}
	for _, category := range entry.Categories {
		switch strings.ToLower(category) {
		case "cap", "hair", "eye", "mouth", "ear", "cladorn", "thadorn", "fpack", "npack", "bomb", "huanying", "footprint", "bg", "frame", "enter", "namecard":
			return SourceEquipment
		}
	}
	return SourceAccountItem
}

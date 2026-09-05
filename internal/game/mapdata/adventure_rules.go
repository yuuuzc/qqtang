package mapdata

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
)

const adventureRulesSchemaVersion = 5

// AdventureRules is server-owned static PVE data. The encrypted client
// battlefield files remain authoritative evidence for map composition, while
// this JSON provides the values the retired official game server used to own.
type AdventureRules struct {
	SchemaVersion   uint32                  `json:"schema_version"`
	ReferencePolicy string                  `json:"reference_policy"`
	SourceFiles     []string                `json:"source_files"`
	NPCDropPools    []AdventureNPCDropPool  `json:"npc_drop_pools"`
	WallDropPools   []AdventureWallDropPool `json:"wall_drop_pools"`
	Maps            []AdventureStageRule    `json:"maps"`

	byMap map[uint32]AdventureStageRule
}

// AdventureStageRule counts NPC instances, not unique NPC type IDs. A stage
// may legitimately contain several instances with the same object type.
type AdventureStageRule struct {
	MapID             uint32                  `json:"map_id"`
	ExpectedNPCDeaths uint32                  `json:"expected_npc_deaths"`
	WallDropPoolID    string                  `json:"wall_drop_pool"`
	WallDropRolls     byte                    `json:"-"`
	WallDropChance    byte                    `json:"-"`
	WallItems         []AdventureNPCDropItem  `json:"-"`
	NPCDropGroups     []AdventureNPCDropGroup `json:"npc_drop_groups"`
}

// AdventureNPCDropGroup is one PVEBOSS_INFO entry sent by the retired official
// server before the client constructs the stage NPCs. BossCount counts actual
// instances of one battlefield NPC base ID, including repeated monsters.
type AdventureNPCDropGroup struct {
	BossID         uint16                      `json:"boss_id"`
	BossCount      byte                        `json:"boss_count"`
	CombatProfiles []AdventureNPCCombatProfile `json:"combat_profiles"`
	DropPoolID     string                      `json:"drop_pool"`
	DropCount      byte                        `json:"-"`
	DropChance     byte                        `json:"-"`
	NormalItems    []AdventureNPCDropItem      `json:"-"`
}

// AdventureNPCCombatProfile preserves the number of indistinguishable map
// instances that share one client-side combat configuration. The death event
// identifies only BossID, so a group with multiple profiles awards their
// count-weighted mean courage per reported death.
type AdventureNPCCombatProfile struct {
	Count byte `json:"count"`
	AdventureNPCCombat
}

func (group AdventureNPCDropGroup) Courage() uint32 {
	if group.BossCount == 0 {
		return 0
	}
	var total uint64
	for _, profile := range group.CombatProfiles {
		total += uint64(profile.Courage()) * uint64(profile.Count)
	}
	average := (total + uint64(group.BossCount)/2) / uint64(group.BossCount)
	if average > uint64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(average)
}

// AdventureNPCCombat is the client battlefield's authoritative per-type
// combat profile. Damage is the NPCInfo Harm field. Courage is derived from
// these values and is deliberately not duplicated in JSON.
type AdventureNPCCombat struct {
	Health  uint32 `json:"health"`
	Damage  uint32 `json:"damage"`
	Defense uint32 `json:"defense"`
	Speed   uint32 `json:"speed"`
	IsBoss  bool   `json:"is_boss"`
}

// Courage returns round(ln(health)*ln(damage)*(1+defense/100)*speed/10),
// doubled for a Boss. Harmless or stationary profiles yield zero instead of
// feeding log(0) or a negative value into settlement arithmetic.
func (combat AdventureNPCCombat) Courage() uint32 {
	if combat.Health == 0 || combat.Damage <= 1 || combat.Speed == 0 {
		return 0
	}
	value := math.Log(float64(combat.Health)) * math.Log(float64(combat.Damage))
	value *= 1 + float64(combat.Defense)/100
	value *= float64(combat.Speed) / 10
	if combat.IsBoss {
		value *= 2
	}
	if math.IsNaN(value) || value <= 0 {
		return 0
	}
	if math.IsInf(value, 1) || value >= float64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(math.Round(value))
}

// AdventureNPCDropPool is a reusable server-owned candidate inventory.
// DropCount is the maximum number selected by the server for one NPC type in
// a match. The adventure client then chooses a non-empty 1..N subset from a
// populated NPC inventory when that NPC dies.
type AdventureNPCDropPool struct {
	ID          string                 `json:"id"`
	Enabled     bool                   `json:"enabled"`
	DropCount   byte                   `json:"max_drop_count"`
	DropChance  byte                   `json:"drop_chance_percent"`
	Confidence  string                 `json:"confidence"`
	Notes       string                 `json:"notes,omitempty"`
	NormalItems []AdventureNPCDropItem `json:"normal_items"`
}

// AdventureWallDropPool is intentionally separate from NPC inventories.
// The public historical material does not contain an authoritative per-wall
// table, so a match performs at most Rolls independent local-policy rolls and
// never reuses a monster pool as a hidden-wall pool.
type AdventureWallDropPool struct {
	ID         string                 `json:"id"`
	Rolls      byte                   `json:"rolls"`
	DropChance byte                   `json:"drop_chance_percent"`
	Confidence string                 `json:"confidence"`
	Notes      string                 `json:"notes,omitempty"`
	Items      []AdventureNPCDropItem `json:"items"`
}

// AdventureNPCDropItem mirrors BOSS_ITEM_INFO. The client copies ItemID and
// ItemCount into each matching NPC's drop inventory. DropTime is retained as a
// named protocol field even though this client build does not consult it when
// constructing that inventory.
type AdventureNPCDropItem struct {
	ItemID    uint32 `json:"item_id"`
	Quantity  uint16 `json:"quantity"`
	DropTime  uint16 `json:"drop_time"`
	Weight    uint16 `json:"weight,omitempty"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Reference string `json:"reference,omitempty"`
}

func LoadAdventureRules(path string) (*AdventureRules, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("adventure rules path is empty")
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read adventure rules: %w", err)
	}
	var rules AdventureRules
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rules); err != nil {
		return nil, fmt.Errorf("decode adventure rules: %w", err)
	}
	if rules.SchemaVersion != adventureRulesSchemaVersion {
		return nil, fmt.Errorf("adventure rules schema_version %d, want %d", rules.SchemaVersion, adventureRulesSchemaVersion)
	}
	if strings.TrimSpace(rules.ReferencePolicy) == "" || len(rules.SourceFiles) == 0 {
		return nil, fmt.Errorf("adventure rules need reference_policy and source_files")
	}
	dropPools := make(map[string]AdventureNPCDropPool, len(rules.NPCDropPools))
	for poolIndex, pool := range rules.NPCDropPools {
		pool.ID = strings.TrimSpace(pool.ID)
		if pool.ID == "" {
			return nil, fmt.Errorf("adventure rules npc_drop_pools[%d] has an empty id", poolIndex)
		}
		if _, duplicate := dropPools[pool.ID]; duplicate {
			return nil, fmt.Errorf("adventure rules repeats drop pool %q", pool.ID)
		}
		if strings.TrimSpace(pool.Confidence) == "" {
			return nil, fmt.Errorf("adventure rules drop pool %q needs confidence", pool.ID)
		}
		if pool.Enabled {
			if err := validateAdventureDropItems(fmt.Sprintf("drop pool %q", pool.ID), pool.NormalItems); err != nil {
				return nil, err
			}
			if pool.DropCount == 0 || int(pool.DropCount) > len(pool.NormalItems) {
				return nil, fmt.Errorf("adventure rules drop pool %q max_drop_count %d is outside 1..%d", pool.ID, pool.DropCount, len(pool.NormalItems))
			}
			if pool.DropChance == 0 || pool.DropChance > 100 {
				return nil, fmt.Errorf("adventure rules drop pool %q drop_chance_percent %d is outside 1..100", pool.ID, pool.DropChance)
			}
		} else if pool.DropCount != 0 || pool.DropChance != 0 || len(pool.NormalItems) != 0 {
			return nil, fmt.Errorf("disabled adventure drop pool %q must have zero count/chance and no items", pool.ID)
		}
		dropPools[pool.ID] = pool
		rules.NPCDropPools[poolIndex] = pool
	}
	if len(dropPools) == 0 {
		return nil, fmt.Errorf("adventure rules contains no NPC drop pools")
	}
	wallPools := make(map[string]AdventureWallDropPool, len(rules.WallDropPools))
	for poolIndex, pool := range rules.WallDropPools {
		pool.ID = strings.TrimSpace(pool.ID)
		if pool.ID == "" {
			return nil, fmt.Errorf("adventure rules wall_drop_pools[%d] has an empty id", poolIndex)
		}
		if _, duplicate := wallPools[pool.ID]; duplicate {
			return nil, fmt.Errorf("adventure rules repeats wall drop pool %q", pool.ID)
		}
		if pool.Rolls == 0 || pool.Rolls > 5 {
			return nil, fmt.Errorf("adventure rules wall pool %q rolls %d is outside 1..5", pool.ID, pool.Rolls)
		}
		if pool.DropChance == 0 || pool.DropChance > 100 {
			return nil, fmt.Errorf("adventure rules wall pool %q drop_chance_percent %d is outside 1..100", pool.ID, pool.DropChance)
		}
		if strings.TrimSpace(pool.Confidence) == "" {
			return nil, fmt.Errorf("adventure rules wall pool %q needs confidence", pool.ID)
		}
		if err := validateAdventureDropItems(fmt.Sprintf("wall pool %q", pool.ID), pool.Items); err != nil {
			return nil, err
		}
		for itemIndex, item := range pool.Items {
			if strings.TrimSpace(item.Kind) != "material" {
				return nil, fmt.Errorf("adventure rules wall pool %q item[%d] kind %q is not material", pool.ID, itemIndex, item.Kind)
			}
		}
		wallPools[pool.ID] = pool
		rules.WallDropPools[poolIndex] = pool
	}
	if len(wallPools) == 0 {
		return nil, fmt.Errorf("adventure rules contains no wall drop pools")
	}
	rules.byMap = make(map[uint32]AdventureStageRule, len(rules.Maps))
	for index, stage := range rules.Maps {
		if stage.MapID == 0 || stage.ExpectedNPCDeaths == 0 {
			return nil, fmt.Errorf("adventure rules maps[%d] needs non-zero map_id and expected_npc_deaths", index)
		}
		if _, duplicate := rules.byMap[stage.MapID]; duplicate {
			return nil, fmt.Errorf("adventure rules has duplicate map_id %d", stage.MapID)
		}
		stage.WallDropPoolID = strings.TrimSpace(stage.WallDropPoolID)
		wallPool, exists := wallPools[stage.WallDropPoolID]
		if !exists {
			return nil, fmt.Errorf("adventure rules map %d references invalid wall drop pool %q", stage.MapID, stage.WallDropPoolID)
		}
		stage.WallDropRolls = wallPool.Rolls
		stage.WallDropChance = wallPool.DropChance
		stage.WallItems = append([]AdventureNPCDropItem(nil), wallPool.Items...)
		seenBosses := make(map[uint16]struct{}, len(stage.NPCDropGroups))
		var instanceCount uint32
		for groupIndex, group := range stage.NPCDropGroups {
			if group.BossID == 0 || group.BossCount == 0 {
				return nil, fmt.Errorf("adventure rules map %d npc_drop_groups[%d] needs non-zero boss_id and boss_count", stage.MapID, groupIndex)
			}
			var combatInstances uint32
			if len(group.CombatProfiles) == 0 {
				return nil, fmt.Errorf("adventure rules map %d boss %d needs combat profiles", stage.MapID, group.BossID)
			}
			for profileIndex, profile := range group.CombatProfiles {
				if profile.Count == 0 || profile.Health == 0 {
					return nil, fmt.Errorf("adventure rules map %d boss %d combat_profiles[%d] needs non-zero count and health", stage.MapID, group.BossID, profileIndex)
				}
				combatInstances += uint32(profile.Count)
			}
			if combatInstances != uint32(group.BossCount) {
				return nil, fmt.Errorf("adventure rules map %d boss %d combat profile instances %d, want boss_count %d", stage.MapID, group.BossID, combatInstances, group.BossCount)
			}
			if _, duplicate := seenBosses[group.BossID]; duplicate {
				return nil, fmt.Errorf("adventure rules map %d repeats boss ID %d", stage.MapID, group.BossID)
			}
			seenBosses[group.BossID] = struct{}{}
			instanceCount += uint32(group.BossCount)
			group.DropPoolID = strings.TrimSpace(group.DropPoolID)
			pool, exists := dropPools[group.DropPoolID]
			if !exists {
				return nil, fmt.Errorf("adventure rules map %d boss %d references unknown drop pool %q", stage.MapID, group.BossID, group.DropPoolID)
			}
			if pool.Enabled {
				group.DropCount = pool.DropCount
				group.DropChance = pool.DropChance
				group.NormalItems = append([]AdventureNPCDropItem(nil), pool.NormalItems...)
			}
			stage.NPCDropGroups[groupIndex] = group
		}
		if instanceCount != stage.ExpectedNPCDeaths {
			return nil, fmt.Errorf("adventure rules map %d NPC group instances %d, want expected_npc_deaths %d", stage.MapID, instanceCount, stage.ExpectedNPCDeaths)
		}
		rules.byMap[stage.MapID] = stage
		rules.Maps[index] = stage
	}
	if len(rules.byMap) == 0 {
		return nil, fmt.Errorf("adventure rules contains no maps")
	}
	return &rules, nil
}

func validateAdventureDropItems(context string, items []AdventureNPCDropItem) error {
	if len(items) == 0 || len(items) > 10 {
		return fmt.Errorf("adventure rules %s normal item count %d is outside 1..10", context, len(items))
	}
	seenItems := make(map[uint32]struct{}, len(items))
	for itemIndex, item := range items {
		if item.ItemID == 0 || item.Quantity == 0 {
			return fmt.Errorf("adventure rules %s normal_items[%d] needs non-zero item_id and quantity", context, itemIndex)
		}
		if item.Weight == 0 {
			return fmt.Errorf("adventure rules %s normal_items[%d] needs non-zero weight", context, itemIndex)
		}
		if _, duplicate := seenItems[item.ItemID]; duplicate {
			return fmt.Errorf("adventure rules %s repeats item ID %d", context, item.ItemID)
		}
		seenItems[item.ItemID] = struct{}{}
	}
	return nil
}

func (rules *AdventureRules) Stage(mapID uint32) (AdventureStageRule, bool) {
	if rules == nil {
		return AdventureStageRule{}, false
	}
	stage, ok := rules.byMap[mapID]
	return stage, ok
}

func (stage AdventureStageRule) NPCDropGroup(npcID uint16) (AdventureNPCDropGroup, bool) {
	for _, group := range stage.NPCDropGroups {
		if group.BossID == npcID {
			return group, true
		}
	}
	return AdventureNPCDropGroup{}, false
}

func (rules *AdventureRules) DropItemIDs() []uint32 {
	if rules == nil {
		return nil
	}
	unique := make(map[uint32]struct{})
	for _, stage := range rules.byMap {
		for _, item := range stage.WallItems {
			unique[item.ItemID] = struct{}{}
		}
		for _, group := range stage.NPCDropGroups {
			for _, item := range group.NormalItems {
				unique[item.ItemID] = struct{}{}
			}
		}
	}
	ids := make([]uint32, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

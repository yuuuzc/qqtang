package mapdata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdventureRulesResolveSharedDropPool(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	data := []byte(`{
  "schema_version": 5,
  "reference_policy": "test",
  "source_files": ["testdata"],
  "npc_drop_pools": [{"id":"materials","enabled":true,"max_drop_count":1,"drop_chance_percent":38,"confidence":"test","normal_items":[{"item_id":30044,"quantity":1,"drop_time":0,"weight":1,"kind":"material"}]}],
  "wall_drop_pools": [{"id":"walls","rolls":5,"drop_chance_percent":35,"confidence":"test","items":[{"item_id":30044,"quantity":1,"drop_time":0,"weight":1,"kind":"material"}]}],
  "maps": [{"map_id":1649,"expected_npc_deaths":2,"wall_drop_pool":"walls","npc_drop_groups":[{"boss_id":30101,"boss_count":2,"combat_profiles":[{"count":2,"health":300,"damage":10,"defense":50,"speed":3,"is_boss":false}],"drop_pool":"materials"}]}]
}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := LoadAdventureRules(path)
	if err != nil {
		t.Fatal(err)
	}
	stage, ok := rules.Stage(1649)
	if !ok || len(stage.NPCDropGroups) != 1 || len(stage.NPCDropGroups[0].NormalItems) != 1 {
		t.Fatalf("resolved stage = %+v, found=%v", stage, ok)
	}
	if stage.NPCDropGroups[0].NormalItems[0].ItemID != 30044 {
		t.Fatalf("resolved drop items = %+v", stage.NPCDropGroups[0].NormalItems)
	}
	if stage.WallDropRolls != 5 || stage.WallDropChance != 35 || len(stage.WallItems) != 1 || stage.WallItems[0].ItemID != 30044 {
		t.Fatalf("resolved wall drop pool = %d/%d/%+v", stage.WallDropRolls, stage.WallDropChance, stage.WallItems)
	}
}

func TestAdventureNPCGroupCourageUsesWeightedMeanForSharedID(t *testing.T) {
	group := AdventureNPCDropGroup{BossCount: 3, CombatProfiles: []AdventureNPCCombatProfile{
		{Count: 2, AdventureNPCCombat: AdventureNPCCombat{Health: 300, Damage: 10, Defense: 50, Speed: 3}},
		{Count: 1, AdventureNPCCombat: AdventureNPCCombat{Health: 300, Damage: 10, Defense: 100, Speed: 3}},
	}}
	if got := group.Courage(); got != 7 {
		t.Fatalf("weighted group courage = %d, want round((6*2+8)/3) = 7", got)
	}
}

func TestAdventureNPCCombatCourageFormula(t *testing.T) {
	ordinary := AdventureNPCCombat{Health: 300, Damage: 10, Defense: 50, Speed: 3}
	if got := ordinary.Courage(); got != 6 {
		t.Fatalf("ordinary courage = %d, want round(ln(300)*ln(10)*1.5*0.3) = 6", got)
	}
	boss := ordinary
	boss.IsBoss = true
	if got := boss.Courage(); got != 12 {
		t.Fatalf("Boss courage = %d, want 12", got)
	}
	for _, harmless := range []AdventureNPCCombat{
		{Health: 300, Damage: 0, Speed: 3},
		{Health: 300, Damage: 1, Speed: 3},
		{Health: 300, Damage: 10, Speed: 0},
	} {
		if got := harmless.Courage(); got != 0 {
			t.Fatalf("boundary combat %+v courage = %d, want 0", harmless, got)
		}
	}
}

func TestProjectAdventureRulesCoverInstalledBattlefields(t *testing.T) {
	path := filepath.Join("..", "..", "..", "configs", "adventure-rules.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("project adventure rules are not present")
	}
	rules, err := LoadAdventureRules(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules.Maps) != 49 {
		t.Fatalf("generated adventure stage count = %d, want 49", len(rules.Maps))
	}
	if len(rules.NPCDropPools) != 24 || len(rules.WallDropPools) != 12 {
		t.Fatalf("generated adventure pool counts = %d/%d, want 24/12", len(rules.NPCDropPools), len(rules.WallDropPools))
	}
	for _, pool := range rules.NPCDropPools {
		if !pool.Enabled {
			t.Fatalf("project drop pool %q must provide an explicit or local-fallback policy", pool.ID)
		}
		if pool.DropCount == 0 || pool.DropCount > 3 || pool.DropChance == 0 || len(pool.NormalItems) == 0 {
			t.Fatalf("enabled drop pool %q candidates/count/chance = %d/%d/%d", pool.ID, len(pool.NormalItems), pool.DropCount, pool.DropChance)
		}
		for _, item := range pool.NormalItems {
			if item.Weight == 0 {
				t.Fatalf("drop pool %q has zero-weight item %+v", pool.ID, item)
			}
		}
	}
	for _, pool := range rules.WallDropPools {
		if pool.Rolls != 5 || pool.DropChance != 35 || len(pool.Items) != 3 {
			t.Fatalf("wall pool %q rolls/chance/items = %d/%d/%d, want 5/35/3", pool.ID, pool.Rolls, pool.DropChance, len(pool.Items))
		}
	}
	var instances uint32
	for _, stage := range rules.Maps {
		instances += stage.ExpectedNPCDeaths
		if stage.WallDropRolls != 5 || stage.WallDropChance != 35 || len(stage.WallItems) != 3 {
			t.Fatalf("map %d wall pool = %d/%d/%d, want 5/35/3", stage.MapID, stage.WallDropRolls, stage.WallDropChance, len(stage.WallItems))
		}
		for _, group := range stage.NPCDropGroups {
			if len(group.NormalItems) == 0 || group.DropCount == 0 || group.DropCount > 3 || group.DropChance == 0 {
				t.Fatalf("map %d NPC %d resolved candidates/count/chance = %d/%d/%d", stage.MapID, group.BossID, len(group.NormalItems), group.DropCount, group.DropChance)
			}
		}
	}
	if instances != 467 {
		t.Fatalf("generated adventure NPC instance count = %d, want 467", instances)
	}
}

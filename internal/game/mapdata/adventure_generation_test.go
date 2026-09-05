package mapdata

import "testing"

func TestSelectNPCDropInventoryUsesWeightsWithoutReplacement(t *testing.T) {
	stage := AdventureStageRule{MapID: 1601}
	group := AdventureNPCDropGroup{
		BossID: 30001, DropCount: 3, DropChance: 100,
		NormalItems: []AdventureNPCDropItem{
			{ItemID: 30001, Quantity: 1, Weight: 100},
			{ItemID: 30002, Quantity: 1, Weight: 10},
			{ItemID: 28001, Quantity: 1, Weight: 1},
			{ItemID: 28002, Quantity: 1, Weight: 1},
		},
	}
	first := stage.SelectNPCDropInventory(group, 0x12345678)
	second := stage.SelectNPCDropInventory(group, 0x12345678)
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("weighted selection lengths = %d/%d, want 3/3", len(first), len(second))
	}
	seen := make(map[uint32]struct{}, len(first))
	for index := range first {
		if first[index].ItemID != second[index].ItemID {
			t.Fatalf("weighted selection is not deterministic: %+v versus %+v", first, second)
		}
		if _, duplicate := seen[first[index].ItemID]; duplicate {
			t.Fatalf("weighted selection repeated item %d", first[index].ItemID)
		}
		seen[first[index].ItemID] = struct{}{}
	}
}

func TestSelectWallItemInventoryUsesOnlyConfiguredMaterials(t *testing.T) {
	stage := AdventureStageRule{
		MapID: 1649, WallDropRolls: 5, WallDropChance: 35,
		WallItems: []AdventureNPCDropItem{
			{ItemID: 30043, Quantity: 1, Kind: "material"},
			{ItemID: 30044, Quantity: 1, Kind: "material"},
			{ItemID: 30094, Quantity: 1, Kind: "material"},
			{ItemID: 20043, Quantity: 1, Kind: "consumable"},
		},
	}
	items, err := stage.SelectWallItemInventory(0x12345678)
	if err != nil {
		t.Fatal(err)
	}
	var total uint32
	for _, item := range items {
		if item.ItemID != 30043 && item.ItemID != 30044 && item.ItemID != 30094 {
			t.Fatalf("unexpected wall item %+v", item)
		}
		total += item.Quantity
	}
	if total > 5 {
		t.Fatalf("expanded wall item count = %d, want 0..5", total)
	}
}

func TestSelectWallItemInventoryReturnsIndependentCopy(t *testing.T) {
	stage := AdventureStageRule{
		MapID: 1, WallDropRolls: 1, WallDropChance: 100,
		WallItems: []AdventureNPCDropItem{{ItemID: 30043, Quantity: 1, Kind: "material"}},
	}
	first, err := stage.SelectWallItemInventory(1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := stage.SelectWallItemInventory(1)
	if err != nil {
		t.Fatal(err)
	}
	first[0].Quantity = 99
	if second[0].Quantity != 1 {
		t.Fatalf("second selection was mutated: %+v", second)
	}
}

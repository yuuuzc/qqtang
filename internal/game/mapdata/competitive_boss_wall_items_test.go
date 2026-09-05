package mapdata

import "testing"

func TestEnhanceCompetitiveBossWallItemsTransfersEachBaseCategoryAndAddsFork(t *testing.T) {
	input := []CompetitiveWallItem{
		{SceneID: 1, Quantity: 4},
		{SceneID: 2, Quantity: 3},
		{SceneID: 3, Quantity: 2},
		{SceneID: 104, Quantity: 2},
	}
	got := EnhanceCompetitiveBossWallItems(input, 3, 0x12345678)
	quantities := competitiveWallItemQuantities(got)

	// ceil(3/2) = 2 from every represented base category.
	if quantities[1] != 2 || quantities[2] != 1 || quantities[3] != 0 ||
		quantities[6] != 2 || quantities[7] != 2 || quantities[8] != 2 {
		t.Fatalf("Boss upgrades = %+v", quantities)
	}
	if quantities[24] != 1 {
		t.Fatalf("fork quantity = %d, want 1: %+v", quantities[24], quantities)
	}
	if competitiveWallItemTotal(got) != competitiveWallItemTotal(input) {
		t.Fatalf("total changed: before=%d after=%d", competitiveWallItemTotal(input), competitiveWallItemTotal(got))
	}
	if input[0].Quantity != 4 {
		t.Fatalf("input was mutated: %+v", input)
	}
}

func TestEnhanceCompetitiveBossWallItemsBoundsUpgradeByActualRoll(t *testing.T) {
	input := []CompetitiveWallItem{{SceneID: 1, Quantity: 1}, {SceneID: 2, Quantity: 2}, {SceneID: 24, Quantity: 1}}
	got := EnhanceCompetitiveBossWallItems(input, 8, 1)
	quantities := competitiveWallItemQuantities(got)
	if quantities[1] != 0 || quantities[2] != 0 || quantities[6] != 1 || quantities[7] != 2 {
		t.Fatalf("bounded Boss upgrades = %+v", quantities)
	}
	if quantities[24] != 1 {
		t.Fatalf("existing fork was changed: %+v", quantities)
	}
	if competitiveWallItemTotal(got) != competitiveWallItemTotal(input) {
		t.Fatalf("total changed: before=%d after=%d", competitiveWallItemTotal(input), competitiveWallItemTotal(got))
	}
}

func TestEnhanceCompetitiveBossWallItemsCountsExistingUpperItemsTowardFloor(t *testing.T) {
	input := []CompetitiveWallItem{
		{SceneID: 1, Quantity: 5},
		{SceneID: 6, Quantity: 2},
		{SceneID: 24, Quantity: 1},
	}
	got := EnhanceCompetitiveBossWallItems(input, 5, 1)
	quantities := competitiveWallItemQuantities(got)
	// ceil(5/2) = 3, so the two existing spike bubbles require only one
	// matching base replacement instead of three more replacements.
	if quantities[1] != 4 || quantities[6] != 3 {
		t.Fatalf("bounded existing Boss upgrades = %+v", quantities)
	}
	if competitiveWallItemTotal(got) != competitiveWallItemTotal(input) {
		t.Fatalf("total changed: before=%d after=%d", competitiveWallItemTotal(input), competitiveWallItemTotal(got))
	}
}

func TestEnhanceCompetitiveBossWallItemsDoesNotInventCellsForEmptyPool(t *testing.T) {
	if got := EnhanceCompetitiveBossWallItems(nil, 2, 1); got != nil {
		t.Fatalf("empty pool = %+v, want nil", got)
	}
}

func TestEnhanceCompetitiveOrdinaryWallItemsFillsQuarterFloorFromMatchingBase(t *testing.T) {
	input := []CompetitiveWallItem{
		{SceneID: 1, Quantity: 4},
		{SceneID: 2, Quantity: 1},
		{SceneID: 3, Quantity: 4},
		{SceneID: 6, Quantity: 1},
		{SceneID: 104, Quantity: 2},
	}
	rules := []CompetitiveWallItemRule{
		{SceneID: 1, Maximum: 8, Probability: 1},
		{SceneID: 2, Maximum: 8, Probability: 0.5},
		{SceneID: 3, Maximum: 8, Probability: 0},
		{SceneID: 104, Maximum: 2, Probability: 1},
	}
	got := EnhanceCompetitiveOrdinaryWallItems(input, rules, 5)
	quantities := competitiveWallItemQuantities(got)

	// ceil(5/4) = 2. The existing spike bubble leaves a deficit of one;
	// power can transfer only its one actually rolled base item; speed is
	// ineligible because its map probability is zero.
	if quantities[1] != 3 || quantities[6] != 2 ||
		quantities[2] != 0 || quantities[7] != 1 ||
		quantities[3] != 4 || quantities[8] != 0 {
		t.Fatalf("ordinary upgrades = %+v", quantities)
	}
	if quantities[24] != 0 {
		t.Fatalf("ordinary round invented a fork: %+v", quantities)
	}
	if competitiveWallItemTotal(got) != competitiveWallItemTotal(input) {
		t.Fatalf("total changed: before=%d after=%d", competitiveWallItemTotal(input), competitiveWallItemTotal(got))
	}
	if input[0].Quantity != 4 {
		t.Fatalf("input was mutated: %+v", input)
	}
}

func TestEnhanceCompetitiveOrdinaryWallItemsKeepsSatisfiedUpperFloor(t *testing.T) {
	input := []CompetitiveWallItem{{SceneID: 1, Quantity: 4}, {SceneID: 6, Quantity: 3}}
	rules := []CompetitiveWallItemRule{{SceneID: 1, Maximum: 8, Probability: 1}}
	got := EnhanceCompetitiveOrdinaryWallItems(input, rules, 8)
	quantities := competitiveWallItemQuantities(got)
	if quantities[1] != 4 || quantities[6] != 3 {
		t.Fatalf("satisfied ordinary floor changed = %+v", quantities)
	}
}

func TestEnhanceCompetitiveOrdinaryWallItemsDoesNotInventCellsForEmptyPool(t *testing.T) {
	if got := EnhanceCompetitiveOrdinaryWallItems(nil, []CompetitiveWallItemRule{{SceneID: 1, Probability: 1}}, 5); got != nil {
		t.Fatalf("empty ordinary pool = %+v, want nil", got)
	}
}

func competitiveWallItemQuantities(items []CompetitiveWallItem) map[uint32]int {
	result := make(map[uint32]int)
	for _, item := range items {
		result[item.SceneID] += int(item.Quantity)
	}
	return result
}

func competitiveWallItemTotal(items []CompetitiveWallItem) int {
	total := 0
	for _, item := range items {
		total += int(item.Quantity)
	}
	return total
}

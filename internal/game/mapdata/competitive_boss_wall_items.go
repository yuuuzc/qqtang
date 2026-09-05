package mapdata

// Native scene IDs recovered from the installed client's common rule-1
// pickup handlers. The upper item follows the same attribute path as its base
// item and sets that attribute to the native maximum.
const (
	competitiveBaseBubbleSceneID uint32 = 1
	competitiveBasePowerSceneID  uint32 = 2
	competitiveBaseSpeedSceneID  uint32 = 3

	competitiveMaxBubbleSceneID uint32 = 6
	competitiveMaxPowerSceneID  uint32 = 7
	competitiveMaxSpeedSceneID  uint32 = 8

	competitiveForkSceneID uint32 = 24
)

// RollFinalCompetitiveBossWallItems is the single authoritative Boss wall
// item entry point. Callers must not reproduce the base-roll plus accessibility
// overlay sequence independently.
func (entry CompetitiveMap) RollFinalCompetitiveBossWallItems(itemSeed uint32, participantCount, capacity, baseCapacity int) []CompetitiveWallItem {
	return EnhanceCompetitiveBossWallItems(
		entry.RollWallItems(itemSeed, participantCount, capacity, baseCapacity),
		participantCount,
		itemSeed,
	)
}

// RollFinalCompetitiveOrdinaryWallItems is the single authoritative ordinary
// competitive wall-item entry point shared by live match start, offline
// training and generated evaluation rounds.
func (entry CompetitiveMap) RollFinalCompetitiveOrdinaryWallItems(itemSeed uint32, participantCount, capacity, baseCapacity int) []CompetitiveWallItem {
	return EnhanceCompetitiveOrdinaryWallItems(
		entry.RollWallItems(itemSeed, participantCount, capacity, baseCapacity),
		entry.WallItemRules,
		participantCount,
	)
}

// EnhanceCompetitiveBossWallItems overlays the local Boss accessibility rule
// on the already-rolled ordinary wall pool. For every represented base
// attribute it fills the maximum-strength counterpart to ceil(players/2).
// Existing upper copies count toward that floor, and the remaining transfer is
// bounded by the matching base item's actual roll. It also conservatively
// turns one existing wall item into a fork when the pool has none. Quantities
// are transferred rather than added, so native hidden-cell capacity is
// preserved.
//
// Objective carriers are deliberately not accepted here: callers must apply
// this overlay before merging rule-owned treasure/sculpture/tank items.
func EnhanceCompetitiveBossWallItems(items []CompetitiveWallItem, participantCount int, itemSeed uint32) []CompetitiveWallItem {
	if len(items) == 0 {
		return nil
	}
	if participantCount < 1 {
		participantCount = 1
	}

	quantities := make(map[uint32]int, len(items)+4)
	order := make([]uint32, 0, len(items)+4)
	remember := func(sceneID uint32) {
		if _, exists := quantities[sceneID]; !exists {
			order = append(order, sceneID)
		}
	}
	for _, item := range items {
		if item.SceneID == 0 || item.Quantity <= 0 {
			continue
		}
		remember(item.SceneID)
		quantities[item.SceneID] += int(item.Quantity)
	}
	if len(order) == 0 {
		return nil
	}

	upgradeFloor := ceilingPositiveRatio(participantCount, 2)
	for _, pair := range [][2]uint32{
		{competitiveBaseBubbleSceneID, competitiveMaxBubbleSceneID},
		{competitiveBasePowerSceneID, competitiveMaxPowerSceneID},
		{competitiveBaseSpeedSceneID, competitiveMaxSpeedSceneID},
	} {
		transfer := upgradeFloor - quantities[pair[1]]
		if transfer <= 0 {
			continue
		}
		if quantities[pair[0]] < transfer {
			transfer = quantities[pair[0]]
		}
		if transfer <= 0 {
			continue
		}
		quantities[pair[0]] -= transfer
		remember(pair[1])
		quantities[pair[1]] += transfer
	}

	if quantities[competitiveForkSceneID] == 0 {
		candidates := make([]uint32, 0, len(order))
		for _, sceneID := range order {
			if sceneID != competitiveForkSceneID && quantities[sceneID] > 0 {
				candidates = append(candidates, sceneID)
			}
		}
		if len(candidates) != 0 {
			state := itemSeed ^ 0xB055F04B
			state ^= state << 13
			state ^= state >> 17
			state ^= state << 5
			donor := candidates[int(state%uint32(len(candidates)))]
			quantities[donor]--
			remember(competitiveForkSceneID)
			quantities[competitiveForkSceneID] = 1
		}
	}

	result := make([]CompetitiveWallItem, 0, len(order))
	for _, sceneID := range order {
		quantity := quantities[sceneID]
		if quantity <= 0 {
			continue
		}
		result = append(result, CompetitiveWallItem{SceneID: sceneID, Quantity: int16(quantity)})
	}
	return result
}

// EnhanceCompetitiveOrdinaryWallItems applies the local non-Boss progression
// floor after the map's ordinary wall-item roll is complete. Every base
// attribute whose map rule has a non-zero probability receives up to
// ceil(players/4) copies of its maximum-strength counterpart. Existing upper
// copies count toward that floor, and only the remaining deficit is transferred
// from the matching base item. The transfer is therefore bounded by the actual
// rolled base quantity and never creates another hidden-item cell.
//
// This is deliberately separate from EnhanceCompetitiveBossWallItems: Boss
// rounds retain their older ceil(players/2) accessibility rule and fork
// guarantee, while ordinary rounds do not receive a fork.
func EnhanceCompetitiveOrdinaryWallItems(items []CompetitiveWallItem, rules []CompetitiveWallItemRule, participantCount int) []CompetitiveWallItem {
	if len(items) == 0 {
		return nil
	}
	if participantCount < 1 {
		participantCount = 1
	}

	quantities := make(map[uint32]int, len(items)+3)
	order := make([]uint32, 0, len(items)+3)
	remember := func(sceneID uint32) {
		if _, exists := quantities[sceneID]; !exists {
			order = append(order, sceneID)
		}
	}
	for _, item := range items {
		if item.SceneID == 0 || item.Quantity <= 0 {
			continue
		}
		remember(item.SceneID)
		quantities[item.SceneID] += int(item.Quantity)
	}
	if len(order) == 0 {
		return nil
	}

	eligibleBase := make(map[uint32]bool, 3)
	for _, rule := range rules {
		if rule.SceneID >= competitiveBaseBubbleSceneID && rule.SceneID <= competitiveBaseSpeedSceneID && rule.Probability > 0 {
			eligibleBase[rule.SceneID] = true
		}
	}
	upgradeFloor := ceilingPositiveRatio(participantCount, 4)
	for _, pair := range [][2]uint32{
		{competitiveBaseBubbleSceneID, competitiveMaxBubbleSceneID},
		{competitiveBasePowerSceneID, competitiveMaxPowerSceneID},
		{competitiveBaseSpeedSceneID, competitiveMaxSpeedSceneID},
	} {
		if !eligibleBase[pair[0]] {
			continue
		}
		deficit := upgradeFloor - quantities[pair[1]]
		if deficit <= 0 {
			continue
		}
		transfer := deficit
		if quantities[pair[0]] < transfer {
			transfer = quantities[pair[0]]
		}
		if transfer <= 0 {
			continue
		}
		quantities[pair[0]] -= transfer
		remember(pair[1])
		quantities[pair[1]] += transfer
	}

	result := make([]CompetitiveWallItem, 0, len(order))
	for _, sceneID := range order {
		quantity := quantities[sceneID]
		if quantity <= 0 {
			continue
		}
		result = append(result, CompetitiveWallItem{SceneID: sceneID, Quantity: int16(quantity)})
	}
	return result
}

// ceilingPositiveRatio avoids the numerator addition used by
// (value+denominator-1)/denominator, so even an unexpectedly large caller
// value cannot overflow before the replacement is bounded by actual items.
func ceilingPositiveRatio(value, denominator int) int {
	if value <= 0 || denominator <= 0 {
		return 0
	}
	result := value / denominator
	if value%denominator != 0 {
		result++
	}
	return result
}

package mapdata

import "fmt"

const (
	adventureDropMapMix       = uint32(0x9E3779B9)
	adventureDropNPCMix       = uint32(0x85EBCA6B)
	adventureDropWallMix      = uint32(0xC2B2AE35)
	adventureDropFallbackSeed = uint32(0xA341316C)
)

// AdventureWallItem is a protocol-independent hidden-wall selection. The
// legacy wire adapter is responsible for checking its narrower quantity type
// and converting it to GAME_ITEM_TYPE.
type AdventureWallItem struct {
	ItemID   uint32
	Quantity uint32
}

// SelectNPCDropInventory performs the server-owned deterministic weighted
// sample for one NPC type. The returned inventory is installed on every NPC
// instance in the group; the original client later chooses the visible subset
// when an instance is damaged or dies.
func (stage AdventureStageRule) SelectNPCDropInventory(group AdventureNPCDropGroup, itemSeed uint32) []AdventureNPCDropItem {
	candidates := append([]AdventureNPCDropItem(nil), group.NormalItems...)
	count := int(group.DropCount)
	if count > len(candidates) {
		count = len(candidates)
	}
	if count <= 0 {
		return nil
	}
	state := itemSeed ^ stage.MapID*adventureDropMapMix ^ uint32(group.BossID)*adventureDropNPCMix
	if state == 0 {
		state = adventureDropFallbackSeed
	}
	// Avalanche the map/seed/NPC tuple before the probability check. Using the
	// raw xor/multiply value made adjacent NPC IDs visibly correlated.
	state = mixAdventureDropState(state)
	if state%100 >= uint32(group.DropChance) {
		return nil
	}
	selected := make([]AdventureNPCDropItem, 0, count)
	for len(selected) < count && len(candidates) > 0 {
		var totalWeight uint32
		for _, candidate := range candidates {
			totalWeight += uint32(candidate.Weight)
		}
		if totalWeight == 0 {
			break
		}
		state = mixAdventureDropState(state + uint32(len(selected)+1)*adventureDropNPCMix)
		draw := state % totalWeight
		chosen := 0
		for index, candidate := range candidates {
			weight := uint32(candidate.Weight)
			if draw < weight {
				chosen = index
				break
			}
			draw -= weight
		}
		selected = append(selected, candidates[chosen])
		candidates = append(candidates[:chosen], candidates[chosen+1:]...)
	}
	return selected
}

// SelectWallItemInventory performs the route-specific hidden-wall rolls. It
// only knows static map policy; client scene-element support is deliberately a
// wire-adapter concern.
func (stage AdventureStageRule) SelectWallItemInventory(itemSeed uint32) ([]AdventureWallItem, error) {
	rolls := int(stage.WallDropRolls)
	if rolls == 0 {
		return nil, nil
	}
	if rolls > 5 || stage.WallDropChance == 0 || stage.WallDropChance > 100 {
		return nil, fmt.Errorf("map %d wall roll policy %d/%d is invalid", stage.MapID, rolls, stage.WallDropChance)
	}
	candidates := make([]AdventureNPCDropItem, 0, len(stage.WallItems))
	for _, item := range stage.WallItems {
		if item.Kind == "material" {
			candidates = append(candidates, item)
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("map %d has no wall material candidates", stage.MapID)
	}

	state := itemSeed ^ stage.MapID*adventureDropMapMix ^ adventureDropWallMix
	if state == 0 {
		state = adventureDropFallbackSeed
	}
	quantities := make(map[uint32]uint32, len(candidates))
	for index := 0; index < rolls; index++ {
		state = mixAdventureDropState(state + uint32(index+1)*adventureDropWallMix)
		if state%100 >= uint32(stage.WallDropChance) {
			continue
		}
		state = mixAdventureDropState(state + adventureDropNPCMix)
		selected := candidates[int(state%uint32(len(candidates)))]
		quantities[selected.ItemID] += uint32(selected.Quantity)
	}

	items := make([]AdventureWallItem, 0, len(quantities))
	for _, candidate := range candidates {
		if quantity := quantities[candidate.ItemID]; quantity > 0 {
			items = append(items, AdventureWallItem{ItemID: candidate.ItemID, Quantity: quantity})
		}
	}
	return items, nil
}

func mixAdventureDropState(state uint32) uint32 {
	state ^= state >> 16
	state *= 0x7FEB352D
	state ^= state >> 15
	state *= 0x846CA68B
	state ^= state >> 16
	return state
}

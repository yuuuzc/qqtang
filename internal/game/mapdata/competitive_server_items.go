package mapdata

import (
	"sort"

	"qqtang/internal/clientdata/sceneelement"
)

const competitiveTankWallItemDonorMapID uint32 = 1201

// restoreCompetitiveWallItemRules runs after every non-PVE client map has been
// parsed, so an empty no-item-field table can be compared with the complete
// installed competitive catalog. A recovery donor must be an original
// embedded table with the same presentation family and native rule. Item-field
// maps retain their own proven contact-pickup table when present. An item field
// whose table becomes empty is recovered separately from an original embedded
// table with the same family and native rule, and the action-slot filter is
// applied again. This leaves all-empty special families (for example airborne
// kick-bomb maps) untouched instead of inventing drops for another mechanic.
func (catalog *Catalog) restoreCompetitiveWallItemRules() {
	if catalog == nil {
		return
	}
	mapIDs := make([]uint32, 0, len(catalog.competitiveMaps))
	for mapID := range catalog.competitiveMaps {
		mapIDs = append(mapIDs, mapID)
	}
	sort.Slice(mapIDs, func(i, j int) bool { return mapIDs[i] < mapIDs[j] })
	for _, mapID := range mapIDs {
		entry := catalog.competitiveMaps[mapID]
		if entry.RequiredItemField != 0 || len(entry.WallItemRules) != 0 {
			continue
		}
		var donor CompetitiveMap
		var donorDistance uint32
		for _, candidateID := range mapIDs {
			candidate := catalog.competitiveMaps[candidateID]
			if candidate.RequiredItemField != 0 || candidate.WallItemSource != CompetitiveWallItemsEmbedded ||
				candidate.Family != entry.Family || candidate.NativeRule != entry.NativeRule {
				continue
			}
			distance := mapIDDistance(mapID, candidateID)
			if donor.ID == 0 || distance < donorDistance || (distance == donorDistance && candidateID < donor.ID) {
				donor, donorDistance = candidate, distance
			}
		}
		if donor.ID == 0 {
			continue
		}
		entry.WallItemRules = append([]CompetitiveWallItemRule(nil), donor.WallItemRules...)
		entry.WallItemSource = CompetitiveWallItemsFamilyRecovery
		entry.WallItemDonorMapID = donor.ID
		catalog.competitiveMaps[mapID] = entry
	}
	catalog.restoreCompetitiveItemFieldWallItemRules(mapIDs)
	catalog.restoreCompetitiveTankWallItemRules()
}

func (catalog *Catalog) restoreCompetitiveItemFieldWallItemRules(mapIDs []uint32) {
	for _, mapID := range mapIDs {
		entry := catalog.competitiveMaps[mapID]
		if entry.RequiredItemField == 0 || len(entry.WallItemRules) != 0 {
			continue
		}
		var donor CompetitiveMap
		var donorRules []CompetitiveWallItemRule
		var donorDistance uint32
		for _, candidateID := range mapIDs {
			candidate := catalog.competitiveMaps[candidateID]
			if candidate.RequiredItemField != 0 || candidate.WallItemSource != CompetitiveWallItemsEmbedded ||
				candidate.Family != entry.Family || candidate.NativeRule != entry.NativeRule {
				continue
			}
			filtered := filterItemFieldWallItemRules(candidate.WallItemRules)
			if len(filtered) == 0 {
				continue
			}
			distance := mapIDDistance(mapID, candidateID)
			if donor.ID == 0 || distance < donorDistance || (distance == donorDistance && candidateID < donor.ID) {
				donor, donorRules, donorDistance = candidate, filtered, distance
			}
		}
		if donor.ID == 0 {
			continue
		}
		entry.WallItemRules = append([]CompetitiveWallItemRule(nil), donorRules...)
		entry.WallItemSource = CompetitiveWallItemsItemFieldRecovery
		entry.WallItemDonorMapID = donor.ID
		catalog.competitiveMaps[mapID] = entry
	}
}

// restoreCompetitiveTankWallItemRules is an explicit cross-family recovery,
// not another nearest-map heuristic. The installed tank01..08 files all have
// an empty rule-13 table, while original gameplay has both ordinary and
// rule-specific objects inside destructible walls. Hero Legend 01 is the
// concrete installed donor: it uses the same GAME_BEGIN wall pool mechanism,
// has an ordinary no-item-field pool, and contains no transformation object.
// Keep the transformation filter as an invariant so later resource changes
// cannot silently add a forbidden tank transformation.
func (catalog *Catalog) restoreCompetitiveTankWallItemRules() {
	donor, ok := catalog.competitiveMaps[competitiveTankWallItemDonorMapID]
	if !ok || donor.RequiredItemField != 0 || donor.WallItemSource != CompetitiveWallItemsEmbedded || len(donor.WallItemRules) == 0 {
		return
	}
	rules := make([]CompetitiveWallItemRule, 0, len(donor.WallItemRules))
	for _, rule := range donor.WallItemRules {
		if sceneelement.IsTransformationPickup(sceneelement.ID(rule.SceneID)) {
			continue
		}
		rules = append(rules, rule)
	}
	if len(rules) == 0 {
		return
	}
	for mapID, entry := range catalog.competitiveMaps {
		if entry.Rule != CompetitiveRuleTank || entry.RequiredItemField != 0 || len(entry.WallItemRules) != 0 {
			continue
		}
		entry.WallItemRules = append([]CompetitiveWallItemRule(nil), rules...)
		entry.WallItemSource = CompetitiveWallItemsExplicitDonor
		entry.WallItemDonorMapID = donor.ID
		catalog.competitiveMaps[mapID] = entry
	}
}

func mapIDDistance(left, right uint32) uint32 {
	if left >= right {
		return left - right
	}
	return right - left
}

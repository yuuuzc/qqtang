package functionitem

import (
	"sort"

	"qqtang/internal/game/itemcatalog"
	"qqtang/internal/game/itemeffect"
)

type Class string

const (
	ClassOwnershipPassive Class = "ownership_passive"
	ClassRoleClientEffect Class = "role_enabled_client_effect"
	ClassActiveOperation  Class = "active_operation"
	ClassMatchPrepared    Class = "prepared_match_prop"
	ClassQualification    Class = "qualification"
	ClassRelationship     Class = "relationship"
	ClassMaterial         Class = "material_or_task"
)

type CatalogEntry struct {
	ItemID      uint32            `json:"item_id"`
	ResourceID  uint32            `json:"resource_id"`
	Category    string            `json:"category"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Class       Class             `json:"class"`
	Status      itemeffect.Status `json:"status"`
	Rules       []itemeffect.Rule `json:"rules,omitempty"`
}

// BuildCatalog extracts the original shop's complete “功能道具” surface.
// The client groups platform/item/card under that one tab but gives each
// category one independent active slot. Classification describes the server
// trigger; it never changes the shop category or invents an item effect.
func BuildCatalog(entries []itemcatalog.Entry, effects *itemeffect.Catalog) []CatalogEntry {
	result := make([]CatalogEntry, 0)
	for _, entry := range entries {
		if !isFunctionCategory(entry.RegistryCategory) {
			continue
		}
		definition, hasRules := effects.Lookup(entry.ID)
		status := itemeffect.StatusCatalogued
		var rules []itemeffect.Rule
		if hasRules {
			rules = append([]itemeffect.Rule(nil), definition.Rules...)
			status = combinedStatus(rules)
		}
		result = append(result, CatalogEntry{
			ItemID: entry.ID, ResourceID: entry.Index, Category: entry.RegistryCategory,
			Name: entry.Name, Description: entry.Description, Class: classify(entry.ID),
			Status: status, Rules: rules,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Category != result[j].Category {
			return result[i].Category < result[j].Category
		}
		return result[i].ItemID < result[j].ItemID
	})
	return result
}

func isFunctionCategory(category string) bool {
	return category == "platform" || category == "item" || category == "card"
}

func classify(itemID uint32) Class {
	if _, ok := itemeffect.PreparedScope(itemID); ok {
		return ClassMatchPrepared
	}
	switch itemID {
	case 99, 402, 403, 435, 436, 437, 453, 460, 464, 469, 1077, 2235, 2897:
		return ClassQualification
	case 204:
		return ClassOwnershipPassive
	case 199, 467, 468:
		return ClassRoleClientEffect
	case 294, 4237, 4238, 9001, 9002, 9003, 9004, 9011, 9012, 9013:
		return ClassActiveOperation
	case 9020, 9021, 9022, 9023, 9024, 9025, 9026, 4061:
		return ClassRelationship
	case 100, 121, 4052, 4055, 4058:
		return ClassOwnershipPassive
	default:
		return ClassMaterial
	}
}

func combinedStatus(rules []itemeffect.Rule) itemeffect.Status {
	if len(rules) == 0 {
		return itemeffect.StatusCatalogued
	}
	allImplemented := true
	needsEvidence := false
	for _, rule := range rules {
		if rule.Status != itemeffect.StatusImplemented {
			allImplemented = false
		}
		if rule.Status == itemeffect.StatusNeedsEvidence {
			needsEvidence = true
		}
	}
	if allImplemented {
		return itemeffect.StatusImplemented
	}
	if needsEvidence {
		return itemeffect.StatusNeedsEvidence
	}
	return itemeffect.StatusCatalogued
}

package functionitem

import (
	"testing"

	"qqtang/internal/game/itemcatalog"
	"qqtang/internal/game/itemeffect"
)

func TestBuildCatalogPreservesShopCategoryAndClassifiesTrigger(t *testing.T) {
	entries := []itemcatalog.Entry{
		{ID: 199, Index: 18, RegistryCategory: "platform", Name: "闪电卡"},
		{ID: 20043, RegistryCategory: "item", Name: "大体力药水"},
		{ID: 9021, RegistryCategory: "card", Name: "水晶戒指"},
		{ID: 22, RegistryCategory: "cap", Name: "帽子"},
	}
	effects := itemeffect.NewCatalog(itemeffect.Discover(entries))
	got := BuildCatalog(entries, effects)
	if len(got) != 3 {
		t.Fatalf("functional catalog = %+v", got)
	}
	if got[0].Category != "card" || got[0].Class != ClassRelationship || got[1].Category != "item" || got[1].Class != ClassMatchPrepared || got[2].Category != "platform" || got[2].Class != ClassRoleClientEffect {
		t.Fatalf("functional classifications = %+v", got)
	}
}

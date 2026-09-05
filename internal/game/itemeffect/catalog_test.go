package itemeffect

import (
	"testing"

	"qqtang/internal/game/itemcatalog"
)

func TestPreparedScopesFollowInstalledDescriptions(t *testing.T) {
	for _, id := range []uint32{20008, 20009, 20020, 20046, 20058, 20064} {
		if !AllowsPrepared(id, ScopeAdventure) || !AllowsPrepared(id, ScopeCompetitiveItem) {
			t.Fatalf("dual-mode item %d has wrong scope", id)
		}
	}
	if AllowsPrepared(20003, ScopeAdventure) || !AllowsPrepared(20003, ScopeCompetitiveItem) {
		t.Fatal("competitive-only prepared item has wrong scope")
	}
	if !AllowsPrepared(20043, ScopeAdventure) || AllowsPrepared(20043, ScopeCompetitiveItem) {
		t.Fatal("adventure potion escaped its mode")
	}
	if !AllowsPrepared(20031, ScopeAdventure) {
		t.Fatal("adventure attack trap is missing from prepared scope")
	}
	if _, ok := PreparedScope(99); ok {
		t.Fatal("single-adventure ticket is a start requirement, not a quickbar prop")
	}
}

func TestDiscoverSeparatesDeterministicAndChancePetEffects(t *testing.T) {
	entries := []itemcatalog.Entry{
		{ID: 100, Name: "防踢卡", Description: "可以享受和至尊紫钻一样的防踢特权。"},
		{ID: 3913, Name: "五叶草糖泡", Categories: []string{"bomb"}, Description: "携带此道具，所获积分增加5%。"},
		{ID: 27051, Name: "积分多多1级", Kind: "pet-skill-book", Description: "有3%的概率使得获得的积分翻倍"},
	}
	got := Discover(entries)
	if len(got) != 3 || got[0].Rules[0].Kind != KindKickProtection || got[1].Rules[0].Percent != 5 {
		t.Fatalf("discovered rules = %+v", got)
	}
	last := got[2].Rules[0]
	if last.Target != "积分:chance_double" || last.Chance != 3 || last.Multiplier != 2 {
		t.Fatalf("chance rule = %+v", last)
	}
}

func TestPetSlotExpansionIsAnImplementedAccountCapacity(t *testing.T) {
	got := Discover([]itemcatalog.Entry{{
		ID: PetSlotExpansion20ItemID, Name: "20格宠物栏", Kind: "pet-card",
		Description: "拥有20格宠物栏，可以同时召唤20个宠物。",
	}})
	if len(got) != 1 || got[0].Source != SourceAccountItem || len(got[0].Rules) != 1 {
		t.Fatalf("pet slot definition = %+v", got)
	}
	rule := got[0].Rules[0]
	if rule.Kind != KindAccountCapacity || rule.Trigger != TriggerPetAdoption || rule.Status != StatusImplemented {
		t.Fatalf("pet slot rule = %+v", rule)
	}
}

func TestKinCreationQualificationUsesOriginalBookCounts(t *testing.T) {
	got := Discover([]itemcatalog.Entry{
		{ID: 1077, Name: "盟约书", Description: "拥有100份盟约书才能创建家族。"},
		{ID: 2235, Name: "超级盟约书", Description: "拥有1份超级盟约书就能创建家族。"},
	})
	if len(got) != 2 || got[0].Rules[0].Multiplier != 100 || got[1].Rules[0].Multiplier != 1 {
		t.Fatalf("kin creation rules = %+v", got)
	}
	for _, definition := range got {
		if definition.Rules[0].Kind != KindKinCreationQualification || definition.Rules[0].Status != StatusImplemented {
			t.Fatalf("kin creation rule = %+v", definition.Rules[0])
		}
	}
}

func TestFunctionalItemsSeparateImplementedTransportFromUnprovenRules(t *testing.T) {
	got := NewCatalog(Discover([]itemcatalog.Entry{
		{ID: 199, Name: "闪电卡", RegistryCategory: "platform"},
		{ID: 204, Name: "包子卡", RegistryCategory: "platform"},
		{ID: 9001, Name: "铁蛋", RegistryCategory: "platform"},
		{ID: 9021, Name: "水晶戒指", RegistryCategory: "card"},
	}))
	lightning, ok := got.Lookup(199)
	if !ok || lightning.Rules[0].Kind != KindRoleClientEffect || lightning.Rules[0].Trigger != TriggerNativeMapAction || lightning.Rules[0].Status != StatusNeedsEvidence {
		t.Fatalf("lightning rule = %+v", lightning)
	}
	bun, _ := got.Lookup(204)
	if bun.Rules[0].Trigger != TriggerMatchStart || bun.Rules[0].Status != StatusImplemented || len(bun.Rules[0].Scopes) != 1 || bun.Rules[0].Scopes[0] != ScopeCompetitive {
		t.Fatalf("bun rule = %+v", bun)
	}
	egg, _ := got.Lookup(9001)
	if egg.Rules[0].Trigger != TriggerBreakEgg || egg.Rules[0].Status != StatusImplemented {
		t.Fatalf("egg rule = %+v", egg)
	}
	ring, _ := got.Lookup(9021)
	if len(ring.Rules) != 2 || ring.Rules[0].Status != StatusImplemented || ring.Rules[1].Amount != 1 || ring.Rules[1].Status != StatusNeedsEvidence {
		t.Fatalf("ring rules = %+v", ring.Rules)
	}
	eternal := NewCatalog(Discover([]itemcatalog.Entry{{ID: 9026, Name: "永恒爱神之戒", RegistryCategory: "card"}}))
	eternalRing, _ := eternal.Lookup(9026)
	if len(eternalRing.Rules) != 2 || eternalRing.Rules[0].Status != StatusImplemented || eternalRing.Rules[1].Amount != 10 {
		t.Fatalf("eternal ring rules = %+v", eternalRing.Rules)
	}
}

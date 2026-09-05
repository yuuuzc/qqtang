package probe

import (
	"testing"

	"qqtang/internal/game/itemcatalog"
	"qqtang/internal/game/itemeffect"
	"qqtang/internal/protocol/game"
)

func TestCompetitivePointEffectsRequireEquippedMatchingRole(t *testing.T) {
	entries := []itemcatalog.Entry{
		{ID: 121, Name: "VIP卡", Description: "获胜奖励积分加倍。"},
		{ID: 3913, Name: "五叶草糖泡", Categories: []string{"bomb"}, Description: "携带此道具，所获积分增加5%。"},
		{ID: 3931, Name: "太阳之光", Categories: []string{"thadorn"}, Description: "携带此道具，所获积分增加1%。"},
		{ID: 3932, Name: "月亮之光", Categories: []string{"thadorn"}, Description: "携带此道具，所获积分增加7%。"},
	}
	server := &Server{itemEffectCatalog: itemeffect.NewCatalog(itemeffect.Discover(entries))}
	profile := game.DefaultPlayerProfile()
	profile.Inventory = []game.ItemInfo{
		{ItemID: 121, NumOfItem: 1, AvailPeriod: game.LocalPermanentAvailablePeriod},
		{ItemID: 3913, NumOfItem: 1, AvailPeriod: game.LocalPermanentAvailablePeriod, ItemStatus: 1, ItemRoleID: 7},
		{ItemID: 3931, NumOfItem: 1, AvailPeriod: game.LocalPermanentAvailablePeriod, ItemStatus: 1, ItemRoleID: 8},
		{ItemID: 3932, NumOfItem: 1, AvailPeriod: game.LocalPermanentAvailablePeriod, ItemStatus: 1},
	}
	got := server.applyCompetitiveItemPointEffects(profile, 7, game.GameResultWin, 100)
	if got.EquipmentPercent != 5 || got.WinMultiplier != 2 || got.FinalPoints != 210 {
		t.Fatalf("point effects = %+v", got)
	}
	draw := server.applyCompetitiveItemPointEffects(profile, 7, game.GameResultDraw, 100)
	if draw.FinalPoints != 105 || draw.WinMultiplier != 1 {
		t.Fatalf("draw effects = %+v", draw)
	}
}

func TestPetRewardBonusesCoverAuthoritativeCurrencies(t *testing.T) {
	entries := []itemcatalog.Entry{
		{ID: 27001, Kind: "pet-skill-book", Description: "增加所获得的糖币5%。"},
		{ID: 27011, Kind: "pet-skill-book", Description: "增加所获得的勇气3%。"},
		{ID: 27041, Kind: "pet-skill-book", Description: "增加所获得的材料2%。"},
		{ID: 27071, Kind: "pet-skill-book", Description: "有100%的概率使得获得的材料翻倍。"},
	}
	server := &Server{itemEffectCatalog: itemeffect.NewCatalog(itemeffect.Discover(entries))}
	if percent, chance := server.petRewardBonuses([]uint16{27001}, "糖币"); percent != 5 || chance != 0 {
		t.Fatalf("money bonuses = %d/%d", percent, chance)
	}
	if percent, chance := server.petRewardBonuses([]uint16{27011}, "勇气"); percent != 3 || chance != 0 {
		t.Fatalf("courage bonuses = %d/%d", percent, chance)
	}
	if percent, chance := server.petRewardBonuses([]uint16{27041, 27071}, "材料"); percent != 2 || chance != 100 {
		t.Fatalf("material bonuses = %d/%d", percent, chance)
	}
	effects := server.applyPetRewardEffects(&connectionSession{UIN: 1, CurrentGameID: 2}, []uint16{27041, 27071}, 3, "材料", 100, 30067)
	if effects.FinalValue != 204 || !effects.DoubleApplied {
		t.Fatalf("material reward effects = %+v", effects)
	}
}

func TestKickProtectionComesFromIdentityOrOwnedCard(t *testing.T) {
	server := &Server{}
	profile := game.DefaultPlayerProfile()
	if server.profileHasKickProtection(profile) {
		t.Fatal("ordinary profile unexpectedly protected")
	}
	profile.Identity = uint32(game.IdentityPurpleDiamond)
	if !server.profileHasKickProtection(profile) {
		t.Fatal("purple-diamond identity not protected")
	}
	profile.Identity = 0
	profile.Inventory = []game.ItemInfo{game.NewPermanentItemInfo(antiKickCardItemID, 1)}
	if !server.profileHasKickProtection(profile) {
		t.Fatal("anti-kick card not protected")
	}
}

func TestCompetitivePetPointsCombineInnateAndLearnedRules(t *testing.T) {
	entries := []itemcatalog.Entry{
		{ID: 28008, Name: "布鲁斯", Kind: "pet-card", Description: "具有天赋：积分之光5%。"},
		{ID: 27021, Name: "积分加加1级", Kind: "pet-skill-book", Description: "增加所获得的积分3%。"},
		{ID: 27051, Name: "积分多多1级", Kind: "pet-skill-book", Description: "有3%的概率使得获得的积分翻倍。"},
	}
	server := &Server{itemEffectCatalog: itemeffect.NewCatalog(itemeffect.Discover(entries))}
	percent, chance := server.competitivePetPointBonuses([]uint16{28008, 27021, 27051})
	if percent != 8 || chance != 3 {
		t.Fatalf("pet point bonuses = %d%%/%d%%", percent, chance)
	}
}

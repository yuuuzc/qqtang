package game

import "testing"

func TestPlayerProfileTutorialCompletionUsesNeutralDraw(t *testing.T) {
	profile := DefaultPlayerProfile()
	profile.GameInfo.WinNum = 0
	profile.GameInfo.LossNum = 0
	profile.GameInfo.EqualNum = 0
	login := profile.ToLocalLoginConfig()
	if login.GameInfo.EqualNum != 1 {
		t.Fatalf("tutorial completion draw = %d, want 1", login.GameInfo.EqualNum)
	}
}

func TestPlayerProfilePreservesConfiguredRecord(t *testing.T) {
	profile := DefaultPlayerProfile()
	profile.GameInfo.WinNum = 5200
	profile.GameInfo.LossNum = 520
	profile.GameInfo.EqualNum = 52
	login := profile.ToLocalLoginConfig()
	if login.GameInfo.WinNum != 5200 || login.GameInfo.LossNum != 520 || login.GameInfo.EqualNum != 52 {
		t.Fatalf("configured record was not preserved: %+v", login.GameInfo)
	}
}

func TestPlayerProfileRejectsAboveClientMaximum(t *testing.T) {
	profile := DefaultPlayerProfile()
	profile.GameInfo.Degree = MaxPlayerLevel + 1
	if err := profile.Validate(); err == nil {
		t.Fatal("expected above-maximum degree to be rejected")
	}
	profile = DefaultPlayerProfile()
	profile.GameInfo.Point = MaxPlayerExperience + 1
	if err := profile.Validate(); err == nil {
		t.Fatal("expected above-maximum points to be rejected")
	}
}

func TestSetPermanentInventoryItemPreservesEquippedMetadata(t *testing.T) {
	profile := DefaultPlayerProfile()
	profile.Inventory = []ItemInfo{{
		ItemID: LargeStaminaPotionItemID, NumOfItem: 10, ItemStatus: 1,
		ItemRoleID: 2, ItemEffect: 3, ItemColor: 4, AvailPeriod: LocalPermanentAvailablePeriod,
	}}
	if err := profile.SetPermanentInventoryItem(LargeStaminaPotionItemID, 11); err != nil {
		t.Fatal(err)
	}
	got := profile.Inventory[0]
	if got.NumOfItem != 11 || got.ItemStatus != 1 || got.ItemRoleID != 2 || got.ItemEffect != 3 || got.ItemColor != 4 {
		t.Fatalf("updated inventory item lost metadata: %+v", got)
	}
}

func TestClientLoginProjectionKeepsLocalRoomControlCards(t *testing.T) {
	profile := DefaultPlayerProfile()
	profile.Inventory = []ItemInfo{
		NewPermanentItemInfo(SinglePlayerAdventureCardItemID, 1),
		NewPermanentItemInfo(SinglePlayerBossCardItemID, 1),
		NewPermanentItemInfo(CompetitiveAICardItemID, 1),
		NewPermanentItemInfo(LargeStaminaPotionItemID, 3),
	}

	client := profile.ToClientLoginConfig()
	if len(client.Items) != 4 || len(profile.Inventory) != 4 {
		t.Fatalf("client inventory lost local control cards: config=%+v profile=%+v", client.Items, profile.Inventory)
	}
	for index, want := range []uint16{SinglePlayerAdventureCardItemID, SinglePlayerBossCardItemID, CompetitiveAICardItemID, LargeStaminaPotionItemID} {
		if client.Items[index].ItemID != want {
			t.Fatalf("client inventory[%d] = %d, want %d", index, client.Items[index].ItemID, want)
		}
	}
}

func TestProfileProjectsSharedSocialFields(t *testing.T) {
	profile := DefaultPlayerProfile()
	profile.KinIndex = 7
	profile.KinName = "糖家族"
	profile.KinFlagID[0] = 3
	profile.SpouseUIN = 1_000_002
	profile.PatternPoints = []PatternPoint{{GameMode: 1, PatternPoint: 20, PatternLevel: 2, LevelValue: 40}}
	if err := profile.Validate(); err != nil {
		t.Fatal(err)
	}
	list := PlayerListEntryFromProfile(1_000_001, profile)
	if list.Player.KinIndex != 7 || list.Player.KinName != "糖家族" || len(list.Patterns) != 1 {
		t.Fatalf("lobby projection lost social fields: %+v", list)
	}
	room := PlayerInfoInRoomOldFromProfile(1_000_001, profile, 1, 1, 0)
	if room.KinIndex != 7 || room.KinName != "糖家族" || room.KinFlagID[0] != 3 {
		t.Fatalf("room projection lost kin fields: %+v", room)
	}
}

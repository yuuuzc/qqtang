package itemuse

import "testing"

func TestWorldItemNeverConsumesAccountInventory(t *testing.T) {
	plan, err := Resolve(Intent{Path: PathBattleWorldItem, Context: ContextCompetitive, ItemID: 42})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Effect != EffectWorldTemporary || plan.Consumption != ConsumeNone {
		t.Fatalf("world item plan = %+v", plan)
	}
}

func TestPreparedSpecialEffectsRemainPreparedWireActions(t *testing.T) {
	for _, test := range []struct {
		id     uint32
		effect Effect
	}{
		{LargeStaminaPotionID, EffectRestoreHealth},
		{FirstAidKitItemID, EffectReviveTeammate},
		{SmallSelfReviveCardID, EffectReviveSelf},
	} {
		plan, err := Resolve(Intent{Path: PathPreparedProp, Context: ContextAdventure, ItemID: test.id, Kind: "inventory-consumable"})
		if err != nil {
			t.Fatalf("item %d: %v", test.id, err)
		}
		if plan.Effect != test.effect || plan.Consumption != ConsumeOnAccepted {
			t.Fatalf("item %d plan = %+v", test.id, plan)
		}
	}
}

func TestPetAndRecipeConsumptionIsAtomicWithDurableEffect(t *testing.T) {
	for _, intent := range []Intent{
		{Path: PathPetOperation, Context: ContextLobby, ItemID: 28036, Kind: "pet-card"},
		{Path: PathPetOperation, Context: ContextRoom, ItemID: 27001, Kind: "pet-skill-book"},
		{Path: PathRoomInventory, Context: ContextLobby, ItemID: 24043, Kind: "craft-recipe"},
	} {
		plan, err := Resolve(intent)
		if err != nil {
			t.Fatal(err)
		}
		if plan.Consumption != ConsumeAtomic {
			t.Fatalf("plan = %+v", plan)
		}
	}
}

func TestPreparedPetCardIsRejectedBeforeConsumption(t *testing.T) {
	if _, err := Resolve(Intent{Path: PathPreparedProp, Context: ContextAdventure, ItemID: 28036, Kind: "pet-card"}); err == nil {
		t.Fatal("pet card accepted as prepared match prop")
	}
}

func TestCompetitivePreparedPropsRequireCompatibleRegistration(t *testing.T) {
	if _, err := Resolve(Intent{Path: PathPreparedProp, Context: ContextCompetitive, ItemID: 20020, Kind: "inventory-consumable"}); err != nil {
		t.Fatalf("dual-mode panacea rejected: %v", err)
	}
	if _, err := Resolve(Intent{Path: PathPreparedProp, Context: ContextCompetitive, ItemID: 20003, Kind: "inventory-consumable"}); err != nil {
		t.Fatalf("competitive-only prepared item rejected: %v", err)
	}
	if _, err := Resolve(Intent{Path: PathPreparedProp, Context: ContextAdventure, ItemID: 20003, Kind: "inventory-consumable"}); err == nil {
		t.Fatal("competitive-only prepared item accepted in adventure")
	}
	if _, err := Resolve(Intent{Path: PathPreparedProp, Context: ContextCompetitive, ItemID: LargeStaminaPotionID, Kind: "inventory-consumable"}); err == nil {
		t.Fatal("adventure-only stamina potion accepted in a competitive item room")
	}
}

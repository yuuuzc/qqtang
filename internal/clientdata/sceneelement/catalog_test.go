package sceneelement

import "testing"

func TestNativeRewardKeepsInventoryAndSceneSemanticsSeparate(t *testing.T) {
	tests := []struct {
		id    ID
		kind  RewardKind
		value uint32
	}{
		{SugarCoin50, RewardMatchSugar, 50},
		{SugarChest1000, RewardMatchSugar, 1000},
		{Experience20, RewardCompetitiveExperience, 20},
		{TreasureGemThree, RewardTreasureScore, 3},
	}
	for _, test := range tests {
		got, ok := NativeReward(test.id)
		if !ok || got.Kind != test.kind || got.Value != test.value {
			t.Fatalf("NativeReward(%d) = %+v, %t; want kind %d value %d", test.id, got, ok, test.kind, test.value)
		}
	}
	if _, ok := NativeReward(RewardChest91); ok {
		t.Fatal("unresolved reward chest must not acquire inferred accounting semantics")
	}
	if IsClientID(20044) {
		t.Fatal("account inventory item 20044 must not be accepted as a scene element")
	}
}

func TestNativeTransformationCatalogMatchesClientFactory(t *testing.T) {
	want := map[ID]uint16{
		101: 43,
		104: 41,
		107: 45,
		108: 46,
		109: 44,
		110: 42,
		114: 54,
		115: 55,
	}
	for sceneID, avatarRoleID := range want {
		got, ok := NativeTransformation(sceneID)
		if !ok || got.SceneID != sceneID || got.AvatarRoleID != avatarRoleID || got.DurationMS != 30_000 || got.TraverseStaticTerrain != (avatarRoleID == 41) {
			t.Fatalf("NativeTransformation(%d) = %+v/%v, want avatar %d duration 30000", sceneID, got, ok, avatarRoleID)
		}
		if !IsTransformationPickup(sceneID) {
			t.Fatalf("IsTransformationPickup(%d) = false", sceneID)
		}
	}
	for _, sceneID := range []ID{100, 102, 103, 105, 106, 111, 112, 113, 116} {
		if _, ok := NativeTransformation(sceneID); ok || IsTransformationPickup(sceneID) {
			t.Fatalf("non-transformation SceneID %d was accepted", sceneID)
		}
	}
}

func TestNativeBattleActionPickupMappings(t *testing.T) {
	want := map[ID]BattleActionPickupDefinition{
		21: {SceneID: 21, ActionID: 41, Count: 1},
		23: {SceneID: 23, ActionID: 42, Count: 1},
		24: {SceneID: 24, ActionID: 63, Count: 1},
		25: {SceneID: 25, ActionID: 43, Count: 3},
		27: {SceneID: 27, ActionID: 44, Count: 2},
		28: {SceneID: 28, ActionID: 64, Count: 1},
	}
	for sceneID, expected := range want {
		got, ok := NativeBattleActionPickup(sceneID)
		if !ok || got != expected {
			t.Fatalf("NativeBattleActionPickup(%d) = %+v/%v, want %+v/true", sceneID, got, ok, expected)
		}
	}
	for _, sceneID := range []ID{20, 22, 26, 29, 62} {
		if _, ok := NativeBattleActionPickup(sceneID); ok {
			t.Fatalf("passive/non-action SceneID %d was accepted as a held pickup", sceneID)
		}
	}
}

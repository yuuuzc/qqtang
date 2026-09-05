package craftcatalog

import (
	"bytes"
	"reflect"
	"testing"

	"qqtang/internal/game/itemcatalog"
	"qqtang/internal/protocol/game"
)

func TestForgeSplitClearsColorAndKeepsEffectWhileRevertClearsBothAxes(t *testing.T) {
	catalog := &Catalog{
		Split:  MaterialCost{ItemID: 30001, Quantity: 1},
		Revert: MaterialCost{ItemID: 30002, Quantity: 1},
	}
	forged := game.NewPermanentItemInfo(300, 1)
	forged.ItemEffect = 5
	forged.ItemColor = byte(ForgeColorPurple)

	split, err := catalog.Plan(ForgeSplit, forged, catalog.Split.ItemID, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !split.Succeeded || split.Effect != forged.ItemEffect || split.Color != 0 {
		t.Fatalf("split plan = %+v", split)
	}

	effectOnly := forged
	effectOnly.ItemEffect = split.Effect
	effectOnly.ItemColor = split.Color
	revert, err := catalog.Plan(ForgeRevert, effectOnly, catalog.Revert.ItemID, bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !revert.Succeeded || revert.Effect != 0 || revert.Color != 0 {
		t.Fatalf("revert plan = %+v", revert)
	}
}

func TestForgeMaterialRulesComeFromInstalledDescriptions(t *testing.T) {
	entries := []itemcatalog.RegistryEntry{
		{ItemID: 20051, Description: "大概率锻造出1级2级特效和橙色蓝色。"},
		{ItemID: 20052, Description: "大概率锻造出2级特效和橙色绿色。"},
		{ItemID: 20053, Description: "大概率锻造出2级3级特效和绿色蓝色。"},
		{ItemID: 20054, Description: "大概率锻造出3级特效和绿色蓝色。"},
		{ItemID: 20055, Description: "大概率锻造出3级4级特效和橙色绿色。"},
		{ItemID: 20056, Description: "大概率锻造出4级特效和橙色蓝色。"},
		{ItemID: 20057, Description: "大概率锻造出5极特效和紫色。"},
	}
	rules, err := forgeMaterialRulesFromItemCFG(entries)
	if err != nil {
		t.Fatal(err)
	}
	want := map[uint16]MaterialRule{
		20051: {ItemID: 20051, PreferredLevels: []byte{1, 2}, PreferredColors: []ForgeColor{ForgeColorRed, ForgeColorBlue}},
		20052: {ItemID: 20052, PreferredLevels: []byte{2}, PreferredColors: []ForgeColor{ForgeColorRed, ForgeColorGreen}},
		20053: {ItemID: 20053, PreferredLevels: []byte{2, 3}, PreferredColors: []ForgeColor{ForgeColorBlue, ForgeColorGreen}},
		20054: {ItemID: 20054, PreferredLevels: []byte{3}, PreferredColors: []ForgeColor{ForgeColorBlue, ForgeColorGreen}},
		20055: {ItemID: 20055, PreferredLevels: []byte{3, 4}, PreferredColors: []ForgeColor{ForgeColorRed, ForgeColorGreen}},
		20056: {ItemID: 20056, PreferredLevels: []byte{4}, PreferredColors: []ForgeColor{ForgeColorRed, ForgeColorBlue}},
		20057: {ItemID: 20057, PreferredLevels: []byte{5}, PreferredColors: []ForgeColor{ForgeColorPurple}},
	}
	if !reflect.DeepEqual(rules, want) {
		t.Fatalf("material rules = %#v, want %#v", rules, want)
	}
}

func TestForgeApplyAfterSplitUpgradesButNeverDegradesEffectAndRerollsColor(t *testing.T) {
	catalog := &Catalog{
		ApplySuccessPercent: 100,
		Items:               map[uint16]ForgeItem{300: {ItemID: 300, Class: ForgeCap}},
		Effects: map[byte]EffectForm{
			1: {ID: 1, Class: ForgeCap, Level: 1},
			2: {ID: 2, Class: ForgeCap, Level: 2},
			5: {ID: 5, Class: ForgeCap, Level: 5},
		},
		Materials: map[uint16]MaterialRule{
			20051: {ItemID: 20051, PreferredLevels: []byte{1, 2}, PreferredColors: []ForgeColor{ForgeColorRed, ForgeColorBlue}},
		},
	}
	splitItem := game.NewPermanentItemInfo(300, 1)
	splitItem.ItemEffect = 5
	splitItem.ItemColor = 0

	first, err := catalog.Plan(ForgeApply, splitItem, 20051, bytes.NewReader([]byte{0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	if first.Effect != 5 || first.Color != byte(ForgeColorRed) {
		t.Fatalf("first non-degrading reroll = %+v", first)
	}
	second, err := catalog.Plan(ForgeApply, splitItem, 20051, bytes.NewReader([]byte{1, 1}))
	if err != nil {
		t.Fatal(err)
	}
	if second.Effect != 5 || second.Color != byte(ForgeColorBlue) {
		t.Fatalf("second non-degrading reroll = %+v", second)
	}

	levelOne := splitItem
	levelOne.ItemEffect = 1
	reforged, err := catalog.Plan(ForgeApply, levelOne, 20051, bytes.NewReader([]byte{0, 0}))
	if err != nil {
		t.Fatal(err)
	}
	if reforged.Effect != 1 || reforged.Color != byte(ForgeColorRed) {
		t.Fatalf("existing-effect reroll = %+v", reforged)
	}
	upgraded, err := catalog.Plan(ForgeApply, levelOne, 20051, bytes.NewReader([]byte{1, 0}))
	if err != nil {
		t.Fatal(err)
	}
	if upgraded.Effect != 2 || upgraded.Color != byte(ForgeColorRed) {
		t.Fatalf("upgrading reroll = %+v", upgraded)
	}

	unforged := splitItem
	unforged.ItemEffect = 0
	initial, err := catalog.Plan(ForgeApply, unforged, 20051, bytes.NewReader([]byte{1, 1}))
	if err != nil {
		t.Fatal(err)
	}
	if initial.Effect != 2 || initial.Color != byte(ForgeColorBlue) {
		t.Fatalf("initial forge = %+v", initial)
	}
}

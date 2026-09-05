package game

import (
	"reflect"
	"testing"
)

func TestCreateNPCBossRoundTrip(t *testing.T) {
	// This is a codec-only fixture. Use a visibly arbitrary value so it cannot
	// be confused with the native rule-8 producer's local 60-second timer; the
	// native wire template carries Time=0.
	want := CreateNPCBoss{Time: 0x11223344, Bosses: []CompetitiveBossInfo{{
		Label: 30_001, BossID: 17, RoleID: 10, TeamID: 7, AIType: 1,
		Row: 5, Col: 9, HP: 500, Rate: 3, Bubble: 2, Power: 501,
		NormalItems: []BossItemInfo{{ItemID: 402, ItemCount: 1, DropTime: 2}},
		OutfitItems: []BossItemInfo{{ItemID: 404, ItemCount: 1, DropTime: 0}},
		Skills:      []uint32{11, 12}, Metadata: []byte{1, 2, 3},
	}}}
	encoded, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseCreateNPCBossNetwork(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip = %#v, want %#v", got, want)
	}
}

func TestCreateNPCBossRejectsCountAndTrailingBytes(t *testing.T) {
	if _, err := (CreateNPCBoss{}).MarshalNetworkBinary(); err == nil {
		t.Fatal("empty boss table was accepted")
	}
	valid := CreateNPCBoss{Time: 1, Bosses: []CompetitiveBossInfo{{
		Label: 1, BossID: 1, RoleID: 1, TeamID: 1, Row: 1, Col: 1, HP: 1,
	}}}
	encoded, err := valid.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ParseCreateNPCBossNetwork(append(encoded, 0)); err == nil {
		t.Fatal("trailing byte was accepted")
	}
}

func TestCreateNPCBossAcceptsNativeRule8ZeroLabelAndHP(t *testing.T) {
	want := CreateNPCBoss{Bosses: []CompetitiveBossInfo{{
		BossID: 20_001, RoleID: 25, TeamID: 7, AIType: 2, Row: 5, Col: 9,
		NormalItems: []BossItemInfo{}, OutfitItems: []BossItemInfo{{ItemID: 500, ItemCount: 3}, {ItemID: 501, ItemCount: 2}}, Skills: []uint32{},
	}}}
	encoded, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseCreateNPCBossNetwork(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native rule-8 round trip = %#v, want %#v", got, want)
	}
}

func TestCreateNPCBossAcceptsRule1LocalSpawnCoordinates(t *testing.T) {
	want := CreateNPCBoss{Bosses: []CompetitiveBossInfo{{
		BossID: 30_001, RoleID: 10, TeamID: 7, AIType: 1, Rate: 3, Bubble: 3, Power: 3,
		NormalItems: []BossItemInfo{},
		OutfitItems: []BossItemInfo{{ItemID: 500, ItemCount: 3}, {ItemID: 501, ItemCount: 2}},
		Skills:      []uint32{},
	}}}
	encoded, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseCreateNPCBossNetwork(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native rule-1 round trip = %#v, want %#v", got, want)
	}
}

func TestCreateNPCBossPreservesNativeSkillPlaceholder(t *testing.T) {
	want := CreateNPCBoss{Bosses: []CompetitiveBossInfo{{
		BossID: 30_001, RoleID: 35, TeamID: 7, AIType: 5,
		NormalItems: []BossItemInfo{}, OutfitItems: []BossItemInfo{}, Skills: []uint32{0, 11, 12},
	}}}
	encoded, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseCreateNPCBossNetwork(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("placeholder round trip = %#v, want %#v", got, want)
	}
}

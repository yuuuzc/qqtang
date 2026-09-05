package roledata

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRulesResolveRandomAndPurpleDiamondRoles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roles.json")
	if err := os.WriteFile(path, []byte(`{
		"schema_version":2,
		"random_placeholder_role_id":23,
		"purple_diamond_identity_mask":8192,
		"normal_role_ids":[1,2],
		"adventure_random_extra_role_ids":[],
		"competitive_random_extra_role_ids":[10,11],
		"purple_diamond_role_ids":[13]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := LoadRules(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := rules.ResolveForModeWithReader(23, 0, RandomModeAdventure, bytes.NewReader([]byte{0}))
	if err != nil || resolved != 1 {
		t.Fatalf("random role = %d, %v; want normal role 1", resolved, err)
	}
	resolved, err = rules.ResolveForModeWithReader(23, 0, RandomModeCompetitive, bytes.NewReader([]byte{3}))
	if err != nil || resolved != 11 {
		t.Fatalf("competitive hidden random role = %d, %v; want 11", resolved, err)
	}
	if err := rules.ValidateRoomSelection(12, 0); err == nil {
		t.Fatal("incomplete role 12 unexpectedly accepted as a room selection")
	}
	if _, err := rules.ResolveForModeWithReader(13, 0, RandomModeCompetitive, bytes.NewReader([]byte{0})); err == nil {
		t.Fatal("purple-diamond role unexpectedly accepted without identity flag")
	}
	resolved, err = rules.ResolveForModeWithReader(13, 8192, RandomModeCompetitive, bytes.NewReader([]byte{0}))
	if err != nil || resolved != 13 {
		t.Fatalf("purple-diamond role = %d, %v; want 13", resolved, err)
	}
	if err := rules.ValidateRoomSelection(23, 0); err != nil {
		t.Fatalf("random placeholder rejected in room: %v", err)
	}
}

func TestRulesResolveCompetitiveBunCardIsExactlySeparateFiftyPercentRoll(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roles.json")
	if err := os.WriteFile(path, []byte(`{
		"schema_version":2,
		"random_placeholder_role_id":23,
		"purple_diamond_identity_mask":8192,
		"normal_role_ids":[1,2],
		"adventure_random_extra_role_ids":[],
		"competitive_random_extra_role_ids":[10,11],
		"purple_diamond_role_ids":[13]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}
	rules, err := LoadRules(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := rules.ResolveCompetitiveWithBunCardWithReader(23, 0, true, bytes.NewReader([]byte{0}))
	if err != nil || resolved != BunHeadRoleID {
		t.Fatalf("bun-card success = %d, %v; want role %d", resolved, err, BunHeadRoleID)
	}
	resolved, err = rules.ResolveCompetitiveWithBunCardWithReader(23, 0, true, bytes.NewReader([]byte{1, 2}))
	if err != nil || resolved != 11 {
		t.Fatalf("bun-card miss = %d, %v; want non-bun role 11", resolved, err)
	}
	resolved, err = rules.ResolveCompetitiveWithBunCardWithReader(23, 0, false, bytes.NewReader([]byte{2}))
	if err != nil || resolved != BunHeadRoleID {
		t.Fatalf("ordinary competitive pool = %d, %v; want its normal role-10 result", resolved, err)
	}
	resolved, err = rules.ResolveCompetitiveWithBunCardWithReader(1, 0, true, bytes.NewReader(nil))
	if err != nil || resolved != 1 {
		t.Fatalf("concrete selection changed by bun card: %d, %v", resolved, err)
	}
}

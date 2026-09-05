package rolecatalog

import (
	"reflect"
	"testing"
)

func TestPlayableCombatProfilesMatchNativeRows(t *testing.T) {
	wants := map[uint16]CombatProfile{
		1: {2, 6, 2, 8, 4, 7}, 2: {2, 6, 3, 9, 4, 6}, 3: {1, 5, 2, 9, 5, 7},
		4: {1, 7, 3, 9, 4, 7}, 5: {2, 7, 3, 8, 3, 7}, 6: {1, 6, 2, 7, 5, 8},
		7: {1, 8, 3, 9, 3, 8}, 8: {1, 7, 2, 6, 5, 8}, 9: {2, 6, 3, 7, 5, 8},
		10: {3, 8, 4, 9, 5, 8}, 11: {3, 8, 4, 9, 5, 8},
		13: {2, 8, 3, 7, 4, 6}, 14: {1, 7, 2, 6, 5, 8},
		15: {2, 7, 3, 9, 4, 8}, 16: {1, 8, 3, 9, 4, 8},
		18: {1, 6, 1, 6, 5, 8}, 19: {2, 8, 1, 6, 4, 7},
		20: {2, 6, 1, 5, 4, 6}, 21: {2, 8, 1, 5, 3, 6},
		22: {2, 6, 3, 7, 5, 8},
	}
	if !reflect.DeepEqual(playableCombatProfiles, wants) {
		t.Fatalf("playable combat profiles = %#v, want %#v", playableCombatProfiles, wants)
	}
	for _, roleID := range []uint16{0, 12, 17, 23, 24, 36, 65535} {
		if _, ok := PlayableCombatProfile(roleID); ok {
			t.Fatalf("unsupported RoleID %d received a playable combat profile", roleID)
		}
	}
}

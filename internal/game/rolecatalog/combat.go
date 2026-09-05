package rolecatalog

// CombatProfile is the immutable six-value row used by the final client's
// native character-state constructor. The values are current/maximum pairs
// for simultaneous bubbles, bubble power and movement-rate respectively.
//
// The source table is Client.exe DAT_007f2680. FUN_005aa74e indexes it directly
// by RoleID and decodes every DWORD with XOR key 0x1234 before storing the
// values in the live character state.
type CombatProfile struct {
	BombCapacity    byte
	MaxBombCapacity byte
	BombPower       byte
	MaxBombPower    byte
	SpeedRate       byte
	MaxSpeedRate    byte
}

// playableCombatProfiles contains only complete player models accepted by the
// restored role rules. RoleID 12 is an incomplete component model, RoleID 17
// has no usable bubble profile, and RoleID 23 is the random-selection
// placeholder; none of them may become an in-battle actor.
var playableCombatProfiles = map[uint16]CombatProfile{
	1:  {2, 6, 2, 8, 4, 7},
	2:  {2, 6, 3, 9, 4, 6},
	3:  {1, 5, 2, 9, 5, 7},
	4:  {1, 7, 3, 9, 4, 7},
	5:  {2, 7, 3, 8, 3, 7},
	6:  {1, 6, 2, 7, 5, 8},
	7:  {1, 8, 3, 9, 3, 8},
	8:  {1, 7, 2, 6, 5, 8},
	9:  {2, 6, 3, 7, 5, 8},
	10: {3, 8, 4, 9, 5, 8},
	11: {3, 8, 4, 9, 5, 8},
	13: {2, 8, 3, 7, 4, 6},
	14: {1, 7, 2, 6, 5, 8},
	15: {2, 7, 3, 9, 4, 8},
	16: {1, 8, 3, 9, 4, 8},
	18: {1, 6, 1, 6, 5, 8},
	19: {2, 8, 1, 6, 4, 7},
	20: {2, 6, 1, 5, 4, 6},
	21: {2, 8, 1, 5, 3, 6},
	22: {2, 6, 3, 7, 5, 8},
}

// PlayableCombatProfile returns a native profile only for a complete player
// model that the server is allowed to resolve into a running match.
func PlayableCombatProfile(roleID uint16) (CombatProfile, bool) {
	profile, ok := playableCombatProfiles[roleID]
	return profile, ok
}

package roledata

import (
	cryptorand "crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"os"
	"strings"
)

const rulesSchemaVersion = 2

// BunHeadRoleID is the concrete competitive role represented by the bun-card
// bonus. The card itself remains an ordinary account inventory item; only this
// resolved role is serialized into the match snapshot.
const BunHeadRoleID byte = 10

type RandomMode byte

const (
	RandomModeAdventure RandomMode = iota
	RandomModeCompetitive
)

// Rules describes the client-visible character selector. The question-mark
// entry is a selection instruction, not a playable role, and must therefore
// never be serialized into GAME_INFO or PLAYER_GAME_INFO.
type Rules struct {
	SchemaVersion             uint32 `json:"schema_version"`
	RandomPlaceholderRoleID   byte   `json:"random_placeholder_role_id"`
	PurpleDiamondIdentityMask uint32 `json:"purple_diamond_identity_mask"`
	NormalRoleIDs             []byte `json:"normal_role_ids"`
	AdventureRandomExtraIDs   []byte `json:"adventure_random_extra_role_ids"`
	CompetitiveRandomExtraIDs []byte `json:"competitive_random_extra_role_ids"`
	PurpleDiamondRoleIDs      []byte `json:"purple_diamond_role_ids"`

	normalRoles        map[byte]struct{}
	purpleDiamondRoles map[byte]struct{}
}

func LoadRules(path string) (*Rules, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("role rules path is empty")
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read role rules: %w", err)
	}
	var rules Rules
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rules); err != nil {
		return nil, fmt.Errorf("decode role rules: %w", err)
	}
	if err := rules.initialize(); err != nil {
		return nil, err
	}
	return &rules, nil
}

func (rules *Rules) initialize() error {
	if rules == nil {
		return fmt.Errorf("role rules are nil")
	}
	if rules.SchemaVersion != rulesSchemaVersion {
		return fmt.Errorf("role rules schema_version %d, want %d", rules.SchemaVersion, rulesSchemaVersion)
	}
	if rules.RandomPlaceholderRoleID == 0 {
		return fmt.Errorf("role rules random_placeholder_role_id must be non-zero")
	}
	if rules.PurpleDiamondIdentityMask == 0 {
		return fmt.Errorf("role rules purple_diamond_identity_mask must be non-zero")
	}
	if len(rules.NormalRoleIDs) == 0 {
		return fmt.Errorf("role rules contains no normal roles")
	}
	rules.normalRoles = make(map[byte]struct{}, len(rules.NormalRoleIDs))
	rules.purpleDiamondRoles = make(map[byte]struct{}, len(rules.PurpleDiamondRoleIDs))
	for index, roleID := range rules.NormalRoleIDs {
		if roleID == 0 || roleID == rules.RandomPlaceholderRoleID {
			return fmt.Errorf("role rules normal_role_ids[%d] has reserved role ID %d", index, roleID)
		}
		if _, duplicate := rules.normalRoles[roleID]; duplicate {
			return fmt.Errorf("role rules repeats normal role ID %d", roleID)
		}
		rules.normalRoles[roleID] = struct{}{}
	}
	for index, roleID := range rules.PurpleDiamondRoleIDs {
		if roleID == 0 || roleID == rules.RandomPlaceholderRoleID {
			return fmt.Errorf("role rules purple_diamond_role_ids[%d] has reserved role ID %d", index, roleID)
		}
		if _, duplicate := rules.normalRoles[roleID]; duplicate {
			return fmt.Errorf("role rules role ID %d is both normal and purple-diamond-only", roleID)
		}
		if _, duplicate := rules.purpleDiamondRoles[roleID]; duplicate {
			return fmt.Errorf("role rules repeats purple-diamond role ID %d", roleID)
		}
		rules.purpleDiamondRoles[roleID] = struct{}{}
	}
	if err := rules.validateRandomExtras("adventure_random_extra_role_ids", rules.AdventureRandomExtraIDs); err != nil {
		return err
	}
	if err := rules.validateRandomExtras("competitive_random_extra_role_ids", rules.CompetitiveRandomExtraIDs); err != nil {
		return err
	}
	for _, adventureRoleID := range rules.AdventureRandomExtraIDs {
		for _, competitiveRoleID := range rules.CompetitiveRandomExtraIDs {
			if adventureRoleID == competitiveRoleID {
				return fmt.Errorf("role rules random extra role ID %d appears in both modes", adventureRoleID)
			}
		}
	}
	return nil
}

func (rules *Rules) validateRandomExtras(field string, roleIDs []byte) error {
	seen := make(map[byte]struct{}, len(roleIDs))
	for index, roleID := range roleIDs {
		if roleID == 0 || roleID == rules.RandomPlaceholderRoleID {
			return fmt.Errorf("role rules %s[%d] has reserved role ID %d", field, index, roleID)
		}
		if _, selectable := rules.normalRoles[roleID]; selectable {
			return fmt.Errorf("role rules %s role ID %d is already normally selectable", field, roleID)
		}
		if _, premium := rules.purpleDiamondRoles[roleID]; premium {
			return fmt.Errorf("role rules %s role ID %d is purple-diamond-only", field, roleID)
		}
		if _, duplicate := seen[roleID]; duplicate {
			return fmt.Errorf("role rules %s repeats role ID %d", field, roleID)
		}
		seen[roleID] = struct{}{}
	}
	return nil
}

func (rules *Rules) HasPurpleDiamond(identity uint32) bool {
	return rules != nil && identity&rules.PurpleDiamondIdentityMask != 0
}

func (rules *Rules) IsRandomPlaceholder(roleID byte) bool {
	return rules != nil && roleID == rules.RandomPlaceholderRoleID
}

// ValidateRoomSelection accepts the question-mark entry as a real room UI
// selection without resolving it. A concrete role is chosen only when a match
// snapshot is created; this preserves the original "random every round"
// behavior and keeps the question-mark model valid in the waiting room.
func (rules *Rules) ValidateRoomSelection(roleID byte, identity uint32) error {
	if rules == nil || rules.normalRoles == nil || rules.purpleDiamondRoles == nil {
		return fmt.Errorf("role rules are not initialized")
	}
	if rules.IsRandomPlaceholder(roleID) {
		return nil
	}
	if _, normal := rules.normalRoles[roleID]; normal {
		return nil
	}
	if _, premium := rules.purpleDiamondRoles[roleID]; premium {
		if !rules.HasPurpleDiamond(identity) {
			return fmt.Errorf("role ID %d requires purple-diamond identity mask 0x%08X", roleID, rules.PurpleDiamondIdentityMask)
		}
		return nil
	}
	return fmt.Errorf("role ID %d is not selectable", roleID)
}

// ResolveForMode converts one room selection into a concrete match-snapshot
// role. The room member is intentionally not changed: after settlement the
// player still has the question-mark selection and the next round rerolls.
// Purple-diamond selector roles are never part of either random pool.
func (rules *Rules) ResolveForMode(requestedRoleID byte, identity uint32, mode RandomMode) (byte, error) {
	return rules.ResolveForModeWithReader(requestedRoleID, identity, mode, cryptorand.Reader)
}

func (rules *Rules) ResolveForModeWithReader(requestedRoleID byte, identity uint32, mode RandomMode, entropy io.Reader) (byte, error) {
	if rules == nil || rules.normalRoles == nil || rules.purpleDiamondRoles == nil {
		return 0, fmt.Errorf("role rules are not initialized")
	}
	var extra []byte
	switch mode {
	case RandomModeAdventure:
		extra = rules.AdventureRandomExtraIDs
	case RandomModeCompetitive:
		extra = rules.CompetitiveRandomExtraIDs
	default:
		return 0, fmt.Errorf("unsupported random-role mode %d", mode)
	}
	if requestedRoleID == rules.RandomPlaceholderRoleID {
		if entropy == nil {
			return 0, fmt.Errorf("role-selection entropy reader is nil")
		}
		poolSize := len(rules.NormalRoleIDs) + len(extra)
		index, err := cryptorand.Int(entropy, big.NewInt(int64(poolSize)))
		if err != nil {
			return 0, fmt.Errorf("choose random role: %w", err)
		}
		selected := int(index.Int64())
		if selected < len(rules.NormalRoleIDs) {
			return rules.NormalRoleIDs[selected], nil
		}
		return extra[selected-len(rules.NormalRoleIDs)], nil
	}
	if err := rules.ValidateRoomSelection(requestedRoleID, identity); err != nil {
		return 0, err
	}
	return requestedRoleID, nil
}

// ResolveCompetitiveWithBunCard applies item 204's passive rule without
// projecting an equipped/enabled state to the client. For a question-mark
// selection the first unbiased roll is exactly 50 percent for BunHeadRoleID;
// the other half samples the normal competitive pool with BunHeadRoleID
// removed, so the advertised probability cannot be increased accidentally.
func (rules *Rules) ResolveCompetitiveWithBunCard(requestedRoleID byte, identity uint32, ownsBunCard bool) (byte, error) {
	return rules.ResolveCompetitiveWithBunCardWithReader(requestedRoleID, identity, ownsBunCard, cryptorand.Reader)
}

func (rules *Rules) ResolveCompetitiveWithBunCardWithReader(requestedRoleID byte, identity uint32, ownsBunCard bool, entropy io.Reader) (byte, error) {
	if !ownsBunCard || !rules.IsRandomPlaceholder(requestedRoleID) {
		return rules.ResolveForModeWithReader(requestedRoleID, identity, RandomModeCompetitive, entropy)
	}
	if entropy == nil {
		return 0, fmt.Errorf("role-selection entropy reader is nil")
	}
	roll, err := cryptorand.Int(entropy, big.NewInt(2))
	if err != nil {
		return 0, fmt.Errorf("choose bun-card outcome: %w", err)
	}
	if roll.Sign() == 0 {
		return BunHeadRoleID, nil
	}
	pool := make([]byte, 0, len(rules.NormalRoleIDs)+len(rules.CompetitiveRandomExtraIDs))
	pool = append(pool, rules.NormalRoleIDs...)
	for _, roleID := range rules.CompetitiveRandomExtraIDs {
		if roleID != BunHeadRoleID {
			pool = append(pool, roleID)
		}
	}
	if len(pool) == 0 {
		return 0, fmt.Errorf("competitive random-role pool contains no non-bun role")
	}
	index, err := cryptorand.Int(entropy, big.NewInt(int64(len(pool))))
	if err != nil {
		return 0, fmt.Errorf("choose non-bun random role: %w", err)
	}
	return pool[int(index.Int64())], nil
}

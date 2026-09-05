package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"

	"qqtang/internal/protocol/game"
)

const (
	KinNameMaximum             = 17
	KinDeclarationMaximum      = 257
	KinTitleMaximum            = 200
	KinNotificationMaximum     = 200
	KinMemberMaximum           = 200
	KinDefaultMemberCapacity   = 100
	KinAllianceBookItemID      = 1077
	KinSuperAllianceBookItemID = 2235
	KinAllianceBookRequired    = 100
	defaultKinBadgeID          = 1

	// QQTSection does not use a zero-based role enum here. Its native family
	// manager installs exactly five authority grades and serializes them in
	// KinMemberOld.Grade: 12, 10, 8, 6 and 4.
	KinAuthorityDisciple   uint32 = 4
	KinAuthorityGuardian   uint32 = 6
	KinAuthorityHallMaster uint32 = 8
	KinAuthorityElder      uint32 = 10
	KinAuthorityOwner      uint32 = 12

	// Compatibility names for callers which only need the lowest member or
	// the minimum management grade.
	KinAuthorityMember  = KinAuthorityDisciple
	KinAuthorityOfficer = KinAuthorityElder

	kinCapacityShift = 20
	kinCapacityMask  = uint32(0xfff00000)
	kinStatusMask    = ^kinCapacityMask
	kinInvitationTTL = 10 * time.Minute
)

var defaultKinAuthorityTitles = []string{"族长", "长老", "堂主", "护法", "弟子"}
var kinAuthorityGrades = []uint32{KinAuthorityOwner, KinAuthorityElder, KinAuthorityHallMaster, KinAuthorityGuardian, KinAuthorityDisciple}

func DefaultKinAuthorityTitle() string {
	// Persistence keeps the five editable names as logical text. The game
	// protocol layer alone owns QQTSection's ASCII-framed GBK title object.
	return strings.Join(defaultKinAuthorityTitles, "\x07") + "\x07"
}

func KinMemberCapacity(status uint32) uint32 {
	return status >> kinCapacityShift
}

func withKinMemberCapacity(status, capacity uint32) uint32 {
	return status&kinStatusMask | capacity<<kinCapacityShift
}

func defaultKinFlagID() game.KinFlagID {
	return game.NewKinFlagID(0, defaultKinBadgeID)
}

type Kin struct {
	Index           uint32
	OwnerUIN        uint32
	CreatedUnix     uint32
	Status          uint32
	Grade           uint32
	FlagID          game.KinFlagID
	Name            string
	Declaration     string
	Title           string
	Section         uint32
	BaseUpdate      uint32
	ListUpdate      uint32
	Notification    string
	Honor           uint32
	ActivePoint     uint32
	LastHonor       uint32
	LastActivePoint uint32
}

type KinMember struct {
	KinIndex      uint32
	UIN           uint32
	Nickname      string
	JoinedUnix    uint32
	Status        uint32
	StatusTime    uint32
	Grade         uint32
	AuthorityID   uint32
	CustomTitle   string
	OnlineTime    uint32
	LastLoginTime uint32
	Honor         uint32
	ActivePoint   uint32
}

type KinRankingEntry struct {
	Index       uint32
	Name        string
	Value       uint32
	Order       uint16
	BeforeOrder uint16
}

func (store *PlayerStore) projectKinMembership(ctx context.Context, uin uint32, profile *game.PlayerProfile) error {
	if profile == nil {
		return fmt.Errorf("profile is nil")
	}
	var index uint32
	var name string
	var flag []byte
	err := store.db.QueryRowContext(ctx, `SELECT f.kin_index, f.name, f.flag_id
		FROM kin_members m JOIN kin_families f ON f.kin_index = m.kin_index
		WHERE m.uin = ?`, uin).Scan(&index, &name, &flag)
	if errors.Is(err, sql.ErrNoRows) {
		profile.KinIndex = 0
		profile.KinName = ""
		profile.KinFlagID = game.KinFlagID{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("load kin membership for UIN %d: %w", uin, err)
	}
	if len(flag) != len(profile.KinFlagID) {
		return fmt.Errorf("kin %d flag length %d, want %d", index, len(flag), len(profile.KinFlagID))
	}
	profile.KinIndex = index
	profile.KinName = name
	copy(profile.KinFlagID[:], flag)
	return nil
}

func (store *PlayerStore) CreateKin(ctx context.Context, ownerUIN uint32, name, declaration string, status, section uint32) (Kin, error) {
	name = strings.TrimSpace(name)
	declaration = strings.TrimSpace(declaration)
	if ownerUIN == 0 {
		return Kin{}, fmt.Errorf("kin owner UIN must be non-zero")
	}
	if err := validateKinText("name", name, KinNameMaximum, false); err != nil {
		return Kin{}, err
	}
	if err := validateKinText("declaration", declaration, KinDeclarationMaximum, true); err != nil {
		return Kin{}, err
	}
	now := uint32(time.Now().Unix())
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Kin{}, fmt.Errorf("begin create kin: %w", err)
	}
	defer tx.Rollback()
	if err := requirePlayerTx(ctx, tx, ownerUIN); err != nil {
		return Kin{}, err
	}
	qualified, qualificationErr := hasKinCreationQualificationTx(ctx, tx, ownerUIN)
	if qualificationErr != nil {
		return Kin{}, qualificationErr
	}
	if !qualified {
		return Kin{}, ErrKinCreationItemMissing
	}
	var existing uint32
	if err := tx.QueryRowContext(ctx, `SELECT kin_index FROM kin_members WHERE uin = ?`, ownerUIN).Scan(&existing); err == nil {
		return Kin{}, ErrKinAlreadyMember
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Kin{}, fmt.Errorf("check owner kin membership: %w", err)
	}
	nowText := time.Now().UTC().Format(time.RFC3339Nano)
	defaultFlag := defaultKinFlagID()
	status = withKinMemberCapacity(status, KinDefaultMemberCapacity)
	result, err := tx.ExecContext(ctx, `INSERT INTO kin_families(
		owner_uin, name, declaration, title, status, grade, kin_section, flag_id, created_unix, updated_utc
	) VALUES(?, ?, ?, ?, ?, 1, ?, ?, ?, ?)`, ownerUIN, name, declaration, DefaultKinAuthorityTitle(), status, section, defaultFlag[:], now, nowText)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return Kin{}, ErrKinNameOwned
		}
		return Kin{}, fmt.Errorf("insert kin: %w", err)
	}
	index64, err := result.LastInsertId()
	if err != nil || index64 <= 0 || index64 > int64(^uint32(0)) {
		return Kin{}, fmt.Errorf("read created kin index: %w", err)
	}
	index := uint32(index64)
	if _, err = tx.ExecContext(ctx, `INSERT INTO kin_members(
		kin_index, uin, joined_unix, status, status_time, grade, authority_id, last_login_time
	) VALUES(?, ?, ?, 1, ?, ?, ?, ?)`, index, ownerUIN, now, now, KinAuthorityOwner, KinAuthorityOwner, now); err != nil {
		return Kin{}, fmt.Errorf("insert kin owner: %w", err)
	}
	if err := updateProfileKinTx(ctx, tx, ownerUIN, index, name, defaultFlag); err != nil {
		return Kin{}, err
	}
	if err := tx.Commit(); err != nil {
		return Kin{}, fmt.Errorf("commit create kin: %w", err)
	}
	return store.LoadKin(ctx, index)
}

func hasKinCreationQualificationTx(ctx context.Context, tx *sql.Tx, uin uint32) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT item_id, quantity, available_period FROM player_inventory
		WHERE uin = ? AND item_id IN (?, ?)`, uin, KinAllianceBookItemID, KinSuperAllianceBookItemID)
	if err != nil {
		return false, fmt.Errorf("read kin creation qualification for UIN %d: %w", uin, err)
	}
	defer rows.Close()
	for rows.Next() {
		var itemID uint16
		var quantity uint32
		var availablePeriod uint32
		if err := rows.Scan(&itemID, &quantity, &availablePeriod); err != nil {
			return false, fmt.Errorf("scan kin creation qualification: %w", err)
		}
		if availablePeriod == 0 {
			continue
		}
		if itemID == KinSuperAllianceBookItemID && quantity >= 1 || itemID == KinAllianceBookItemID && quantity >= KinAllianceBookRequired {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate kin creation qualification: %w", err)
	}
	return false, nil
}

func (store *PlayerStore) LoadKin(ctx context.Context, index uint32) (Kin, error) {
	if index == 0 {
		return Kin{}, ErrKinNotFound
	}
	var result Kin
	var flag []byte
	err := store.db.QueryRowContext(ctx, `SELECT kin_index, owner_uin, created_unix, status, grade,
		flag_id, name, declaration, title, kin_section, base_update, list_update,
		notification, honor, active_point
		FROM kin_families WHERE kin_index = ?`, index).Scan(
		&result.Index, &result.OwnerUIN, &result.CreatedUnix, &result.Status, &result.Grade,
		&flag, &result.Name, &result.Declaration, &result.Title, &result.Section,
		&result.BaseUpdate, &result.ListUpdate, &result.Notification, &result.Honor, &result.ActivePoint,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Kin{}, ErrKinNotFound
	}
	if err != nil {
		return Kin{}, fmt.Errorf("load kin %d: %w", index, err)
	}
	if len(flag) != len(result.FlagID) {
		return Kin{}, fmt.Errorf("kin %d flag length %d, want %d", index, len(flag), len(result.FlagID))
	}
	copy(result.FlagID[:], flag)
	return result, nil
}

func (store *PlayerStore) ListKinMembers(ctx context.Context, index uint32) ([]KinMember, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT m.kin_index, m.uin, p.profile_json,
		m.joined_unix, m.status, m.status_time, m.grade, m.authority_id, m.custom_title,
		m.online_time, m.last_login_time, m.honor, m.active_point
		FROM kin_members m JOIN local_players p ON p.uin = m.uin
		WHERE m.kin_index = ? ORDER BY m.authority_id DESC, m.joined_unix, m.uin`, index)
	if err != nil {
		return nil, fmt.Errorf("list kin %d members: %w", index, err)
	}
	defer rows.Close()
	members := make([]KinMember, 0)
	for rows.Next() {
		var member KinMember
		var profileJSON string
		if err := rows.Scan(&member.KinIndex, &member.UIN, &profileJSON, &member.JoinedUnix,
			&member.Status, &member.StatusTime, &member.Grade, &member.AuthorityID, &member.CustomTitle,
			&member.OnlineTime, &member.LastLoginTime, &member.Honor, &member.ActivePoint); err != nil {
			return nil, fmt.Errorf("scan kin member: %w", err)
		}
		var profile game.PlayerProfile
		if err := decodeProfileJSON(profileJSON, &profile); err != nil {
			return nil, fmt.Errorf("decode kin member UIN %d: %w", member.UIN, err)
		}
		member.Nickname = profile.Nickname
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate kin members: %w", err)
	}
	return members, nil
}

// ListKinRanking returns a stable top-family projection. Historical order is
// initialized to the current order until a periodic ranking snapshot exists;
// this is preferable to fabricating movement on every fetch.
func (store *PlayerStore) ListKinRanking(ctx context.Context, activePoint bool, limit int) ([]KinRankingEntry, error) {
	if limit <= 0 || limit > 20 {
		return nil, fmt.Errorf("kin ranking limit %d is outside 1..20", limit)
	}
	column := "honor"
	if activePoint {
		column = "active_point"
	}
	rows, err := store.db.QueryContext(ctx, `SELECT kin_index, name, `+column+`
		FROM kin_families ORDER BY `+column+` DESC, kin_index ASC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list kin %s ranking: %w", column, err)
	}
	defer rows.Close()
	entries := make([]KinRankingEntry, 0, limit)
	for rows.Next() {
		var entry KinRankingEntry
		if err := rows.Scan(&entry.Index, &entry.Name, &entry.Value); err != nil {
			return nil, fmt.Errorf("scan kin %s ranking: %w", column, err)
		}
		entry.Order = uint16(len(entries) + 1)
		entry.BeforeOrder = entry.Order
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate kin %s ranking: %w", column, err)
	}
	return entries, nil
}

func (store *PlayerStore) ApplyToKin(ctx context.Context, uin, index uint32) error {
	if uin == 0 || index == 0 {
		return fmt.Errorf("kin application requires non-zero UIN and kin index")
	}
	if _, err := store.LoadKin(ctx, index); err != nil {
		return err
	}
	var existing uint32
	if err := store.db.QueryRowContext(ctx, `SELECT kin_index FROM kin_members WHERE uin = ?`, uin).Scan(&existing); err == nil {
		return ErrKinAlreadyMember
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("check applicant membership: %w", err)
	}
	_, err := store.db.ExecContext(ctx, `INSERT INTO kin_applications(kin_index, uin, applied_unix)
		VALUES(?, ?, ?) ON CONFLICT(kin_index, uin) DO UPDATE SET applied_unix = excluded.applied_unix`,
		index, uin, uint32(time.Now().Unix()))
	if err != nil {
		return fmt.Errorf("save kin application: %w", err)
	}
	return nil
}

func (store *PlayerStore) AcceptKinApplication(ctx context.Context, actorUIN, applicantUIN, index uint32) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin accept kin application: %w", err)
	}
	defer tx.Rollback()
	if err := requireKinOfficerTx(ctx, tx, actorUIN, index); err != nil {
		return err
	}
	kin, err := loadKinTx(ctx, tx, index)
	if err != nil {
		return err
	}
	if err := requireKinCapacityTx(ctx, tx, kin); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM kin_applications WHERE kin_index = ? AND uin = ?`, index, applicantUIN)
	if err != nil {
		return fmt.Errorf("consume kin application: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrKinApplicationMissing
	}
	if err := addKinMemberTx(ctx, tx, kin, applicantUIN); err != nil {
		return err
	}
	return tx.Commit()
}

// InviteToKin persists the one invitation that QQTSection can cache for a
// player. The native mode-4 sender permits authority 10 and 12 only, so the
// same threshold is revalidated in the transaction instead of trusting UI
// visibility.
func (store *PlayerStore) InviteToKin(ctx context.Context, inviterUIN, targetUIN, index uint32) (Kin, error) {
	if inviterUIN == 0 || targetUIN == 0 || index == 0 || inviterUIN == targetUIN {
		return Kin{}, fmt.Errorf("kin invitation identities are invalid")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Kin{}, fmt.Errorf("begin kin invitation: %w", err)
	}
	defer tx.Rollback()
	kin, err := loadKinTx(ctx, tx, index)
	if err != nil {
		return Kin{}, err
	}
	if err := requireKinOfficerTx(ctx, tx, inviterUIN, index); err != nil {
		return Kin{}, err
	}
	if err := requirePlayerTx(ctx, tx, targetUIN); err != nil {
		return Kin{}, err
	}
	var existing uint32
	if err := tx.QueryRowContext(ctx, `SELECT kin_index FROM kin_members WHERE uin = ?`, targetUIN).Scan(&existing); err == nil {
		return Kin{}, ErrKinAlreadyMember
	} else if !errors.Is(err, sql.ErrNoRows) {
		return Kin{}, fmt.Errorf("check invited player membership: %w", err)
	}
	if err := requireKinCapacityTx(ctx, tx, kin); err != nil {
		return Kin{}, err
	}
	now := uint32(time.Now().Unix())
	cutoff := uint32(0)
	if now > uint32(kinInvitationTTL/time.Second) {
		cutoff = now - uint32(kinInvitationTTL/time.Second)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM kin_invitations WHERE created_unix < ?`, cutoff); err != nil {
		return Kin{}, fmt.Errorf("expire kin invitations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO kin_invitations(inviter_uin, target_uin, kin_index, created_unix)
		VALUES(?, ?, ?, ?) ON CONFLICT(target_uin) DO UPDATE SET
		inviter_uin = excluded.inviter_uin, kin_index = excluded.kin_index, created_unix = excluded.created_unix`,
		inviterUIN, targetUIN, index, now); err != nil {
		return Kin{}, fmt.Errorf("save kin invitation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Kin{}, fmt.Errorf("commit kin invitation: %w", err)
	}
	return store.LoadKin(ctx, index)
}

// AnswerKinInvitation consumes an exact pending invitation. Accepting adds a
// lowest-grade member and updates the profile projection in the same
// transaction; rejecting only consumes the invitation.
func (store *PlayerStore) AnswerKinInvitation(ctx context.Context, responderUIN, inviterUIN, index uint32, accept bool) (Kin, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Kin{}, fmt.Errorf("begin answer kin invitation: %w", err)
	}
	defer tx.Rollback()
	kin, err := loadKinTx(ctx, tx, index)
	if err != nil {
		return Kin{}, err
	}
	var createdUnix uint32
	if err := tx.QueryRowContext(ctx, `SELECT created_unix FROM kin_invitations
		WHERE inviter_uin = ? AND target_uin = ? AND kin_index = ?`, inviterUIN, responderUIN, index).Scan(&createdUnix); errors.Is(err, sql.ErrNoRows) {
		return Kin{}, ErrKinInvitationMissing
	} else if err != nil {
		return Kin{}, fmt.Errorf("load kin invitation: %w", err)
	}
	now := uint32(time.Now().Unix())
	if now > createdUnix && time.Duration(now-createdUnix)*time.Second > kinInvitationTTL {
		return Kin{}, ErrKinInvitationMissing
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM kin_invitations
		WHERE inviter_uin = ? AND target_uin = ? AND kin_index = ?`, inviterUIN, responderUIN, index); err != nil {
		return Kin{}, fmt.Errorf("consume kin invitation: %w", err)
	}
	if !accept {
		if err := tx.Commit(); err != nil {
			return Kin{}, fmt.Errorf("commit rejected kin invitation: %w", err)
		}
		return store.LoadKin(ctx, index)
	}
	if err := requireKinOfficerTx(ctx, tx, inviterUIN, index); err != nil {
		return Kin{}, err
	}
	if err := requireKinCapacityTx(ctx, tx, kin); err != nil {
		return Kin{}, err
	}
	if err := addKinMemberTx(ctx, tx, kin, responderUIN); err != nil {
		return Kin{}, err
	}
	if err := tx.Commit(); err != nil {
		return Kin{}, fmt.Errorf("commit accepted kin invitation: %w", err)
	}
	return store.LoadKin(ctx, index)
}

func (store *PlayerStore) LeaveKin(ctx context.Context, uin, index uint32) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin leave kin: %w", err)
	}
	defer tx.Rollback()
	kin, err := loadKinTx(ctx, tx, index)
	if err != nil {
		return err
	}
	if kin.OwnerUIN == uin {
		return fmt.Errorf("kin owner must dismiss the family: %w", ErrKinPermissionDenied)
	}
	if err := removeKinMemberTx(ctx, tx, uin, index); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *PlayerStore) KickKinMember(ctx context.Context, actorUIN, targetUIN, index uint32) error {
	if actorUIN == targetUIN {
		return fmt.Errorf("use leave-kin for the current member")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin kick kin member: %w", err)
	}
	defer tx.Rollback()
	kin, err := loadKinTx(ctx, tx, index)
	if err != nil {
		return err
	}
	if kin.OwnerUIN == targetUIN || kin.OwnerUIN != actorUIN {
		return ErrKinPermissionDenied
	}
	if err := removeKinMemberTx(ctx, tx, targetUIN, index); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *PlayerStore) DismissKin(ctx context.Context, ownerUIN, index uint32) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin dismiss kin: %w", err)
	}
	defer tx.Rollback()
	kin, err := loadKinTx(ctx, tx, index)
	if err != nil {
		return err
	}
	if kin.OwnerUIN != ownerUIN {
		return ErrKinPermissionDenied
	}
	rows, err := tx.QueryContext(ctx, `SELECT uin FROM kin_members WHERE kin_index = ?`, index)
	if err != nil {
		return fmt.Errorf("list members before dismiss: %w", err)
	}
	var uins []uint32
	for rows.Next() {
		var uin uint32
		if err := rows.Scan(&uin); err != nil {
			rows.Close()
			return fmt.Errorf("scan member before dismiss: %w", err)
		}
		uins = append(uins, uin)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate members before dismiss: %w", err)
	}
	rows.Close()
	for _, uin := range uins {
		if err := updateProfileKinTx(ctx, tx, uin, 0, "", game.KinFlagID{}); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM kin_families WHERE kin_index = ?`, index); err != nil {
		return fmt.Errorf("delete kin: %w", err)
	}
	return tx.Commit()
}

func (store *PlayerStore) SetKinMemberAuthority(ctx context.Context, actorUIN, targetUIN, index, authority uint32) error {
	if !isAssignableKinAuthority(authority) {
		return fmt.Errorf("assignable kin authority %d is not one of 4, 6, 8 or 10", authority)
	}
	if actorUIN == targetUIN {
		return ErrKinPermissionDenied
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin set kin member authority: %w", err)
	}
	defer tx.Rollback()
	kin, err := loadKinTx(ctx, tx, index)
	if err != nil {
		return err
	}
	if targetUIN == kin.OwnerUIN {
		return ErrKinPermissionDenied
	}
	var actorAuthority, targetAuthority uint32
	if err := tx.QueryRowContext(ctx, `SELECT authority_id FROM kin_members WHERE kin_index = ? AND uin = ?`, index, actorUIN).Scan(&actorAuthority); errors.Is(err, sql.ErrNoRows) {
		return ErrKinNotMember
	} else if err != nil {
		return fmt.Errorf("load acting kin authority: %w", err)
	}
	if actorUIN == kin.OwnerUIN {
		actorAuthority = KinAuthorityOwner
	}
	if err := tx.QueryRowContext(ctx, `SELECT authority_id FROM kin_members WHERE kin_index = ? AND uin = ?`, index, targetUIN).Scan(&targetAuthority); errors.Is(err, sql.ErrNoRows) {
		return ErrKinNotMember
	} else if err != nil {
		return fmt.Errorf("load target kin authority: %w", err)
	}
	// Native QQTSection only emits one-step changes. Promotion additionally
	// requires the promoted result to remain below the actor (the target was
	// at least two levels lower); demotion requires the target to be below the
	// actor before the change.
	promotion := authority == targetAuthority+2 && authority < actorAuthority
	demotion := authority+2 == targetAuthority && targetAuthority < actorAuthority
	if !promotion && !demotion {
		return ErrKinPermissionDenied
	}
	result, err := tx.ExecContext(ctx, `UPDATE kin_members SET grade = ?, authority_id = ?, status_time = ? WHERE kin_index = ? AND uin = ?`, authority, authority, uint32(time.Now().Unix()), index, targetUIN)
	if err != nil {
		return fmt.Errorf("set kin member authority: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrKinNotMember
	}
	if _, err := tx.ExecContext(ctx, `UPDATE kin_families SET list_update = list_update + 1, updated_utc = ? WHERE kin_index = ?`, time.Now().UTC().Format(time.RFC3339Nano), index); err != nil {
		return fmt.Errorf("advance kin member list revision after authority change: %w", err)
	}
	return tx.Commit()
}

func isAssignableKinAuthority(authority uint32) bool {
	switch authority {
	case KinAuthorityDisciple, KinAuthorityGuardian, KinAuthorityHallMaster, KinAuthorityElder:
		return true
	default:
		return false
	}
}

func (store *PlayerStore) SetKinTitle(ctx context.Context, actorUIN, index uint32, title string) (Kin, error) {
	normalized, err := normalizeKinAuthorityTitle(title)
	if err != nil {
		return Kin{}, err
	}
	return store.updateKinText(ctx, actorUIN, index, "title", normalized, KinTitleMaximum)
}

func (store *PlayerStore) SetKinDeclaration(ctx context.Context, actorUIN, index uint32, declaration string) (Kin, error) {
	return store.updateKinText(ctx, actorUIN, index, "declaration", declaration, KinDeclarationMaximum)
}

func (store *PlayerStore) SetKinNotification(ctx context.Context, actorUIN, index uint32, notification string) (Kin, error) {
	return store.updateKinText(ctx, actorUIN, index, "notification", notification, KinNotificationMaximum)
}

func (store *PlayerStore) updateKinText(ctx context.Context, actorUIN, index uint32, column, value string, maximum int) (Kin, error) {
	// Native authority-title encoding ends in a significant separator space.
	// Human-authored declaration/announcement text still uses normal trimming.
	if column != "title" {
		value = strings.TrimSpace(value)
	}
	if err := validateKinText(column, value, maximum, true); err != nil {
		return Kin{}, err
	}
	if column != "title" && column != "declaration" && column != "notification" {
		return Kin{}, fmt.Errorf("unsupported kin text column %q", column)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Kin{}, fmt.Errorf("begin update kin %s: %w", column, err)
	}
	defer tx.Rollback()
	// The native family-manager page only enables declaration, notification
	// and the five custom authority names for the immutable owner. Officers
	// have a separate invitation/application path; accepting an officer here
	// would create server-side state the stock UI can never authoritatively
	// edit again.
	err = requireKinOwnerTx(ctx, tx, actorUIN, index)
	if err != nil {
		return Kin{}, err
	}
	statement := `UPDATE kin_families SET ` + column + ` = ?, base_update = base_update + 1, updated_utc = ? WHERE kin_index = ?`
	if _, err := tx.ExecContext(ctx, statement, value, time.Now().UTC().Format(time.RFC3339Nano), index); err != nil {
		return Kin{}, fmt.Errorf("update kin %s: %w", column, err)
	}
	if err := tx.Commit(); err != nil {
		return Kin{}, fmt.Errorf("commit update kin %s: %w", column, err)
	}
	return store.LoadKin(ctx, index)
}

func (store *PlayerStore) SetKinFlag(ctx context.Context, actorUIN, index uint32, flag game.KinFlagID) (Kin, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Kin{}, fmt.Errorf("begin update kin flag: %w", err)
	}
	defer tx.Rollback()
	if err := requireKinOwnerTx(ctx, tx, actorUIN, index); err != nil {
		return Kin{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE kin_families SET flag_id = ?, base_update = base_update + 1, updated_utc = ? WHERE kin_index = ?`, flag[:], time.Now().UTC().Format(time.RFC3339Nano), index); err != nil {
		return Kin{}, fmt.Errorf("update kin flag: %w", err)
	}
	rows, err := tx.QueryContext(ctx, `SELECT uin FROM kin_members WHERE kin_index = ?`, index)
	if err != nil {
		return Kin{}, fmt.Errorf("list members for flag update: %w", err)
	}
	var uins []uint32
	for rows.Next() {
		var uin uint32
		if err := rows.Scan(&uin); err != nil {
			rows.Close()
			return Kin{}, fmt.Errorf("scan member for flag update: %w", err)
		}
		uins = append(uins, uin)
	}
	rows.Close()
	kin, err := loadKinTx(ctx, tx, index)
	if err != nil {
		return Kin{}, err
	}
	for _, uin := range uins {
		if err := updateProfileKinTx(ctx, tx, uin, index, kin.Name, flag); err != nil {
			return Kin{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Kin{}, fmt.Errorf("commit update kin flag: %w", err)
	}
	return store.LoadKin(ctx, index)
}

func removeKinMemberTx(ctx context.Context, tx *sql.Tx, uin, index uint32) error {
	result, err := tx.ExecContext(ctx, `DELETE FROM kin_members WHERE kin_index = ? AND uin = ?`, index, uin)
	if err != nil {
		return fmt.Errorf("remove kin member: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrKinNotMember
	}
	if err := updateProfileKinTx(ctx, tx, uin, 0, "", game.KinFlagID{}); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE kin_families SET list_update = list_update + 1, updated_utc = ? WHERE kin_index = ?`, time.Now().UTC().Format(time.RFC3339Nano), index); err != nil {
		return fmt.Errorf("advance kin member list revision: %w", err)
	}
	return nil
}

func addKinMemberTx(ctx context.Context, tx *sql.Tx, kin Kin, uin uint32) error {
	now := uint32(time.Now().Unix())
	if _, err := tx.ExecContext(ctx, `INSERT INTO kin_members(
		kin_index, uin, joined_unix, status, status_time, grade, authority_id, last_login_time
	) VALUES(?, ?, ?, 1, ?, ?, ?, ?)`, kin.Index, uin, now, now, KinAuthorityMember, KinAuthorityMember, now); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return ErrKinAlreadyMember
		}
		return fmt.Errorf("insert kin member: %w", err)
	}
	if err := updateProfileKinTx(ctx, tx, uin, kin.Index, kin.Name, kin.FlagID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM kin_applications WHERE uin = ?`, uin); err != nil {
		return fmt.Errorf("clear accepted kin applications: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM kin_invitations WHERE target_uin = ?`, uin); err != nil {
		return fmt.Errorf("clear accepted kin invitations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE kin_families SET list_update = list_update + 1, updated_utc = ? WHERE kin_index = ?`, time.Now().UTC().Format(time.RFC3339Nano), kin.Index); err != nil {
		return fmt.Errorf("advance kin member list revision: %w", err)
	}
	return nil
}

func requireKinCapacityTx(ctx context.Context, tx *sql.Tx, kin Kin) error {
	capacity := KinMemberCapacity(kin.Status)
	if capacity == 0 || capacity > KinMemberMaximum {
		return fmt.Errorf("kin member capacity %d is invalid", capacity)
	}
	var count uint32
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM kin_members WHERE kin_index = ?`, kin.Index).Scan(&count); err != nil {
		return fmt.Errorf("count kin members: %w", err)
	}
	if count >= capacity {
		return ErrKinMemberLimitReached
	}
	return nil
}

func requirePlayerTx(ctx context.Context, tx *sql.Tx, uin uint32) error {
	var present int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM local_players WHERE uin = ?`, uin).Scan(&present); err != nil {
		return fmt.Errorf("load local player UIN %d: %w", uin, err)
	}
	return nil
}

func requireKinOfficerTx(ctx context.Context, tx *sql.Tx, uin, index uint32) error {
	var authority uint32
	if err := tx.QueryRowContext(ctx, `SELECT authority_id FROM kin_members WHERE kin_index = ? AND uin = ?`, index, uin).Scan(&authority); errors.Is(err, sql.ErrNoRows) {
		return ErrKinNotMember
	} else if err != nil {
		return fmt.Errorf("load kin member authority: %w", err)
	}
	if authority < KinAuthorityOfficer {
		return ErrKinPermissionDenied
	}
	return nil
}

func requireKinOwnerTx(ctx context.Context, tx *sql.Tx, uin, index uint32) error {
	var ownerUIN uint32
	if err := tx.QueryRowContext(ctx, `SELECT owner_uin FROM kin_families WHERE kin_index = ?`, index).Scan(&ownerUIN); errors.Is(err, sql.ErrNoRows) {
		return ErrKinNotFound
	} else if err != nil {
		return fmt.Errorf("load kin owner: %w", err)
	}
	if ownerUIN != uin {
		return ErrKinPermissionDenied
	}
	return nil
}

func loadKinTx(ctx context.Context, tx *sql.Tx, index uint32) (Kin, error) {
	var result Kin
	var flag []byte
	err := tx.QueryRowContext(ctx, `SELECT kin_index, owner_uin, created_unix, status, grade,
		flag_id, name, declaration, title, kin_section, base_update, list_update,
		notification, honor, active_point FROM kin_families WHERE kin_index = ?`, index).Scan(
		&result.Index, &result.OwnerUIN, &result.CreatedUnix, &result.Status, &result.Grade,
		&flag, &result.Name, &result.Declaration, &result.Title, &result.Section,
		&result.BaseUpdate, &result.ListUpdate, &result.Notification, &result.Honor, &result.ActivePoint,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Kin{}, ErrKinNotFound
	}
	if err != nil {
		return Kin{}, fmt.Errorf("load kin %d: %w", index, err)
	}
	if len(flag) != len(result.FlagID) {
		return Kin{}, fmt.Errorf("kin %d flag length %d, want %d", index, len(flag), len(result.FlagID))
	}
	copy(result.FlagID[:], flag)
	return result, nil
}

func updateProfileKinTx(ctx context.Context, tx *sql.Tx, uin, index uint32, name string, flag game.KinFlagID) error {
	profile, err := readProfileRecordTx(ctx, tx, uin)
	if err != nil {
		return err
	}
	profile.KinIndex = index
	profile.KinName = name
	profile.KinFlagID = flag
	return writeProfileRecordTx(ctx, tx, uin, profile)
}

func validateKinText(field, value string, maximum int, allowEmpty bool) error {
	if !allowEmpty && value == "" {
		return fmt.Errorf("kin %s is empty", field)
	}
	encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(value))
	if err != nil {
		return fmt.Errorf("kin %s is not GBK encodable: %w", field, err)
	}
	if len(encoded) > maximum {
		return fmt.Errorf("kin %s encoded length %d exceeds %d", field, len(encoded), maximum)
	}
	return nil
}

func normalizeKinAuthorityTitle(value string) (string, error) {
	parts, err := decodeKinAuthorityTitles(value)
	if err != nil {
		// Schema 20 incorrectly stored an ASCII rendering of the native binary
		// object. Schemas 18/19 used the same rendering but counted a trailing
		// separator. Accept both only as migration inputs.
		parts, err = decodeASCIIKinAuthorityTitles(value)
		if err != nil {
			parts, err = decodeLegacyKinAuthorityTitles(value)
			if err != nil {
				return "", err
			}
		}
	}
	normalized, err := encodeKinAuthorityTitles(parts)
	if err != nil {
		return "", err
	}
	if err := validateKinText("title", normalized, KinTitleMaximum, false); err != nil {
		return "", err
	}
	return normalized, nil
}

func encodeKinAuthorityTitles(titles []string) (string, error) {
	if len(titles) != len(kinAuthorityGrades) {
		return "", fmt.Errorf("kin authority title contains %d positions, want %d", len(titles), len(kinAuthorityGrades))
	}
	for index, title := range titles {
		titles[index] = strings.TrimSpace(title)
		title = titles[index]
		if title == "" {
			return "", fmt.Errorf("kin authority title position %d is empty", index)
		}
		gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(title))
		if err != nil {
			return "", fmt.Errorf("kin authority title position %d is not GBK encodable: %w", index, err)
		}
		// QQTSection's serializer writes strlen(title) and copies exactly that
		// many bytes. It does not append a title separator; the next grade starts
		// immediately after this byte span.
		if len(gbk) == 0 || len(gbk) > 10 {
			return "", fmt.Errorf("kin authority title position %d is %d GBK bytes, want 1..10", index, len(gbk))
		}
	}
	return strings.Join(titles, "\x07") + "\x07", nil
}

func decodeKinAuthorityTitles(value string) ([]string, error) {
	logical := strings.TrimSuffix(value, "\x07")
	parts := strings.Split(logical, "\x07")
	if len(parts) != len(kinAuthorityGrades) {
		return nil, fmt.Errorf("kin authority title contains %d positions, want %d", len(parts), len(kinAuthorityGrades))
	}
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
		gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(parts[index]))
		if err != nil || len(gbk) < 1 || len(gbk) > 10 {
			return nil, fmt.Errorf("kin authority title position %d is invalid", index)
		}
	}
	return parts, nil
}

// decodeASCIIKinAuthorityTitles reads schema 20's mistaken textual rendering
// of QQTSection's binary count/grade/length object.
func decodeASCIIKinAuthorityTitles(value string) ([]string, error) {
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(value))
	if err != nil {
		return nil, fmt.Errorf("kin authority title is not GBK encodable: %w", err)
	}
	cursor := 0
	readNumber := func() (int, error) {
		start := cursor
		for cursor < len(gbk) && gbk[cursor] >= '0' && gbk[cursor] <= '9' {
			cursor++
		}
		if start == cursor || cursor >= len(gbk) || gbk[cursor] != ' ' {
			return 0, fmt.Errorf("kin authority title has invalid numeric field at GBK byte %d", start)
		}
		number, parseErr := strconv.Atoi(string(gbk[start:cursor]))
		cursor++
		return number, parseErr
	}
	count, err := readNumber()
	if err != nil || count != len(kinAuthorityGrades) {
		return nil, fmt.Errorf("kin authority title count is invalid")
	}
	titles := make([]string, 0, count)
	for index, expectedGrade := range kinAuthorityGrades {
		grade, parseErr := readNumber()
		if parseErr != nil || uint32(grade) != expectedGrade {
			return nil, fmt.Errorf("kin authority title grade %d is invalid", index)
		}
		length, parseErr := readNumber()
		if parseErr != nil || length < 1 || length > 10 || cursor+length > len(gbk) {
			return nil, fmt.Errorf("kin authority title position %d length is invalid", index)
		}
		titleGBK := gbk[cursor : cursor+length]
		decoded, decodeErr := simplifiedchinese.GBK.NewDecoder().Bytes(titleGBK)
		if decodeErr != nil || strings.TrimSpace(string(decoded)) == "" {
			return nil, fmt.Errorf("kin authority title position %d text is invalid", index)
		}
		titles = append(titles, string(decoded))
		cursor += length
	}
	if cursor != len(gbk) {
		return nil, fmt.Errorf("kin authority title has %d trailing GBK bytes", len(gbk)-cursor)
	}
	return titles, nil
}

// decodeLegacyKinAuthorityTitles reads the non-native schema-18/19 form whose
// length included one trailing ASCII space. It is intentionally private to the
// migration normalizer; all newly emitted and persisted values use the exact
// client serializer above.
func decodeLegacyKinAuthorityTitles(value string) ([]string, error) {
	gbk, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(value))
	if err != nil {
		return nil, fmt.Errorf("legacy kin authority title is not GBK encodable: %w", err)
	}
	cursor := 0
	readNumber := func() (int, error) {
		start := cursor
		for cursor < len(gbk) && gbk[cursor] >= '0' && gbk[cursor] <= '9' {
			cursor++
		}
		if start == cursor || cursor >= len(gbk) || gbk[cursor] != ' ' {
			return 0, fmt.Errorf("legacy kin authority title has invalid numeric field at GBK byte %d", start)
		}
		number, parseErr := strconv.Atoi(string(gbk[start:cursor]))
		cursor++
		return number, parseErr
	}
	count, err := readNumber()
	if err != nil || count != len(kinAuthorityGrades) {
		return nil, fmt.Errorf("legacy kin authority title count is invalid")
	}
	titles := make([]string, 0, count)
	for index, expectedGrade := range kinAuthorityGrades {
		grade, parseErr := readNumber()
		if parseErr != nil || uint32(grade) != expectedGrade {
			return nil, fmt.Errorf("legacy kin authority title grade %d is invalid", index)
		}
		length, parseErr := readNumber()
		if parseErr != nil || length < 2 || length > 11 || cursor+length > len(gbk) || gbk[cursor+length-1] != ' ' {
			return nil, fmt.Errorf("legacy kin authority title position %d length is invalid", index)
		}
		decoded, decodeErr := simplifiedchinese.GBK.NewDecoder().Bytes(gbk[cursor : cursor+length-1])
		if decodeErr != nil || strings.TrimSpace(string(decoded)) == "" {
			return nil, fmt.Errorf("legacy kin authority title position %d text is invalid", index)
		}
		titles = append(titles, string(decoded))
		cursor += length
	}
	if cursor != len(gbk) {
		return nil, fmt.Errorf("legacy kin authority title has %d trailing GBK bytes", len(gbk)-cursor)
	}
	return titles, nil
}

func decodeProfileJSON(encoded string, profile *game.PlayerProfile) error {
	return json.Unmarshal([]byte(encoded), profile)
}

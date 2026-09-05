package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"qqtang/internal/protocol/game"
)

const (
	MarriageProposalItemID    uint16 = 9020 // 水晶之恋：原始物品说明为“求婚必备道具”。
	MarriageRingItemFirst     uint16 = 9021 // RingID 9021: 水晶戒指
	MarriageRingItemLast      uint16 = 9026 // RingID 9026: 永恒爱神之戒
	MarriageTextMaximum              = 200
	MarriageInitialLevel      uint16 = 1
	MarriageInitialLevelValue uint32 = 100
	MarriageProposalLifetime         = 10 * time.Minute
)

// Marriage is the authoritative relation shared by exactly two local players.
// Nicknames are projections from local_players rather than duplicated state.
type Marriage struct {
	ID             uint32
	UIN            uint32
	SpouseUIN      uint32
	Nickname       string
	SpouseNickname string
	MarriedUnix    uint32
	Loyalty        uint32
	Level          uint16
	LevelValue     uint32
	LoveWord       string
	RingID         uint32
}

type MarriageProposal struct {
	ProposerUIN      uint32
	TargetUIN        uint32
	ProposerNickname string
	TargetNickname   string
	Message          string
	CreatedUnix      uint32
}

func (store *PlayerStore) projectMarriage(ctx context.Context, uin uint32, profile *game.PlayerProfile) error {
	if profile == nil {
		return fmt.Errorf("profile is nil")
	}
	var spouseUIN uint32
	err := store.db.QueryRowContext(ctx, `SELECT other.uin
		FROM marriage_members self
		JOIN marriage_members other ON other.marriage_id = self.marriage_id AND other.uin <> self.uin
		WHERE self.uin = ?`, uin).Scan(&spouseUIN)
	if errors.Is(err, sql.ErrNoRows) {
		profile.SpouseUIN = 0
		return nil
	}
	if err != nil {
		return fmt.Errorf("load spouse projection for UIN %d: %w", uin, err)
	}
	profile.SpouseUIN = spouseUIN
	return nil
}

func (store *PlayerStore) LoadMarriage(ctx context.Context, uin uint32) (Marriage, error) {
	if store == nil || store.db == nil || uin == 0 {
		return Marriage{}, ErrMarriageNotFound
	}
	return loadMarriageQuery(ctx, store.db, uin)
}

// SynchronizeMarriageRing projects the best active ring owned by either
// spouse into MarriageInfo.RingID. The original client directly formats this
// wire value as ring%d.img, and its object archive contains ring9021.img
// through ring9026.img, so the wire value is the commodity ID itself.
func (store *PlayerStore) SynchronizeMarriageRing(ctx context.Context, uin uint32) (Marriage, error) {
	if store == nil || store.db == nil || uin == 0 {
		return Marriage{}, ErrMarriageNotFound
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Marriage{}, fmt.Errorf("begin synchronize marriage ring: %w", err)
	}
	defer tx.Rollback()
	marriage, err := loadMarriageQuery(ctx, tx, uin)
	if err != nil {
		return Marriage{}, err
	}
	var itemID uint32
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(inventory.item_id), 0)
		FROM marriage_members member
		JOIN player_inventory inventory ON inventory.uin = member.uin
		WHERE member.marriage_id = ?
		  AND inventory.item_id BETWEEN ? AND ?
		  AND inventory.quantity > 0
		  AND inventory.available_period <> 0`,
		marriage.ID, MarriageRingItemFirst, MarriageRingItemLast).Scan(&itemID); err != nil {
		return Marriage{}, fmt.Errorf("resolve marriage %d ring inventory: %w", marriage.ID, err)
	}
	ringID := itemID
	if marriage.RingID != ringID {
		if _, err := tx.ExecContext(ctx, `UPDATE marriages SET ring_id = ? WHERE marriage_id = ?`, ringID, marriage.ID); err != nil {
			return Marriage{}, fmt.Errorf("update marriage %d ring: %w", marriage.ID, err)
		}
		marriage.RingID = ringID
	}
	if err := tx.Commit(); err != nil {
		return Marriage{}, fmt.Errorf("commit marriage ring synchronization: %w", err)
	}
	return marriage, nil
}

type marriageQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadMarriageQuery(ctx context.Context, queryer marriageQueryer, uin uint32) (Marriage, error) {
	var result Marriage
	var ownProfileJSON, spouseProfileJSON string
	err := queryer.QueryRowContext(ctx, `SELECT m.marriage_id, self.uin, other.uin,
		m.married_unix, m.loyalty, m.level, m.level_value, m.love_word, m.ring_id,
		self_player.profile_json, spouse_player.profile_json
		FROM marriage_members self
		JOIN marriage_members other ON other.marriage_id = self.marriage_id AND other.uin <> self.uin
		JOIN marriages m ON m.marriage_id = self.marriage_id
		JOIN local_players self_player ON self_player.uin = self.uin
		JOIN local_players spouse_player ON spouse_player.uin = other.uin
		WHERE self.uin = ?`, uin).Scan(
		&result.ID, &result.UIN, &result.SpouseUIN, &result.MarriedUnix,
		&result.Loyalty, &result.Level, &result.LevelValue, &result.LoveWord, &result.RingID,
		&ownProfileJSON, &spouseProfileJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Marriage{}, ErrMarriageNotFound
	}
	if err != nil {
		return Marriage{}, fmt.Errorf("load marriage for UIN %d: %w", uin, err)
	}
	var own, spouse game.PlayerProfile
	if err := decodeProfileJSON(ownProfileJSON, &own); err != nil {
		return Marriage{}, fmt.Errorf("decode marriage member UIN %d: %w", uin, err)
	}
	if err := decodeProfileJSON(spouseProfileJSON, &spouse); err != nil {
		return Marriage{}, fmt.Errorf("decode marriage spouse UIN %d: %w", result.SpouseUIN, err)
	}
	result.Nickname = own.Nickname
	result.SpouseNickname = spouse.Nickname
	return result, nil
}

func (store *PlayerStore) CreateMarriageProposal(ctx context.Context, proposerUIN, targetUIN uint32, message string) (MarriageProposal, error) {
	message = strings.TrimSpace(message)
	if proposerUIN == 0 || targetUIN == 0 || proposerUIN == targetUIN {
		return MarriageProposal{}, fmt.Errorf("marriage proposal requires two different non-zero UINs")
	}
	if err := validateKinText("marriage proposal", message, MarriageTextMaximum, true); err != nil {
		return MarriageProposal{}, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return MarriageProposal{}, fmt.Errorf("begin marriage proposal: %w", err)
	}
	defer tx.Rollback()
	if err := requirePlayerTx(ctx, tx, proposerUIN); err != nil {
		return MarriageProposal{}, err
	}
	if err := requirePlayerTx(ctx, tx, targetUIN); err != nil {
		return MarriageProposal{}, err
	}
	if qualified, err := hasActiveInventoryItemTx(ctx, tx, proposerUIN, MarriageProposalItemID); err != nil {
		return MarriageProposal{}, err
	} else if !qualified {
		return MarriageProposal{}, ErrMarriageProposalItemMissing
	}
	var occupied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM marriage_members WHERE uin IN (?, ?)`, proposerUIN, targetUIN).Scan(&occupied); err != nil {
		return MarriageProposal{}, fmt.Errorf("check marriage members: %w", err)
	}
	if occupied != 0 {
		return MarriageProposal{}, ErrMarriageAlreadyExists
	}
	now := uint32(time.Now().Unix())
	cutoff := uint32(0)
	if now > uint32(MarriageProposalLifetime/time.Second) {
		cutoff = now - uint32(MarriageProposalLifetime/time.Second)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM marriage_proposals WHERE created_unix < ?`, cutoff); err != nil {
		return MarriageProposal{}, fmt.Errorf("expire marriage proposals: %w", err)
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM marriage_proposals
		WHERE proposer_uin IN (?, ?) OR target_uin IN (?, ?)`, proposerUIN, targetUIN, proposerUIN, targetUIN).Scan(&occupied); err != nil {
		return MarriageProposal{}, fmt.Errorf("check pending marriage proposals: %w", err)
	}
	if occupied != 0 {
		return MarriageProposal{}, ErrMarriageProposalBusy
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO marriage_proposals(proposer_uin, target_uin, message, created_unix)
		VALUES(?, ?, ?, ?)`, proposerUIN, targetUIN, message, now); err != nil {
		return MarriageProposal{}, fmt.Errorf("save marriage proposal: %w", err)
	}
	proposal, err := loadMarriageProposalQuery(ctx, tx, proposerUIN, targetUIN)
	if err != nil {
		return MarriageProposal{}, err
	}
	if err := tx.Commit(); err != nil {
		return MarriageProposal{}, fmt.Errorf("commit marriage proposal: %w", err)
	}
	return proposal, nil
}

func loadMarriageProposalQuery(ctx context.Context, queryer marriageQueryer, proposerUIN, targetUIN uint32) (MarriageProposal, error) {
	var result MarriageProposal
	var proposerJSON, targetJSON string
	err := queryer.QueryRowContext(ctx, `SELECT p.proposer_uin, p.target_uin, p.message, p.created_unix,
		proposer.profile_json, target.profile_json
		FROM marriage_proposals p
		JOIN local_players proposer ON proposer.uin = p.proposer_uin
		JOIN local_players target ON target.uin = p.target_uin
		WHERE p.proposer_uin = ? AND p.target_uin = ?`, proposerUIN, targetUIN).Scan(
		&result.ProposerUIN, &result.TargetUIN, &result.Message, &result.CreatedUnix,
		&proposerJSON, &targetJSON,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MarriageProposal{}, ErrMarriageProposalMissing
	}
	if err != nil {
		return MarriageProposal{}, fmt.Errorf("load marriage proposal: %w", err)
	}
	var proposer, target game.PlayerProfile
	if err := decodeProfileJSON(proposerJSON, &proposer); err != nil {
		return MarriageProposal{}, fmt.Errorf("decode proposer profile: %w", err)
	}
	if err := decodeProfileJSON(targetJSON, &target); err != nil {
		return MarriageProposal{}, fmt.Errorf("decode proposal target profile: %w", err)
	}
	result.ProposerNickname = proposer.Nickname
	result.TargetNickname = target.Nickname
	return result, nil
}

func (store *PlayerStore) AnswerMarriageProposal(ctx context.Context, targetUIN, proposerUIN uint32, accepted bool) (Marriage, error) {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Marriage{}, fmt.Errorf("begin answer marriage proposal: %w", err)
	}
	defer tx.Rollback()
	proposal, err := loadMarriageProposalQuery(ctx, tx, proposerUIN, targetUIN)
	if err != nil {
		return Marriage{}, err
	}
	if time.Since(time.Unix(int64(proposal.CreatedUnix), 0)) > MarriageProposalLifetime {
		if _, err := tx.ExecContext(ctx, `DELETE FROM marriage_proposals WHERE proposer_uin = ? AND target_uin = ?`, proposerUIN, targetUIN); err != nil {
			return Marriage{}, fmt.Errorf("expire marriage proposal: %w", err)
		}
		if err := tx.Commit(); err != nil {
			return Marriage{}, fmt.Errorf("commit expired marriage proposal: %w", err)
		}
		return Marriage{}, ErrMarriageProposalMissing
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM marriage_proposals WHERE proposer_uin = ? AND target_uin = ?`, proposerUIN, targetUIN); err != nil {
		return Marriage{}, fmt.Errorf("consume marriage proposal: %w", err)
	}
	if !accepted {
		if err := tx.Commit(); err != nil {
			return Marriage{}, fmt.Errorf("commit rejected marriage proposal: %w", err)
		}
		return Marriage{UIN: targetUIN, SpouseUIN: proposerUIN}, nil
	}
	var occupied int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM marriage_members WHERE uin IN (?, ?)`, proposerUIN, targetUIN).Scan(&occupied); err != nil {
		return Marriage{}, fmt.Errorf("recheck marriage members: %w", err)
	}
	if occupied != 0 {
		return Marriage{}, ErrMarriageAlreadyExists
	}
	now := uint32(time.Now().Unix())
	result, err := tx.ExecContext(ctx, `INSERT INTO marriages(married_unix, loyalty, level, level_value)
		VALUES(?, 0, ?, ?)`, now, MarriageInitialLevel, MarriageInitialLevelValue)
	if err != nil {
		return Marriage{}, fmt.Errorf("create marriage: %w", err)
	}
	id64, err := result.LastInsertId()
	if err != nil || id64 <= 0 || id64 > int64(^uint32(0)) {
		return Marriage{}, fmt.Errorf("read marriage ID: %w", err)
	}
	marriageID := uint32(id64)
	for index, uin := range []uint32{proposerUIN, targetUIN} {
		if _, err := tx.ExecContext(ctx, `INSERT INTO marriage_members(marriage_id, uin, position) VALUES(?, ?, ?)`, marriageID, uin, index+1); err != nil {
			return Marriage{}, fmt.Errorf("insert marriage member UIN %d: %w", uin, err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM marriage_proposals
		WHERE proposer_uin IN (?, ?) OR target_uin IN (?, ?)`, proposerUIN, targetUIN, proposerUIN, targetUIN); err != nil {
		return Marriage{}, fmt.Errorf("clear related marriage proposals: %w", err)
	}
	marriage, err := loadMarriageQuery(ctx, tx, targetUIN)
	if err != nil {
		return Marriage{}, err
	}
	if err := tx.Commit(); err != nil {
		return Marriage{}, fmt.Errorf("commit accepted marriage proposal: %w", err)
	}
	return marriage, nil
}

func (store *PlayerStore) UpdateMarriageLoveWord(ctx context.Context, uin, spouseUIN uint32, loveWord string) (Marriage, error) {
	loveWord = strings.TrimSpace(loveWord)
	if err := validateKinText("marriage love word", loveWord, MarriageTextMaximum, true); err != nil {
		return Marriage{}, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return Marriage{}, fmt.Errorf("begin update marriage love word: %w", err)
	}
	defer tx.Rollback()
	marriage, err := loadMarriageQuery(ctx, tx, uin)
	if err != nil {
		return Marriage{}, err
	}
	if marriage.SpouseUIN != spouseUIN {
		return Marriage{}, ErrMarriageNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE marriages SET love_word = ? WHERE marriage_id = ?`, loveWord, marriage.ID); err != nil {
		return Marriage{}, fmt.Errorf("update marriage love word: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Marriage{}, fmt.Errorf("commit marriage love word: %w", err)
	}
	return store.LoadMarriage(ctx, uin)
}

func (store *PlayerStore) Divorce(ctx context.Context, uin, spouseUIN uint32) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin divorce: %w", err)
	}
	defer tx.Rollback()
	marriage, err := loadMarriageQuery(ctx, tx, uin)
	if err != nil {
		return err
	}
	if marriage.SpouseUIN != spouseUIN {
		return ErrMarriageNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM marriages WHERE marriage_id = ?`, marriage.ID); err != nil {
		return fmt.Errorf("delete marriage: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit divorce: %w", err)
	}
	return nil
}

func hasActiveInventoryItemTx(ctx context.Context, tx *sql.Tx, uin uint32, itemID uint16) (bool, error) {
	var quantity, availablePeriod uint32
	err := tx.QueryRowContext(ctx, `SELECT quantity, available_period FROM player_inventory WHERE uin = ? AND item_id = ?`, uin, itemID).Scan(&quantity, &availablePeriod)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load inventory item %d for UIN %d: %w", itemID, uin, err)
	}
	return quantity > 0 && availablePeriod != 0, nil
}

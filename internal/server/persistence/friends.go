package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

const (
	FriendMaximumCount       = 200
	FriendRequestTextMaximum = 512
)

// FriendRequest and the resulting list row are both directed. Accepting A's
// request adds B to A's list. The native dialog's optional "also add" checkbox
// sends a second B -> A envelope rather than silently creating reciprocity.
type FriendRequest struct {
	RequesterUIN uint32
	TargetUIN    uint32
	Message      string
	CreatedUnix  uint32
}

func (store *PlayerStore) ListFriends(ctx context.Context, ownerUIN uint32) ([]uint32, error) {
	if store == nil || store.db == nil || ownerUIN == 0 {
		return nil, fmt.Errorf("friend owner UIN must be non-zero")
	}
	rows, err := store.db.QueryContext(ctx, `SELECT friend_uin FROM friendships WHERE owner_uin = ? ORDER BY friend_uin`, ownerUIN)
	if err != nil {
		return nil, fmt.Errorf("list friends for UIN %d: %w", ownerUIN, err)
	}
	defer rows.Close()
	result := make([]uint32, 0)
	for rows.Next() {
		var friendUIN uint32
		if err := rows.Scan(&friendUIN); err != nil {
			return nil, fmt.Errorf("scan friend for UIN %d: %w", ownerUIN, err)
		}
		result = append(result, friendUIN)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate friends for UIN %d: %w", ownerUIN, err)
	}
	return result, nil
}

// ListFriendOwners returns the players whose directed friend list contains
// friendUIN. It is the exact audience for online/offline profile refreshes.
func (store *PlayerStore) ListFriendOwners(ctx context.Context, friendUIN uint32) ([]uint32, error) {
	if store == nil || store.db == nil || friendUIN == 0 {
		return nil, fmt.Errorf("friend UIN must be non-zero")
	}
	rows, err := store.db.QueryContext(ctx, `SELECT owner_uin FROM friendships WHERE friend_uin = ? ORDER BY owner_uin`, friendUIN)
	if err != nil {
		return nil, fmt.Errorf("list friend owners for UIN %d: %w", friendUIN, err)
	}
	defer rows.Close()
	result := make([]uint32, 0)
	for rows.Next() {
		var ownerUIN uint32
		if err := rows.Scan(&ownerUIN); err != nil {
			return nil, fmt.Errorf("scan friend owner for UIN %d: %w", friendUIN, err)
		}
		result = append(result, ownerUIN)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate friend owners for UIN %d: %w", friendUIN, err)
	}
	return result, nil
}

func (store *PlayerStore) RequestFriend(ctx context.Context, requesterUIN, targetUIN uint32, message string) (FriendRequest, error) {
	message = strings.TrimSpace(message)
	if requesterUIN == 0 || targetUIN == 0 || requesterUIN == targetUIN {
		return FriendRequest{}, fmt.Errorf("friend request requires two different non-zero UINs")
	}
	if err := validateFriendText(message); err != nil {
		return FriendRequest{}, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return FriendRequest{}, fmt.Errorf("begin friend request: %w", err)
	}
	defer tx.Rollback()
	if err := requirePlayerTx(ctx, tx, requesterUIN); err != nil {
		return FriendRequest{}, err
	}
	if err := requirePlayerTx(ctx, tx, targetUIN); err != nil {
		return FriendRequest{}, err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM friendships WHERE owner_uin = ?`, requesterUIN).Scan(&count); err != nil {
		return FriendRequest{}, fmt.Errorf("count friends for UIN %d: %w", requesterUIN, err)
	}
	if count >= FriendMaximumCount {
		return FriendRequest{}, ErrFriendLimitReached
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM friendships WHERE owner_uin = ? AND friend_uin = ?`, requesterUIN, targetUIN).Scan(&exists); err != nil {
		return FriendRequest{}, fmt.Errorf("check friend relation: %w", err)
	}
	if exists != 0 {
		return FriendRequest{}, ErrFriendAlreadyExists
	}
	now := uint32(time.Now().Unix())
	if _, err := tx.ExecContext(ctx, `INSERT INTO friend_requests(requester_uin, target_uin, message, created_unix)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(requester_uin, target_uin) DO UPDATE SET message = excluded.message, created_unix = excluded.created_unix`,
		requesterUIN, targetUIN, message, now); err != nil {
		return FriendRequest{}, fmt.Errorf("save friend request: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return FriendRequest{}, fmt.Errorf("commit friend request: %w", err)
	}
	return FriendRequest{RequesterUIN: requesterUIN, TargetUIN: targetUIN, Message: message, CreatedUnix: now}, nil
}

// AnswerFriendRequest returns the requester whose directed list changed.
func (store *PlayerStore) AnswerFriendRequest(ctx context.Context, responderUIN, requesterUIN uint32, accepted bool) ([]uint32, error) {
	if responderUIN == 0 || requesterUIN == 0 || responderUIN == requesterUIN {
		return nil, fmt.Errorf("friend answer requires two different non-zero UINs")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin friend answer: %w", err)
	}
	defer tx.Rollback()
	var pending int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM friend_requests WHERE requester_uin = ? AND target_uin = ?`, requesterUIN, responderUIN).Scan(&pending); err != nil {
		return nil, fmt.Errorf("load friend request: %w", err)
	}
	if pending == 0 {
		return nil, ErrFriendRequestMissing
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM friend_requests
		WHERE requester_uin = ? AND target_uin = ?`, requesterUIN, responderUIN); err != nil {
		return nil, fmt.Errorf("consume friend request: %w", err)
	}
	if !accepted {
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit rejected friend request: %w", err)
		}
		return nil, nil
	}
	if err := ensureFriendCapacityTx(ctx, tx, requesterUIN); err != nil {
		return nil, err
	}
	now := uint32(time.Now().Unix())
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO friendships(owner_uin, friend_uin, created_unix) VALUES(?, ?, ?)`, requesterUIN, responderUIN, now); err != nil {
		return nil, fmt.Errorf("create friend relation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit accepted friend request: %w", err)
	}
	return []uint32{requesterUIN}, nil
}

func (store *PlayerStore) RemoveFriend(ctx context.Context, ownerUIN, friendUIN uint32) error {
	if store == nil || store.db == nil || ownerUIN == 0 || friendUIN == 0 || ownerUIN == friendUIN {
		return ErrFriendNotFound
	}
	result, err := store.db.ExecContext(ctx, `DELETE FROM friendships
		WHERE owner_uin = ? AND friend_uin = ?`, ownerUIN, friendUIN)
	if err != nil {
		return fmt.Errorf("remove friend %d from UIN %d: %w", friendUIN, ownerUIN, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read removed friend count: %w", err)
	}
	if count == 0 {
		return ErrFriendNotFound
	}
	return nil
}

func ensureFriendCapacityTx(ctx context.Context, tx *sql.Tx, ownerUIN uint32) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM friendships WHERE owner_uin = ?`, ownerUIN).Scan(&count); err != nil {
		return fmt.Errorf("count friends for UIN %d: %w", ownerUIN, err)
	}
	if count >= FriendMaximumCount {
		return ErrFriendLimitReached
	}
	return nil
}

func validateFriendText(message string) error {
	encoded, _, err := transform.String(simplifiedchinese.GBK.NewEncoder(), message)
	if err != nil {
		return fmt.Errorf("friend request text cannot be represented in GBK: %w", err)
	}
	if len(encoded) > FriendRequestTextMaximum {
		return fmt.Errorf("friend request text is %d GBK bytes, maximum is %d", len(encoded), FriendRequestTextMaximum)
	}
	return nil
}

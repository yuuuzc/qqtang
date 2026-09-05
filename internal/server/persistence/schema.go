package persistence

import (
	"context"
	"fmt"
)

// normalize ensures that a new database has the final player-store schema.
// Existing player databases are already on this schema, so startup no longer
// carries or executes historical data migrations.
func (store *PlayerStore) normalize(ctx context.Context) error {
	for _, statement := range []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA foreign_keys = ON`,
		`PRAGMA busy_timeout = 5000`,
	} {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure SQLite player store: %w", err)
		}
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SQLite player-store normalization: %w", err)
	}
	defer tx.Rollback()

	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS local_players (
			uin INTEGER PRIMARY KEY CHECK (uin > 0 AND uin <= 4294967295),
			profile_json TEXT NOT NULL,
			created_utc TEXT NOT NULL,
			updated_utc TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS player_inventory (
			uin INTEGER NOT NULL REFERENCES local_players(uin) ON DELETE CASCADE,
			item_id INTEGER NOT NULL CHECK (item_id > 0 AND item_id <= 65535),
			quantity INTEGER NOT NULL CHECK (quantity >= 0 AND quantity <= 4294967295),
			item_status INTEGER NOT NULL CHECK (item_status >= 0 AND item_status <= 255),
			item_role_id INTEGER NOT NULL CHECK (item_role_id >= 0 AND item_role_id <= 255),
			item_effect INTEGER NOT NULL CHECK (item_effect >= 0 AND item_effect <= 255),
			item_color INTEGER NOT NULL CHECK (item_color >= 0 AND item_color <= 255),
			buy_time INTEGER NOT NULL CHECK (buy_time >= 0 AND buy_time <= 4294967295),
			available_period INTEGER NOT NULL CHECK (available_period >= 0 AND available_period <= 4294967295),
			PRIMARY KEY (uin, item_id)
		)`,
		`CREATE TABLE IF NOT EXISTS player_loadouts (
			uin INTEGER NOT NULL REFERENCES local_players(uin) ON DELETE CASCADE,
			role_id INTEGER NOT NULL CHECK (role_id > 0 AND role_id <= 255),
			slot TEXT NOT NULL CHECK (length(slot) > 0 AND length(slot) <= 32),
			item_id INTEGER NOT NULL CHECK (item_id > 0 AND item_id <= 65535),
			PRIMARY KEY (uin, role_id, slot)
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS player_loadouts_unique_item ON player_loadouts(uin, item_id)`,
		`CREATE TABLE IF NOT EXISTS player_seed_history (
			uin INTEGER NOT NULL REFERENCES local_players(uin) ON DELETE CASCADE,
			seed_key TEXT NOT NULL CHECK (length(seed_key) > 0 AND length(seed_key) <= 128),
			applied_utc TEXT NOT NULL,
			PRIMARY KEY (uin, seed_key)
		)`,
		`CREATE TABLE IF NOT EXISTS player_credentials (
			uin INTEGER PRIMARY KEY REFERENCES local_players(uin) ON DELETE CASCADE,
			iterations INTEGER NOT NULL CHECK (iterations >= 100000 AND iterations <= 2000000),
			salt BLOB NOT NULL CHECK (length(salt) >= 16 AND length(salt) <= 64),
			password_verifier BLOB NOT NULL CHECK (length(password_verifier) = 32),
			updated_utc TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS player_pets (
			pet_id INTEGER PRIMARY KEY AUTOINCREMENT CHECK (pet_id > 0 AND pet_id <= 4294967295),
			uin INTEGER NOT NULL REFERENCES local_players(uin) ON DELETE CASCADE,
			pet_type_id INTEGER NOT NULL CHECK (pet_type_id > 0 AND pet_type_id <= 4294967295),
			experience INTEGER NOT NULL CHECK (experience >= 0 AND experience <= 4294967295),
			loyalty INTEGER NOT NULL CHECK (loyalty >= 0 AND loyalty <= 4294967295),
			level INTEGER NOT NULL CHECK (level > 0 AND level <= 65535),
			mood INTEGER NOT NULL CHECK (mood >= 0 AND mood <= 65535),
			state INTEGER NOT NULL CHECK (state IN (1, 2)),
			name TEXT NOT NULL CHECK (length(name) <= 12),
			skills BLOB NOT NULL CHECK (length(skills) <= 20),
			UNIQUE (uin, pet_type_id)
		)`,
		`CREATE INDEX IF NOT EXISTS player_pets_uin ON player_pets(uin, pet_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS player_pets_one_active ON player_pets(uin) WHERE state = 1`,
		`CREATE TABLE IF NOT EXISTS player_recipes (
			uin INTEGER NOT NULL REFERENCES local_players(uin) ON DELETE CASCADE,
			book_item_id INTEGER NOT NULL CHECK (book_item_id > 0 AND book_item_id <= 65535),
			product_item_id INTEGER NOT NULL CHECK (product_item_id > 0 AND product_item_id <= 65535),
			learned_utc TEXT NOT NULL,
			PRIMARY KEY (uin, product_item_id)
		)`,
		`CREATE TABLE IF NOT EXISTS kin_families (
			kin_index INTEGER PRIMARY KEY AUTOINCREMENT CHECK (kin_index > 0 AND kin_index <= 4294967295),
			owner_uin INTEGER NOT NULL REFERENCES local_players(uin) ON DELETE RESTRICT,
			name TEXT NOT NULL UNIQUE CHECK (length(name) > 0 AND length(name) <= 17),
			declaration TEXT NOT NULL DEFAULT '' CHECK (length(declaration) <= 257),
			title TEXT NOT NULL DEFAULT '' CHECK (length(title) <= 200),
			notification TEXT NOT NULL DEFAULT '' CHECK (length(notification) <= 200),
			status INTEGER NOT NULL DEFAULT 0 CHECK (status >= 0 AND status <= 4294967295),
			grade INTEGER NOT NULL DEFAULT 1 CHECK (grade > 0 AND grade <= 4294967295),
			kin_section INTEGER NOT NULL DEFAULT 0 CHECK (kin_section >= 0 AND kin_section <= 4294967295),
			flag_id BLOB NOT NULL DEFAULT X'0000000000000001' CHECK (length(flag_id) = 8),
			base_update INTEGER NOT NULL DEFAULT 1 CHECK (base_update >= 0 AND base_update <= 4294967295),
			list_update INTEGER NOT NULL DEFAULT 1 CHECK (list_update >= 0 AND list_update <= 4294967295),
			honor INTEGER NOT NULL DEFAULT 0 CHECK (honor >= 0 AND honor <= 4294967295),
			active_point INTEGER NOT NULL DEFAULT 0 CHECK (active_point >= 0 AND active_point <= 4294967295),
			created_unix INTEGER NOT NULL CHECK (created_unix >= 0 AND created_unix <= 4294967295),
			updated_utc TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS kin_members (
			kin_index INTEGER NOT NULL REFERENCES kin_families(kin_index) ON DELETE CASCADE,
			uin INTEGER NOT NULL UNIQUE REFERENCES local_players(uin) ON DELETE CASCADE,
			joined_unix INTEGER NOT NULL CHECK (joined_unix >= 0 AND joined_unix <= 4294967295),
			status INTEGER NOT NULL DEFAULT 1 CHECK (status >= 0 AND status <= 4294967295),
			status_time INTEGER NOT NULL DEFAULT 0 CHECK (status_time >= 0 AND status_time <= 4294967295),
			grade INTEGER NOT NULL DEFAULT 1 CHECK (grade >= 0 AND grade <= 4294967295),
			authority_id INTEGER NOT NULL DEFAULT 0 CHECK (authority_id >= 0 AND authority_id <= 4294967295),
			custom_title TEXT NOT NULL DEFAULT '' CHECK (length(custom_title) <= 200),
			online_time INTEGER NOT NULL DEFAULT 0 CHECK (online_time >= 0 AND online_time <= 4294967295),
			last_login_time INTEGER NOT NULL DEFAULT 0 CHECK (last_login_time >= 0 AND last_login_time <= 4294967295),
			honor INTEGER NOT NULL DEFAULT 0 CHECK (honor >= 0 AND honor <= 4294967295),
			active_point INTEGER NOT NULL DEFAULT 0 CHECK (active_point >= 0 AND active_point <= 4294967295),
			PRIMARY KEY (kin_index, uin)
		)`,
		`CREATE INDEX IF NOT EXISTS kin_members_family ON kin_members(kin_index, authority_id DESC, joined_unix, uin)`,
		`CREATE TABLE IF NOT EXISTS kin_applications (
			kin_index INTEGER NOT NULL REFERENCES kin_families(kin_index) ON DELETE CASCADE,
			uin INTEGER NOT NULL REFERENCES local_players(uin) ON DELETE CASCADE,
			applied_unix INTEGER NOT NULL CHECK (applied_unix >= 0 AND applied_unix <= 4294967295),
			PRIMARY KEY (kin_index, uin)
		)`,
		`CREATE TABLE IF NOT EXISTS kin_invitations (
			inviter_uin INTEGER NOT NULL REFERENCES local_players(uin) ON DELETE CASCADE,
			target_uin INTEGER NOT NULL UNIQUE REFERENCES local_players(uin) ON DELETE CASCADE,
			kin_index INTEGER NOT NULL REFERENCES kin_families(kin_index) ON DELETE CASCADE,
			created_unix INTEGER NOT NULL CHECK (created_unix >= 0 AND created_unix <= 4294967295),
			PRIMARY KEY (inviter_uin, target_uin, kin_index),
			CHECK (inviter_uin <> target_uin)
		)`,
		`CREATE INDEX IF NOT EXISTS kin_invitations_family ON kin_invitations(kin_index, created_unix)`,
		`CREATE TABLE IF NOT EXISTS marriages (
			marriage_id INTEGER PRIMARY KEY AUTOINCREMENT CHECK (marriage_id > 0 AND marriage_id <= 4294967295),
			married_unix INTEGER NOT NULL CHECK (married_unix >= 0 AND married_unix <= 4294967295),
			loyalty INTEGER NOT NULL DEFAULT 0 CHECK (loyalty >= 0 AND loyalty <= 4294967295),
			level INTEGER NOT NULL DEFAULT 1 CHECK (level > 0 AND level <= 65535),
			level_value INTEGER NOT NULL DEFAULT 100 CHECK (level_value > 0 AND level_value <= 4294967295),
			love_word TEXT NOT NULL DEFAULT '' CHECK (length(love_word) <= 200),
			ring_id INTEGER NOT NULL DEFAULT 0 CHECK (ring_id >= 0 AND ring_id <= 4294967295)
		)`,
		`CREATE TABLE IF NOT EXISTS marriage_members (
			marriage_id INTEGER NOT NULL REFERENCES marriages(marriage_id) ON DELETE CASCADE,
			uin INTEGER NOT NULL UNIQUE REFERENCES local_players(uin) ON DELETE CASCADE,
			position INTEGER NOT NULL CHECK (position IN (1, 2)),
			PRIMARY KEY (marriage_id, position),
			UNIQUE (marriage_id, uin)
		)`,
		`CREATE INDEX IF NOT EXISTS marriage_members_marriage ON marriage_members(marriage_id, position)`,
		`CREATE TRIGGER IF NOT EXISTS marriage_delete_after_member
			AFTER DELETE ON marriage_members
			BEGIN
				DELETE FROM marriages WHERE marriage_id = OLD.marriage_id;
			END`,
		`CREATE TABLE IF NOT EXISTS marriage_proposals (
			proposer_uin INTEGER PRIMARY KEY REFERENCES local_players(uin) ON DELETE CASCADE,
			target_uin INTEGER NOT NULL UNIQUE REFERENCES local_players(uin) ON DELETE CASCADE,
			message TEXT NOT NULL DEFAULT '' CHECK (length(message) <= 200),
			created_unix INTEGER NOT NULL CHECK (created_unix >= 0 AND created_unix <= 4294967295),
			CHECK (proposer_uin <> target_uin)
		)`,
		`CREATE TABLE IF NOT EXISTS friendships (
			owner_uin INTEGER NOT NULL REFERENCES local_players(uin) ON DELETE CASCADE,
			friend_uin INTEGER NOT NULL REFERENCES local_players(uin) ON DELETE CASCADE,
			created_unix INTEGER NOT NULL CHECK (created_unix >= 0 AND created_unix <= 4294967295),
			PRIMARY KEY (owner_uin, friend_uin),
			CHECK (owner_uin <> friend_uin)
		)`,
		`CREATE INDEX IF NOT EXISTS friendships_friend ON friendships(friend_uin, owner_uin)`,
		`CREATE TABLE IF NOT EXISTS friend_requests (
			requester_uin INTEGER NOT NULL REFERENCES local_players(uin) ON DELETE CASCADE,
			target_uin INTEGER NOT NULL REFERENCES local_players(uin) ON DELETE CASCADE,
			message TEXT NOT NULL DEFAULT '',
			created_unix INTEGER NOT NULL CHECK (created_unix >= 0 AND created_unix <= 4294967295),
			PRIMARY KEY (requester_uin, target_uin),
			CHECK (requester_uin <> target_uin)
		)`,
		`CREATE INDEX IF NOT EXISTS friend_requests_target ON friend_requests(target_uin, requester_uin)`,
		`DROP TABLE IF EXISTS schema_version`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("normalize SQLite player store: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SQLite player-store normalization: %w", err)
	}
	return nil
}

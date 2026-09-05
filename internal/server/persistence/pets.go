package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"qqtang/internal/game/itemeffect"
	"qqtang/internal/game/petcatalog"
	"qqtang/internal/protocol/game"
)

// ListPets returns the account's independent pet entities. PetTypeID values
// come from PetCfg.ini and intentionally never pass through player_inventory.
func (store *PlayerStore) ListPets(ctx context.Context, uin uint32) ([]game.PetInfo, error) {
	if store == nil || store.db == nil || uin == 0 {
		return nil, fmt.Errorf("pet lookup requires a player store and non-zero UIN")
	}
	rows, err := store.db.QueryContext(ctx, `SELECT pet_id, pet_type_id, experience, loyalty, level, mood, state, name, skills
		FROM player_pets WHERE uin = ? ORDER BY pet_id`, uin)
	if err != nil {
		return nil, fmt.Errorf("load pets for UIN %d: %w", uin, err)
	}
	defer rows.Close()
	pets := make([]game.PetInfo, 0)
	for rows.Next() {
		var petID, petTypeID, experience, loyalty, level, mood, state int64
		var name string
		var skills []byte
		if err := rows.Scan(&petID, &petTypeID, &experience, &loyalty, &level, &mood, &state, &name, &skills); err != nil {
			return nil, fmt.Errorf("scan pet for UIN %d: %w", uin, err)
		}
		pet := game.PetInfo{
			PetID: uint32(petID), PetTypeID: uint32(petTypeID), PetExperience: uint32(experience),
			PetLoyalty: uint32(loyalty), PetLevel: uint16(level), PetMood: uint16(mood),
			PetState: uint16(state), PetName: name, Skills: append([]byte(nil), skills...),
		}
		if err := pet.Validate(); err != nil {
			return nil, fmt.Errorf("validate pet %d for UIN %d: %w", pet.PetID, uin, err)
		}
		pets = append(pets, pet)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pets for UIN %d: %w", uin, err)
	}
	return pets, nil
}

// GrantPet creates one level-one inactive pet, or returns the already-owned
// pet of the same type. The original protocol has a hard capacity of 25.
func (store *PlayerStore) GrantPet(ctx context.Context, uin, petTypeID uint32, name string) (game.PetInfo, error) {
	if store == nil || store.db == nil || uin == 0 || petTypeID == 0 {
		return game.PetInfo{}, fmt.Errorf("pet grant requires a player store, UIN, and pet type ID")
	}
	name = game.DefaultPetName(name)
	seed := game.PetInfo{
		PetID: 1, PetTypeID: petTypeID, PetLevel: 1, PetLoyalty: 1000,
		PetMood: 100, PetState: game.PetStateInactive, PetName: name,
	}
	if err := seed.Validate(); err != nil {
		return game.PetInfo{}, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return game.PetInfo{}, fmt.Errorf("begin pet grant for UIN %d: %w", uin, err)
	}
	defer tx.Rollback()
	var playerExists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM local_players WHERE uin = ?`, uin).Scan(&playerExists); err != nil {
		return game.PetInfo{}, fmt.Errorf("pet player UIN %d is unavailable: %w", uin, err)
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM player_pets WHERE uin = ?`, uin).Scan(&count); err != nil {
		return game.PetInfo{}, fmt.Errorf("count pets for UIN %d: %w", uin, err)
	}
	capacity, err := playerPetCapacityTx(ctx, tx, uin)
	if err != nil {
		return game.PetInfo{}, err
	}
	if count >= capacity {
		return game.PetInfo{}, fmt.Errorf("UIN %d already has its current capacity of %d pets", uin, capacity)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO player_pets(
		uin, pet_type_id, experience, loyalty, level, mood, state, name, skills
	) VALUES(?, ?, 0, 1000, 1, 100, ?, ?, x'')
	ON CONFLICT(uin, pet_type_id) DO NOTHING`, uin, petTypeID, game.PetStateInactive, name); err != nil {
		return game.PetInfo{}, fmt.Errorf("grant pet type %d to UIN %d: %w", petTypeID, uin, err)
	}
	var pet game.PetInfo
	var petID, storedTypeID, experience, loyalty, level, mood, state int64
	var skills []byte
	if err := tx.QueryRowContext(ctx, `SELECT pet_id, pet_type_id, experience, loyalty, level, mood, state, name, skills
		FROM player_pets WHERE uin = ? AND pet_type_id = ?`, uin, petTypeID).
		Scan(&petID, &storedTypeID, &experience, &loyalty, &level, &mood, &state, &pet.PetName, &skills); err != nil {
		return game.PetInfo{}, fmt.Errorf("reload granted pet type %d: %w", petTypeID, err)
	}
	pet.PetID, pet.PetTypeID = uint32(petID), uint32(storedTypeID)
	pet.PetExperience, pet.PetLoyalty = uint32(experience), uint32(loyalty)
	pet.PetLevel, pet.PetMood, pet.PetState = uint16(level), uint16(mood), uint16(state)
	pet.Skills = append([]byte(nil), skills...)
	if err := pet.Validate(); err != nil {
		return game.PetInfo{}, err
	}
	if err := tx.Commit(); err != nil {
		return game.PetInfo{}, fmt.Errorf("commit pet grant for UIN %d: %w", uin, err)
	}
	return pet, nil
}

func (store *PlayerStore) DeletePet(ctx context.Context, uin, petID uint32) (bool, error) {
	if store == nil || store.db == nil || uin == 0 || petID == 0 {
		return false, fmt.Errorf("pet removal requires a player store, UIN, and pet ID")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin pet removal for UIN %d: %w", uin, err)
	}
	defer tx.Rollback()
	var encoded string
	if err := tx.QueryRowContext(ctx, `SELECT profile_json FROM local_players WHERE uin = ?`, uin).Scan(&encoded); err != nil {
		return false, fmt.Errorf("load profile for pet removal from UIN %d: %w", uin, err)
	}
	var profile game.PlayerProfile
	if err := json.Unmarshal([]byte(encoded), &profile); err != nil {
		return false, fmt.Errorf("decode profile for pet removal from UIN %d: %w", uin, err)
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM player_pets WHERE uin = ? AND pet_id = ?`, uin, petID)
	if err != nil {
		return false, fmt.Errorf("remove pet %d from UIN %d: %w", petID, uin, err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read removed pet count: %w", err)
	}
	if count == 0 {
		return false, nil
	}
	if profile.GameInfo.PetID == petID {
		profile.GameInfo.PetID = 0
		profileRecord := profile
		profileRecord.Inventory = nil
		updated, marshalErr := json.Marshal(profileRecord)
		if marshalErr != nil {
			return false, fmt.Errorf("encode profile after pet removal from UIN %d: %w", uin, marshalErr)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE local_players SET profile_json = ?, updated_utc = ? WHERE uin = ?`,
			string(updated), time.Now().UTC().Format(time.RFC3339Nano), uin); err != nil {
			return false, fmt.Errorf("clear equipped pet %d from UIN %d: %w", petID, uin, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit pet removal for UIN %d: %w", uin, err)
	}
	return true, nil
}

// AdoptPetFromInventory consumes one canonical pet-card item and creates the
// independent pet entity in the same SQLite transaction. Rejecting a duplicate
// or a full pet list never consumes the card.
func (store *PlayerStore) AdoptPetFromInventory(ctx context.Context, uin uint32, cardItemID uint16, petTypeID uint32, name string) (game.PetInfo, uint32, error) {
	if store == nil || store.db == nil || uin == 0 || cardItemID == 0 || petTypeID == 0 {
		return game.PetInfo{}, 0, fmt.Errorf("pet adoption requires a player store, UIN, card item ID, and pet type ID")
	}
	name = game.DefaultPetName(name)
	seed := game.PetInfo{
		PetID: 1, PetTypeID: petTypeID, PetLevel: 1, PetLoyalty: 1000,
		PetMood: 100, PetState: game.PetStateInactive, PetName: name,
	}
	if err := seed.Validate(); err != nil {
		return game.PetInfo{}, 0, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("begin pet adoption for UIN %d: %w", uin, err)
	}
	defer tx.Rollback()
	var existing int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM player_pets WHERE uin = ? AND pet_type_id = ?`, uin, petTypeID).Scan(&existing)
	if err == nil {
		return game.PetInfo{}, 0, fmt.Errorf("pet type %d: %w", petTypeID, ErrPetAlreadyOwned)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return game.PetInfo{}, 0, fmt.Errorf("check existing pet type %d: %w", petTypeID, err)
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM player_pets WHERE uin = ?`, uin).Scan(&count); err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("count pets for UIN %d: %w", uin, err)
	}
	capacity, err := playerPetCapacityTx(ctx, tx, uin)
	if err != nil {
		return game.PetInfo{}, 0, err
	}
	if count >= capacity {
		return game.PetInfo{}, 0, fmt.Errorf("UIN %d has %d pets: %w", uin, count, ErrPetCapacityReached)
	}
	remaining, err := consumeInventoryItemTx(ctx, tx, uin, cardItemID)
	if err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("consume pet card %d: %w", cardItemID, err)
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO player_pets(
		uin, pet_type_id, experience, loyalty, level, mood, state, name, skills
	) VALUES(?, ?, 0, 1000, 1, 100, ?, ?, x'')`, uin, petTypeID, game.PetStateInactive, name)
	if err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("create pet type %d for UIN %d: %w", petTypeID, uin, err)
	}
	petID, err := result.LastInsertId()
	if err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("read adopted pet ID: %w", err)
	}
	if petID <= 0 || uint64(petID) > math.MaxUint32 {
		return game.PetInfo{}, 0, fmt.Errorf("adopted pet ID %d is outside uint32", petID)
	}
	pet, err := readPetTx(ctx, tx, uin, uint32(petID))
	if err != nil {
		return game.PetInfo{}, 0, err
	}
	if err = tx.Commit(); err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("commit pet adoption for UIN %d: %w", uin, err)
	}
	return pet, remaining, nil
}

func playerPetCapacityTx(ctx context.Context, tx *sql.Tx, uin uint32) (int, error) {
	capacity := game.PlayerPetsBaseCapacity
	rows, err := tx.QueryContext(ctx, `SELECT item_id FROM player_inventory
		WHERE uin = ? AND quantity > 0 AND item_id IN (?, ?)`,
		uin, itemeffect.PetSlotExpansion10ItemID, itemeffect.PetSlotExpansion20ItemID)
	if err != nil {
		return 0, fmt.Errorf("load pet-slot expansions for UIN %d: %w", uin, err)
	}
	defer rows.Close()
	for rows.Next() {
		var itemID uint32
		if err := rows.Scan(&itemID); err != nil {
			return 0, fmt.Errorf("scan pet-slot expansion for UIN %d: %w", uin, err)
		}
		switch itemID {
		case itemeffect.PetSlotExpansion20ItemID:
			capacity = game.PlayerPetsBaseCapacity + 20
		case itemeffect.PetSlotExpansion10ItemID:
			if capacity < game.PlayerPetsBaseCapacity+10 {
				capacity = game.PlayerPetsBaseCapacity + 10
			}
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate pet-slot expansions for UIN %d: %w", uin, err)
	}
	if capacity > game.PlayerPetsCapacity {
		capacity = game.PlayerPetsCapacity
	}
	return capacity, nil
}

func validatePlayerPetCapacityTx(ctx context.Context, tx *sql.Tx, uin uint32) error {
	capacity, err := playerPetCapacityTx(ctx, tx, uin)
	if err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM player_pets WHERE uin = ?`, uin).Scan(&count); err != nil {
		return fmt.Errorf("count pets for UIN %d capacity validation: %w", uin, err)
	}
	if count > capacity {
		return fmt.Errorf("UIN %d owns %d pets but the resulting capacity is %d: %w", uin, count, capacity, ErrPetCapacityReduction)
	}
	return nil
}

// LearnPetSkillFromInventory consumes one skill book and appends its canonical
// one-byte SkillID atomically. The original client groups skill IDs in runs of
// ten levels and disallows two different skills of the same level.
func (store *PlayerStore) LearnPetSkillFromInventory(ctx context.Context, uin, petID uint32, bookItemID uint16, skillID, skillLevel byte) (game.PetInfo, uint32, error) {
	if store == nil || store.db == nil || uin == 0 || petID == 0 || bookItemID == 0 || skillID == 0 || skillLevel == 0 {
		return game.PetInfo{}, 0, fmt.Errorf("pet skill learning requires a player store, UIN, pet, book, and skill ID")
	}
	if canonicalLevel, valid := petcatalog.LearnedSkillLevel(skillID); !valid || canonicalLevel != skillLevel {
		return game.PetInfo{}, 0, fmt.Errorf("pet skill %d level %d is not a canonical PetCfg learned skill", skillID, skillLevel)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("begin pet skill learning for UIN %d: %w", uin, err)
	}
	defer tx.Rollback()
	pet, err := readPetTx(ctx, tx, uin, petID)
	if err != nil {
		return game.PetInfo{}, 0, err
	}
	for _, known := range pet.Skills {
		if known == skillID {
			return game.PetInfo{}, 0, fmt.Errorf("skill %d: %w", skillID, ErrPetSkillKnown)
		}
		if knownLevel, learned := petcatalog.LearnedSkillLevel(known); learned && knownLevel == skillLevel {
			return game.PetInfo{}, 0, fmt.Errorf("skill level %d (known %d, requested %d): %w", skillLevel, known, skillID, ErrPetSkillLevelOccupied)
		}
	}
	if len(pet.Skills) >= game.PetSkillsSlotSize {
		return game.PetInfo{}, 0, fmt.Errorf("pet %d already has the protocol maximum of %d skills", petID, game.PetSkillsSlotSize)
	}
	remaining, err := consumeInventoryItemTx(ctx, tx, uin, bookItemID)
	if err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("consume pet skill book %d: %w", bookItemID, err)
	}
	pet.Skills = append(pet.Skills, skillID)
	if _, err = tx.ExecContext(ctx, `UPDATE player_pets SET skills = ? WHERE uin = ? AND pet_id = ?`, pet.Skills, uin, petID); err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("persist skill %d for pet %d: %w", skillID, petID, err)
	}
	if err = tx.Commit(); err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("commit pet skill learning for UIN %d: %w", uin, err)
	}
	return pet, remaining, nil
}

// FeedPetFromInventory consumes the food and updates the pet in one SQLite
// transaction. Original client caps are enforced before persisting, and the
// pet cannot gain experience beyond the final PetCfg threshold.
func (store *PlayerStore) FeedPetFromInventory(ctx context.Context, uin, petID uint32, foodItemID uint16, effect petcatalog.FoodEffect, thresholds []uint32) (game.PetInfo, uint32, error) {
	if store == nil || store.db == nil || uin == 0 || petID == 0 || foodItemID == 0 || effect == (petcatalog.FoodEffect{}) {
		return game.PetInfo{}, 0, fmt.Errorf("pet feeding requires a player store, UIN, pet, food, and effect")
	}
	if err := validatePetExperienceThresholds(thresholds); err != nil {
		return game.PetInfo{}, 0, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("begin pet feeding for UIN %d: %w", uin, err)
	}
	defer tx.Rollback()
	pet, err := readPetTx(ctx, tx, uin, petID)
	if err != nil {
		return game.PetInfo{}, 0, err
	}
	remaining, err := consumeInventoryItemTx(ctx, tx, uin, foodItemID)
	if err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("consume pet food %d: %w", foodItemID, err)
	}
	pet.PetExperience = boundedAddUint32(pet.PetExperience, effect.Experience, thresholds[len(thresholds)-1])
	pet.PetLoyalty = boundedAddUint32(pet.PetLoyalty, effect.Loyalty, game.PetMaxLoyalty)
	pet.PetMood = boundedAddUint16(pet.PetMood, effect.Mood, game.PetMaxMood)
	pet.PetLevel = petLevelForExperience(pet.PetExperience, thresholds)
	if _, err = tx.ExecContext(ctx, `UPDATE player_pets SET experience = ?, loyalty = ?, level = ?, mood = ? WHERE uin = ? AND pet_id = ?`,
		pet.PetExperience, pet.PetLoyalty, pet.PetLevel, pet.PetMood, uin, petID); err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("persist feeding for pet %d: %w", petID, err)
	}
	if err = tx.Commit(); err != nil {
		return game.PetInfo{}, 0, fmt.Errorf("commit pet feeding for UIN %d: %w", uin, err)
	}
	return pet, remaining, nil
}

func (store *PlayerStore) SetPetActive(ctx context.Context, uin, petID uint32, active bool) (game.PetInfo, error) {
	if store == nil || store.db == nil || uin == 0 || petID == 0 {
		return game.PetInfo{}, fmt.Errorf("pet state change requires a player store, UIN, and pet ID")
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return game.PetInfo{}, fmt.Errorf("begin pet state change for UIN %d: %w", uin, err)
	}
	defer tx.Rollback()
	pet, err := readPetTx(ctx, tx, uin, petID)
	if err != nil {
		return game.PetInfo{}, err
	}
	profile, err := readProfileRecordTx(ctx, tx, uin)
	if err != nil {
		return game.PetInfo{}, err
	}
	if active {
		if _, err = tx.ExecContext(ctx, `UPDATE player_pets SET state = ? WHERE uin = ?`, game.PetStateInactive, uin); err != nil {
			return game.PetInfo{}, fmt.Errorf("clear active pet for UIN %d: %w", uin, err)
		}
		pet.PetState = game.PetStateActive
		profile.GameInfo.PetID = petID
	} else {
		pet.PetState = game.PetStateInactive
		if profile.GameInfo.PetID == petID {
			profile.GameInfo.PetID = 0
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE player_pets SET state = ? WHERE uin = ? AND pet_id = ?`, pet.PetState, uin, petID); err != nil {
		return game.PetInfo{}, fmt.Errorf("persist pet %d state: %w", petID, err)
	}
	if err = writeProfileRecordTx(ctx, tx, uin, profile); err != nil {
		return game.PetInfo{}, err
	}
	if err = tx.Commit(); err != nil {
		return game.PetInfo{}, fmt.Errorf("commit pet state change for UIN %d: %w", uin, err)
	}
	return pet, nil
}

func (store *PlayerStore) RenamePet(ctx context.Context, uin, petID uint32, name string) (game.PetInfo, error) {
	if store == nil || store.db == nil || uin == 0 || petID == 0 {
		return game.PetInfo{}, fmt.Errorf("pet rename requires a player store, UIN, and pet ID")
	}
	name = strings.TrimSpace(name)
	probe := game.PetInfo{PetID: petID, PetTypeID: 1, PetLevel: 1, PetState: game.PetStateInactive, PetName: name}
	if err := probe.Validate(); err != nil {
		return game.PetInfo{}, err
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return game.PetInfo{}, fmt.Errorf("begin pet rename for UIN %d: %w", uin, err)
	}
	defer tx.Rollback()
	pet, err := readPetTx(ctx, tx, uin, petID)
	if err != nil {
		return game.PetInfo{}, err
	}
	pet.PetName = name
	if _, err = tx.ExecContext(ctx, `UPDATE player_pets SET name = ? WHERE uin = ? AND pet_id = ?`, name, uin, petID); err != nil {
		return game.PetInfo{}, fmt.Errorf("persist pet %d name: %w", petID, err)
	}
	if err = tx.Commit(); err != nil {
		return game.PetInfo{}, fmt.Errorf("commit pet rename for UIN %d: %w", uin, err)
	}
	return pet, nil
}

func validatePetExperienceThresholds(thresholds []uint32) error {
	if len(thresholds) == 0 || thresholds[0] != 0 || len(thresholds) > math.MaxUint16 {
		return fmt.Errorf("pet experience thresholds must begin at zero and be non-empty")
	}
	for index := 1; index < len(thresholds); index++ {
		if thresholds[index] <= thresholds[index-1] {
			return fmt.Errorf("pet experience threshold %d is not greater than its predecessor", index)
		}
	}
	return nil
}

func petLevelForExperience(experience uint32, thresholds []uint32) uint16 {
	level := uint16(1)
	for index := 1; index < len(thresholds); index++ {
		if experience < thresholds[index] {
			break
		}
		level = uint16(index + 1)
	}
	return level
}

func boundedAddUint32(value, increment, maximum uint32) uint32 {
	if value >= maximum || increment >= maximum-value {
		return maximum
	}
	return value + increment
}

func boundedAddUint16(value, increment, maximum uint16) uint16 {
	if value >= maximum || increment >= maximum-value {
		return maximum
	}
	return value + increment
}

func consumeInventoryItemTx(ctx context.Context, tx *sql.Tx, uin uint32, itemID uint16) (uint32, error) {
	var quantity uint64
	if err := tx.QueryRowContext(ctx, `SELECT quantity FROM player_inventory WHERE uin = ? AND item_id = ?`, uin, itemID).Scan(&quantity); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrInventoryItemMissing
		}
		return 0, err
	}
	if quantity == 0 {
		return 0, ErrInventoryItemMissing
	}
	quantity--
	if quantity == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM player_loadouts WHERE uin = ? AND item_id = ?`, uin, itemID); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM player_inventory WHERE uin = ? AND item_id = ?`, uin, itemID); err != nil {
			return 0, err
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE player_inventory SET quantity = ? WHERE uin = ? AND item_id = ?`, quantity, uin, itemID); err != nil {
		return 0, err
	}
	return uint32(quantity), nil
}

func readPetTx(ctx context.Context, tx *sql.Tx, uin, petID uint32) (game.PetInfo, error) {
	var pet game.PetInfo
	var storedPetID, petTypeID, experience, loyalty, level, mood, state int64
	var skills []byte
	err := tx.QueryRowContext(ctx, `SELECT pet_id, pet_type_id, experience, loyalty, level, mood, state, name, skills
		FROM player_pets WHERE uin = ? AND pet_id = ?`, uin, petID).
		Scan(&storedPetID, &petTypeID, &experience, &loyalty, &level, &mood, &state, &pet.PetName, &skills)
	if errors.Is(err, sql.ErrNoRows) {
		return game.PetInfo{}, fmt.Errorf("pet %d: %w", petID, ErrPetNotFound)
	}
	if err != nil {
		return game.PetInfo{}, fmt.Errorf("load pet %d for UIN %d: %w", petID, uin, err)
	}
	pet.PetID, pet.PetTypeID = uint32(storedPetID), uint32(petTypeID)
	pet.PetExperience, pet.PetLoyalty = uint32(experience), uint32(loyalty)
	pet.PetLevel, pet.PetMood, pet.PetState = uint16(level), uint16(mood), uint16(state)
	pet.Skills = append([]byte(nil), skills...)
	if err := pet.Validate(); err != nil {
		return game.PetInfo{}, err
	}
	return pet, nil
}

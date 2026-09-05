package game

import (
	"fmt"

	"qqtang/internal/protocol/directory"
)

// PlayerProfile contains the local character fields that are currently proven
// to be consumed from the schema-0x07D5 game-login response. It deliberately
// excludes the transport key and the UIN: the key remains fixed to the local
// protocol identity, while the UIN is validated against the client's request.
type PlayerProfile struct {
	Nickname             string         `json:"nickname"`
	Gender               byte           `json:"gender"`
	IconID               byte           `json:"icon_id"`
	PlayerID             uint16         `json:"player_id"`
	Identity             uint32         `json:"identity"`
	SectionID            uint16         `json:"section_id"`
	RoomCount            uint16         `json:"room_count"`
	MinimumRoomID        uint16         `json:"minimum_room_id"`
	TutorialCompleted    bool           `json:"tutorial_completed"`
	GameInfo             GameInfo       `json:"game_info"`
	Inventory            []ItemInfo     `json:"inventory"`
	SectionMode          byte           `json:"section_mode"`
	ExtraSectionModeInfo uint32         `json:"extra_section_mode_info"`
	SectionIdentity      uint32         `json:"section_identity"`
	Reason               string         `json:"reason"`
	Honor                uint32         `json:"honor"`
	TaskCount            uint16         `json:"task_count"`
	TaskGrade            uint16         `json:"task_grade"`
	TaskGameCount        uint16         `json:"task_game_count"`
	TaskGameFinished     uint16         `json:"task_game_finished"`
	KinIndex             uint32         `json:"kin_index,omitempty"`
	KinName              string         `json:"kin_name,omitempty"`
	KinFlagID            KinFlagID      `json:"kin_flag_id,omitempty"`
	SpouseUIN            uint32         `json:"spouse_uin,omitempty"`
	PatternPoints        []PatternPoint `json:"pattern_points,omitempty"`
}

func DefaultPlayerProfile() PlayerProfile {
	return PlayerProfile{
		Nickname:          "LocalPlayer",
		PlayerID:          1,
		SectionID:         1,
		MinimumRoomID:     1,
		TutorialCompleted: true,
		GameInfo: GameInfo{
			Money:  99_999_999,
			Degree: 1,
			RoleID: 1,
		},
	}
}

func (profile PlayerProfile) Validate() error {
	if profile.Nickname != "" {
		if _, err := encodeLegacyGBKText(profile.Nickname, PlayerNicknameSlotSize); err != nil {
			return fmt.Errorf("nickname: %w", err)
		}
	}
	if profile.PlayerID == 0 {
		return fmt.Errorf("player_id must be non-zero")
	}
	if profile.SectionID == 0 {
		return fmt.Errorf("section_id must be non-zero")
	}
	if profile.MinimumRoomID == 0 {
		return fmt.Errorf("minimum_room_id must be non-zero")
	}
	if _, err := encodeLegacyGBKText(profile.KinName, playerKinNameMaximum); err != nil {
		return fmt.Errorf("kin_name: %w", err)
	}
	if len(profile.PatternPoints) > patternPointMaxCount {
		return fmt.Errorf("pattern point count %d exceeds %d", len(profile.PatternPoints), patternPointMaxCount)
	}
	if err := profile.GameInfo.ValidateLocalClientBounds(); err != nil {
		return fmt.Errorf("game_info: %w", err)
	}
	seenItems := make(map[uint16]struct{}, len(profile.Inventory))
	for index, item := range profile.Inventory {
		if item.ItemID == 0 {
			return fmt.Errorf("inventory[%d].id must be non-zero", index)
		}
		if _, duplicate := seenItems[item.ItemID]; duplicate {
			return fmt.Errorf("inventory has duplicate item ID %d", item.ItemID)
		}
		seenItems[item.ItemID] = struct{}{}
	}
	if _, err := BuildLocalLoginPayload(1, profile.ToLocalLoginConfig()); err != nil {
		return fmt.Errorf("local player profile: %w", err)
	}
	return nil
}

// ToLocalLoginConfig performs the explicit domain-to-protocol conversion from
// editable JSON profile data to the confirmed RESPONSE_LOGIN fields.
func (profile PlayerProfile) ToLocalLoginConfig() LocalLoginConfig {
	gameInfo := profile.GameInfo
	// QQTSection calls the first-begin tutorial only when all three outcome
	// counters are zero. Preserve user-authored statistics; otherwise use one
	// draw as the neutral, proven completion marker.
	if profile.TutorialCompleted {
		gameInfo = gameInfo.WithTutorialCompletionMarker()
	}
	items := append([]ItemInfo(nil), profile.Inventory...)
	for index := range items {
		// Local inventory never expires. Besides keeping the storage policy
		// simple, the original 0xffffffff sentinel makes QQTSection report a
		// negative leave-time so its shop UI renders stack quantities.
		if items[index].NumOfItem > 0 {
			items[index].AvailPeriod = LocalPermanentAvailablePeriod
		}
	}
	return LocalLoginConfig{
		PlayerID:             profile.PlayerID,
		Identity:             profile.Identity,
		SectionID:            profile.SectionID,
		KeyGameData:          append([]byte(nil), directory.LocalKey...),
		NumOfRoom:            profile.RoomCount,
		MinRoomID:            profile.MinimumRoomID,
		GameInfo:             gameInfo,
		Items:                items,
		SectionMode:          profile.SectionMode,
		ExtraSectionModeInfo: profile.ExtraSectionModeInfo,
		SectionIdentity:      profile.SectionIdentity,
		Reason:               []byte(profile.Reason),
		Honor:                profile.Honor,
		TaskCount:            profile.TaskCount,
		TaskGrade:            profile.TaskGrade,
		TaskGameCount:        profile.TaskGameCount,
		TaskGameFinished:     profile.TaskGameFinished,
		Patterns:             append([]PatternPoint(nil), profile.PatternPoints...),
	}
}

// ToClientLoginConfig returns the complete client inventory. Local room-control
// cards must remain in ITEM_INFO so the categorized shop tabs can display them
// and the client's native item cache can retain ownership. Their complete
// itemCFG/commodityCFG registrations also let the two ordinary backpack views
// render the shared artwork, name and description instead of a loading tile.
func (profile PlayerProfile) ToClientLoginConfig() LocalLoginConfig {
	return profile.ToLocalLoginConfig()
}

// SetPermanentInventoryItem updates a typed local inventory entry without
// creating duplicate IDs. A zero quantity removes the item.
func (profile *PlayerProfile) SetPermanentInventoryItem(itemID uint16, quantity uint32) error {
	if profile == nil {
		return fmt.Errorf("player profile is nil")
	}
	if itemID == 0 {
		return fmt.Errorf("inventory item ID must be non-zero")
	}
	for index := range profile.Inventory {
		if profile.Inventory[index].ItemID != itemID {
			continue
		}
		if quantity == 0 {
			profile.Inventory = append(profile.Inventory[:index], profile.Inventory[index+1:]...)
			return nil
		}
		// Quantity changes (for example settlement rewards) must not silently
		// unequip an item or erase its role/effect metadata.
		profile.Inventory[index].NumOfItem = quantity
		profile.Inventory[index].AvailPeriod = LocalPermanentAvailablePeriod
		return nil
	}
	if quantity > 0 {
		profile.Inventory = append(profile.Inventory, NewPermanentItemInfo(itemID, quantity))
	}
	return nil
}

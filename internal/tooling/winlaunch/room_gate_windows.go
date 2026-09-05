package winlaunch

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"syscall"
	"time"

	"qqtang/internal/protocol/game"
)

const (
	qqtSectionRoomModeControllerOffset = uintptr(0x11FDC)
	qqtSectionRoomCurrentUINOffset     = uintptr(0x11FE4)
	qqtSectionRoomModeOffset           = uintptr(0xC8B4)
	qqtSectionRoomObjectPointerOffset  = uintptr(0xC558)
	qqtSectionRoomPlayerOffset         = uintptr(0x9C)
	qqtSectionRoomPlayerStride         = uintptr(0x23E8)
	qqtSectionRoomPlayerCount          = 8
	qqtSectionRoomPlayerReadyOffset    = 0x1B
	qqtSectionRoomPlayerTermSeatOffset = 0x1A
	qqtSectionRoomPlayerIdentityOffset = 0x20
	qqtSectionRoomPlayerGameInfoOffset = 0x24
	qqtSectionRoomInventoryCountOffset = 0x54
	qqtSectionRoomInventoryItemsOffset = 0x56
	qqtSectionRoomInventoryItemSize    = game.ItemInfoBinarySize
	qqtSectionRoomInventoryMaxItems    = game.MaxItemInfoCount
	qqtSectionProfileInventoryOffset   = uintptr(0x1FC)
	qqtSectionProfileItemsOffset       = uintptr(0x1FE)
	qqtSectionProfileIdentityOffset    = uintptr(0x1C8)
	qqtSectionProfileGameInfoOffset    = uintptr(0x1CC)
)

type RoomInventoryItemCapture struct {
	Index int `json:"index"`
	game.ItemInfo
}

type RoomPlayerCapture struct {
	Slot             int                        `json:"slot"`
	RecordAddress    string                     `json:"record_address"`
	HeaderHex        string                     `json:"header_hex"`
	UIN              uint32                     `json:"uin"`
	Ready            uint8                      `json:"ready"`
	TermAndSeat      uint8                      `json:"term_and_seat"`
	TeamColor        uint8                      `json:"team_color"`
	SeatIndex        uint8                      `json:"seat_index"`
	Identity         uint32                     `json:"identity"`
	GameInfo         game.GameInfo              `json:"game_info"`
	InventoryCount   uint16                     `json:"inventory_count"`
	InventoryItems   []RoomInventoryItemCapture `json:"inventory_items"`
	InventoryReadErr string                     `json:"inventory_read_error,omitempty"`
}

type RoomGateCapture struct {
	PID                          uint32                     `json:"pid"`
	ModuleBase                   string                     `json:"module_base"`
	LobbyObject                  string                     `json:"lobby_object"`
	RoomAvailable                bool                       `json:"room_available"`
	RoomReadError                string                     `json:"room_read_error,omitempty"`
	RoomObjectPointerAddress     string                     `json:"room_object_pointer_address"`
	RoomObject                   string                     `json:"room_object"`
	CurrentUINAddress            string                     `json:"current_uin_address"`
	CurrentUIN                   uint32                     `json:"current_uin"`
	ModeControllerPointerAddress string                     `json:"mode_controller_pointer_address"`
	ModeController               string                     `json:"mode_controller"`
	ModeAddress                  string                     `json:"mode_address"`
	Mode                         uint8                      `json:"mode"`
	OccupiedPlayerCount          int                        `json:"occupied_player_count"`
	LocalPlayerSlot              int                        `json:"local_player_slot"`
	LocalAdventureCardQuantity   uint32                     `json:"local_adventure_card_quantity"`
	ProfileInventoryCount        uint16                     `json:"profile_inventory_count"`
	ProfileIdentity              uint32                     `json:"profile_identity"`
	ProfileGameInfo              game.GameInfo              `json:"profile_game_info"`
	ProfileInventoryItems        []RoomInventoryItemCapture `json:"profile_inventory_items"`
	ProfileAdventureCardQuantity uint32                     `json:"profile_adventure_card_quantity"`
	Players                      []RoomPlayerCapture        `json:"players"`
	Behavior                     string                     `json:"behavior"`
}

// ReadRoomGate reads the exact fields used by QQTSection's local Start button
// gate. It does not modify process memory. Mode 2 permits a single player only
// when the local room-player record contains item ID 99 with positive quantity.
func ReadRoomGate(pid uint32, timeout time.Duration) (RoomGateCapture, error) {
	process, err := syscall.OpenProcess(0x0400|0x0010, false, pid)
	if err != nil {
		return RoomGateCapture{}, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)

	location, err := locateLobbyStage(process, pid, timeout)
	if err != nil {
		return RoomGateCapture{}, err
	}
	capture := RoomGateCapture{
		PID:                   pid,
		ModuleBase:            fmt.Sprintf("0x%08X", location.moduleBase),
		LobbyObject:           fmt.Sprintf("0x%08X", location.lobbyObject),
		LocalPlayerSlot:       -1,
		Players:               make([]RoomPlayerCapture, 0, qqtSectionRoomPlayerCount),
		ProfileInventoryItems: []RoomInventoryItemCapture{},
		Behavior:              "read-only QQTSection profile and room Start-gate snapshot",
	}
	profileIdentityBytes, profileIdentityOK := readRemote(process, location.lobbyObject+qqtSectionProfileIdentityOffset, 4)
	if profileIdentityOK {
		capture.ProfileIdentity = binary.LittleEndian.Uint32(profileIdentityBytes)
	}
	profileGameInfoBytes, profileGameInfoOK := readRemote(process, location.lobbyObject+qqtSectionProfileGameInfoOffset, game.GameInfoNetworkBinarySize)
	if profileGameInfoOK {
		capture.ProfileGameInfo, _ = game.ParseGameInfoMemory(profileGameInfoBytes)
	}
	profileCountBytes, profileCountOK := readRemote(process, location.lobbyObject+qqtSectionProfileInventoryOffset, 2)
	if profileCountOK && len(profileCountBytes) == 2 {
		capture.ProfileInventoryCount = binary.LittleEndian.Uint16(profileCountBytes)
		if capture.ProfileInventoryCount <= qqtSectionRoomInventoryMaxItems && capture.ProfileInventoryCount > 0 {
			profileItemsBytes, itemsOK := readRemote(process, location.lobbyObject+qqtSectionProfileItemsOffset, int(capture.ProfileInventoryCount)*qqtSectionRoomInventoryItemSize)
			if itemsOK {
				capture.ProfileInventoryItems = parseRoomInventoryItems(profileItemsBytes, int(capture.ProfileInventoryCount))
				for _, item := range capture.ProfileInventoryItems {
					if item.ItemID == game.SinglePlayerAdventureCardItemID {
						capture.ProfileAdventureCardQuantity = item.NumOfItem
					}
				}
			}
		}
	}
	roomObjectPointerAddress := location.lobbyObject + qqtSectionRoomObjectPointerOffset
	capture.RoomObjectPointerAddress = fmt.Sprintf("0x%08X", roomObjectPointerAddress)
	roomObjectBytes, ok := readRemote(process, roomObjectPointerAddress, 4)
	if !ok || len(roomObjectBytes) != 4 {
		capture.RoomReadError = fmt.Sprintf("read room object pointer at 0x%08X", roomObjectPointerAddress)
		return capture, nil
	}
	roomObject := uintptr(binary.LittleEndian.Uint32(roomObjectBytes))
	capture.RoomObject = fmt.Sprintf("0x%08X", roomObject)
	if roomObject < 0x10000 {
		capture.RoomReadError = fmt.Sprintf("room object pointer is invalid: 0x%08X", roomObject)
		return capture, nil
	}

	currentUINAddress := roomObject + qqtSectionRoomCurrentUINOffset
	currentUINBytes, ok := readRemote(process, currentUINAddress, 4)
	if !ok || len(currentUINBytes) != 4 {
		return RoomGateCapture{}, fmt.Errorf("read room current UIN at 0x%08X", currentUINAddress)
	}
	currentUIN := binary.LittleEndian.Uint32(currentUINBytes)

	modeControllerPointerAddress := roomObject + qqtSectionRoomModeControllerOffset
	modeControllerBytes, ok := readRemote(process, modeControllerPointerAddress, 4)
	if !ok || len(modeControllerBytes) != 4 {
		return RoomGateCapture{}, fmt.Errorf("read room mode controller pointer at 0x%08X", modeControllerPointerAddress)
	}
	modeController := uintptr(binary.LittleEndian.Uint32(modeControllerBytes))
	if modeController < 0x10000 {
		capture.RoomReadError = fmt.Sprintf("room mode controller pointer is invalid: 0x%08X", modeController)
		return capture, nil
	}
	modeAddress := modeController + qqtSectionRoomModeOffset
	modeBytes, ok := readRemote(process, modeAddress, 1)
	if !ok || len(modeBytes) != 1 {
		return RoomGateCapture{}, fmt.Errorf("read room mode at 0x%08X", modeAddress)
	}

	capture.RoomAvailable = true
	capture.CurrentUINAddress = fmt.Sprintf("0x%08X", currentUINAddress)
	capture.CurrentUIN = currentUIN
	capture.ModeControllerPointerAddress = fmt.Sprintf("0x%08X", modeControllerPointerAddress)
	capture.ModeController = fmt.Sprintf("0x%08X", modeController)
	capture.ModeAddress = fmt.Sprintf("0x%08X", modeAddress)
	capture.Mode = modeBytes[0]

	for slot := 0; slot < qqtSectionRoomPlayerCount; slot++ {
		recordAddress := roomObject + qqtSectionRoomPlayerOffset + uintptr(slot)*qqtSectionRoomPlayerStride
		header, ok := readRemote(process, recordAddress, qqtSectionRoomInventoryItemsOffset)
		if !ok || len(header) != qqtSectionRoomInventoryItemsOffset {
			return RoomGateCapture{}, fmt.Errorf("read room player %d at 0x%08X", slot+1, recordAddress)
		}
		termAndSeat := header[qqtSectionRoomPlayerTermSeatOffset]
		gameInfo, gameInfoErr := game.ParseGameInfoMemory(header[qqtSectionRoomPlayerGameInfoOffset:qqtSectionRoomInventoryCountOffset])
		if gameInfoErr != nil {
			return RoomGateCapture{}, fmt.Errorf("parse room player %d GAME_INFO: %w", slot+1, gameInfoErr)
		}
		player := RoomPlayerCapture{
			Slot:           slot + 1,
			RecordAddress:  fmt.Sprintf("0x%08X", recordAddress),
			HeaderHex:      hex.EncodeToString(header),
			UIN:            binary.LittleEndian.Uint32(header[0:4]),
			Ready:          header[qqtSectionRoomPlayerReadyOffset],
			TermAndSeat:    termAndSeat,
			TeamColor:      termAndSeat >> 4,
			SeatIndex:      termAndSeat & 0x0F,
			Identity:       binary.LittleEndian.Uint32(header[qqtSectionRoomPlayerIdentityOffset:qqtSectionRoomPlayerGameInfoOffset]),
			GameInfo:       gameInfo,
			InventoryCount: binary.LittleEndian.Uint16(header[qqtSectionRoomInventoryCountOffset : qqtSectionRoomInventoryCountOffset+2]),
			InventoryItems: []RoomInventoryItemCapture{},
		}
		if player.UIN != 0 {
			capture.OccupiedPlayerCount++
		}
		if player.InventoryCount > qqtSectionRoomInventoryMaxItems {
			player.InventoryReadErr = fmt.Sprintf("count %d exceeds validated maximum %d", player.InventoryCount, qqtSectionRoomInventoryMaxItems)
		} else if player.InventoryCount > 0 {
			itemBytes, itemOK := readRemote(process, recordAddress+qqtSectionRoomInventoryItemsOffset, int(player.InventoryCount)*qqtSectionRoomInventoryItemSize)
			if !itemOK {
				player.InventoryReadErr = "ReadProcessMemory failed"
			} else {
				player.InventoryItems = parseRoomInventoryItems(itemBytes, int(player.InventoryCount))
			}
		}
		if player.UIN == currentUIN {
			capture.LocalPlayerSlot = player.Slot
			for _, item := range player.InventoryItems {
				if item.ItemID == game.SinglePlayerAdventureCardItemID {
					capture.LocalAdventureCardQuantity = item.NumOfItem
				}
			}
		}
		capture.Players = append(capture.Players, player)
	}
	return capture, nil
}

func parseRoomInventoryItems(data []byte, count int) []RoomInventoryItemCapture {
	items := make([]RoomInventoryItemCapture, 0, count)
	for index := 0; index < count; index++ {
		offset := index * qqtSectionRoomInventoryItemSize
		if offset+qqtSectionRoomInventoryItemSize > len(data) {
			break
		}
		item, err := game.ParseItemInfoMemory(data[offset : offset+qqtSectionRoomInventoryItemSize])
		if err != nil {
			break
		}
		items = append(items, RoomInventoryItemCapture{
			Index:    index,
			ItemInfo: item,
		})
	}
	return items
}

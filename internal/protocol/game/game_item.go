package game

import (
	"encoding/binary"
	"fmt"
)

const (
	gameItemWireSize = 6
	gameItemMaxCount = 64
)

// GameItem is the shared QQT_GAME_ITEM wire object used by death drops and
// item-distribution events. Coordinates are map grid cells, not screen pixels.
type GameItem struct {
	ItemID uint32
	Row    byte
	Col    byte
}

func (item GameItem) appendNetworkBinary(dst []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, item.ItemID)
	return append(dst, item.Row, item.Col)
}

func parseGameItems(encoded []byte, count byte) ([]GameItem, error) {
	if count > gameItemMaxCount {
		return nil, fmt.Errorf("QQT_GAME_ITEM count %d exceeds %d", count, gameItemMaxCount)
	}
	want := int(count) * gameItemWireSize
	if len(encoded) != want {
		return nil, fmt.Errorf("QQT_GAME_ITEM bytes %d, want %d for count %d", len(encoded), want, count)
	}
	items := make([]GameItem, 0, count)
	for offset := 0; offset < len(encoded); offset += gameItemWireSize {
		items = append(items, GameItem{
			ItemID: binary.BigEndian.Uint32(encoded[offset : offset+4]),
			Row:    encoded[offset+4],
			Col:    encoded[offset+5],
		})
	}
	return items, nil
}

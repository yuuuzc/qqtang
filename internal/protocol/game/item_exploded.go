package game

import (
	"encoding/binary"
	"fmt"
)

const itemExplodedMaxCount = 32

// ItemsExplodedEvent mirrors 0x0FBF / NOTIFY_ITEM_BEEXPLODED. Like the third
// bounded vector in 0x0FA4, it removes already-visible scene objects; 0x0FBF
// is the standalone notification used by item-destruction paths outside the
// complete bomb-explosion result.
type ItemsExplodedEvent struct {
	PlayerID uint16
	Time     uint32
	Items    []GameItem
}

func (event ItemsExplodedEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 {
		return nil, fmt.Errorf("NOTIFY_ITEM_BEEXPLODED player ID must be non-zero")
	}
	if len(event.Items) > itemExplodedMaxCount {
		return nil, fmt.Errorf("NOTIFY_ITEM_BEEXPLODED item count %d exceeds %d", len(event.Items), itemExplodedMaxCount)
	}
	body := make([]byte, 0, 7+len(event.Items)*gameItemWireSize)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.Time)
	body = append(body, byte(len(event.Items)))
	for _, item := range event.Items {
		if item.ItemID == 0 {
			return nil, fmt.Errorf("NOTIFY_ITEM_BEEXPLODED contains zero item ID")
		}
		body = item.appendNetworkBinary(body)
	}
	return body, nil
}

func ParseItemsExplodedEvent(event GameEvent) (ItemsExplodedEvent, error) {
	if event.Schema != NotifyItemExploded {
		return ItemsExplodedEvent{}, fmt.Errorf("item-exploded schema 0x%04X, want 0x%04X", event.Schema, NotifyItemExploded)
	}
	if len(event.Body) < 7 {
		return ItemsExplodedEvent{}, fmt.Errorf("NOTIFY_ITEM_BEEXPLODED body length %d is too short", len(event.Body))
	}
	result := ItemsExplodedEvent{
		PlayerID: binary.BigEndian.Uint16(event.Body[:2]),
		Time:     binary.BigEndian.Uint32(event.Body[2:6]),
	}
	if result.PlayerID == 0 {
		return ItemsExplodedEvent{}, fmt.Errorf("NOTIFY_ITEM_BEEXPLODED player ID must be non-zero")
	}
	count := event.Body[6]
	if count > itemExplodedMaxCount {
		return ItemsExplodedEvent{}, fmt.Errorf("NOTIFY_ITEM_BEEXPLODED item count %d exceeds %d", count, itemExplodedMaxCount)
	}
	items, err := parseGameItems(event.Body[7:], count)
	if err != nil {
		return ItemsExplodedEvent{}, fmt.Errorf("NOTIFY_ITEM_BEEXPLODED: %w", err)
	}
	for index, item := range items {
		if item.ItemID == 0 {
			return ItemsExplodedEvent{}, fmt.Errorf("NOTIFY_ITEM_BEEXPLODED item %d has zero ID", index)
		}
	}
	result.Items = items
	return result, nil
}

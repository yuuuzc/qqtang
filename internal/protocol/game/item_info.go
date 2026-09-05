package game

import (
	"encoding/binary"
	"fmt"
)

// ITEM_INFO is the fixed-size inventory entry named by the shipped
// QQTMsgData.bin schema. Network payloads use big endian; the x86 client's
// in-process objects use little endian.
const (
	ItemInfoOffsetItemID      = 0
	ItemInfoOffsetNumOfItem   = ItemInfoOffsetItemID + 2
	ItemInfoOffsetItemStatus  = ItemInfoOffsetNumOfItem + 4
	ItemInfoOffsetItemRoleID  = ItemInfoOffsetItemStatus + 1
	ItemInfoOffsetItemEffect  = ItemInfoOffsetItemRoleID + 1
	ItemInfoOffsetItemColor   = ItemInfoOffsetItemEffect + 1
	ItemInfoOffsetBuyTime     = ItemInfoOffsetItemColor + 1
	ItemInfoOffsetAvailPeriod = ItemInfoOffsetBuyTime + 4
	ItemInfoBinarySize        = ItemInfoOffsetAvailPeriod + 4
	MaxItemInfoCount          = 500

	// ItemStatusAvailable is an owned item shown in the normal inventory or
	// storage list without being active for gameplay or role appearance.
	ItemStatusAvailable byte = 0
	// ItemStatusActive is shared by equipped cosmetics and prepared match
	// items in the legacy ITEM_INFO projection.
	ItemStatusActive byte = 1
	// ItemStatusCollected is the shop's 收藏柜 state. The client sends status
	// 2 when collecting an item and status 0 when restoring it.
	ItemStatusCollected byte = 2
	// ItemStatusLearnedRecipe is the state tested by QQTSection's original
	// recipe lookup before it allows a combine request.
	ItemStatusLearnedRecipe byte = 4
)

// ItemInfo mirrors QQTMsgData.bin's ITEM_INFO names while exposing concise
// JSON names for editable local player profiles.
type ItemInfo struct {
	ItemID      uint16 `json:"id"`
	NumOfItem   uint32 `json:"quantity"`
	ItemStatus  byte   `json:"status,omitempty"`
	ItemRoleID  byte   `json:"role_id,omitempty"`
	ItemEffect  byte   `json:"effect,omitempty"`
	ItemColor   byte   `json:"color,omitempty"`
	BuyTime     uint32 `json:"buy_time,omitempty"`
	AvailPeriod uint32 `json:"available_period,omitempty"`
}

func NewPermanentItemInfo(itemID uint16, quantity uint32) ItemInfo {
	return ItemInfo{
		ItemID:      itemID,
		NumOfItem:   quantity,
		AvailPeriod: LocalPermanentAvailablePeriod,
	}
}

// Active matches QQTSection's generic inventory reconciliation rule: either a
// zero quantity or a zero AvailPeriod invalidates the entry.
func (item ItemInfo) Active() bool {
	return item.NumOfItem > 0 && item.AvailPeriod > 0
}

func (item ItemInfo) AppendNetworkBinary(dst []byte) []byte {
	start := len(dst)
	dst = append(dst, make([]byte, ItemInfoBinarySize)...)
	encodeItemInfo(dst[start:], item, binary.BigEndian)
	return dst
}

func (item ItemInfo) MemoryBinary() [ItemInfoBinarySize]byte {
	var encoded [ItemInfoBinarySize]byte
	encodeItemInfo(encoded[:], item, binary.LittleEndian)
	return encoded
}

func ParseItemInfoNetwork(data []byte) (ItemInfo, error) {
	return parseItemInfo(data, binary.BigEndian, "network")
}

func ParseItemInfoMemory(data []byte) (ItemInfo, error) {
	return parseItemInfo(data, binary.LittleEndian, "client memory")
}

func encodeItemInfo(dst []byte, item ItemInfo, order binary.ByteOrder) {
	order.PutUint16(dst[ItemInfoOffsetItemID:ItemInfoOffsetNumOfItem], item.ItemID)
	order.PutUint32(dst[ItemInfoOffsetNumOfItem:ItemInfoOffsetItemStatus], item.NumOfItem)
	dst[ItemInfoOffsetItemStatus] = item.ItemStatus
	dst[ItemInfoOffsetItemRoleID] = item.ItemRoleID
	dst[ItemInfoOffsetItemEffect] = item.ItemEffect
	dst[ItemInfoOffsetItemColor] = item.ItemColor
	order.PutUint32(dst[ItemInfoOffsetBuyTime:ItemInfoOffsetAvailPeriod], item.BuyTime)
	order.PutUint32(dst[ItemInfoOffsetAvailPeriod:ItemInfoBinarySize], item.AvailPeriod)
}

func parseItemInfo(data []byte, order binary.ByteOrder, source string) (ItemInfo, error) {
	if len(data) < ItemInfoBinarySize {
		return ItemInfo{}, fmt.Errorf("ITEM_INFO %s bytes %d, need %d", source, len(data), ItemInfoBinarySize)
	}
	return ItemInfo{
		ItemID:      order.Uint16(data[ItemInfoOffsetItemID:ItemInfoOffsetNumOfItem]),
		NumOfItem:   order.Uint32(data[ItemInfoOffsetNumOfItem:ItemInfoOffsetItemStatus]),
		ItemStatus:  data[ItemInfoOffsetItemStatus],
		ItemRoleID:  data[ItemInfoOffsetItemRoleID],
		ItemEffect:  data[ItemInfoOffsetItemEffect],
		ItemColor:   data[ItemInfoOffsetItemColor],
		BuyTime:     order.Uint32(data[ItemInfoOffsetBuyTime:ItemInfoOffsetAvailPeriod]),
		AvailPeriod: order.Uint32(data[ItemInfoOffsetAvailPeriod:ItemInfoBinarySize]),
	}, nil
}

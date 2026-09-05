package game

import (
	"encoding/binary"
	"fmt"
	"io"
)

// QQTMsgData.bin defines command 0x00D9 as ID_SMC_NOTIFYPLAYERITEMADD
// and maps it to schema 0x081D / NOTIFY_PLAYER_ITEMADD. Unlike the unrelated
// nine-byte schema 0x03FC, this notification carries a header followed by up
// to twenty complete ITEM_INFO records. QQTSection replaces matching
// inventory records with these absolute ITEM_INFO values.
const (
	PlayerItemAddNotifySchema uint16 = 0x081D
	PlayerItemAddHeaderSize          = 18
	MaxPlayerItemAddItems            = 20
)

// PlayerItemAddNotification mirrors QQTMsgData's NOTIFY_PLAYER_ITEMADD fields.
// SourceUIN equals UIN for a self-originated inventory refresh. CommodityID is
// zero when no shop commodity or gift transaction is associated with it.
type PlayerItemAddNotification struct {
	UIN         uint32
	Time        uint32
	SourceUIN   uint32
	CommodityID uint32
	Items       []ItemInfo
}

func (notification PlayerItemAddNotification) MarshalNetworkBinary() ([]byte, error) {
	if notification.UIN == 0 {
		return nil, fmt.Errorf("NOTIFY_PLAYER_ITEMADD Uin must be non-zero")
	}
	if notification.SourceUIN == 0 {
		return nil, fmt.Errorf("NOTIFY_PLAYER_ITEMADD SrcUin must be non-zero")
	}
	if len(notification.Items) == 0 || len(notification.Items) > MaxPlayerItemAddItems {
		return nil, fmt.Errorf("NOTIFY_PLAYER_ITEMADD item count %d is outside 1..%d", len(notification.Items), MaxPlayerItemAddItems)
	}
	body := make([]byte, PlayerItemAddHeaderSize)
	binary.BigEndian.PutUint32(body[0:4], notification.UIN)
	binary.BigEndian.PutUint32(body[4:8], notification.Time)
	binary.BigEndian.PutUint32(body[8:12], notification.SourceUIN)
	binary.BigEndian.PutUint32(body[12:16], notification.CommodityID)
	binary.BigEndian.PutUint16(body[16:18], uint16(len(notification.Items)))
	for index, item := range notification.Items {
		if item.ItemID == 0 {
			return nil, fmt.Errorf("NOTIFY_PLAYER_ITEMADD Items[%d].ItemID must be non-zero", index)
		}
		body = item.AppendNetworkBinary(body)
	}
	return body, nil
}

func ParsePlayerItemAddNotification(body []byte) (PlayerItemAddNotification, error) {
	if len(body) < PlayerItemAddHeaderSize {
		return PlayerItemAddNotification{}, fmt.Errorf("NOTIFY_PLAYER_ITEMADD body length %d, need at least %d", len(body), PlayerItemAddHeaderSize)
	}
	itemCount := int(binary.BigEndian.Uint16(body[16:18]))
	if itemCount == 0 || itemCount > MaxPlayerItemAddItems {
		return PlayerItemAddNotification{}, fmt.Errorf("NOTIFY_PLAYER_ITEMADD item count %d is outside 1..%d", itemCount, MaxPlayerItemAddItems)
	}
	want := PlayerItemAddHeaderSize + itemCount*ItemInfoBinarySize
	if len(body) != want {
		return PlayerItemAddNotification{}, fmt.Errorf("NOTIFY_PLAYER_ITEMADD body length %d, want %d for %d items", len(body), want, itemCount)
	}
	notification := PlayerItemAddNotification{
		UIN: binary.BigEndian.Uint32(body[0:4]), Time: binary.BigEndian.Uint32(body[4:8]),
		SourceUIN: binary.BigEndian.Uint32(body[8:12]), CommodityID: binary.BigEndian.Uint32(body[12:16]),
		Items: make([]ItemInfo, 0, itemCount),
	}
	if notification.UIN == 0 || notification.SourceUIN == 0 {
		return PlayerItemAddNotification{}, fmt.Errorf("NOTIFY_PLAYER_ITEMADD Uin and SrcUin must be non-zero")
	}
	for index := 0; index < itemCount; index++ {
		offset := PlayerItemAddHeaderSize + index*ItemInfoBinarySize
		item, err := ParseItemInfoNetwork(body[offset : offset+ItemInfoBinarySize])
		if err != nil {
			return PlayerItemAddNotification{}, fmt.Errorf("NOTIFY_PLAYER_ITEMADD Items[%d]: %w", index, err)
		}
		if item.ItemID == 0 {
			return PlayerItemAddNotification{}, fmt.Errorf("NOTIFY_PLAYER_ITEMADD Items[%d].ItemID must be non-zero", index)
		}
		notification.Items = append(notification.Items, item)
	}
	return notification, nil
}

// BuildLocalPlayerItemAddNotification emits the complete 0x081D object on the
// player-profile route. The packet sequences are notification-owned and do
// not inherit the in-match request route.
func BuildLocalPlayerItemAddNotification(requestPacket []byte, notification PlayerItemAddNotification) ([]byte, error) {
	return BuildLocalPlayerItemAddNotificationWithReader(requestPacket, notification, nil)
}

func BuildLocalPlayerItemAddNotificationWithReader(requestPacket []byte, notification PlayerItemAddNotification, entropy io.Reader) ([]byte, error) {
	request, err := decodeLocalPacket(requestPacket)
	if err != nil {
		return nil, err
	}
	body, err := notification.MarshalNetworkBinary()
	if err != nil {
		return nil, err
	}
	return buildLocalNotificationFromRequestWithRoute(
		requestPacket,
		request,
		PlayerItemAddNotifyCommand,
		body,
		localMessageRoute{Route: localPlayerProfileRoute, SectionID: localPlayerProfileSectionID},
		entropy,
	)
}

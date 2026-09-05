package game

import (
	"bytes"
	"encoding/hex"
	"testing"
)

func TestPlayerItemAddNotificationBodyRoundTrip(t *testing.T) {
	item := ItemInfo{
		ItemID: 20043, NumOfItem: 465, ItemStatus: 1,
		ItemRoleID: 2, ItemEffect: 3, ItemColor: 4, AvailPeriod: LocalPermanentAvailablePeriod,
	}
	notification := PlayerItemAddNotification{
		UIN: 1_000_001, Time: 0x01020304, SourceUIN: 1_000_001, Items: []ItemInfo{item},
	}
	body, err := notification.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	want, _ := hex.DecodeString("000f424101020304000f42410000000000014e4b000001d10102030400000000ffffffff")
	if !bytes.Equal(body, want) {
		t.Fatalf("NOTIFY_PLAYER_ITEMADD body = %x, want %x", body, want)
	}
	decoded, err := ParsePlayerItemAddNotification(body)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.UIN != notification.UIN || decoded.Time != notification.Time || decoded.SourceUIN != notification.SourceUIN || len(decoded.Items) != 1 || decoded.Items[0] != item {
		t.Fatalf("decoded NOTIFY_PLAYER_ITEMADD = %+v", decoded)
	}
}

func TestPlayerItemAddNotificationRejectsEmptyItems(t *testing.T) {
	if _, err := (PlayerItemAddNotification{UIN: 1, SourceUIN: 1}).MarshalNetworkBinary(); err == nil {
		t.Fatal("empty item list unexpectedly accepted")
	}
}

func TestBuildLocalPlayerItemAddNotificationUsesProfileRouteAndAbsoluteItemInfo(t *testing.T) {
	plaintext := make([]byte, localInnerHeaderSize)
	// Reproduce an in-match trigger so the test proves the builder does not
	// accidentally inherit its room route and section.
	plaintext[8], plaintext[9] = 0, 3
	plaintext[10], plaintext[11] = 0xff, 0xff
	plaintext[12], plaintext[13] = 0, 1
	request := makeLocalPacketForTest(t, plaintext)
	item := NewPermanentItemInfo(LargeStaminaPotionItemID, 465)
	notification, err := BuildLocalPlayerItemAddNotificationWithReader(request, PlayerItemAddNotification{
		UIN: 1_000_001, Time: 1234, SourceUIN: 1_000_001, Items: []ItemInfo{item},
	}, bytes.NewReader(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectLocalPacket(notification)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Command != PlayerItemAddNotifyCommand || inspection.RouteSequence != 0 || inspection.InnerSequence != 0 {
		t.Fatalf("notification command/sequences = 0x%04X/%d/%d", inspection.Command, inspection.RouteSequence, inspection.InnerSequence)
	}
	if inspection.Route != localPlayerProfileRoute || inspection.SectionID != localPlayerProfileSectionID {
		t.Fatalf("notification route/section = %d/0x%04X", inspection.Route, inspection.SectionID)
	}
	decoded, err := ParsePlayerItemAddNotification(inspection.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Items) != 1 || decoded.Items[0].ItemID != LargeStaminaPotionItemID || decoded.Items[0].NumOfItem != 465 {
		t.Fatalf("decoded notification = %+v", decoded)
	}
}

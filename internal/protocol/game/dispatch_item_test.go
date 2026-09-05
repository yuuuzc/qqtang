package game

import (
	"bytes"
	"testing"
)

func TestDispatchItemDataRoundTrip(t *testing.T) {
	want := DispatchItemData{
		Time: 0x11223344,
		Items: []GameItem{
			{ItemID: 7, Row: 12, Col: 13},
			{ItemID: 8, Row: 22, Col: 23},
		},
		DelayedItems: []DelayedGameItem{
			{ItemID: 101, DispatchTime: 5000, Row: 31, Col: 32},
			{ItemID: 102, DispatchTime: 9000, Row: 41, Col: 42},
		},
	}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	event := GameEvent{Schema: NotifyDispatchItem, Body: body}
	got, err := ParseDispatchItemEvent(event)
	if err != nil {
		t.Fatal(err)
	}
	if got.Time != want.Time || !itemsEqual(got.Items, want.Items) || !delayedItemsEqual(got.DelayedItems, want.DelayedItems) {
		t.Fatalf("dispatch data = %+v, want %+v", got, want)
	}
	encodedAgain, err := got.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encodedAgain, body) {
		t.Fatalf("round-trip bytes = %x, want %x", encodedAgain, body)
	}
}

func TestParseDispatchItemEventRejectsTruncatedParallelCoordinates(t *testing.T) {
	body, err := (DispatchItemData{
		DelayedItems: []DelayedGameItem{{ItemID: 101, DispatchTime: 5000, Row: 1, Col: 2}},
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseDispatchItemEvent(GameEvent{Schema: NotifyDispatchItem, Body: body[:len(body)-1]}); err == nil {
		t.Fatal("truncated delayed Col array unexpectedly decoded")
	}
}

func itemsEqual(left, right []GameItem) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func delayedItemsEqual(left, right []DelayedGameItem) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

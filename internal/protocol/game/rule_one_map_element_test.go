package game

import (
	"bytes"
	"testing"
)

func TestParseOrdinaryRuleMapElementMovePair(t *testing.T) {
	requestBody := []byte{0x00, 0x02, 0x00, 0x00, 0x0d, 0x2f, 0x00, 0x00, 0x0b, 0xbc, 0x01, 0x06, 0x02, 0x00, 0x05}
	request, err := ParseMoveMapElementRequest(GameEvent{Schema: RequestMoveMapElement, Body: requestBody})
	if err != nil {
		t.Fatal(err)
	}
	if request.PlayerID != 2 || request.ClientTime != 0x0d2f || request.ElementID != 3004 || request.Row != 1 || request.Col != 6 || request.Direction != 2 || request.Sequence != 5 {
		t.Fatalf("request = %+v", request)
	}
	encodedRequest, err := request.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encodedRequest, requestBody) {
		t.Fatalf("request round trip = %x, want %x", encodedRequest, requestBody)
	}

	notifyBody := []byte{0x00, 0x02, 0x00, 0x00, 0x0d, 0x4e, 0x00, 0x00, 0x0b, 0xbc, 0x00, 0x01, 0x00, 0x06, 0x02, 0x00, 0x05}
	notify, err := ParseMapElementMovedEvent(GameEvent{Schema: NotifyMapElementMoved, Body: notifyBody})
	if err != nil {
		t.Fatal(err)
	}
	if notify.PlayerID != 2 || notify.ClientTime != 0x0d4e || notify.ElementID != 3004 || notify.Row != 1 || notify.Col != 6 || notify.Direction != 2 || notify.Sequence != 5 {
		t.Fatalf("notify = %+v", notify)
	}
	encoded, err := notify.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, notifyBody) {
		t.Fatalf("notify round trip = %x, want %x", encoded, notifyBody)
	}
}

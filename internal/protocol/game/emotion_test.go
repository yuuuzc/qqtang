package game

import "testing"

func TestUseEmotionEventRoundTripAndIdentityCheck(t *testing.T) {
	original := UseEmotionEvent{UIN: 1_000_001, Time: 123, EmotionID: 7}
	body, err := original.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseUseEmotionEvent(GameEvent{UIN: original.UIN, Schema: NotifyUseEmotion, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if decoded != original {
		t.Fatalf("decoded emotion = %+v, want %+v", decoded, original)
	}
	if _, err := ParseUseEmotionEvent(GameEvent{UIN: 1_000_002, Schema: NotifyUseEmotion, Body: body}); err == nil {
		t.Fatal("emotion body accepted a different wrapper UIN")
	}
}

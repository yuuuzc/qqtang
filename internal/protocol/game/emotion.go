package game

import (
	"encoding/binary"
	"fmt"
)

const UseEmotionBodySize = 12

// UseEmotionEvent mirrors QQTMsgData.bin's NOTIFY_USE_EMOTION (0x1194).
// The old client emits it through REQUEST_PLAY even though the schema name is
// NOTIFY; the game service then relays the direction-specific game event to
// the other participants in the room.
type UseEmotionEvent struct {
	UIN       uint32
	Time      uint32
	EmotionID uint32
}

func ParseUseEmotionEvent(event GameEvent) (UseEmotionEvent, error) {
	if event.Schema != NotifyUseEmotion {
		return UseEmotionEvent{}, fmt.Errorf("use-emotion schema 0x%04X, want 0x%04X", event.Schema, NotifyUseEmotion)
	}
	if len(event.Body) != UseEmotionBodySize {
		return UseEmotionEvent{}, fmt.Errorf("NOTIFY_USE_EMOTION body length %d, want %d", len(event.Body), UseEmotionBodySize)
	}
	decoded := UseEmotionEvent{
		UIN:       binary.BigEndian.Uint32(event.Body[0:4]),
		Time:      binary.BigEndian.Uint32(event.Body[4:8]),
		EmotionID: binary.BigEndian.Uint32(event.Body[8:12]),
	}
	if decoded.UIN == 0 || decoded.UIN != event.UIN {
		return UseEmotionEvent{}, fmt.Errorf("NOTIFY_USE_EMOTION body UIN %d does not match wrapper UIN %d", decoded.UIN, event.UIN)
	}
	if decoded.EmotionID == 0 {
		return UseEmotionEvent{}, fmt.Errorf("NOTIFY_USE_EMOTION emotion ID is zero")
	}
	return decoded, nil
}

func (event UseEmotionEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.UIN == 0 || event.EmotionID == 0 {
		return nil, fmt.Errorf("NOTIFY_USE_EMOTION requires non-zero UIN and emotion ID")
	}
	body := make([]byte, 0, UseEmotionBodySize)
	body = binary.BigEndian.AppendUint32(body, event.UIN)
	body = binary.BigEndian.AppendUint32(body, event.Time)
	body = binary.BigEndian.AppendUint32(body, event.EmotionID)
	return body, nil
}

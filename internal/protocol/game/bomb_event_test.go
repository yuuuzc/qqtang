package game

import (
	"encoding/binary"
	"testing"
)

func TestParsePlayerUseBombEvent(t *testing.T) {
	body := make([]byte, PlayerUseBombBodySize)
	binary.BigEndian.PutUint16(body[0:2], 30001)
	binary.BigEndian.PutUint32(body[2:6], 1234)
	binary.BigEndian.PutUint32(body[6:10], 5678)
	body[10], body[11] = 4, 9
	binary.BigEndian.PutUint16(body[12:14], 3)
	body[14] = 2
	got, err := ParsePlayerUseBombEvent(GameEvent{Schema: PlayerUseBomb, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if got.PlayerID != 30001 || got.BombID != 5678 || got.Row != 4 || got.Column != 9 || got.Power != 3 || got.Property != 2 {
		t.Fatalf("PLAYER_USE_BOMB = %+v", got)
	}
	if _, err = ParsePlayerUseBombEvent(GameEvent{Schema: PlayerUseBomb, Body: body[:len(body)-1]}); err == nil {
		t.Fatal("truncated PLAYER_USE_BOMB was accepted")
	}
}

func TestParseBombExplodeEvent(t *testing.T) {
	body := make([]byte, 7+2*bombExplodeBombWireSize+1+bombExplodeMapElemWireSize+1+3*bombExplodeItemWireSize)
	binary.BigEndian.PutUint16(body[0:2], 30001)
	binary.BigEndian.PutUint32(body[2:6], 4321)
	body[6] = 2
	mapCountOffset := 7 + 2*bombExplodeBombWireSize
	body[mapCountOffset] = 1
	itemCountOffset := mapCountOffset + 1 + bombExplodeMapElemWireSize
	body[itemCountOffset] = 3

	got, err := ParseBombExplodeEvent(GameEvent{Schema: NotifyBombExplode, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if got.PlayerID != 30001 || got.ClientTime != 4321 || got.BombCount != 2 || got.MapElemCount != 1 || got.ItemCount != 3 {
		t.Fatalf("bomb explosion = %+v", got)
	}
	if _, err = ParseBombExplodeEvent(GameEvent{Schema: NotifyBombExplode, Body: body[:len(body)-1]}); err == nil {
		t.Fatal("truncated bomb explosion was accepted")
	}
	body[6] = bombExplodeMaxBombs + 1
	if _, err = ParseBombExplodeEvent(GameEvent{Schema: NotifyBombExplode, Body: body}); err == nil {
		t.Fatal("oversized bomb array was accepted")
	}
}

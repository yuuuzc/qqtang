package game

import (
	"bytes"
	"testing"
)

func TestEatBombActionRoundTrip(t *testing.T) {
	want := EatBombAction{
		PlayerID: 2, Time: 0x1234, PlayerAvatar: 9, PosX: 240, PosY: 360,
		BombPlayerID: 1, BombTime: 0x10203040, BombRow: 12, BombCol: 14,
	}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != eatBombActionWireSize {
		t.Fatalf("eat-bomb body length = %d, want %d", len(body), eatBombActionWireSize)
	}
	got, err := ParseEatBombAction(GameEvent{Schema: RequestEatBomb, Body: body})
	if err != nil || got != want {
		t.Fatalf("eat-bomb round trip = %+v, %v", got, err)
	}
	body[len(body)-1] = 15
	if _, err = ParseEatBombAction(GameEvent{Schema: NotifyPlayerEatBomb, Body: body}); err == nil {
		t.Fatal("eat-bomb accepted a column outside the original board")
	}
}

func TestKickBombActionRoundTrip(t *testing.T) {
	want := KickBombAction{
		BombRowAndCol: 0x35, BombDestRowAndCol: 0x38, BombProp: 2,
		Direction: 3, Power: 4, Time: 0x01020304, PlayerID: 7, BombPower: 9,
	}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != kickBombActionWireSize {
		t.Fatalf("kick-bomb body length = %d, want %d", len(body), kickBombActionWireSize)
	}
	got, err := ParseKickBombAction(GameEvent{Schema: PlayerThrowBomb, Body: body})
	if err != nil || got != want {
		t.Fatalf("kick-bomb round trip = %+v, %v", got, err)
	}
}

func TestProduceItemRoundTrip(t *testing.T) {
	want := ProduceItem{RowAndCol: 0x3a, ItemID: 0x01020304}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, []byte{0x3a, 1, 2, 3, 4}) {
		t.Fatalf("produce-item body = % X", body)
	}
	got, err := ParseProduceItem(GameEvent{Schema: NotifyProduceItem, Body: body})
	if err != nil || got != want {
		t.Fatalf("produce-item round trip = %+v, %v", got, err)
	}
	if _, err = (ProduceItem{RowAndCol: 0xd0, ItemID: 1}).MarshalNetworkBinary(); err == nil {
		t.Fatal("out-of-range produce-item row was accepted")
	}
}

func TestWrestleActionRoundTrip(t *testing.T) {
	want := WrestleAction{SkillType: 3, FirstTeam: 1, PlayerPos: 7, PlayerID: 0x1234, Time: 0x55667788}
	body, err := want.MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, []byte{3, 1, 7, 0x12, 0x34, 0x55, 0x66, 0x77, 0x88}) {
		t.Fatalf("wrestle body = % X", body)
	}
	got, err := ParseWrestleAction(GameEvent{Schema: RequestWrestle, Body: body})
	if err != nil || got != want {
		t.Fatalf("wrestle round trip = %+v, %v", got, err)
	}
}

func TestWrestleActionRejectsWrongSchemaAndPlayer(t *testing.T) {
	if _, err := ParseWrestleAction(GameEvent{Schema: RequestPushMapElement, Body: make([]byte, wrestleActionWireSize)}); err == nil {
		t.Fatal("accepted non-wrestle schema")
	}
	if _, err := ParseWrestleAction(GameEvent{Schema: RequestWrestle, Body: make([]byte, wrestleActionWireSize)}); err == nil {
		t.Fatal("accepted zero player ID")
	}
}

func TestPassiveFunctionProjectionUsesCommonActionEnvelope(t *testing.T) {
	body, err := (PassiveFunctionProjection{
		Rule:     PassiveFunctionRuleKickBomb,
		PlayerID: 0x1234,
		Effects:  PassiveEffectLightning | PassiveEffectSwordArc | PassiveEffectKickImpact,
		Reset:    true,
	}).MarshalNetworkBinary()
	if err != nil {
		t.Fatal(err)
	}
	// Kick-bomb maps accept only the sword and kick bits; the machine-only
	// lightning bit is deliberately removed at the protocol boundary.
	want := []byte{0xfe, 'Q', 'F', 0x82, 0x12, 0x34, 0, 0, 0, 0x06, 0, 0}
	if !bytes.Equal(body, want) {
		t.Fatalf("passive projection body = % X, want % X", body, want)
	}
}

package game

import (
	"encoding/binary"
	"testing"
)

func TestParseNativeRuleEventFamilies(t *testing.T) {
	machine := make([]byte, 14)
	binary.BigEndian.PutUint16(machine[0:2], 2)
	binary.BigEndian.PutUint32(machine[2:6], 1234)
	machine[6] = 1
	binary.BigEndian.PutUint32(machine[7:11], 77)
	machine[11], machine[12], machine[13] = 3, 12, 14
	state, err := ParseMachineBombState(GameEvent{Schema: NotifyMachineCool, Body: machine})
	if err != nil || state.PlayerID != 2 || len(state.Bombs) != 1 || state.Bombs[0].SceneID != 77 {
		t.Fatalf("machine state = %+v, %v", state, err)
	}
	machine = append(machine, make([]byte, dispatchBombWireSize)...)
	machine[6] = 2
	if _, err = ParseMachineBombState(GameEvent{Schema: NotifyMachineCool, Body: machine}); err == nil {
		t.Fatal("COOL accepted more than its one-bomb table capacity")
	}

	push := make([]byte, 26)
	binary.BigEndian.PutUint16(push[0:2], 2)
	push[2], push[3], push[4], push[5] = 3, 4, 12, 14
	binary.BigEndian.PutUint32(push[6:10], 520)
	binary.BigEndian.PutUint32(push[10:14], 600)
	binary.BigEndian.PutUint32(push[14:18], 2000)
	push[18] = 9
	notify, err := ParsePushMapElementNotify(GameEvent{Schema: NotifyPushMapElement, Body: push})
	if err != nil || notify.DestinationRow != 12 || notify.DestinationCol != 14 || notify.Element[0] != 9 {
		t.Fatalf("push notify = %+v, %v", notify, err)
	}
	push[5] = 15
	if _, err = ParsePushMapElementNotify(GameEvent{Schema: NotifyPushMapElement, Body: push}); err == nil {
		t.Fatal("push notify accepted a column outside the original 13x15 board")
	}

	boxes := make([]byte, 1+2*8)
	boxes[0], boxes[1], boxes[9] = 2, 0x11, 0x22
	generated, err := ParseGenerateBoxesEvent(GameEvent{Schema: NotifyGenerateBox, Body: boxes})
	if err != nil || len(generated.Elements) != 2 || generated.Elements[0][0] != 0x11 || generated.Elements[1][0] != 0x22 {
		t.Fatalf("generated boxes = %+v, %v", generated, err)
	}

	fire := make([]byte, 7)
	binary.BigEndian.PutUint16(fire[0:2], 2)
	binary.BigEndian.PutUint32(fire[2:6], 3000)
	fire[6] = 0x0a
	if state, fireErr := ParsePlayerTimedStateEvent(GameEvent{Schema: NotifyFire, Body: fire}); fireErr != nil || state.Value != 0x0a {
		t.Fatalf("box fire = %+v, %v", state, fireErr)
	}
	for _, invalidMask := range []byte{0, 0x10, 0xff} {
		fire[6] = invalidMask
		if _, fireErr := ParsePlayerTimedStateEvent(GameEvent{Schema: NotifyFire, Body: fire}); fireErr == nil {
			t.Fatalf("box fire accepted invalid launcher mask 0x%02X", invalidMask)
		}
	}
}

func TestParseTankBaseHPKeepsSignedWireValue(t *testing.T) {
	body := make([]byte, 10)
	binary.BigEndian.PutUint16(body[0:2], 1)
	binary.BigEndian.PutUint32(body[2:6], 500)
	binary.BigEndian.PutUint16(body[6:8], 3)
	binary.BigEndian.PutUint16(body[8:10], 0xffff)
	change, err := ParseTankBaseHPChangeEvent(GameEvent{Schema: RequestTankBaseHP, Body: body})
	if err != nil || change.ChangeHP != -1 {
		t.Fatalf("tank HP change = %+v, %v", change, err)
	}
}

func TestParsePlayerHarmedEvent(t *testing.T) {
	body := make([]byte, 13)
	binary.BigEndian.PutUint16(body[0:2], 7)
	binary.BigEndian.PutUint32(body[2:6], 4321)
	binary.BigEndian.PutUint16(body[6:8], 240)
	binary.BigEndian.PutUint16(body[8:10], 360)
	body[10] = 1
	binary.BigEndian.PutUint16(body[11:13], 25)
	harmed, err := ParseEntityHarmedEvent(GameEvent{Schema: PlayerBeHarmed, Body: body})
	if err != nil || harmed.ObjectID != 7 || harmed.Time != 4321 || harmed.PosX != 240 || harmed.PosY != 360 || !harmed.IsAvatar || harmed.LossHP != 25 {
		t.Fatalf("player harmed = %+v, %v", harmed, err)
	}
	body[11], body[12] = 0, 0
	zero, err := ParsePlayerHarmedEvent(GameEvent{Schema: PlayerBeHarmed, Body: body})
	if err != nil || zero.LossHP != 0 {
		t.Fatalf("native-durability zero-loss marker = %+v, %v", zero, err)
	}
}

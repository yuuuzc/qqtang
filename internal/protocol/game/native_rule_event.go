package game

import (
	"encoding/binary"
	"fmt"
)

const (
	maximumMachineBombs = 10
	maximumBoxElements  = 20
	maximumBoxPairs     = 16
	maximumStoppedBoxes = 32
)

type MachineLaunchEvent struct {
	FirstTeam byte
	RoleID    byte
	PlayerID  uint16
	Time      uint32
}

type MachineBombState struct {
	PlayerID uint16
	Time     uint32
	Bombs    []DispatchBomb
}

type MapElementWire [8]byte

type BoxWire struct {
	Row, Col         byte
	StopRow, StopCol byte
	MapElementID     uint32
	UniqueID         uint32
}

type PushMapElementRequest struct {
	PlayerID  uint16
	Direction byte
	Power     byte
	Time      uint32
	Element   MapElementWire
}

type PushMapElementNotify struct {
	PlayerID           uint16
	Direction, Power   byte
	DestinationRow     byte
	DestinationCol     byte
	CurrentX, CurrentY uint32
	Time               uint32
	Element            MapElementWire
}

type DestroyBoxRequest struct {
	PlayerID uint16
	Time     uint32
	PosX     uint16
	PosY     uint16
	Element  MapElementWire
}

type GenerateBoxesEvent struct{ Elements []MapElementWire }

type MapElementKnockEvent struct {
	Time   uint32
	First  []MapElementWire
	Second []MapElementWire
}

type MapElementStopEvent struct {
	Time  uint32
	Boxes []BoxWire
}

type BoxStoppedEvent struct {
	PlayerID uint16
	Time     uint32
	Boxes    []BoxWire
}

type PlayerKnockedEvent struct {
	PlayerID uint16
	Time     uint32
	PosX     uint16
	PosY     uint16
	IsAvatar bool
}

// MapElementsExplodedEvent is the variable NOTIFY_MAPELEM_BEEXPLODED body.
// The native rule-8 controller uses it for both participant- and NPC-owned
// collisions, so PlayerID is an entity identity rather than necessarily a
// human player.
type MapElementsExplodedEvent struct {
	PlayerID uint16
	Time     uint32
	Elements []MapElementWire
}

// EntityHarmedEvent is the shared native-durability damage record used by the
// mechanical-world, push-box and competitive Boss rule objects. QQTMsgData
// names the first field PlayerID, but the rule-1/2 consumer also accepts the
// registered NPC object range 30000..30200. ObjectID records that runtime
// meaning. LossHP is mode-dependent: Boss rules use the fixed 500-unit scalar,
// while mechanical/box rules emit zero as a one-layer durability marker. The
// mode-specific server boundary validates that value after structural decode.
type EntityHarmedEvent struct {
	ObjectID uint16
	Time     uint32
	PosX     uint16
	PosY     uint16
	IsAvatar bool
	LossHP   uint16
}

// PlayerHarmedEvent is retained as a source-compatible alias for the older
// name. New dispatch code should use EntityHarmedEvent/ObjectID because the
// target is not necessarily a player.
type PlayerHarmedEvent = EntityHarmedEvent

type PlayerTimedStateEvent struct {
	PlayerID uint16
	Time     uint32
	Value    byte
}

type TankBaseHPChangeEvent struct {
	PlayerID uint16
	Time     uint32
	TeamID   uint16
	ChangeHP int16
}

func ParseMachineLaunchEvent(event GameEvent) (MachineLaunchEvent, error) {
	if event.Schema != RequestLaunchMachine || len(event.Body) != 8 {
		return MachineLaunchEvent{}, fmt.Errorf("machine-launch schema/body 0x%04X/%d, want 0x%04X/8", event.Schema, len(event.Body), RequestLaunchMachine)
	}
	decoded := MachineLaunchEvent{FirstTeam: event.Body[0], RoleID: event.Body[1], PlayerID: binary.BigEndian.Uint16(event.Body[2:4]), Time: binary.BigEndian.Uint32(event.Body[4:8])}
	if decoded.PlayerID == 0 || decoded.RoleID == 0 {
		return MachineLaunchEvent{}, fmt.Errorf("machine-launch player/role IDs %d/%d must be non-zero", decoded.PlayerID, decoded.RoleID)
	}
	return decoded, nil
}

func ParseMachineBombState(event GameEvent) (MachineBombState, error) {
	maximum := maximumMachineBombs
	switch event.Schema {
	case NotifyMachineFree, NotifyMachineMove:
	case NotifyMachineAngry, NotifyMachineCool:
		maximum = 1
	case NotifyMachineGrace:
		maximum = 9
	default:
		return MachineBombState{}, fmt.Errorf("schema 0x%04X is not a machine-bomb state", event.Schema)
	}
	if len(event.Body) < 7 {
		return MachineBombState{}, fmt.Errorf("machine-bomb body length %d is too short", len(event.Body))
	}
	count := int(event.Body[6])
	if count > maximum || len(event.Body) != 7+count*dispatchBombWireSize {
		return MachineBombState{}, fmt.Errorf("machine-bomb count/body %d/%d is invalid (max %d)", count, len(event.Body), maximum)
	}
	decoded := MachineBombState{PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), Time: binary.BigEndian.Uint32(event.Body[2:6]), Bombs: make([]DispatchBomb, count)}
	if decoded.PlayerID == 0 {
		return MachineBombState{}, fmt.Errorf("machine-bomb player ID must be non-zero")
	}
	offset := 7
	for index := range decoded.Bombs {
		decoded.Bombs[index] = DispatchBomb{SceneID: binary.BigEndian.Uint32(event.Body[offset : offset+4]), Prop: event.Body[offset+4], Row: event.Body[offset+5], Col: event.Body[offset+6]}
		bomb := decoded.Bombs[index]
		if bomb.SceneID == 0 || !validCompetitiveCell(bomb.Row, bomb.Col) {
			return MachineBombState{}, fmt.Errorf("machine-bomb[%d] is invalid", index)
		}
		offset += dispatchBombWireSize
	}
	return decoded, nil
}

func ParsePushMapElementRequest(event GameEvent) (PushMapElementRequest, error) {
	if event.Schema != RequestPushMapElement || len(event.Body) != 16 {
		return PushMapElementRequest{}, fmt.Errorf("push-map-element schema/body 0x%04X/%d, want 0x%04X/16", event.Schema, len(event.Body), RequestPushMapElement)
	}
	decoded := PushMapElementRequest{PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), Direction: event.Body[2], Power: event.Body[3], Time: binary.BigEndian.Uint32(event.Body[4:8])}
	copy(decoded.Element[:], event.Body[8:16])
	if decoded.PlayerID == 0 {
		return PushMapElementRequest{}, fmt.Errorf("push-map-element player ID must be non-zero")
	}
	return decoded, nil
}

func ParsePushMapElementNotify(event GameEvent) (PushMapElementNotify, error) {
	if event.Schema != NotifyPushMapElement || len(event.Body) != 26 {
		return PushMapElementNotify{}, fmt.Errorf("notify-push-map-element schema/body 0x%04X/%d, want 0x%04X/26", event.Schema, len(event.Body), NotifyPushMapElement)
	}
	decoded := PushMapElementNotify{
		PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), Direction: event.Body[2], Power: event.Body[3],
		DestinationRow: event.Body[4], DestinationCol: event.Body[5], CurrentX: binary.BigEndian.Uint32(event.Body[6:10]),
		CurrentY: binary.BigEndian.Uint32(event.Body[10:14]), Time: binary.BigEndian.Uint32(event.Body[14:18]),
	}
	copy(decoded.Element[:], event.Body[18:26])
	if decoded.PlayerID == 0 || !validCompetitiveCell(decoded.DestinationRow, decoded.DestinationCol) {
		return PushMapElementNotify{}, fmt.Errorf("notify-push-map-element player/cell %d/%d,%d is invalid", decoded.PlayerID, decoded.DestinationRow, decoded.DestinationCol)
	}
	return decoded, nil
}

func ParseDestroyBoxRequest(event GameEvent) (DestroyBoxRequest, error) {
	if event.Schema != RequestDestroyBox || len(event.Body) != 18 {
		return DestroyBoxRequest{}, fmt.Errorf("destroy-box schema/body 0x%04X/%d, want 0x%04X/18", event.Schema, len(event.Body), RequestDestroyBox)
	}
	decoded := DestroyBoxRequest{PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), Time: binary.BigEndian.Uint32(event.Body[2:6]), PosX: binary.BigEndian.Uint16(event.Body[6:8]), PosY: binary.BigEndian.Uint16(event.Body[8:10])}
	copy(decoded.Element[:], event.Body[10:18])
	if decoded.PlayerID == 0 {
		return DestroyBoxRequest{}, fmt.Errorf("destroy-box player ID must be non-zero")
	}
	return decoded, nil
}

func ParseGenerateBoxesEvent(event GameEvent) (GenerateBoxesEvent, error) {
	if event.Schema != NotifyGenerateBox || len(event.Body) < 1 {
		return GenerateBoxesEvent{}, fmt.Errorf("generate-box schema/body 0x%04X/%d is invalid", event.Schema, len(event.Body))
	}
	count := int(event.Body[0])
	if count > maximumBoxElements || len(event.Body) != 1+count*8 {
		return GenerateBoxesEvent{}, fmt.Errorf("generate-box count/body %d/%d is invalid", count, len(event.Body))
	}
	decoded := GenerateBoxesEvent{Elements: make([]MapElementWire, count)}
	for index := range decoded.Elements {
		copy(decoded.Elements[index][:], event.Body[1+index*8:1+(index+1)*8])
	}
	return decoded, nil
}

func parseBoxWire(body []byte) BoxWire {
	return BoxWire{Row: body[0], Col: body[1], StopRow: body[2], StopCol: body[3], MapElementID: binary.BigEndian.Uint32(body[4:8]), UniqueID: binary.BigEndian.Uint32(body[8:12])}
}

func ParseMapElementKnockEvent(event GameEvent) (MapElementKnockEvent, error) {
	if event.Schema != NotifyMapElementKnock || len(event.Body) < 5 {
		return MapElementKnockEvent{}, fmt.Errorf("map-element-knock schema/body 0x%04X/%d is invalid", event.Schema, len(event.Body))
	}
	count := int(event.Body[4])
	if count > maximumBoxPairs || len(event.Body) != 5+count*16 {
		return MapElementKnockEvent{}, fmt.Errorf("map-element-knock count/body %d/%d is invalid", count, len(event.Body))
	}
	decoded := MapElementKnockEvent{Time: binary.BigEndian.Uint32(event.Body[0:4]), First: make([]MapElementWire, count), Second: make([]MapElementWire, count)}
	for index := 0; index < count; index++ {
		copy(decoded.First[index][:], event.Body[5+index*8:13+index*8])
		copy(decoded.Second[index][:], event.Body[5+count*8+index*8:13+count*8+index*8])
	}
	return decoded, nil
}

func ParseMapElementStopEvent(event GameEvent) (MapElementStopEvent, error) {
	if event.Schema != NotifyMapElementStop || len(event.Body) < 5 {
		return MapElementStopEvent{}, fmt.Errorf("map-element-stop schema/body 0x%04X/%d is invalid", event.Schema, len(event.Body))
	}
	count := int(event.Body[0])
	if count > maximumStoppedBoxes || len(event.Body) != 5+count*12 {
		return MapElementStopEvent{}, fmt.Errorf("map-element-stop count/body %d/%d is invalid", count, len(event.Body))
	}
	decoded := MapElementStopEvent{Time: binary.BigEndian.Uint32(event.Body[1:5]), Boxes: make([]BoxWire, count)}
	for index := range decoded.Boxes {
		decoded.Boxes[index] = parseBoxWire(event.Body[5+index*12 : 17+index*12])
	}
	return decoded, nil
}

func ParsePlayerKnockedEvent(event GameEvent) (PlayerKnockedEvent, error) {
	if (event.Schema != NotifyPlayerKnocked && event.Schema != PlayerKnocked) || len(event.Body) != 11 {
		return PlayerKnockedEvent{}, fmt.Errorf("player-knocked schema/body 0x%04X/%d is invalid", event.Schema, len(event.Body))
	}
	decoded := PlayerKnockedEvent{PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), Time: binary.BigEndian.Uint32(event.Body[2:6]), PosX: binary.BigEndian.Uint16(event.Body[6:8]), PosY: binary.BigEndian.Uint16(event.Body[8:10]), IsAvatar: event.Body[10] != 0}
	if decoded.PlayerID == 0 {
		return PlayerKnockedEvent{}, fmt.Errorf("player-knocked player ID must be non-zero")
	}
	return decoded, nil
}

func ParseMapElementsExplodedEvent(event GameEvent) (MapElementsExplodedEvent, error) {
	if event.Schema != NotifyMapElementExploded || len(event.Body) < 7 {
		return MapElementsExplodedEvent{}, fmt.Errorf("map-elements-exploded schema/body 0x%04X/%d is invalid", event.Schema, len(event.Body))
	}
	count := int(event.Body[6])
	if count > maximumStoppedBoxes || len(event.Body) != 7+count*len(MapElementWire{}) {
		return MapElementsExplodedEvent{}, fmt.Errorf("map-elements-exploded count/body %d/%d is invalid", count, len(event.Body))
	}
	decoded := MapElementsExplodedEvent{
		PlayerID: binary.BigEndian.Uint16(event.Body[0:2]),
		Time:     binary.BigEndian.Uint32(event.Body[2:6]),
		Elements: make([]MapElementWire, count),
	}
	if decoded.PlayerID == 0 {
		return MapElementsExplodedEvent{}, fmt.Errorf("map-elements-exploded player ID must be non-zero")
	}
	for index := range decoded.Elements {
		copy(decoded.Elements[index][:], event.Body[7+index*8:15+index*8])
	}
	return decoded, nil
}

func ParseEntityHarmedEvent(event GameEvent) (EntityHarmedEvent, error) {
	if event.Schema != PlayerBeHarmed || len(event.Body) != 13 {
		return EntityHarmedEvent{}, fmt.Errorf("entity-harmed schema/body 0x%04X/%d, want 0x%04X/13", event.Schema, len(event.Body), PlayerBeHarmed)
	}
	decoded := EntityHarmedEvent{
		ObjectID: binary.BigEndian.Uint16(event.Body[0:2]),
		Time:     binary.BigEndian.Uint32(event.Body[2:6]),
		PosX:     binary.BigEndian.Uint16(event.Body[6:8]),
		PosY:     binary.BigEndian.Uint16(event.Body[8:10]),
		IsAvatar: event.Body[10] != 0,
		LossHP:   binary.BigEndian.Uint16(event.Body[11:13]),
	}
	if decoded.ObjectID == 0 {
		return EntityHarmedEvent{}, fmt.Errorf("entity-harmed object ID must be non-zero")
	}
	return decoded, nil
}

func ParsePlayerHarmedEvent(event GameEvent) (PlayerHarmedEvent, error) {
	return ParseEntityHarmedEvent(event)
}

func ParsePlayerTimedStateEvent(event GameEvent) (PlayerTimedStateEvent, error) {
	want := 6
	isFire := false
	switch event.Schema {
	case NotifyPlayerTrapped:
	case NotifyFire:
		want = 7
		isFire = true
	default:
		return PlayerTimedStateEvent{}, fmt.Errorf("schema 0x%04X is not a timed player state", event.Schema)
	}
	if len(event.Body) != want {
		return PlayerTimedStateEvent{}, fmt.Errorf("timed player state body length %d, want %d", len(event.Body), want)
	}
	decoded := PlayerTimedStateEvent{PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), Time: binary.BigEndian.Uint32(event.Body[2:6])}
	if want == 7 {
		decoded.Value = event.Body[6]
	}
	if decoded.PlayerID == 0 {
		return PlayerTimedStateEvent{}, fmt.Errorf("timed player state player ID must be non-zero")
	}
	if isFire && (decoded.Value == 0 || decoded.Value&0xf0 != 0) {
		return PlayerTimedStateEvent{}, fmt.Errorf("box-fire launcher mask 0x%02X is outside native low four bits", decoded.Value)
	}
	return decoded, nil
}

func ParseTankBaseHPChangeEvent(event GameEvent) (TankBaseHPChangeEvent, error) {
	if event.Schema != RequestTankBaseHP || len(event.Body) != 10 {
		return TankBaseHPChangeEvent{}, fmt.Errorf("tank-base HP schema/body 0x%04X/%d, want 0x%04X/10", event.Schema, len(event.Body), RequestTankBaseHP)
	}
	decoded := TankBaseHPChangeEvent{PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), Time: binary.BigEndian.Uint32(event.Body[2:6]), TeamID: binary.BigEndian.Uint16(event.Body[6:8]), ChangeHP: int16(binary.BigEndian.Uint16(event.Body[8:10]))}
	if decoded.PlayerID == 0 || decoded.TeamID == 0 || decoded.ChangeHP == 0 {
		return TankBaseHPChangeEvent{}, fmt.Errorf("tank-base player/team/change %d/%d/%d is invalid", decoded.PlayerID, decoded.TeamID, decoded.ChangeHP)
	}
	return decoded, nil
}

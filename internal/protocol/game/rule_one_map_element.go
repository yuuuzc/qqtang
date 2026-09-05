package game

import (
	"encoding/binary"
	"fmt"
)

// MoveMapElementRequest is the ordinary-rule 0x0FB2 request. It predates the
// unrelated rule-8 0x115E push-box message and must not share that wire type.
type MoveMapElementRequest struct {
	PlayerID   uint16
	ClientTime uint32
	ElementID  uint32
	Row        byte
	Col        byte
	Direction  byte
	Sequence   uint16
}

// MapElementMovedEvent is the arbitrator-confirmed ordinary-rule 0x0FB3
// movement. Row/Col widen to uint16 in this direction, matching Client.exe's
// aligned receive structure and the 17-byte recorded body.
type MapElementMovedEvent struct {
	PlayerID   uint16
	ClientTime uint32
	ElementID  uint32
	Row        uint16
	Col        uint16
	Direction  byte
	Sequence   uint16
}

func (event MoveMapElementRequest) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 || event.ElementID == 0 || !validCompetitiveCell(event.Row, event.Col) || event.Direction > 3 {
		return nil, fmt.Errorf("move-map-element request player/element/cell/direction %d/%d/%d,%d/%d is invalid", event.PlayerID, event.ElementID, event.Row, event.Col, event.Direction)
	}
	body := make([]byte, 0, 15)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.ClientTime)
	body = binary.BigEndian.AppendUint32(body, event.ElementID)
	body = append(body, event.Row, event.Col, event.Direction)
	body = binary.BigEndian.AppendUint16(body, event.Sequence)
	return body, nil
}

func (event MapElementMovedEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.PlayerID == 0 || event.ElementID == 0 || event.Row >= 13 || event.Col >= 15 || event.Direction > 3 {
		return nil, fmt.Errorf("map-element-moved player/element/cell/direction %d/%d/%d,%d/%d is invalid", event.PlayerID, event.ElementID, event.Row, event.Col, event.Direction)
	}
	body := make([]byte, 0, 17)
	body = binary.BigEndian.AppendUint16(body, event.PlayerID)
	body = binary.BigEndian.AppendUint32(body, event.ClientTime)
	body = binary.BigEndian.AppendUint32(body, event.ElementID)
	body = binary.BigEndian.AppendUint16(body, event.Row)
	body = binary.BigEndian.AppendUint16(body, event.Col)
	body = append(body, event.Direction)
	body = binary.BigEndian.AppendUint16(body, event.Sequence)
	return body, nil
}

func ParseMoveMapElementRequest(event GameEvent) (MoveMapElementRequest, error) {
	if event.Schema != RequestMoveMapElement || len(event.Body) != 15 {
		return MoveMapElementRequest{}, fmt.Errorf("move-map-element request schema/body 0x%04X/%d, want 0x%04X/15", event.Schema, len(event.Body), RequestMoveMapElement)
	}
	decoded := MoveMapElementRequest{
		PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), ClientTime: binary.BigEndian.Uint32(event.Body[2:6]),
		ElementID: binary.BigEndian.Uint32(event.Body[6:10]), Row: event.Body[10], Col: event.Body[11],
		Direction: event.Body[12], Sequence: binary.BigEndian.Uint16(event.Body[13:15]),
	}
	if decoded.PlayerID == 0 || decoded.ElementID == 0 || !validCompetitiveCell(decoded.Row, decoded.Col) || decoded.Direction > 3 {
		return MoveMapElementRequest{}, fmt.Errorf("move-map-element request player/element/cell/direction %d/%d/%d,%d/%d is invalid", decoded.PlayerID, decoded.ElementID, decoded.Row, decoded.Col, decoded.Direction)
	}
	return decoded, nil
}

func ParseMapElementMovedEvent(event GameEvent) (MapElementMovedEvent, error) {
	if event.Schema != NotifyMapElementMoved || len(event.Body) != 17 {
		return MapElementMovedEvent{}, fmt.Errorf("map-element-moved schema/body 0x%04X/%d, want 0x%04X/17", event.Schema, len(event.Body), NotifyMapElementMoved)
	}
	decoded := MapElementMovedEvent{
		PlayerID: binary.BigEndian.Uint16(event.Body[0:2]), ClientTime: binary.BigEndian.Uint32(event.Body[2:6]),
		ElementID: binary.BigEndian.Uint32(event.Body[6:10]), Row: binary.BigEndian.Uint16(event.Body[10:12]),
		Col: binary.BigEndian.Uint16(event.Body[12:14]), Direction: event.Body[14], Sequence: binary.BigEndian.Uint16(event.Body[15:17]),
	}
	if decoded.PlayerID == 0 || decoded.ElementID == 0 || decoded.Row >= 13 || decoded.Col >= 15 || decoded.Direction > 3 {
		return MapElementMovedEvent{}, fmt.Errorf("map-element-moved player/element/cell/direction %d/%d/%d,%d/%d is invalid", decoded.PlayerID, decoded.ElementID, decoded.Row, decoded.Col, decoded.Direction)
	}
	return decoded, nil
}

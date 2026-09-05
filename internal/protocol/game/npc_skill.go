package game

import (
	"encoding/binary"
	"fmt"
)

const (
	npcUseSkillHeaderWireSize = 13
	npcSkillItemWireSize      = 6
	npcSkillMaxItems          = 64
	npcTalkHeaderWireSize     = 4
	npcTalkMaxBytes           = 512
)

// NPCSkillItem is one scene object produced by NOTIFY_NPCUSE_SKILL. The
// native Boss controller writes a DWORD scene ID followed by an 8-bit row and
// column. Coordinates remain client-authored by the elected arbitrator.
type NPCSkillItem struct {
	SceneID uint32
	Row     byte
	Col     byte
}

// NPCUseSkillEvent is the variable body of schema 0x1169. ObjectID retains
// QQTMsgData's PlayerID wire field.  It is not always the event author: inside
// a QQT_DATA_PACKAGE the package PlayerID is the author, while targeted Boss
// skills place their target participant in this field.  Other native branches
// may place the NPC itself here, so callers must interpret it with the
// surrounding transport and active rule.
type NPCUseSkillEvent struct {
	ObjectID uint16
	Time     uint32
	PosX     uint16
	PosY     uint16
	SkillID  uint16
	Items    []NPCSkillItem
}

type NPCTalkEvent struct {
	ObjectID uint16
	Words    []byte
}

func ParseNPCUseSkillEvent(event GameEvent) (NPCUseSkillEvent, error) {
	if event.Schema != NotifyNPCUseSkill {
		return NPCUseSkillEvent{}, fmt.Errorf("NPC-use-skill schema 0x%04X, want 0x%04X", event.Schema, NotifyNPCUseSkill)
	}
	if len(event.Body) < npcUseSkillHeaderWireSize {
		return NPCUseSkillEvent{}, fmt.Errorf("NOTIFY_NPCUSE_SKILL body length %d, want at least %d", len(event.Body), npcUseSkillHeaderWireSize)
	}
	count := int(event.Body[12])
	if count > npcSkillMaxItems || len(event.Body) != npcUseSkillHeaderWireSize+count*npcSkillItemWireSize {
		return NPCUseSkillEvent{}, fmt.Errorf("NOTIFY_NPCUSE_SKILL item count/length %d/%d is invalid", count, len(event.Body))
	}
	decoded := NPCUseSkillEvent{
		ObjectID: binary.BigEndian.Uint16(event.Body[0:2]),
		Time:     binary.BigEndian.Uint32(event.Body[2:6]),
		PosX:     binary.BigEndian.Uint16(event.Body[6:8]),
		PosY:     binary.BigEndian.Uint16(event.Body[8:10]),
		SkillID:  binary.BigEndian.Uint16(event.Body[10:12]),
		Items:    make([]NPCSkillItem, 0, count),
	}
	if decoded.ObjectID == 0 || decoded.SkillID == 0 {
		return NPCUseSkillEvent{}, fmt.Errorf("NOTIFY_NPCUSE_SKILL object/skill IDs %d/%d must be non-zero", decoded.ObjectID, decoded.SkillID)
	}
	for offset := npcUseSkillHeaderWireSize; offset < len(event.Body); offset += npcSkillItemWireSize {
		item := NPCSkillItem{SceneID: binary.BigEndian.Uint32(event.Body[offset : offset+4]), Row: event.Body[offset+4], Col: event.Body[offset+5]}
		if item.SceneID == 0 {
			return NPCUseSkillEvent{}, fmt.Errorf("NOTIFY_NPCUSE_SKILL scene ID must be non-zero")
		}
		decoded.Items = append(decoded.Items, item)
	}
	return decoded, nil
}

func (event NPCUseSkillEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.ObjectID == 0 || event.SkillID == 0 || len(event.Items) > npcSkillMaxItems {
		return nil, fmt.Errorf("NPC-use-skill object/skill/items %d/%d/%d is invalid", event.ObjectID, event.SkillID, len(event.Items))
	}
	body := make([]byte, 0, npcUseSkillHeaderWireSize+len(event.Items)*npcSkillItemWireSize)
	body = binary.BigEndian.AppendUint16(body, event.ObjectID)
	body = binary.BigEndian.AppendUint32(body, event.Time)
	body = binary.BigEndian.AppendUint16(body, event.PosX)
	body = binary.BigEndian.AppendUint16(body, event.PosY)
	body = binary.BigEndian.AppendUint16(body, event.SkillID)
	body = append(body, byte(len(event.Items)))
	for _, item := range event.Items {
		if item.SceneID == 0 {
			return nil, fmt.Errorf("NPC-use-skill scene ID must be non-zero")
		}
		body = binary.BigEndian.AppendUint32(body, item.SceneID)
		body = append(body, item.Row, item.Col)
	}
	return body, nil
}

func ParseNPCTalkEvent(event GameEvent) (NPCTalkEvent, error) {
	if event.Schema != NotifyNPCTalk {
		return NPCTalkEvent{}, fmt.Errorf("NPC-talk schema 0x%04X, want 0x%04X", event.Schema, NotifyNPCTalk)
	}
	if len(event.Body) < npcTalkHeaderWireSize {
		return NPCTalkEvent{}, fmt.Errorf("NOTIFY_NPC_TALK body length %d, want at least %d", len(event.Body), npcTalkHeaderWireSize)
	}
	length := int(binary.BigEndian.Uint16(event.Body[2:4]))
	if length > npcTalkMaxBytes || len(event.Body) != npcTalkHeaderWireSize+length {
		return NPCTalkEvent{}, fmt.Errorf("NOTIFY_NPC_TALK word length/body length %d/%d is invalid", length, len(event.Body))
	}
	decoded := NPCTalkEvent{ObjectID: binary.BigEndian.Uint16(event.Body[0:2]), Words: append([]byte(nil), event.Body[4:]...)}
	if decoded.ObjectID == 0 {
		return NPCTalkEvent{}, fmt.Errorf("NOTIFY_NPC_TALK object ID must be non-zero")
	}
	return decoded, nil
}

func (event NPCTalkEvent) MarshalNetworkBinary() ([]byte, error) {
	if event.ObjectID == 0 || len(event.Words) > npcTalkMaxBytes {
		return nil, fmt.Errorf("NPC-talk object/word length %d/%d is invalid", event.ObjectID, len(event.Words))
	}
	body := make([]byte, 0, npcTalkHeaderWireSize+len(event.Words))
	body = binary.BigEndian.AppendUint16(body, event.ObjectID)
	body = binary.BigEndian.AppendUint16(body, uint16(len(event.Words)))
	body = append(body, event.Words...)
	return body, nil
}

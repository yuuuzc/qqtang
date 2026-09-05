package qbv

import (
	"fmt"

	"qqtang/internal/protocol/game"
)

type NativeEventKind byte

const (
	NativeEventOpaque NativeEventKind = iota
	NativeEventMovement
	NativeEventBombPlaced
	NativeEventBombExploded
	NativeEventActorTrapped
	NativeEventActorDied
	NativeEventActorInteraction
	NativeEventActorKilled
	NativeEventItem
	NativeEventDispatchItems
	NativeEventUseItem
	NativeEventMoveBombRequest
	NativeEventBombMoved
	NativeEventMoveMapElementRequest
	NativeEventMapElementMoved
)

// NativeEvent is a lossless QBV event plus the one typed protocol view selected
// by Schema. Unknown messages remain NativeEventOpaque and retain Record.Data;
// malformed messages from a known battle family fail normalization instead of
// silently entering a differential replay with shifted fields.
type NativeEvent struct {
	Index              int
	Record             Event
	Kind               NativeEventKind
	Movement           *game.PlayerMove
	BombPlaced         *game.PlayerUseBombEvent
	BombExplode        *game.BombExplodeEvent
	Trapped            *game.PlayerExplodedEvent
	Died               *game.PlayerDeathEvent
	Interaction        *game.PlayerInteractionEvent
	Killed             *game.PlayerKilledEvent
	Item               *game.PlayerItemEvent
	Dispatch           *game.DispatchItemData
	UseItem            *game.WorldUseItemEvent
	MoveBomb           *game.MoveBombEvent
	MoveElementRequest *game.MoveMapElementRequest
	MapElementMoved    *game.MapElementMovedEvent
}

func DecodeNativeEvents(recording Recording) ([]NativeEvent, error) {
	result := make([]NativeEvent, 0, len(recording.Events))
	for index, record := range recording.Events {
		event, err := decodeNativeEvent(index, record)
		if err != nil {
			return nil, fmt.Errorf("QBV event %d at %d ms schema 0x%04X: %w", index, record.TimeMS, record.Schema, err)
		}
		result = append(result, event)
	}
	return result, nil
}

func decodeNativeEvent(index int, record Event) (NativeEvent, error) {
	result := NativeEvent{Index: index, Record: record}
	protocolEvent := game.GameEvent{Schema: record.Schema, Body: record.Data}
	switch record.Schema {
	case game.PlayerMoveSchema:
		decoded, err := game.ParsePlayerMove(record.Data)
		if err != nil {
			return NativeEvent{}, err
		}
		result.Kind, result.Movement = NativeEventMovement, &decoded
	case game.PlayerUseBomb, game.NotifyPlayerUseBomb:
		// The request and peer notification bodies are byte-identical.
		protocolEvent.Schema = game.PlayerUseBomb
		decoded, err := game.ParsePlayerUseBombEvent(protocolEvent)
		if err != nil {
			return NativeEvent{}, err
		}
		result.Kind, result.BombPlaced = NativeEventBombPlaced, &decoded
	case game.NotifyBombExplode:
		decoded, err := game.ParseBombExplodeEvent(protocolEvent)
		if err != nil {
			return NativeEvent{}, err
		}
		result.Kind, result.BombExplode = NativeEventBombExploded, &decoded
	case game.PlayerBeExploded, game.NotifyPlayerExploded:
		// The active client emits and consumes the same 0x0FA5 body. The
		// adjacent 0x0FA6 table entry is not used by this actor-hit path.
		protocolEvent.Schema = game.PlayerBeExploded
		decoded, err := game.ParsePlayerExplodedEvent(protocolEvent)
		if err != nil {
			return NativeEvent{}, err
		}
		result.Kind, result.Trapped = NativeEventActorTrapped, &decoded
	case game.NotifyPlayerDieEvent:
		decoded, err := game.ParsePlayerDeathEvent(protocolEvent)
		if err != nil {
			return NativeEvent{}, err
		}
		result.Kind, result.Died = NativeEventActorDied, &decoded
	case game.RequestKillPlayer, game.RequestSavePlayer, game.NotifyPlayerSaved:
		decoded, err := game.ParsePlayerInteractionEvent(protocolEvent)
		if err != nil {
			return NativeEvent{}, err
		}
		result.Kind, result.Interaction = NativeEventActorInteraction, &decoded
	case game.NotifyPlayerKilled:
		decoded, err := game.ParsePlayerKilledEvent(protocolEvent)
		if err != nil {
			return NativeEvent{}, err
		}
		result.Kind, result.Killed = NativeEventActorKilled, &decoded
	case game.RequestGetItem, game.NotifyPlayerGetItem:
		decoded, err := game.ParsePlayerItemEvent(protocolEvent)
		if err != nil {
			return NativeEvent{}, err
		}
		result.Kind, result.Item = NativeEventItem, &decoded
	case game.NotifyDispatchItem:
		decoded, err := game.ParseDispatchItemEvent(protocolEvent)
		if err != nil {
			return NativeEvent{}, err
		}
		result.Kind, result.Dispatch = NativeEventDispatchItems, &decoded
	case game.RequestUseItem, game.NotifyPlayerUseItem, game.NotifyPlayerAffection:
		decoded, err := game.ParseWorldUseItemEvent(protocolEvent)
		if err != nil {
			return NativeEvent{}, err
		}
		result.Kind, result.UseItem = NativeEventUseItem, &decoded
	case game.RequestMoveBomb, game.NotifyPlayerMoveBomb:
		decoded, err := game.ParseMoveBombEvent(protocolEvent)
		if err != nil {
			return NativeEvent{}, err
		}
		result.MoveBomb = &decoded
		if record.Schema == game.RequestMoveBomb {
			result.Kind = NativeEventMoveBombRequest
		} else {
			result.Kind = NativeEventBombMoved
		}
	case game.RequestMoveMapElement:
		decoded, err := game.ParseMoveMapElementRequest(protocolEvent)
		if err != nil {
			return NativeEvent{}, err
		}
		result.Kind, result.MoveElementRequest = NativeEventMoveMapElementRequest, &decoded
	case game.NotifyMapElementMoved:
		decoded, err := game.ParseMapElementMovedEvent(protocolEvent)
		if err != nil {
			return NativeEvent{}, err
		}
		result.Kind, result.MapElementMoved = NativeEventMapElementMoved, &decoded
	default:
		result.Kind = NativeEventOpaque
	}
	return result, nil
}

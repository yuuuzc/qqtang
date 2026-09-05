package battleengine

import "fmt"

// Step advances exactly one configured tick. Input order is irrelevant: at
// most one action is accepted per player and actors are always processed by
// ascending PlayerID.
func (engine *Engine) Step(actions []Action) ([]Event, error) {
	if engine == nil {
		return nil, fmt.Errorf("battle engine is nil")
	}
	if engine.outcome.Ended {
		return nil, nil
	}
	engine.activatePendingPickupDispatches()
	actionByPlayer := make(map[uint16]Action, len(actions))
	for _, action := range actions {
		if engine.actorIndex(action.PlayerID) < 0 {
			return nil, fmt.Errorf("battle action player %d is not a participant", action.PlayerID)
		}
		if _, duplicate := actionByPlayer[action.PlayerID]; duplicate {
			return nil, fmt.Errorf("battle action repeats player %d", action.PlayerID)
		}
		if _, _, ok := action.Move.delta(); !ok {
			return nil, fmt.Errorf("battle action player %d has invalid direction %d", action.PlayerID, action.Move)
		}
		if action.UseActionID != 0 {
			if _, ok := nativeActionDiscreteID(action.UseActionID); !ok {
				return nil, fmt.Errorf("battle action player %d uses unsupported native item %d", action.PlayerID, action.UseActionID)
			}
		}
		actionByPlayer[action.PlayerID] = action
	}
	for index := range engine.actors {
		playerID := engine.actors[index].PlayerID
		action, ok := actionByPlayer[playerID]
		if !ok {
			continue
		}
		if action.UseActionID == 0 {
			continue
		}
		actor := &engine.actors[index]
		if !canUseHeldAction(actor, action.UseActionID) {
			return nil, fmt.Errorf("battle action player %d cannot use native item %d in state %d", playerID, action.UseActionID, actor.State)
		}
	}
	events := make([]Event, 0, len(actions)+4)
	for index := range engine.actors {
		engine.expireNativePassState(&engine.actors[index])
	}
	for index := range engine.actors {
		action, ok := actionByPlayer[engine.actors[index].PlayerID]
		if !ok || action.UseActionID == 0 {
			continue
		}
		usedEvents, _ := engine.useHeldAction(index, action.UseActionID)
		events = append(events, usedEvents...)
	}

	// Placement snapshots the actor's pre-movement cell while the held
	// direction continues to advance the actor in the same input frame. Local
	// clients each check occupancy before their own 0xFA3; the remote consumer
	// does not recheck it. Preserve that native simultaneous-input behavior by
	// evaluating every request against the bomb cells present at phase start.
	occupiedBeforePlacement := make(map[Cell]struct{}, len(engine.bombs))
	for _, bomb := range engine.bombs {
		occupiedBeforePlacement[bomb.Cell] = struct{}{}
	}
	for index := range engine.actors {
		action, ok := actionByPlayer[engine.actors[index].PlayerID]
		if !ok || !action.PlaceBomb || engine.actors[index].State != ActorActive {
			continue
		}
		if _, occupied := occupiedBeforePlacement[engine.actors[index].Position.Cell()]; occupied {
			continue
		}
		if event, placed := engine.placeBombOnSnapshotEmptyCell(index); placed {
			events = append(events, event)
			// FUN_005b08e0 invokes the actor vtable slot +0x1c only after
			// successfully creating the local bubble. For normal actors that
			// slot reaches FUN_005d33ab and is the sole activation gate for the
			// charged native pass state. Collision updates merely charge the
			// state; they never activate it on a frame by themselves.
			actor := &engine.actors[index]
			if engine.activateNativePassState(actor) {
				events = append(events, Event{
					Kind: EventNativePassStarted, TimeMS: engine.elapsedMS,
					PlayerID: actor.PlayerID, Cell: actor.Position.Cell(), Position: actor.Position,
				})
			}
		}
	}
	for index := range engine.actors {
		action := actionByPlayer[engine.actors[index].PlayerID]
		if engine.actors[index].State != ActorActive {
			continue
		}
		direction := action.Move
		forcedSlide := engine.actors[index].MovementStatus == MovementStatusForcedSlide
		if forcedSlide {
			direction = engine.actors[index].Facing
		}
		if direction == DirectionNone {
			continue
		}
		var moveEvents []Event
		var moved bool
		if forcedSlide {
			moveEvents, moved = engine.moveActorInWorldDirection(index, direction)
		} else {
			moveEvents, moved = engine.moveActor(index, direction)
		}
		events = append(events, moveEvents...)
		if moved {
		} else if forcedSlide {
			events = append(events, engine.clearMovementStatus(&engine.actors[index]))
		}
	}
	engine.releaseFieldObjectPassThrough()
	events = append(events, engine.resolvePickupContacts()...)
	events = append(events, engine.resolveFieldObjectContacts()...)
	events = append(events, engine.resolveActorContacts()...)

	engine.elapsedMS = saturatingAdd(engine.elapsedMS, engine.rules.TickMS)
	engine.activatePendingPickupDispatches()
	engine.expireFlames()
	engine.resolveActionProjectiles()
	// In a mixed live match the elected original-client arbitrator owns every
	// scene explosion, including bubbles placed by a virtual room participant.
	// Captures show its 0x0FA4 contains virtual bomb owners, destroyed walls and
	// destroyed visible items. Hidden wall contents are already deterministic
	// from GAME_BEGIN.ItemSeed/NewItems and become visible only when the listed
	// wall is destroyed. Advancing any bomb here would create a second competing
	// scene mutation before that authenticated result is mirrored back.
	if !engine.rules.NativeOutcomeAuthority {
		events = append(events, engine.explodeDueBombs()...)
		events = append(events, engine.dispatchRecycledPickups()...)
	}
	events = append(events, engine.expireTransformations()...)
	events = append(events, engine.expireMovementStatuses()...)
	events = append(events, engine.applyFlameHazards()...)
	events = append(events, engine.expireTraps()...)
	if event, ended := engine.evaluateTerminal(); ended {
		events = append(events, event)
	}
	engine.recordPublicBehavior(events)
	return events, nil
}

func saturatingAdd(value, delta uint32) uint32 {
	if ^uint32(0)-value < delta {
		return ^uint32(0)
	}
	return value + delta
}

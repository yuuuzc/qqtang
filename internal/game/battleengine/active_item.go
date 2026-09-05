package battleengine

import (
	"fmt"
	"sort"
)

const nativeActionProjectileDurationMS uint32 = 1_000

func nativeFieldSceneID(sceneID uint32) bool {
	return sceneID >= 41 && sceneID <= 43
}

func (engine *Engine) useHeldAction(actorIndex int, actionID uint8) ([]Event, bool) {
	actor := &engine.actors[actorIndex]
	targetBombID := uint32(0)
	if actionID == 44 || actionID == 46 {
		targetBombID = engine.firstBombInFacingRay(actor)
	}
	if engine.rules.NativeOutcomeAuthority && actor.Source == ParticipantVirtualAI {
		if !canUseHeldAction(actor, actionID) {
			return nil, false
		}
		// The local handler produces only REQUEST_USE_ITEM. Inventory
		// consumption and scene mutation wait for NOTIFY_PLAYER_USE_ITEM.
		return []Event{{
			Kind: EventBattleActionUseRequested, TimeMS: engine.elapsedMS,
			PlayerID: actor.PlayerID, BombID: targetBombID,
			Cell: actor.Position.Cell(), Position: actor.Position, ActionID: actionID,
		}}, true
	}
	return engine.useHeldActionAt(actorIndex, actionID, actor.Position, targetBombID)
}

func (engine *Engine) useHeldActionAt(actorIndex int, actionID uint8, position Position, targetBombID uint32) ([]Event, bool) {
	actor := &engine.actors[actorIndex]
	if !canUseHeldAction(actor, actionID) {
		return nil, false
	}
	if actor.State == ActorTrapped {
		switch actionID {
		case 63:
			actor.State = ActorActive
			actor.TrappedBy = 0
			actor.TrapExpiresAt = 0
		default:
			return nil, false
		}
		consumeHeldAction(actor, actionID)
		events := []Event{{
			Kind: EventBattleActionUsed, TimeMS: engine.elapsedMS, PlayerID: actor.PlayerID,
			Cell: actor.Position.Cell(), Position: actor.Position, ActionID: actionID,
		}}
		if actionID == 63 {
			events = append(events, Event{
				Kind: EventActorRescued, TimeMS: engine.elapsedMS, PlayerID: actor.PlayerID,
				TargetID: actor.PlayerID, Cell: actor.Position.Cell(), Position: actor.Position,
				ActionID: actionID,
			})
		}
		return events, true
	}
	if actor.State != ActorActive {
		return nil, false
	}
	var events []Event
	switch actionID {
	case 41, 42, 43:
		object := FieldObject{
			ID: engine.nextFieldObjectID, ActionID: actionID, OwnerID: actor.PlayerID,
			Cell: position.Cell(), PassableBy: engine.overlappingActiveActorIDs(position.Cell()),
		}
		engine.nextFieldObjectID++
		engine.fieldObjects = append(engine.fieldObjects, object)
		events = append(events, Event{
			Kind: EventFieldObjectPlaced, TimeMS: engine.elapsedMS, PlayerID: actor.PlayerID,
			Cell: object.Cell, Position: position, ActionID: actionID, ObjectID: object.ID,
		})
	case 44, 46:
		if targetBombID != 0 {
			targetIndex := engine.bombIndexByID(targetBombID)
			if targetIndex < 0 {
				return nil, false
			}
			projectile := ActionProjectile{
				ID: engine.nextProjectileID, ActionID: actionID, OwnerID: actor.PlayerID,
				TargetBombID: targetBombID, TargetCell: engine.bombs[targetIndex].Cell,
				ResolveAtMS: saturatingAdd(engine.elapsedMS, nativeActionProjectileDurationMS),
			}
			engine.nextProjectileID++
			engine.projectiles = append(engine.projectiles, projectile)
		}
	case 64:
		for index := range engine.bombs {
			if engine.bombs[index].OwnerID == actor.PlayerID && engine.bombs[index].ExplodeAtMS > engine.elapsedMS {
				engine.bombs[index].ExplodeAtMS = engine.elapsedMS
			}
		}
	default:
		return nil, false
	}
	consumeHeldAction(actor, actionID)
	events = append(events, Event{
		Kind: EventBattleActionUsed, TimeMS: engine.elapsedMS, PlayerID: actor.PlayerID,
		BombID: targetBombID, Cell: position.Cell(), Position: position, ActionID: actionID,
	})
	return events, true
}

// ApplyVerifiedBattleAction applies one 0x0FAF/0x0FB0 action already produced
// by an original client. Position and the optional projectile target come from
// that native message; no policy-side targeting is rerun at this boundary.
func (engine *Engine) ApplyVerifiedBattleAction(playerID uint16, actionID uint8, position Position, targetBombID uint32) ([]Event, error) {
	if engine == nil {
		return nil, fmt.Errorf("battle engine is nil")
	}
	if position.X < 0 || position.Y < 0 || position.X >= int32(engine.grid.Width)*CellSizePixels || position.Y >= int32(engine.grid.Height)*CellSizePixels {
		return nil, fmt.Errorf("verified battle action for player %d is outside map at %d,%d", playerID, position.X, position.Y)
	}
	actorIndex := engine.actorIndex(playerID)
	if actorIndex < 0 {
		return nil, fmt.Errorf("verified battle action player %d is not a participant", playerID)
	}
	if targetBombID != 0 && engine.bombIndexByID(targetBombID) < 0 {
		return nil, fmt.Errorf("verified battle action player %d targets unknown bomb %d", playerID, targetBombID)
	}
	events, used := engine.useHeldActionAt(actorIndex, actionID, position, targetBombID)
	if !used {
		return nil, fmt.Errorf("verified battle action player %d cannot use native item %d in state %d", playerID, actionID, engine.actors[actorIndex].State)
	}
	return events, nil
}

func canUseHeldAction(actor *Actor, actionID uint8) bool {
	if actorHeldActionCount(actor, actionID) == 0 {
		return false
	}
	if actor.State == ActorTrapped {
		return actionID == 63
	}
	if actor.State != ActorActive {
		return false
	}
	switch actionID {
	case 41, 42, 43, 44, 46, 64:
		return true
	default:
		return false
	}
}

func (engine *Engine) overlappingActiveActorIDs(cell Cell) []uint16 {
	result := make([]uint16, 0, len(engine.actors))
	for index := range engine.actors {
		actor := &engine.actors[index]
		if actor.State != ActorEliminated && engine.positionOverlapsCell(actor.Position, cell) {
			result = append(result, actor.PlayerID)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func (engine *Engine) firstBombInFacingRay(actor *Actor) uint32 {
	dx, dy, ok := actor.Facing.delta()
	if !ok || actor.Facing == DirectionNone {
		return 0
	}
	cell := actor.Position.Cell()
	// FUN_00610631 scans up to max(map width, map height), stopping at the
	// first blocked transition or object. There is no separate 3/5-screen
	// range cap in the final client; the map edge is the maximum range.
	for distance := 1; distance < max(int(engine.grid.Width), int(engine.grid.Height)); distance++ {
		cell.Row += int16(dy)
		cell.Col += int16(dx)
		tile, inside := engine.grid.Cell(cell)
		if !inside || tile.Kind != CellOpen {
			return 0
		}
		if index := engine.bombAt(cell); index >= 0 {
			return engine.bombs[index].ID
		}
	}
	return 0
}

func (engine *Engine) resolveActionProjectiles() {
	kept := engine.projectiles[:0]
	for _, projectile := range engine.projectiles {
		if projectile.ResolveAtMS > engine.elapsedMS {
			kept = append(kept, projectile)
			continue
		}
		for index := range engine.bombs {
			if engine.bombs[index].ID == projectile.TargetBombID && engine.bombs[index].Cell == projectile.TargetCell {
				engine.bombs[index].ExplodeAtMS = engine.elapsedMS
				break
			}
		}
	}
	engine.projectiles = kept
}

func (engine *Engine) releaseFieldObjectPassThrough() {
	for objectIndex := range engine.fieldObjects {
		object := &engine.fieldObjects[objectIndex]
		for _, playerID := range append([]uint16(nil), object.PassableBy...) {
			actorIndex := engine.actorIndex(playerID)
			if actorIndex < 0 || engine.actors[actorIndex].State == ActorEliminated || !engine.positionOverlapsCell(engine.actors[actorIndex].Position, object.Cell) {
				object.PassableBy = removeSortedPlayerID(object.PassableBy, playerID)
			}
		}
	}
}

func (engine *Engine) resolveFieldObjectContacts() []Event {
	events := make([]Event, 0)
	kept := engine.fieldObjects[:0]
	for _, object := range engine.fieldObjects {
		consumed := false
		for actorIndex := range engine.actors {
			actor := &engine.actors[actorIndex]
			// Native movement fields sample the actor's current coordinate cell
			// (position / NativeCellPixels), not the sprite footprint. In a live
			// authority trace a field in (0,6) rejected contacts while the virtual
			// actor's centre was still in (0,5), even though its footprint already
			// overlapped the field cell.
			if actor.State != ActorActive || engine.actorHarmProtected(actor) || containsSortedPlayerID(object.PassableBy, actor.PlayerID) || actor.Position.Cell() != object.Cell {
				continue
			}
			// A real participant reports its own native contact. A virtual actor has
			// no client process, so Go supplies the missing request, while the
			// elected arbitrator still owns the resulting actor state.
			if engine.rules.NativeOutcomeAuthority && actor.Source == ParticipantHuman {
				continue
			}
			events = append(events, Event{
				Kind: EventFieldObjectTriggered, TimeMS: engine.elapsedMS, PlayerID: object.OwnerID,
				TargetID: actor.PlayerID, Cell: object.Cell, Position: actor.Position,
				ActionID: object.ActionID, ObjectID: object.ID,
			})
			switch object.ActionID {
			case 41:
				if engine.rules.NativeOutcomeAuthority {
					events = append(events, Event{
						Kind: EventActorHitRequested, TimeMS: engine.elapsedMS,
						PlayerID: actor.PlayerID, TargetID: object.OwnerID,
						Cell: actor.Position.Cell(), Position: actor.Position,
						SceneID: actor.TransformationSceneID,
					})
					// Capture fields use the 0x0FA5 hit path and disappear on
					// contact just as before; only movement fields 42/43 wait for
					// their distinct 0x0FAD confirmation.
					consumed = true
					break
				}
				if actor.TransformationSceneID != 0 {
					events = append(events, engine.endTransformation(actor, TransformationEndHit, object.OwnerID))
				} else {
					actor.State = ActorTrapped
					actor.TrappedBy = object.OwnerID
					if duration := engine.trapDuration(actor); duration != 0 {
						actor.TrapExpiresAt = saturatingAdd(engine.elapsedMS, duration)
					}
					events = append(events, Event{Kind: EventActorTrapped, TimeMS: engine.elapsedMS, PlayerID: object.OwnerID, TargetID: actor.PlayerID, Cell: actor.Position.Cell(), Position: actor.Position})
				}
				consumed = true
			case 42:
				if engine.rules.NativeOutcomeAuthority {
					// The missing virtual Client.exe must author 0x0FAC. Keep the
					// field and actor state unchanged until the arbitrator's 0x0FAD.
					break
				}
				engine.installMovementStatus(actor, MovementStatusForcedSlide)
				events = append(events, Event{Kind: EventMovementStatusStarted, TimeMS: engine.elapsedMS, PlayerID: actor.PlayerID, Cell: actor.Position.Cell(), Position: actor.Position, MovementStatus: MovementStatusForcedSlide})
				consumed = true
			case 43:
				if engine.rules.NativeOutcomeAuthority {
					// Slow glue follows the same native request/notification pair as
					// forced slide. Applying it here would race the authoritative client.
					break
				}
				engine.installMovementStatus(actor, MovementStatusSlow)
				events = append(events, Event{Kind: EventMovementStatusStarted, TimeMS: engine.elapsedMS, PlayerID: actor.PlayerID, Cell: actor.Position.Cell(), Position: actor.Position, MovementStatus: MovementStatusSlow, EffectExpiresAt: actor.MovementStatusExpiresAt})
				consumed = true
			}
			break
		}
		if !consumed {
			kept = append(kept, object)
		}
	}
	engine.fieldObjects = kept
	return events
}

// ApplyVerifiedFieldObjectContact commits one arbitrator-confirmed 0x0FAD for
// a placed native movement field. Action 42/43 contacts use the same wire item
// body as ordinary pickups, but they consume FieldObject state rather than a
// wall-spawned Pickup.
func (engine *Engine) ApplyVerifiedFieldObjectContact(playerID uint16, actionID uint8, position Position) ([]Event, error) {
	if engine == nil {
		return nil, fmt.Errorf("battle engine is nil")
	}
	if actionID != 42 && actionID != 43 {
		return nil, fmt.Errorf("verified field contact uses unsupported action %d", actionID)
	}
	actorIndex := engine.actorIndex(playerID)
	if actorIndex < 0 {
		return nil, fmt.Errorf("verified field contact player %d is not a participant", playerID)
	}
	actor := &engine.actors[actorIndex]
	if actor.State != ActorActive {
		return nil, fmt.Errorf("verified field contact player %d is not active", playerID)
	}
	objectIndex := -1
	for index := range engine.fieldObjects {
		object := &engine.fieldObjects[index]
		// The arbitrator echoes the native current-coordinate position. Match it
		// by the same exact cell rule used to generate the contact request.
		if object.ActionID == actionID && position.Cell() == object.Cell {
			objectIndex = index
			break
		}
	}
	if objectIndex < 0 {
		return nil, fmt.Errorf("verified field contact player %d action %d has no matching field at %d,%d", playerID, actionID, position.X, position.Y)
	}
	object := engine.fieldObjects[objectIndex]
	status := MovementStatusForcedSlide
	if actionID == 43 {
		status = MovementStatusSlow
	}
	engine.installMovementStatus(actor, status)
	events := []Event{
		{
			Kind: EventFieldObjectTriggered, TimeMS: engine.elapsedMS,
			PlayerID: object.OwnerID, TargetID: playerID, Cell: object.Cell,
			Position: position, ActionID: object.ActionID, ObjectID: object.ID,
		},
		{
			Kind: EventMovementStatusStarted, TimeMS: engine.elapsedMS,
			PlayerID: playerID, Cell: actor.Position.Cell(), Position: position,
			MovementStatus: status, EffectExpiresAt: actor.MovementStatusExpiresAt,
		},
	}
	engine.fieldObjects = append(engine.fieldObjects[:objectIndex], engine.fieldObjects[objectIndex+1:]...)
	return events, nil
}

func (engine *Engine) retireFieldObjectsAtCell(cell Cell) {
	kept := engine.fieldObjects[:0]
	for _, object := range engine.fieldObjects {
		if object.Cell != cell {
			kept = append(kept, object)
		}
	}
	engine.fieldObjects = kept
}

// installAuthoritativeFieldObject mirrors a field scene object carried by an
// authenticated 0x0FAE dispatcher vector. That wire shape has no owner field,
// so zero is retained as the unknown source. Actors already overlapping the
// newly installed cell receive the same leave-only pass state as an ordinary
// 0x0FB0 placement; otherwise the next exact centre-cell contact triggers it.
func (engine *Engine) installAuthoritativeFieldObject(actionID uint8, cell Cell) {
	engine.retirePickupsAtCell(cell)
	engine.retireFieldObjectsAtCell(cell)
	object := FieldObject{
		ID: engine.nextFieldObjectID, ActionID: actionID, Cell: cell,
		PassableBy: engine.overlappingActiveActorIDs(cell),
	}
	engine.nextFieldObjectID++
	engine.fieldObjects = append(engine.fieldObjects, object)
}

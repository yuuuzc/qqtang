package battleengine

import "qqtang/internal/clientdata/sceneelement"

// ActorCapabilities is the gameplay projection of the currently installed
// native avatar object. It is intentionally value-only: both the live server
// and an offline RL worker can derive it from cloned state without callbacks.
type ActorCapabilities struct {
	TraverseStaticTerrain bool
	CanCollectItems       bool
	RecoverOnlyOnOpenCell bool
	ReverseDirection      bool
	CanKickBomb           bool
	CanPushBreakable      bool
	EffectiveSpeedRate    byte
	EffectiveBombCapacity byte
	GrantedActionID       byte
	GrantedActionCount    byte
}

func (engine *Engine) actorCapabilities(actor *Actor, direction Direction) ActorCapabilities {
	capabilities := ActorCapabilities{CanCollectItems: true}
	if actor == nil {
		return capabilities
	}
	capabilities.EffectiveSpeedRate = actor.SpeedRate
	capabilities.EffectiveBombCapacity = actor.BombCapacity
	definition, ok := sceneelement.NativeTransformation(sceneelement.ID(actor.TransformationSceneID))
	if !ok || definition.AvatarRoleID != actor.AvatarRoleID {
		return capabilities
	}
	capabilities.TraverseStaticTerrain = definition.TraverseStaticTerrain
	capabilities.CanCollectItems = definition.CanCollectItems
	capabilities.RecoverOnlyOnOpenCell = definition.RecoverOnlyOnOpenCell
	capabilities.ReverseDirection = definition.ReverseDirection
	capabilities.CanKickBomb = definition.CanKickBomb
	capabilities.CanPushBreakable = definition.CanPushBreakable
	capabilities.GrantedActionID = definition.GrantedActionID
	capabilities.GrantedActionCount = definition.GrantedActionCount
	if definition.BombCapacity != 0 {
		capabilities.EffectiveBombCapacity = definition.BombCapacity
	}
	switch direction {
	case DirectionLeft, DirectionRight:
		if definition.HorizontalSpeedRate != 0 {
			capabilities.EffectiveSpeedRate = definition.HorizontalSpeedRate
		} else if definition.SpeedRate != 0 {
			capabilities.EffectiveSpeedRate = definition.SpeedRate
		}
	case DirectionUp, DirectionDown:
		if definition.VerticalSpeedRate != 0 {
			capabilities.EffectiveSpeedRate = definition.VerticalSpeedRate
		} else if definition.SpeedRate != 0 {
			capabilities.EffectiveSpeedRate = definition.SpeedRate
		}
	default:
		if definition.SpeedRate != 0 {
			capabilities.EffectiveSpeedRate = definition.SpeedRate
		}
	}
	return capabilities
}

func (engine *Engine) transformInput(actor *Actor, direction Direction) Direction {
	// Scene 110 reverses a human player's physical direction keys. A virtual
	// participant already supplies world-space actions and has no keyboard to
	// confuse, so applying the client-side handicap a second time would invert
	// every learned/scripted policy decision.
	if actor == nil || actor.Source == ParticipantVirtualAI || !engine.actorCapabilities(actor, direction).ReverseDirection {
		return direction
	}
	switch direction {
	case DirectionUp:
		return DirectionDown
	case DirectionRight:
		return DirectionLeft
	case DirectionDown:
		return DirectionUp
	case DirectionLeft:
		return DirectionRight
	default:
		return direction
	}
}

func (engine *Engine) effectiveSpeedRate(actor *Actor, direction Direction) byte {
	if actor == nil {
		return 0
	}
	switch actor.MovementStatus {
	case MovementStatusSlow:
		return 2
	case MovementStatusForcedSlide:
		return 10
	case MovementStatusFast:
		return 8
	}
	capabilities := engine.actorCapabilities(actor, direction)
	return capabilities.EffectiveSpeedRate
}

// NativeEffectiveSpeedRate exposes the exact native rate that drives the
// current actor in one world direction. Live movement projection uses the
// same value as deterministic integration so status effects and directional
// avatars cannot publish a wire speed that disagrees with their coordinates.
func (engine *Engine) NativeEffectiveSpeedRate(playerID uint16, direction Direction) (byte, bool) {
	if engine == nil {
		return 0, false
	}
	index := engine.actorIndex(playerID)
	if index < 0 {
		return 0, false
	}
	actor := &engine.actors[index]
	if direction == DirectionNone {
		direction = actor.Facing
	}
	return engine.effectiveSpeedRate(actor, direction), true
}

func (engine *Engine) effectiveSpeedPixelsPerSecond(actor *Actor, direction Direction) uint16 {
	rate := engine.effectiveSpeedRate(actor, direction)
	if rate != 0 && rate <= MaxNativeSpeedRate {
		return engine.rules.SpeedPixelsPerSecondByRate[rate]
	}
	if actor != nil {
		return actor.SpeedPixelsPerSecond
	}
	return 0
}

package battleengine

import "testing"

func TestNativeAvatarCapabilitiesAreCentralized(t *testing.T) {
	engine := mustEngine(t, testConfig())
	actor := &engine.actors[0]
	tests := []struct {
		sceneID uint32
		roleID  uint16
		facing  Direction
		check   func(ActorCapabilities) bool
	}{
		{104, 41, DirectionRight, func(c ActorCapabilities) bool {
			return c.TraverseStaticTerrain && !c.CanCollectItems && c.RecoverOnlyOnOpenCell
		}},
		{110, 42, DirectionRight, func(c ActorCapabilities) bool {
			return c.ReverseDirection && c.EffectiveSpeedRate == 8 && c.EffectiveBombCapacity == 8
		}},
		{109, 44, DirectionRight, func(c ActorCapabilities) bool { return c.CanKickBomb && c.CanPushBreakable }},
		{107, 45, DirectionRight, func(c ActorCapabilities) bool { return c.EffectiveSpeedRate == 8 }},
		{108, 46, DirectionRight, func(c ActorCapabilities) bool { return c.EffectiveBombCapacity == 8 }},
		{114, 54, DirectionRight, func(c ActorCapabilities) bool { return c.EffectiveSpeedRate == 5 }},
		{114, 54, DirectionDown, func(c ActorCapabilities) bool { return c.EffectiveSpeedRate == 2 }},
		{115, 55, DirectionRight, func(c ActorCapabilities) bool { return c.GrantedActionID == 46 && c.GrantedActionCount == 9 }},
	}
	for _, test := range tests {
		actor.TransformationSceneID = test.sceneID
		actor.AvatarRoleID = test.roleID
		if capabilities := engine.actorCapabilities(actor, test.facing); !test.check(capabilities) {
			t.Fatalf("scene %d role %d capabilities = %+v", test.sceneID, test.roleID, capabilities)
		}
	}
}

func TestNativeEffectiveSpeedRateIncludesMovementStatusAndAvatarDirection(t *testing.T) {
	config := testConfig()
	config.Rules.SpeedPixelsPerSecondByRate = nativeSpeedPixelsPerSecondByRate
	engine := mustEngine(t, config)
	actor := &engine.actors[0]

	actor.TransformationSceneID = 114
	actor.AvatarRoleID = 54
	for _, test := range []struct {
		name      string
		status    MovementStatusKind
		direction Direction
		want      byte
	}{
		{name: "avatar horizontal", direction: DirectionRight, want: 5},
		{name: "avatar vertical", direction: DirectionDown, want: 2},
		{name: "slow overrides avatar", status: MovementStatusSlow, direction: DirectionRight, want: 2},
		{name: "forced slide overrides avatar", status: MovementStatusForcedSlide, direction: DirectionDown, want: 10},
		{name: "fast overrides avatar", status: MovementStatusFast, direction: DirectionDown, want: 8},
	} {
		t.Run(test.name, func(t *testing.T) {
			actor.MovementStatus = test.status
			got, ok := engine.NativeEffectiveSpeedRate(actor.PlayerID, test.direction)
			if !ok || got != test.want {
				t.Fatalf("effective speed rate = %d/%v, want %d/true", got, ok, test.want)
			}
			if speed := engine.effectiveSpeedPixelsPerSecond(actor, test.direction); speed != nativeSpeedPixelsPerSecondByRate[test.want] {
				t.Fatalf("effective speed = %d, want native rate %d (%d)", speed, test.want, nativeSpeedPixelsPerSecondByRate[test.want])
			}
		})
	}
}

func TestDemonTransformsInputBeforeMovement(t *testing.T) {
	config := testConfig()
	config.Rules.SpeedPixelsPerSecondByRate = nativeSpeedPixelsPerSecondByRate
	engine := mustEngine(t, config)
	actor := &engine.actors[0]
	actor.TransformationSceneID = 110
	actor.AvatarRoleID = 42
	start := actor.Position
	if _, err := engine.Step([]Action{{PlayerID: actor.PlayerID, Move: DirectionRight}}); err != nil {
		t.Fatal(err)
	}
	if actor.Position.X >= start.X || actor.Facing != DirectionLeft {
		t.Fatalf("demon right input did not move/facing left: start=%+v actor=%+v", start, *actor)
	}
}

func TestDemonVirtualAIUsesWorldDirection(t *testing.T) {
	config := testConfig()
	config.Rules.SpeedPixelsPerSecondByRate = nativeSpeedPixelsPerSecondByRate
	config.Participants[0].Source = ParticipantVirtualAI
	engine := mustEngine(t, config)
	actor := &engine.actors[0]
	actor.TransformationSceneID = 110
	actor.AvatarRoleID = 42
	start := actor.Position
	if _, err := engine.Step([]Action{{PlayerID: actor.PlayerID, Move: DirectionRight}}); err != nil {
		t.Fatal(err)
	}
	if actor.Position.X <= start.X || actor.Facing != DirectionRight {
		t.Fatalf("virtual demon world-right action did not move/facing right: start=%+v actor=%+v", start, *actor)
	}
}

func TestDiscreteActionsRoundTripAndMaskMatchesLegalActions(t *testing.T) {
	engine := mustEngine(t, testConfig())
	mask, err := engine.LegalActionMask(1)
	if err != nil {
		t.Fatal(err)
	}
	for id := ActionID(0); id < DiscreteActionCount; id++ {
		action, ok := ActionFromID(1, id)
		if !ok {
			t.Fatalf("ActionFromID(%d) failed", id)
		}
		got, ok := action.ID()
		if !ok || got != id {
			t.Fatalf("action %d round trip = %d/%v", id, got, ok)
		}
		legal := containsAction(mustLegalActions(t, engine, 1), action)
		if mask[id] != legal {
			t.Fatalf("mask[%d]=%v, legal=%v", id, mask[id], legal)
		}
	}
}

func mustLegalActions(t *testing.T, engine *Engine, playerID uint16) []Action {
	t.Helper()
	actions, err := engine.LegalActions(playerID)
	if err != nil {
		t.Fatal(err)
	}
	return actions
}

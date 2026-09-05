package battlereplay

import (
	"fmt"
	"sort"

	"qqtang/internal/game/battleengine"
	"qqtang/internal/game/qbv"
	"qqtang/internal/protocol/game"
)

type EventCountDiff struct {
	Kind     battleengine.EventKind
	Expected int
	Actual   int
}

type ActionCountDiff struct {
	ActionID uint8
	Expected int
	Actual   int
}

type ActorCellDiff struct {
	PlayerID uint16
	TimeMS   uint32
	Native   battleengine.Cell
	Engine   battleengine.Cell
	Matches  bool
}

type ActorPositionDiff struct {
	PlayerID     uint16
	TimeMS       uint32
	Move         battleengine.Direction
	Native       battleengine.Position
	Engine       battleengine.Position
	NativeCell   battleengine.Cell
	EngineCell   battleengine.Cell
	ExactMatches bool
	CellMatches  bool
}

type Report struct {
	EventAligned        bool
	RecordingDurationMS uint32
	EngineElapsedMS     uint32
	EngineOutcome       battleengine.Outcome
	EventCounts         []EventCountDiff
	ActionCounts        []ActionCountDiff
	ActorCells          []ActorCellDiff
	FirstActorPositions []ActorPositionDiff
	FirstActorCellDiffs []ActorPositionDiff
	EngineEvents        []battleengine.Event
	Warnings            []string
}

type movementPlan struct {
	Direction battleengine.Direction
	End       battleengine.Position
}

func (report Report) Matches() bool {
	for _, count := range report.EventCounts {
		if count.Expected != count.Actual {
			return false
		}
	}
	for _, count := range report.ActionCounts {
		if count.Expected != count.Actual {
			return false
		}
	}
	if !report.EventAligned {
		for _, actor := range report.ActorCells {
			if !actor.Matches {
				return false
			}
		}
		if len(report.FirstActorPositions) != 0 {
			return false
		}
		if len(report.FirstActorCellDiffs) != 0 {
			return false
		}
	}
	return len(report.Warnings) == 0
}

// Run advances a fresh restricted engine using QBV input state changes. Event
// times are quantized only by Config.Rules.TickMS; the report exposes both
// semantic event counts and final native-vs-engine cells. This deliberately
// never snaps actors to recorded coordinates, because doing so would hide a
// movement or collision divergence.
func Run(config battleengine.Config, recording qbv.Recording) (Report, error) {
	return run(config, recording, false)
}

// RunEventAligned validates event-domain rules while treating original-client
// absolute movement checkpoints as authoritative observations. It complements,
// rather than replaces, Run: strict movement equality remains visible in the
// normal report, while this mode prevents one early pixel drift from cascading
// into unrelated placement, pickup, trap and death mismatches.
func RunEventAligned(config battleengine.Config, recording qbv.Recording) (Report, error) {
	return run(config, recording, true)
}

func run(config battleengine.Config, recording qbv.Recording, eventAligned bool) (Report, error) {
	timeline, err := BuildTimeline(recording)
	if err != nil {
		return Report{}, err
	}
	// Native pickup requests are authoritative replay inputs. Do not also
	// instantiate GAME_BEGIN's hidden copies, or a simulated overlap followed
	// by the recorded 0x0FAC/0x0FAD pair could apply one item twice. Placement
	// remains independently covered by the deterministic map adapter tests.
	replayConfig := config
	replayConfig.Pickups = nil
	engine, err := battleengine.New(replayConfig)
	if err != nil {
		return Report{}, err
	}
	if config.Rules.TickMS == 0 {
		return Report{}, fmt.Errorf("battle replay tick must be non-zero")
	}
	moves := make(map[uint16]movementPlan, len(config.Participants))
	inputIndex := 0
	checkpointIndex := 0
	firstPositionDiff := make(map[uint16]ActorPositionDiff, len(config.Participants))
	firstCellDiff := make(map[uint16]ActorPositionDiff, len(config.Participants))
	engineEvents := make([]battleengine.Event, 0, len(recording.Events)/4)
	warnings := make([]string, 0)
	for engine.ElapsedMS() <= recording.DurationMS() && !engine.Terminal().Ended {
		currentTime := engine.ElapsedMS()
		for checkpointIndex < len(timeline.Checkpoints) && timeline.Checkpoints[checkpointIndex].TimeMS <= currentTime {
			checkpoint := timeline.Checkpoints[checkpointIndex]
			if actor, ok := actorByPlayerID(engine.Actors(), checkpoint.PlayerID); ok {
				native := battleengine.Position{X: int32(checkpoint.PositionX), Y: int32(checkpoint.PositionY)}
				if actor.Position != native {
					difference := ActorPositionDiff{
						PlayerID: checkpoint.PlayerID, TimeMS: checkpoint.TimeMS, Move: moves[checkpoint.PlayerID].Direction,
						Native: native, Engine: actor.Position,
						NativeCell: checkpoint.Cell, EngineCell: actor.Position.Cell(),
						ExactMatches: false, CellMatches: checkpoint.Cell == actor.Position.Cell(),
					}
					if _, exists := firstPositionDiff[checkpoint.PlayerID]; !exists {
						firstPositionDiff[checkpoint.PlayerID] = difference
					}
					if !difference.CellMatches {
						if _, exists := firstCellDiff[checkpoint.PlayerID]; !exists {
							firstCellDiff[checkpoint.PlayerID] = difference
						}
					}
				}
				if eventAligned && actor.Position != native {
					if checkpointErr := engine.ApplyVerifiedMovementCheckpoint(checkpoint.PlayerID, native); checkpointErr != nil {
						warnings = append(warnings, fmt.Sprintf("native movement checkpoint at %d ms could not be applied: %v", currentTime, checkpointErr))
					}
				}
			}
			checkpointIndex++
		}
		bombPulse := make(map[uint16]bool)
		usePulse := make(map[uint16]uint8)
		for inputIndex < len(timeline.Inputs) && timeline.Inputs[inputIndex].TimeMS <= currentTime {
			input := timeline.Inputs[inputIndex]
			switch input.Kind {
			case InputMovement:
				moves[input.PlayerID] = movementPlan{Direction: input.Move, End: input.MoveEnd}
			case InputPlaceBomb:
				if eventAligned {
					event, placementErr := engine.ApplyVerifiedBombPlacement(input.PlayerID, input.BombCell, input.BombPower, input.BombProperty)
					if placementErr != nil {
						warnings = append(warnings, fmt.Sprintf("native bomb placement at %d ms could not be applied: %v", currentTime, placementErr))
					} else {
						engineEvents = append(engineEvents, event)
					}
				} else {
					bombPulse[input.PlayerID] = true
				}
			case InputPickup:
				event, pickupErr := engine.ApplyVerifiedPickup(input.PlayerID, input.SceneID)
				if pickupErr != nil {
					warnings = append(warnings, fmt.Sprintf("native pickup at %d ms could not be applied after simulation divergence: %v", currentTime, pickupErr))
					break
				}
				engineEvents = append(engineEvents, event)
			case InputUseAction:
				usePulse[input.PlayerID] = input.UseActionID
			case InputConfirmDeath:
				events, confirmErr := engine.ConfirmTrappedDeath(input.PlayerID)
				if confirmErr != nil {
					// Keep running to surface the surrounding divergences; a native
					// death before the simulated trap is itself part of the report.
				} else {
					engineEvents = append(engineEvents, events...)
				}
			}
			inputIndex++
		}
		actions := make([]battleengine.Action, 0, len(config.Participants))
		for _, participant := range config.Participants {
			move := replayMovementDirection(engine, participant.PlayerID, moves[participant.PlayerID])
			actionID := usePulse[participant.PlayerID]
			if actionID != 0 && !engineCanUseNativeAction(engine, participant.PlayerID, actionID) {
				warnings = append(warnings, fmt.Sprintf("native action %d by player %d at %d ms was unavailable after simulation divergence", actionID, participant.PlayerID, currentTime))
				actionID = 0
			}
			if move != battleengine.DirectionNone || bombPulse[participant.PlayerID] || actionID != 0 {
				actions = append(actions, battleengine.Action{PlayerID: participant.PlayerID, Move: move, PlaceBomb: bombPulse[participant.PlayerID], UseActionID: actionID})
			}
		}
		events, stepErr := engine.Step(actions)
		if stepErr != nil {
			return Report{}, fmt.Errorf("battle replay step at %d ms: %w", currentTime, stepErr)
		}
		engineEvents = append(engineEvents, events...)
	}
	report := Report{
		EventAligned:        eventAligned,
		RecordingDurationMS: recording.DurationMS(), EngineElapsedMS: engine.ElapsedMS(),
		EngineOutcome: engine.Terminal(), EngineEvents: engineEvents, Warnings: warnings,
	}
	report.EventCounts = compareEventCounts(timeline.Native, engineEvents)
	report.ActionCounts = compareActionCounts(timeline.Native, engineEvents)
	report.ActorCells = compareFinalActorCells(timeline.Checkpoints, engine.Actors())
	report.FirstActorPositions = sortedActorPositionDiffs(firstPositionDiff)
	report.FirstActorCellDiffs = sortedActorPositionDiffs(firstCellDiff)
	return report, nil
}

func replayMovementDirection(engine *battleengine.Engine, playerID uint16, plan movementPlan) battleengine.Direction {
	if plan.Direction == battleengine.DirectionNone {
		return battleengine.DirectionNone
	}
	actor, ok := actorByPlayerID(engine.Actors(), playerID)
	if !ok {
		return battleengine.DirectionNone
	}
	switch plan.Direction {
	case battleengine.DirectionUp:
		if actor.Position.Y <= plan.End.Y {
			return battleengine.DirectionNone
		}
	case battleengine.DirectionRight:
		if actor.Position.X >= plan.End.X {
			return battleengine.DirectionNone
		}
	case battleengine.DirectionDown:
		if actor.Position.Y >= plan.End.Y {
			return battleengine.DirectionNone
		}
	case battleengine.DirectionLeft:
		if actor.Position.X <= plan.End.X {
			return battleengine.DirectionNone
		}
	}
	return plan.Direction
}

func engineCanUseNativeAction(engine *battleengine.Engine, playerID uint16, actionID uint8) bool {
	legal, err := engine.LegalActions(playerID)
	if err != nil {
		return false
	}
	for _, action := range legal {
		if action.UseActionID == actionID {
			return true
		}
	}
	return false
}

func compareActionCounts(native []qbv.NativeEvent, actual []battleengine.Event) []ActionCountDiff {
	type actionKey struct {
		PlayerID   uint16
		ClientTime uint32
		ActionID   uint8
	}
	expected := make(map[uint8]int)
	seen := make(map[actionKey]struct{})
	for _, event := range native {
		if event.Kind != qbv.NativeEventUseItem || event.Record.Schema == game.NotifyPlayerAffection || event.UseItem.ItemID > 0xff {
			continue
		}
		actionID := uint8(event.UseItem.ItemID)
		if !supportedActionID(actionID) {
			continue
		}
		key := actionKey{PlayerID: event.UseItem.PlayerID, ClientTime: event.UseItem.ClientTime, ActionID: actionID}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		expected[actionID]++
	}
	observed := make(map[uint8]int)
	for _, event := range actual {
		if event.Kind == battleengine.EventBattleActionUsed {
			observed[event.ActionID]++
		}
	}
	ids := make([]int, 0, len(expected))
	for actionID := range expected {
		ids = append(ids, int(actionID))
	}
	sort.Ints(ids)
	result := make([]ActionCountDiff, 0, len(ids))
	for _, value := range ids {
		actionID := uint8(value)
		result = append(result, ActionCountDiff{ActionID: actionID, Expected: expected[actionID], Actual: observed[actionID]})
	}
	return result
}

func actorByPlayerID(actors []battleengine.Actor, playerID uint16) (battleengine.Actor, bool) {
	for _, actor := range actors {
		if actor.PlayerID == playerID {
			return actor, true
		}
	}
	return battleengine.Actor{}, false
}

func sortedActorPositionDiffs(byPlayer map[uint16]ActorPositionDiff) []ActorPositionDiff {
	ids := make([]int, 0, len(byPlayer))
	for playerID := range byPlayer {
		ids = append(ids, int(playerID))
	}
	sort.Ints(ids)
	diffs := make([]ActorPositionDiff, 0, len(ids))
	for _, value := range ids {
		diffs = append(diffs, byPlayer[uint16(value)])
	}
	return diffs
}

func compareEventCounts(native []qbv.NativeEvent, actual []battleengine.Event) []EventCountDiff {
	expected := make(map[battleengine.EventKind]int)
	for _, event := range native {
		switch event.Kind {
		case qbv.NativeEventBombPlaced:
			expected[battleengine.EventBombPlaced]++
		case qbv.NativeEventBombExploded:
			expected[battleengine.EventBombExploded] += event.BombExplode.BombCount
		case qbv.NativeEventActorTrapped:
			expected[battleengine.EventActorTrapped]++
		case qbv.NativeEventActorKilled:
			expected[battleengine.EventActorEliminated]++
		case qbv.NativeEventActorInteraction:
			if event.Record.Schema == 0x0FAB {
				expected[battleengine.EventActorRescued]++
			}
		case qbv.NativeEventItem:
			if event.Record.Schema == 0x0FAD {
				expected[battleengine.EventPickupCollected]++
			}
		case qbv.NativeEventMapElementMoved:
			expected[battleengine.EventMapElementMoved]++
		case qbv.NativeEventBombMoved:
			// The outgoing 0x0FB4 and byte-identical 0x139C confirmation are
			// one contact kick. Only the arbitrator-confirmed notification is
			// compared with the engine's automatic Panda/bomb collision event.
			expected[battleengine.EventBombKicked]++
		}
	}
	observed := make(map[battleengine.EventKind]int)
	for _, event := range actual {
		observed[event.Kind]++
	}
	kinds := make([]int, 0, len(expected))
	for kind := range expected {
		kinds = append(kinds, int(kind))
	}
	sort.Ints(kinds)
	diffs := make([]EventCountDiff, 0, len(kinds))
	for _, value := range kinds {
		kind := battleengine.EventKind(value)
		diffs = append(diffs, EventCountDiff{Kind: kind, Expected: expected[kind], Actual: observed[kind]})
	}
	return diffs
}

func compareFinalActorCells(checkpoints []ActorCheckpoint, actors []battleengine.Actor) []ActorCellDiff {
	latest := make(map[uint16]ActorCheckpoint)
	for _, checkpoint := range checkpoints {
		latest[checkpoint.PlayerID] = checkpoint
	}
	actorByID := make(map[uint16]battleengine.Actor, len(actors))
	for _, actor := range actors {
		actorByID[actor.PlayerID] = actor
	}
	ids := make([]int, 0, len(latest))
	for playerID := range latest {
		ids = append(ids, int(playerID))
	}
	sort.Ints(ids)
	diffs := make([]ActorCellDiff, 0, len(ids))
	for _, value := range ids {
		playerID := uint16(value)
		checkpoint := latest[playerID]
		actor, ok := actorByID[playerID]
		engineCell := battleengine.Cell{Row: -1, Col: -1}
		if ok {
			engineCell = actor.Position.Cell()
		}
		diffs = append(diffs, ActorCellDiff{PlayerID: playerID, TimeMS: checkpoint.TimeMS, Native: checkpoint.Cell, Engine: engineCell, Matches: ok && checkpoint.Cell == engineCell})
	}
	return diffs
}

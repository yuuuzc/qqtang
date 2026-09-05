package battleengine

import "fmt"

// ActionID is the stable discrete action vocabulary shared by scripted bots,
// search and future RL policies. Existing IDs are append-only: adding active
// items later must not renumber the native movement/placement actions below.
type ActionID uint16

const (
	ActionWait ActionID = iota
	ActionMoveUp
	ActionMoveRight
	ActionMoveDown
	ActionMoveLeft
	ActionPlaceBomb
	ActionMoveUpAndPlaceBomb
	ActionMoveRightAndPlaceBomb
	ActionMoveDownAndPlaceBomb
	ActionMoveLeftAndPlaceBomb
	ActionUse41
	ActionUse42
	ActionUse43
	ActionUse44
	ActionUse46
	ActionUse63
	ActionUse64
	// Native item use does not suspend the independently held direction key.
	// Keep these append-only combinations in item-major, direction-minor order.
	ActionMoveUpAndUse41
	ActionMoveRightAndUse41
	ActionMoveDownAndUse41
	ActionMoveLeftAndUse41
	ActionMoveUpAndUse42
	ActionMoveRightAndUse42
	ActionMoveDownAndUse42
	ActionMoveLeftAndUse42
	ActionMoveUpAndUse43
	ActionMoveRightAndUse43
	ActionMoveDownAndUse43
	ActionMoveLeftAndUse43
	ActionMoveUpAndUse44
	ActionMoveRightAndUse44
	ActionMoveDownAndUse44
	ActionMoveLeftAndUse44
	ActionMoveUpAndUse46
	ActionMoveRightAndUse46
	ActionMoveDownAndUse46
	ActionMoveLeftAndUse46
	ActionMoveUpAndUse63
	ActionMoveRightAndUse63
	ActionMoveDownAndUse63
	ActionMoveLeftAndUse63
	ActionMoveUpAndUse64
	ActionMoveRightAndUse64
	ActionMoveDownAndUse64
	ActionMoveLeftAndUse64
	DiscreteActionCount
)

var nativeUseActionIDs = [...]uint8{41, 42, 43, 44, 46, 63, 64}

// NativeUseActionIDAt returns the stable native item identity represented by
// one fixed inventory feature/action group. Callers must not encode the
// client's pickup-order-dependent shortcut slot index as item semantics.
func NativeUseActionIDAt(index int) (uint8, bool) {
	if index < 0 || index >= len(nativeUseActionIDs) {
		return 0, false
	}
	return nativeUseActionIDs[index], true
}

// ActionMask is a fixed-width legal-action mask suitable for a vectorized
// environment. Its indices have exactly the ActionID meanings above.
type ActionMask [DiscreteActionCount]bool

func ActionFromID(playerID uint16, id ActionID) (Action, bool) {
	if id >= ActionMoveUpAndUse41 && id < DiscreteActionCount {
		offset := id - ActionMoveUpAndUse41
		itemIndex := int(offset / 4)
		direction := Direction(offset%4) + DirectionUp
		return Action{PlayerID: playerID, Move: direction, UseActionID: nativeUseActionIDs[itemIndex]}, true
	}
	switch id {
	case ActionWait, ActionMoveUp, ActionMoveRight, ActionMoveDown, ActionMoveLeft:
		return Action{PlayerID: playerID, Move: Direction(id)}, true
	case ActionPlaceBomb, ActionMoveUpAndPlaceBomb, ActionMoveRightAndPlaceBomb, ActionMoveDownAndPlaceBomb, ActionMoveLeftAndPlaceBomb:
		return Action{PlayerID: playerID, Move: Direction(id - ActionPlaceBomb), PlaceBomb: true}, true
	case ActionUse41:
		return Action{PlayerID: playerID, UseActionID: 41}, true
	case ActionUse42:
		return Action{PlayerID: playerID, UseActionID: 42}, true
	case ActionUse43:
		return Action{PlayerID: playerID, UseActionID: 43}, true
	case ActionUse44:
		return Action{PlayerID: playerID, UseActionID: 44}, true
	case ActionUse46:
		return Action{PlayerID: playerID, UseActionID: 46}, true
	case ActionUse63:
		return Action{PlayerID: playerID, UseActionID: 63}, true
	case ActionUse64:
		return Action{PlayerID: playerID, UseActionID: 64}, true
	default:
		return Action{}, false
	}
}

func (action Action) ID() (ActionID, bool) {
	if action.UseActionID != 0 {
		if action.PlaceBomb {
			return 0, false
		}
		standalone, ok := nativeActionDiscreteID(action.UseActionID)
		if !ok {
			return 0, false
		}
		if action.Move == DirectionNone {
			return standalone, true
		}
		if action.Move < DirectionUp || action.Move > DirectionLeft {
			return 0, false
		}
		itemIndex, ok := nativeActionIndex(action.UseActionID)
		if !ok {
			return 0, false
		}
		return ActionMoveUpAndUse41 + ActionID(itemIndex*4) + ActionID(action.Move-DirectionUp), true
	}
	if _, _, ok := action.Move.delta(); !ok {
		return 0, false
	}
	var id ActionID
	switch action.Move {
	case DirectionNone:
		id = ActionWait
	case DirectionUp:
		id = ActionMoveUp
	case DirectionRight:
		id = ActionMoveRight
	case DirectionDown:
		id = ActionMoveDown
	case DirectionLeft:
		id = ActionMoveLeft
	default:
		return 0, false
	}
	if action.PlaceBomb {
		id += ActionPlaceBomb
	}
	return id, true
}

func nativeActionDiscreteID(actionID uint8) (ActionID, bool) {
	switch actionID {
	case 41:
		return ActionUse41, true
	case 42:
		return ActionUse42, true
	case 43:
		return ActionUse43, true
	case 44:
		return ActionUse44, true
	case 46:
		return ActionUse46, true
	case 63:
		return ActionUse63, true
	case 64:
		return ActionUse64, true
	default:
		return 0, false
	}
}

func nativeActionIndex(actionID uint8) (int, bool) {
	for index, candidate := range nativeUseActionIDs {
		if candidate == actionID {
			return index, true
		}
	}
	return 0, false
}

// LegalActionMask is the allocation-free-width counterpart of LegalActions.
// It keeps policy tensors stable while LegalActions remains convenient for
// live Go policies and small search trees.
func (engine *Engine) LegalActionMask(playerID uint16) (ActionMask, error) {
	var mask ActionMask
	actions, err := engine.LegalActions(playerID)
	if err != nil {
		return mask, err
	}
	for _, action := range actions {
		id, ok := action.ID()
		if !ok {
			return ActionMask{}, fmt.Errorf("battle legal action %+v has no discrete ID", action)
		}
		mask[id] = true
	}
	return mask, nil
}

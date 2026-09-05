package battleengine

func actorHeldActionCount(actor *Actor, actionID uint8) uint8 {
	if actor == nil || actionID == 0 {
		return 0
	}
	for _, slot := range actor.HeldActions {
		if slot.ActionID == actionID {
			return slot.Count
		}
	}
	return 0
}

// grantHeldAction mirrors the native seven-slot inventory: an existing action
// stacks in place, otherwise the first empty slot is used. A full inventory
// rejects the grant while the scene pickup itself may still be consumed.
func grantHeldAction(actor *Actor, actionID, count uint8) bool {
	if actor == nil || actionID == 0 || count == 0 {
		return false
	}
	for index := range actor.HeldActions {
		slot := &actor.HeldActions[index]
		if slot.ActionID != actionID {
			continue
		}
		if count > ^uint8(0)-slot.Count {
			slot.Count = ^uint8(0)
		} else {
			slot.Count += count
		}
		return true
	}
	for index := range actor.HeldActions {
		if actor.HeldActions[index].ActionID == 0 {
			actor.HeldActions[index] = HeldActionSlot{ActionID: actionID, Count: count}
			return true
		}
	}
	return false
}

func consumeHeldAction(actor *Actor, actionID uint8) bool {
	if actor == nil || actionID == 0 {
		return false
	}
	for index := range actor.HeldActions {
		slot := &actor.HeldActions[index]
		if slot.ActionID != actionID || slot.Count == 0 {
			continue
		}
		slot.Count--
		if slot.Count == 0 {
			copy(actor.HeldActions[index:], actor.HeldActions[index+1:])
			actor.HeldActions[len(actor.HeldActions)-1] = HeldActionSlot{}
		}
		return true
	}
	return false
}

func removeHeldAction(actor *Actor, actionID uint8) {
	if actor == nil || actionID == 0 {
		return
	}
	for actorHeldActionCount(actor, actionID) != 0 {
		for index := range actor.HeldActions {
			if actor.HeldActions[index].ActionID != actionID {
				continue
			}
			copy(actor.HeldActions[index:], actor.HeldActions[index+1:])
			actor.HeldActions[len(actor.HeldActions)-1] = HeldActionSlot{}
			break
		}
	}
}

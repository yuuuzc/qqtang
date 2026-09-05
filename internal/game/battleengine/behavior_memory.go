package battleengine

const publicBehaviorRecentWindowMS uint32 = 5_000

// recordPublicBehavior updates round-local memory only from events visible to
// every participant. A rejected private input does not masquerade as public
// movement or placement; its absence is observed as waiting.
func (engine *Engine) recordPublicBehavior(events []Event) {
	if engine == nil {
		return
	}
	var visible [MaxParticipants][PublicBehaviorKindCount]bool
	for _, event := range events {
		kind := PublicBehaviorKindCount
		switch event.Kind {
		case EventActorMoved:
			kind = PublicBehaviorMove
		case EventBombPlaced:
			kind = PublicBehaviorBomb
		case EventBattleActionUsed, EventBattleActionUseRequested:
			kind = PublicBehaviorItem
		}
		if kind >= PublicBehaviorKindCount || event.PlayerID == 0 {
			continue
		}
		if actorIndex := engine.actorIndex(event.PlayerID); actorIndex >= 0 {
			visible[actorIndex][kind] = true
		}
	}
	for index := range engine.actors {
		actor := &engine.actors[index]
		if actor.State == ActorEliminated {
			continue
		}
		flags := visible[index]
		flags[PublicBehaviorWait] = !flags[PublicBehaviorMove] &&
			!flags[PublicBehaviorBomb] && !flags[PublicBehaviorItem]
		if actor.PublicBehavior.Samples != ^uint32(0) {
			actor.PublicBehavior.Samples++
		}
		for kind := PublicBehaviorKind(0); kind < PublicBehaviorKindCount; kind++ {
			if flags[kind] && actor.PublicBehavior.Totals[kind] != ^uint32(0) {
				actor.PublicBehavior.Totals[kind]++
			}
			actor.PublicBehavior.Recent[kind] = updatePublicBehaviorEMA(
				actor.PublicBehavior.Recent[kind], flags[kind], engine.rules.TickMS,
			)
		}
	}
}

func updatePublicBehaviorEMA(current uint16, observed bool, tickMS uint32) uint16 {
	if tickMS == 0 {
		return current
	}
	if tickMS > publicBehaviorRecentWindowMS {
		tickMS = publicBehaviorRecentWindowMS
	}
	target := int64(0)
	if observed {
		target = int64(^uint16(0))
	}
	value := int64(current)
	delta := target - value
	step := delta * int64(tickMS) / int64(publicBehaviorRecentWindowMS)
	// Keep rare events visible and stale signals decaying near fixed-point
	// endpoints, where integer division would otherwise freeze the EMA.
	if step == 0 && delta != 0 {
		if delta > 0 {
			step = 1
		} else {
			step = -1
		}
	}
	value += step
	if value < 0 {
		return 0
	}
	if value > int64(^uint16(0)) {
		return ^uint16(0)
	}
	return uint16(value)
}

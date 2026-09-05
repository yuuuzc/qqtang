package battleengine

import "sort"

func (engine *Engine) evaluateTerminal() (Event, bool) {
	if engine.outcome.Ended {
		return Event{}, false
	}
	teams := make([]byte, 0, len(engine.actors))
	for _, actor := range engine.actors {
		if actor.State == ActorEliminated {
			continue
		}
		index := sort.Search(len(teams), func(index int) bool { return teams[index] >= actor.TeamID })
		if index == len(teams) || teams[index] != actor.TeamID {
			teams = append(teams, 0)
			copy(teams[index+1:], teams[index:])
			teams[index] = actor.TeamID
		}
	}
	roundElapsed := engine.RoundElapsedMS()
	if len(teams) <= 1 {
		engine.outcome = Outcome{Ended: true, EndedAtMS: roundElapsed}
		if len(teams) == 0 {
			engine.outcome.Draw = true
		} else {
			engine.outcome.WinnerTeamID = teams[0]
		}
		return Event{Kind: EventMatchEnded, TimeMS: engine.elapsedMS, TeamID: engine.outcome.WinnerTeamID}, true
	}
	// RoundDurationMS is the native scene's absolute deadline. StartClockMS is
	// already consumed by Ready/Go, so a 240000 rule has 237000 ms of
	// controllable play rather than ending at 243000.
	if engine.elapsedMS >= engine.rules.RoundDurationMS {
		engine.outcome = Outcome{Ended: true, Draw: true, TimedOut: true, EndedAtMS: roundElapsed}
		return Event{Kind: EventMatchEnded, TimeMS: engine.elapsedMS}, true
	}
	return Event{}, false
}

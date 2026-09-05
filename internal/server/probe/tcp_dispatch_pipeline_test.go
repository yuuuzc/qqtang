package probe

import "testing"

func TestStartFollowUpRequiresOrderedTCPDelivery(t *testing.T) {
	outcome := tcpDispatchOutcome{response: []byte{1}, startFollowUp: []byte{2}}
	if !outcome.requiresOrderedDelivery() {
		t.Fatal("ACK plus objective GAME_OVER startFollowUp bypassed ordered delivery")
	}
}

func TestFinalVictoryRequiresOrderedTCPDelivery(t *testing.T) {
	outcome := tcpDispatchOutcome{
		response:             []byte{1},
		finalVictorySchedule: &adventureFinalVictorySchedule{},
	}
	if !outcome.requiresOrderedDelivery() {
		t.Fatal("final adventure victory schedule bypassed ordered delivery")
	}
}

func TestMatchStartCarriesPostResponseIntoTCPDelivery(t *testing.T) {
	called := false
	outcome := tcpDispatchOutcome{}
	outcome.applyMatchStart(matchStartMessageResult{
		handled:      true,
		response:     []byte{1},
		followUp:     []byte{2},
		postResponse: func() { called = true },
	})
	if outcome.postResponse == nil {
		t.Fatal("match start dropped its post-response room projection")
	}
	outcome.postResponse()
	if !called {
		t.Fatal("match start post-response projection was not preserved")
	}
}

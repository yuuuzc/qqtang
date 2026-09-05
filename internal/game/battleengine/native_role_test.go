package battleengine

import "testing"

func TestParticipantFromNativeRoleProjectsExactAttributes(t *testing.T) {
	participant, err := ParticipantFromNativeRole(42, 10, 2, ParticipantVirtualAI)
	if err != nil {
		t.Fatal(err)
	}
	if participant.PlayerID != 42 || participant.RoleID != 10 || participant.TeamID != 2 || participant.Source != ParticipantVirtualAI {
		t.Fatalf("participant identity = %+v", participant)
	}
	if participant.BombCapacity != 3 || participant.MaxBombCapacity != 8 ||
		participant.BombPower != 4 || participant.MaxBombPower != 9 ||
		participant.SpeedRate != 5 || participant.MaxSpeedRate != 8 {
		t.Fatalf("participant native attributes = %+v", participant)
	}
	if participant.SpeedPixelsPerSecond != 0 {
		t.Fatalf("participant speed projection = %d before native map binding", participant.SpeedPixelsPerSecond)
	}

	config := testConfig()
	config.Rules.SpeedPixelsPerSecondByRate = nativeSpeedPixelsPerSecondByRate
	participant.Spawn = Cell{Row: 2, Col: 2}
	config.Participants[1] = participant
	engine, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	actors := engine.Actors()
	if actors[1].RoleID != 10 || actors[1].SpeedPixelsPerSecond != 180 {
		t.Fatalf("resolved native actor = %+v", actors[1])
	}
	observation, err := engine.Observation(42)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Actors[1].RoleID != 10 {
		t.Fatalf("observed RoleID = %d, want 10", observation.Actors[1].RoleID)
	}
}

func TestParticipantFromNativeRoleRejectsNonPlayableModels(t *testing.T) {
	for _, roleID := range []uint16{0, 12, 17, 23, 24, 36} {
		if _, err := ParticipantFromNativeRole(1, roleID, 1, ParticipantHuman); err == nil {
			t.Fatalf("RoleID %d was accepted", roleID)
		}
	}
}

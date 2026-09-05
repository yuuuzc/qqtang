package winlaunch

import "testing"

func TestLobbyStageAddressRelationship(t *testing.T) {
	callbackObject := uintptr(0x1234BF30)
	lobbyObject := callbackObject - qqtSectionLobbyCallbackOffset
	if got, want := lobbyObject+qqtSectionLobbyStageOffset, uintptr(0x123401A0); got != want {
		t.Fatalf("stage address = 0x%X, want 0x%X", got, want)
	}
	if got, want := lobbyObject+qqtSectionLobbyUINOffset, uintptr(0x123401A8); got != want {
		t.Fatalf("UIN address = 0x%X, want 0x%X", got, want)
	}
}

func TestShouldAdvanceTutorialLobbyStage(t *testing.T) {
	if !shouldAdvanceTutorialLobbyStage(1, 1000001, 1000001) {
		t.Fatal("same authenticated UIN at stage 1 must be advanced")
	}
	for _, test := range []struct {
		stage, uin, authenticated uint32
	}{
		{2, 1000001, 1000001},
		{1, 0, 1000001},
		{1, 1000002, 1000001},
		{3, 1000001, 1000001},
	} {
		if shouldAdvanceTutorialLobbyStage(test.stage, test.uin, test.authenticated) {
			t.Fatalf("unexpected advance for stage=%d uin=%d authenticated=%d", test.stage, test.uin, test.authenticated)
		}
	}
}

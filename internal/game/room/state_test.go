package room

import "testing"

func TestGameTypeCategoriesAndCompetitiveFields(t *testing.T) {
	for _, gameType := range []GameType{GameTypeCompetitiveNoItem, GameTypeCompetitiveItem, GameTypeCompetitiveLoot} {
		category, err := gameType.Category()
		if err != nil || category != CategoryCompetitive {
			t.Fatalf("game type %d category = %d, %v", gameType, category, err)
		}
		field, ok := gameType.CompetitiveField()
		if !ok || byte(field) != byte(gameType) {
			t.Fatalf("game type %d competitive field = %d, %v", gameType, field, ok)
		}
	}
	for _, gameType := range []GameType{GameTypeAdventure, GameTypeChat} {
		if _, ok := gameType.CompetitiveField(); ok {
			t.Fatalf("non-competitive game type %d exposed a competitive field", gameType)
		}
	}
}

func TestAdventureRoomCapacityTracksExplicitSeatLocks(t *testing.T) {
	state, err := New(1, Member{PlayerID: 1, TeamID: 1}, MatchSettings{Map: RandomMapSelection(), GameType: GameTypeAdventure})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := state.Snapshot()
	if snapshot.Capacity() != 8 || snapshot.Members[0].SeatID != 1 {
		t.Fatalf("initial adventure seats = capacity %d, members %+v, locks %+v", snapshot.Capacity(), snapshot.Members, snapshot.LockedSeats)
	}
	if _, err := state.SetSeatLocked(1, 5, true); err != nil {
		t.Fatal(err)
	}
	snapshot = state.Snapshot()
	if snapshot.Capacity() != 7 {
		t.Fatalf("capacity after locking seat 5 = %d, want 7", snapshot.Capacity())
	}
	if _, err := state.Join(Member{PlayerID: 2, TeamID: 1, SeatID: 5}); err == nil {
		t.Fatal("locked seat accepted a joining player")
	}
	if _, err := state.SetSeatLocked(1, 5, false); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Join(Member{PlayerID: 2, TeamID: 1, SeatID: 5}); err != nil {
		t.Fatal(err)
	}
}

func TestCompetitiveRoomStartsWithEightOpenSeats(t *testing.T) {
	state, err := New(1, Member{PlayerID: 1, TeamID: 1}, MatchSettings{Map: RandomMapSelection(), GameType: GameTypeCompetitiveItem})
	if err != nil {
		t.Fatal(err)
	}
	if capacity := state.Snapshot().Capacity(); capacity != 8 {
		t.Fatalf("competitive room capacity = %d, want 8", capacity)
	}
}

func TestJoinReusesLowestUnlockedVacatedSeat(t *testing.T) {
	state, err := New(1, Member{PlayerID: 1, TeamID: 1}, MatchSettings{Map: RandomMapSelection(), GameType: GameTypeAdventure})
	if err != nil {
		t.Fatal(err)
	}
	for playerID := uint16(2); playerID <= 4; playerID++ {
		if _, err := state.Join(Member{PlayerID: playerID, TeamID: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := state.SetSeatLocked(1, 2, true); err == nil {
		t.Fatal("occupied seat 2 was locked")
	}
	if _, err := state.Leave(2, LeaveVoluntary); err != nil {
		t.Fatal(err)
	}
	if _, err := state.SetSeatLocked(1, 2, true); err != nil {
		t.Fatal(err)
	}
	if _, err := state.Leave(3, LeaveVoluntary); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.Join(Member{PlayerID: 5, TeamID: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, member := range snapshot.Members {
		if member.PlayerID == 5 {
			if member.SeatID != 3 {
				t.Fatalf("new member seat = %d, want lowest unlocked vacancy 3", member.SeatID)
			}
			return
		}
	}
	t.Fatal("new member missing from room snapshot")
}

func TestNormalMatchCompletionKeepsRoomMembersAndSettings(t *testing.T) {
	state, err := New(1, Member{PlayerID: 7, RoleID: 3, TeamID: 1, Ready: true}, MatchSettings{Map: FixedMapSelection(1649), GameType: GameTypeAdventure})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.StartMatch(99); err != nil {
		t.Fatal(err)
	}
	if _, err := state.BeginSettlement(99); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CompleteMatch(99)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Phase != PhasePreparing || snapshot.ActiveGameID != 0 {
		t.Fatalf("completed room phase/game = %d/%d", snapshot.Phase, snapshot.ActiveGameID)
	}
	if len(snapshot.Members) != 1 || snapshot.Members[0].PlayerID != 7 || snapshot.Members[0].Ready {
		t.Fatalf("members after normal completion = %+v", snapshot.Members)
	}
	if snapshot.OwnerID != 7 || snapshot.Settings.Map != FixedMapSelection(1649) || snapshot.Settings.GameType != GameTypeAdventure {
		t.Fatalf("room identity/settings changed after normal completion: %+v", snapshot)
	}
}

func TestLeavingOwnerTransfersRoomWithoutDissolvingIt(t *testing.T) {
	state, err := New(3, Member{PlayerID: 9, TeamID: 1}, MatchSettings{Map: FixedMapSelection(1601)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.Join(Member{PlayerID: 4, TeamID: 1}); err != nil {
		t.Fatal(err)
	}
	departure, err := state.Leave(9, LeaveDisconnected)
	if err != nil {
		t.Fatal(err)
	}
	if departure.Empty || departure.NewOwnerID != 4 {
		t.Fatalf("departure = %+v", departure)
	}
	if snapshot := state.Snapshot(); len(snapshot.Members) != 1 || snapshot.OwnerID != 4 {
		t.Fatalf("remaining room = %+v", snapshot)
	}
}

func TestLeavingLowestPlayerIDOwnerStillTransfersOwnership(t *testing.T) {
	state, err := New(3, Member{PlayerID: 1, TeamID: 1}, MatchSettings{Map: FixedMapSelection(1601)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.Join(Member{PlayerID: 2, TeamID: 1}); err != nil {
		t.Fatal(err)
	}
	departure, err := state.Leave(1, LeaveVoluntary)
	if err != nil {
		t.Fatal(err)
	}
	if departure.NewOwnerID != 2 || state.Snapshot().OwnerID != 2 {
		t.Fatalf("owner did not migrate from lowest player ID: departure=%+v snapshot=%+v", departure, state.Snapshot())
	}
}

func TestRoomMemberAndMatchConfigurationChangeOnlyWhilePreparing(t *testing.T) {
	state, err := New(4, Member{PlayerID: 1, TeamID: 1}, MatchSettings{Map: FixedMapSelection(1601)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.Join(Member{PlayerID: 2, RoleID: 3, TeamID: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.UpdateMember(Member{PlayerID: 2, RoleID: 5, TeamID: 1, Ready: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.UpdateMatchSettings(1, MatchSettings{Map: FixedMapSelection(1649), GameType: GameTypeAdventure}); err != nil {
		t.Fatal(err)
	}
	if _, err := state.StartMatch(10); err != nil {
		t.Fatal(err)
	}
	if _, err := state.UpdateMember(Member{PlayerID: 2, RoleID: 6, TeamID: 1}); err == nil {
		t.Fatal("member changed after match start")
	}
	if _, err := state.UpdateMatchSettings(1, MatchSettings{Map: FixedMapSelection(1607)}); err == nil {
		t.Fatal("match settings changed after match start")
	}
}

func TestReadyStateIsAuthoritativeIdempotentAndPreparingOnly(t *testing.T) {
	state, err := New(7, Member{PlayerID: 1, TeamID: 1}, MatchSettings{Map: RandomMapSelection(), GameType: GameTypeAdventure})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.Join(Member{PlayerID: 2, RoleID: 7, TeamID: 2}); err != nil {
		t.Fatal(err)
	}
	before := state.Snapshot().Revision
	ready, err := state.SetReady(2, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ready.Members) != 2 || !ready.Members[1].Ready || ready.Revision != before+1 {
		t.Fatalf("ready snapshot = %+v", ready)
	}
	repeated, err := state.SetReady(2, true)
	if err != nil {
		t.Fatal(err)
	}
	if repeated.Revision != ready.Revision {
		t.Fatalf("idempotent ready changed revision %d -> %d", ready.Revision, repeated.Revision)
	}
	if _, err = state.SetReady(99, true); err == nil {
		t.Fatal("non-member ready was accepted")
	}
	if _, err = state.StartMatch(11); err != nil {
		t.Fatal(err)
	}
	if _, err = state.SetReady(2, false); err == nil {
		t.Fatal("ready state changed after match start")
	}
}

func TestRoomStartRequiresOwnerReadyPeersAndAdventureLimit(t *testing.T) {
	state, err := New(8, Member{PlayerID: 1, TeamID: 1}, MatchSettings{Map: RandomMapSelection(), GameType: GameTypeAdventure})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.Join(Member{PlayerID: 2, RoleID: 7, TeamID: 2}); err != nil {
		t.Fatal(err)
	}
	if err = state.ValidateStart(2); err == nil {
		t.Fatal("non-owner start was accepted")
	}
	if err = state.ValidateStart(1); err == nil {
		t.Fatal("unready peer start was accepted")
	}
	if _, err = state.SetReady(2, true); err != nil {
		t.Fatal(err)
	}
	if err = state.ValidateStart(1); err != nil {
		t.Fatalf("ready two-player adventure start: %v", err)
	}
	for playerID := uint16(3); playerID <= 5; playerID++ {
		if playerID == 5 {
			if _, err = state.SetSeatLocked(1, byte(playerID), false); err != nil {
				t.Fatal(err)
			}
		}
		if _, err = state.Join(Member{PlayerID: playerID, RoleID: 7, TeamID: 1, SeatID: byte(playerID), Ready: true}); err != nil {
			t.Fatal(err)
		}
	}
	if err = state.ValidateStart(1); err == nil {
		t.Fatal("five-player adventure start was accepted")
	}
}

func TestCompetitiveStartRequiresOpposingBalancedTeamsUnlessFree(t *testing.T) {
	state, err := New(9, Member{PlayerID: 1, RoleID: 7, TeamID: 1}, MatchSettings{Map: RandomMapSelection(), GameType: GameTypeCompetitiveNoItem})
	if err != nil {
		t.Fatal(err)
	}
	if err = state.ValidateStart(1); err == nil {
		t.Fatal("one-player competitive start was accepted")
	}
	if _, err = state.Join(Member{PlayerID: 2, RoleID: 8, TeamID: 2, Ready: true}); err != nil {
		t.Fatal(err)
	}
	if err = state.ValidateStart(1); err != nil {
		t.Fatalf("balanced competitive start: %v", err)
	}
	if _, err = state.Join(Member{PlayerID: 3, RoleID: 9, TeamID: 1, Ready: true}); err != nil {
		t.Fatal(err)
	}
	if err = state.ValidateStart(1); err == nil {
		t.Fatal("unbalanced standard competitive start was accepted")
	}
	if _, err = state.UpdateProperties(1, Properties{Flag: PropertyFlagFreeRule}); err != nil {
		t.Fatal(err)
	}
	if err = state.ValidateStart(1); err != nil {
		t.Fatalf("unbalanced free competitive start: %v", err)
	}
}

func TestSinglePlayerCompetitiveStartRequiresServerAuthorization(t *testing.T) {
	state, err := New(11, Member{PlayerID: 1, RoleID: 7, TeamID: 1}, MatchSettings{
		Map: FixedMapSelection(11), GameType: GameTypeCompetitiveNoItem,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = state.ValidateStart(1); err == nil {
		t.Fatal("ordinary validation accepted a one-player competitive room")
	}
	if err = state.ValidateStartWithOptions(1, StartValidationOptions{AllowSinglePlayerCompetitive: true}); err != nil {
		t.Fatalf("server-authorized one-player competitive start: %v", err)
	}
}

func TestCompetitiveStartValidatesProjectedVirtualTopologyWithoutRoomMembership(t *testing.T) {
	state, err := New(12, Member{PlayerID: 1, RoleID: 7, TeamID: 1}, MatchSettings{
		Map: FixedMapSelection(11), GameType: GameTypeCompetitiveNoItem,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = state.ValidateStartWithOptions(1, StartValidationOptions{VirtualCompetitiveTeamIDs: []byte{2}}); err != nil {
		t.Fatalf("one human plus one virtual opponent: %v", err)
	}
	if got := len(state.Snapshot().Members); got != 1 {
		t.Fatalf("virtual projection mutated room membership to %d", got)
	}
	if err = state.ValidateStartWithOptions(1, StartValidationOptions{VirtualCompetitiveTeamIDs: []byte{1}}); err == nil {
		t.Fatal("same-team virtual projection was accepted in a standard room")
	}
	if err = state.ValidateStartWithOptions(1, StartValidationOptions{VirtualCompetitiveTeamIDs: []byte{2, 2}}); err == nil {
		t.Fatal("one-versus-two virtual projection was accepted in a standard room")
	}
	if _, err = state.UpdateProperties(1, Properties{Flag: PropertyFlagFreeRule}); err != nil {
		t.Fatal(err)
	}
	if err = state.ValidateStartWithOptions(1, StartValidationOptions{VirtualCompetitiveTeamIDs: []byte{2, 2}}); err != nil {
		t.Fatalf("one-versus-two virtual projection in a free room: %v", err)
	}
	if err = state.ValidateStartWithOptions(1, StartValidationOptions{VirtualCompetitiveTeamIDs: []byte{9}}); err == nil {
		t.Fatal("out-of-range virtual team was accepted")
	}
	if err = state.ValidateStartWithOptions(1, StartValidationOptions{DeferCompetitiveTopology: true}); err != nil {
		t.Fatalf("pre-map topology deferral rejected structural room start: %v", err)
	}
}

func TestRoomPropertiesRejectUnknownClientFlagBits(t *testing.T) {
	state, err := New(10, Member{PlayerID: 1, RoleID: 7, TeamID: 1}, MatchSettings{Map: RandomMapSelection(), GameType: GameTypeCompetitiveNoItem})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.UpdateProperties(1, Properties{Flag: 0x80}); err == nil {
		t.Fatal("room properties accepted an unknown client flag bit")
	}
}

func TestNormalCompletionIsNotARoomLeaveReason(t *testing.T) {
	state, err := New(5, Member{PlayerID: 1, TeamID: 1}, MatchSettings{Map: FixedMapSelection(1601)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.Leave(1, LeaveReason(99)); err == nil {
		t.Fatal("unknown leave reason was accepted")
	}
	if len(state.Snapshot().Members) != 1 {
		t.Fatal("rejected leave mutated room membership")
	}
}

func TestRoomMapSelectionRequiresValidRandomOrFixedState(t *testing.T) {
	owner := Member{PlayerID: 1, TeamID: 1}
	if _, err := New(1, owner, MatchSettings{Map: RandomMapSelection()}); err != nil {
		t.Fatalf("random room selection rejected: %v", err)
	}
	if _, err := New(1, owner, MatchSettings{Map: FixedMapSelection(1)}); err != nil {
		t.Fatalf("fixed room selection rejected: %v", err)
	}
	invalid := []MatchSettings{
		{},
		{Map: MapSelection{Kind: MapSelectionRandom, MapID: 1}},
		{Map: MapSelection{Kind: MapSelectionFixed}},
	}
	for index, settings := range invalid {
		if _, err := New(1, owner, settings); err == nil {
			t.Fatalf("invalid map selection %d was accepted: %+v", index, settings)
		}
	}
}

func TestFixedMapPlayerLimitCapsPreparationMembership(t *testing.T) {
	state, err := New(13, Member{PlayerID: 1, TeamID: 1}, MatchSettings{
		Map: FixedMapSelection(1001), GameType: GameTypeCompetitiveNoItem, PlayerLimit: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Snapshot().Capacity(); got != 2 {
		t.Fatalf("fixed-map preparation capacity = %d, want 2", got)
	}
	if _, err = state.Join(Member{PlayerID: 2, TeamID: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err = state.Join(Member{PlayerID: 3, TeamID: 1}); err == nil {
		t.Fatal("third member joined a two-player map room")
	}
	if _, err = state.UpdateMatchSettings(1, MatchSettings{
		Map: RandomMapSelection(), GameType: GameTypeCompetitiveNoItem, PlayerLimit: 2,
	}); err == nil {
		t.Fatal("random map retained a stale fixed-map player limit")
	}
}

func TestWeddingRoomLifecycleIsOwnerControlledAndPairBound(t *testing.T) {
	state, err := New(12, Member{PlayerID: 1, RoleID: 7, TeamID: 1}, MatchSettings{Map: RandomMapSelection(), GameType: GameTypeChat})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.Join(Member{PlayerID: 2, RoleID: 8, TeamID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = state.SetWeddingMode(2, 1); err == nil {
		t.Fatal("non-owner changed wedding mode")
	}
	if _, err = state.SetWeddingMode(1, 1); err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.StartWedding(1, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.WeddingPending || snapshot.WeddingActive || snapshot.WeddingInitiatorID != 1 || snapshot.WeddingTargetID != 2 {
		t.Fatalf("pending wedding snapshot = %+v", snapshot)
	}
	if _, err = state.AnswerWedding(1, 2, true); err == nil {
		t.Fatal("initiator answered its own wedding request")
	}
	snapshot, err = state.AnswerWedding(2, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.WeddingPending || !snapshot.WeddingActive {
		t.Fatalf("accepted wedding snapshot = %+v", snapshot)
	}
	if _, err = state.SetWeddingMode(1, 0); err == nil {
		t.Fatal("active wedding allowed mode change")
	}
	if _, err = state.Leave(2, LeaveVoluntary); err != nil {
		t.Fatal(err)
	}
	snapshot = state.Snapshot()
	if snapshot.WeddingPending || snapshot.WeddingActive || snapshot.WeddingInitiatorID != 0 || snapshot.WeddingTargetID != 0 {
		t.Fatalf("departed wedding member left stale state = %+v", snapshot)
	}
}

func TestChatBackgroundIsOwnerControlledRoomState(t *testing.T) {
	state, err := New(12, Member{PlayerID: 1, RoleID: 7, TeamID: 1}, MatchSettings{Map: RandomMapSelection(), GameType: GameTypeChat})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = state.Join(Member{PlayerID: 2, RoleID: 8, TeamID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err = state.SetChatBackground(2, 3); err == nil {
		t.Fatal("non-owner changed chat background")
	}
	snapshot, err := state.SetChatBackground(1, 3)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.BackgroundID != 3 {
		t.Fatalf("chat background ID = %d, want 3", snapshot.BackgroundID)
	}

	competitive, err := New(13, Member{PlayerID: 1, RoleID: 7, TeamID: 1}, MatchSettings{Map: RandomMapSelection(), GameType: GameTypeCompetitiveNoItem})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = competitive.SetChatBackground(1, 3); err == nil {
		t.Fatal("competitive room accepted chat background mutation")
	}
}

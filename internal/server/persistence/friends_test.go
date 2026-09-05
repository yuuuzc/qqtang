package persistence

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"qqtang/internal/protocol/game"
)

func TestDirectedFriendRequestLifecycle(t *testing.T) {
	ctx := context.Background()
	store, err := OpenPlayerStore(filepath.Join(t.TempDir(), "friends.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for index, uin := range []uint32{1_000_001, 1_000_002, 1_000_003} {
		profile := game.DefaultPlayerProfile()
		profile.PlayerID = uint16(index + 1)
		profile.Nickname = []string{"糖一", "糖二", "糖三"}[index]
		if err := store.Save(ctx, uin, profile); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := store.RequestFriend(ctx, 1_000_001, 1_000_002, "你好"); err != nil {
		t.Fatal(err)
	}
	changed, err := store.AnswerFriendRequest(ctx, 1_000_002, 1_000_001, true)
	if err != nil || !reflect.DeepEqual(changed, []uint32{1_000_001}) {
		t.Fatalf("directed accept changed=%v err=%v", changed, err)
	}
	one, _ := store.ListFriends(ctx, 1_000_001)
	two, _ := store.ListFriends(ctx, 1_000_002)
	if !reflect.DeepEqual(one, []uint32{1_000_002}) || len(two) != 0 {
		t.Fatalf("directed lists one=%v two=%v", one, two)
	}
	if _, err := store.RequestFriend(ctx, 1_000_001, 1_000_002, "again"); !errors.Is(err, ErrFriendAlreadyExists) {
		t.Fatalf("duplicate request error=%v", err)
	}
	// The accept dialog's optional reciprocal checkbox sends this separate
	// reverse request; only accepting that envelope creates the second row.
	if _, err := store.RequestFriend(ctx, 1_000_002, 1_000_001, "也加你"); err != nil {
		t.Fatal(err)
	}
	if changed, err = store.AnswerFriendRequest(ctx, 1_000_001, 1_000_002, true); err != nil || !reflect.DeepEqual(changed, []uint32{1_000_002}) {
		t.Fatalf("reverse accept changed=%v err=%v", changed, err)
	}
	if err := store.RemoveFriend(ctx, 1_000_001, 1_000_002); err != nil {
		t.Fatal(err)
	}
	one, _ = store.ListFriends(ctx, 1_000_001)
	two, _ = store.ListFriends(ctx, 1_000_002)
	if len(one) != 0 || !reflect.DeepEqual(two, []uint32{1_000_001}) {
		t.Fatalf("removed directed lists one=%v two=%v", one, two)
	}
	if err := store.RemoveFriend(ctx, 1_000_002, 1_000_001); err != nil {
		t.Fatal(err)
	}

	owners, err := store.ListFriendOwners(ctx, 1_000_001)
	if err != nil || len(owners) != 0 {
		t.Fatalf("friend owners=%v err=%v", owners, err)
	}
	if _, err := store.RequestFriend(ctx, 1_000_003, 1_000_001, "reject me"); err != nil {
		t.Fatal(err)
	}
	if changed, err := store.AnswerFriendRequest(ctx, 1_000_001, 1_000_003, false); err != nil || len(changed) != 0 {
		t.Fatalf("rejected request changed=%v err=%v", changed, err)
	}
	three, _ := store.ListFriends(ctx, 1_000_003)
	if len(three) != 0 {
		t.Fatalf("rejected requester friends=%v", three)
	}

	deleted, err := store.Delete(ctx, 1_000_001)
	if err != nil || !deleted {
		t.Fatalf("delete friend account deleted=%v err=%v", deleted, err)
	}
	two, err = store.ListFriends(ctx, 1_000_002)
	if err != nil || len(two) != 0 {
		t.Fatalf("cascade list=%v err=%v", two, err)
	}
}

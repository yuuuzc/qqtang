package persistence

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"qqtang/internal/protocol/game"
)

func TestPlayerStoreCloseCheckpointsWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "players.sqlite")
	store, err := OpenPlayerStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Save(context.Background(), 1_000_001, game.DefaultPlayerProfile()); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if info, statErr := os.Stat(path + "-wal"); statErr == nil && info.Size() != 0 {
		t.Fatalf("WAL retained %d bytes after close", info.Size())
	} else if statErr != nil && !os.IsNotExist(statErr) {
		t.Fatal(statErr)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() <= 4096 {
		t.Fatalf("main database remained %d bytes after checkpoint", info.Size())
	}
}

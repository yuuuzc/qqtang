package probe

import (
	"errors"
	"io"
	"net"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"qqtang/internal/protocol/capture"
	"qqtang/internal/protocol/game"
)

func TestRoomActorSerializesOneRoom(t *testing.T) {
	server := &Server{}
	t.Cleanup(server.roomActors.close)
	var active atomic.Int32
	var maximum atomic.Int32
	var completed atomic.Int32
	start := make(chan struct{})
	var callers sync.WaitGroup
	for index := 0; index < 32; index++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			<-start
			err := runRoomActor(server, 17, "serial-test", func() error {
				current := active.Add(1)
				for observed := maximum.Load(); current > observed && !maximum.CompareAndSwap(observed, current); observed = maximum.Load() {
				}
				time.Sleep(time.Millisecond)
				active.Add(-1)
				completed.Add(1)
				return nil
			})
			if err != nil {
				t.Errorf("room actor call failed: %v", err)
			}
		}()
	}
	close(start)
	callers.Wait()
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent commands = %d, want 1", got)
	}
	if got := completed.Load(); got != 32 {
		t.Fatalf("completed commands = %d, want 32", got)
	}
}

func TestRoomActorPanicReturnsErrorAndKeepsExecutorAlive(t *testing.T) {
	server := &Server{}
	t.Cleanup(server.roomActors.close)
	err := runRoomActor(server, 23, "panic-test", func() error {
		panic("boom")
	})
	if err == nil {
		t.Fatal("panic command unexpectedly succeeded")
	}
	if err = runRoomActor(server, 23, "after-panic", func() error { return nil }); err != nil {
		t.Fatalf("executor did not survive command panic: %v", err)
	}
}

func TestRoomActorRejectsNewCommandAfterShutdown(t *testing.T) {
	server := &Server{}
	server.roomActors.close()
	err := runRoomActor(server, 31, "after-shutdown", func() error {
		return errors.New("must not run")
	})
	if err == nil {
		t.Fatal("command submitted after shutdown unexpectedly ran")
	}
}

func TestRoomActorDrainsAcceptedCommandsDuringShutdown(t *testing.T) {
	server := &Server{}
	started := make(chan struct{})
	release := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- runRoomActor(server, 41, "blocking", func() error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	secondRan := make(chan struct{})
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- runRoomActor(server, 41, "accepted-before-shutdown", func() error {
			close(secondRan)
			return nil
		})
	}()
	shard := &server.roomActors.shards[uint32(41)%roomActorShardCount]
	deadline := time.Now().Add(time.Second)
	for len(shard.commands) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(shard.commands) == 0 {
		t.Fatal("second command was not accepted before shutdown")
	}
	closed := make(chan struct{})
	go func() {
		server.roomActors.close()
		close(closed)
	}()
	close(release)
	if err := <-firstDone; err != nil {
		t.Fatalf("first command failed: %v", err)
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("accepted command failed during drain: %v", err)
	}
	select {
	case <-secondRan:
	default:
		t.Fatal("accepted command did not run during shutdown drain")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("room actor shutdown did not finish after draining commands")
	}
}

func TestTCPRoomCommandWaitsWithoutHoldingSessionMutex(t *testing.T) {
	server := &Server{}
	t.Cleanup(server.roomActors.close)
	server.config.MaxPacketSize = 4096
	writer, err := capture.Open(filepath.Join(t.TempDir(), "capture.jsonl"))
	if err != nil {
		t.Fatalf("open capture: %v", err)
	}
	defer writer.Close()
	server.capture = writer
	server.logWriter = io.Discard
	session := &connectionSession{
		UIN: 1, Profile: game.PlayerProfile{PlayerID: 1, SectionID: 1}, RoomID: 7,
		done: make(chan struct{}),
	}
	session.liveUIN.Store(1)
	session.liveRoomID.Store(7)
	session.livePlayerID.Store(1)
	packet := testLocalRoutedPacketWithPayload(t, 0x7FFE, 2, 0xFFFF, 1, 1, nil)
	serverSide, clientSide := net.Pipe()
	defer serverSide.Close()
	defer clientSide.Close()

	started := make(chan struct{})
	release := make(chan struct{})
	blockerDone := make(chan error, 1)
	go func() {
		blockerDone <- runRoomActor(server, 7, "blocking", func() error {
			close(started)
			<-release
			return nil
		})
	}()
	<-started
	handled := make(chan bool, 1)
	go func() {
		handled <- server.handleTCPMessage(serverSide, ListenerConfig{}, session, "test", "local", "remote", packet)
	}()
	shard := &server.roomActors.shards[uint32(7)%roomActorShardCount]
	deadline := time.Now().Add(time.Second)
	for len(shard.commands) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(shard.commands) == 0 {
		t.Fatal("TCP room command was not queued")
	}
	if !session.mu.TryLock() {
		t.Fatal("TCP dispatcher held session.mu while waiting for the room actor")
	}
	session.mu.Unlock()
	close(release)
	if err := <-blockerDone; err != nil {
		t.Fatalf("blocking command failed: %v", err)
	}
	if keep := <-handled; !keep {
		t.Fatal("unhandled room packet unexpectedly closed the connection")
	}
}

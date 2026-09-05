package probe

import (
	"errors"
	"fmt"
	"sync"

	"qqtang/internal/protocol/game"
)

const (
	// A fixed shard set bounds goroutine and mailbox growth while preserving the
	// only ordering property the authority needs: every command for one room is
	// always consumed by exactly one serial executor. Independent rooms may
	// share an executor; that can add latency under load, but it cannot change
	// their observable order or create another state writer.
	roomActorShardCount = 64
	roomActorQueueSize  = 256
)

var errRoomActorShutdown = errors.New("room actor system is shutting down")

type roomActorCommand struct {
	roomID uint16
	name   string
	run    func() (any, error)
	result chan<- roomActorResult
}

type roomActorResult struct {
	value any
	err   error
}

type roomActorShard struct {
	once     sync.Once
	commands chan roomActorCommand
	stop     chan struct{}
}

// roomActorSystem is the sole scheduling boundary for authoritative room and
// match mutations. Network connections, timers and AI runtimes submit
// commands; they do not acquire a second participant's session lock to make a
// room transition.
//
// Room ID zero is reserved for the global room-allocation lane. Every real
// room is deterministically assigned to one shard for its entire server
// lifetime, so room-ID reuse cannot leave an old executor racing a new one.
type roomActorSystem struct {
	mu     sync.RWMutex
	closed bool
	wg     sync.WaitGroup
	shards [roomActorShardCount]roomActorShard
}

func (system *roomActorSystem) call(roomID uint16, name string, run func() (any, error)) (any, error) {
	if system == nil || run == nil {
		return nil, fmt.Errorf("room actor command %q has no executor", name)
	}
	system.mu.RLock()
	if system.closed {
		system.mu.RUnlock()
		return nil, fmt.Errorf("room actor command %q rejected: %w", name, errRoomActorShutdown)
	}
	shard := &system.shards[uint32(roomID)%roomActorShardCount]
	shard.once.Do(func() {
		shard.commands = make(chan roomActorCommand, roomActorQueueSize)
		shard.stop = make(chan struct{})
		system.wg.Add(1)
		go func() {
			defer system.wg.Done()
			shard.consume()
		}()
	})
	result := make(chan roomActorResult, 1)
	command := roomActorCommand{roomID: roomID, name: name, run: run, result: result}
	// Hold the lifecycle read lock until the command is in the mailbox. Close
	// takes the write lock before stopping executors, so a command is either
	// rejected before submission or guaranteed to be part of the drain set.
	shard.commands <- command
	system.mu.RUnlock()
	// Once accepted, a command always yields a result. During shutdown the
	// consumer drains its accepted mailbox before exiting; returning early here
	// would make the caller unable to distinguish a rejected command from a
	// committed transition.
	completed := <-result
	return completed.value, completed.err
}

func (system *roomActorSystem) close() {
	if system == nil {
		return
	}
	system.mu.Lock()
	if !system.closed {
		system.closed = true
		for index := range system.shards {
			shard := &system.shards[index]
			if shard.stop != nil {
				close(shard.stop)
			}
		}
	}
	system.mu.Unlock()
	system.wg.Wait()
}

func (system *roomActorSystem) wait() {
	if system != nil {
		system.wg.Wait()
	}
}

func (shard *roomActorShard) consume() {
	for {
		select {
		case command := <-shard.commands:
			shard.execute(command)
		case <-shard.stop:
			for {
				select {
				case command := <-shard.commands:
					shard.execute(command)
				default:
					return
				}
			}
		}
	}
}

func (shard *roomActorShard) execute(command roomActorCommand) {
	completed := roomActorResult{}
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				completed.err = fmt.Errorf("room %d actor command %q panicked: %v", command.roomID, command.name, recovered)
			}
		}()
		completed.value, completed.err = command.run()
	}()
	command.result <- completed
}

func callRoomActor[T any](server *Server, roomID uint16, name string, run func() (T, error)) (T, error) {
	var zero T
	if server == nil {
		return zero, fmt.Errorf("room actor command %q has no server", name)
	}
	value, err := server.roomActors.call(roomID, name, func() (any, error) {
		return run()
	})
	if err != nil {
		return zero, err
	}
	typed, ok := value.(T)
	if !ok {
		return zero, fmt.Errorf("room %d actor command %q returned %T", roomID, name, value)
	}
	return typed, nil
}

func runRoomActor(server *Server, roomID uint16, name string, run func() error) error {
	_, err := callRoomActor(server, roomID, name, func() (struct{}, error) {
		return struct{}{}, run()
	})
	return err
}

// sessionRoomActorID resolves the immutable routing projection before a
// caller submits work. It must be called without holding session.mu. The
// atomic route is the production path; the locked fallback keeps focused
// tests that construct sessions directly compatible with the same boundary.
func sessionRoomActorID(session *connectionSession) uint16 {
	if session == nil {
		return 0
	}
	if roomID := uint16(session.liveRoomID.Load()); roomID != 0 {
		return roomID
	}
	session.mu.Lock()
	roomID := session.RoomID
	session.mu.Unlock()
	return roomID
}

// roomActorRouteForTCP resolves commands that enter the room authority before
// the local session has a room projection. Once a player is in a room, every
// authenticated request uses that room's lane; this includes logout and item
// operations whose account effects must not overtake a simultaneous kick or
// settlement. Room creation uses lane zero until World allocates its ID.
func (server *Server) roomActorRouteForTCP(session *connectionSession, packet []byte) (uint16, bool) {
	if session != nil {
		if roomID := uint16(session.liveRoomID.Load()); roomID != 0 {
			return roomID, true
		}
	}
	inspection, err := game.InspectLocalPacket(packet)
	if err != nil {
		return 0, false
	}
	switch inspection.Command {
	case game.EnterRoomCommand:
		request, decodeErr := game.DecodeLocalEnterRoomRequest(packet)
		return request.RoomID, decodeErr == nil && request.RoomID != 0
	case game.CreateRoomCommand:
		return 0, true
	default:
		return 0, false
	}
}

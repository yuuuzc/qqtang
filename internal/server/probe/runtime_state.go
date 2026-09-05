package probe

import (
	"io"
	"sync"
	"sync/atomic"
	"time"

	"qqtang/internal/protocol/game"
)

// serverLifecycle owns process-level listener state. Embedding keeps existing
// diagnostic access stable while making the lock's ownership explicit.
type serverLifecycle struct {
	mu         sync.Mutex
	started    bool
	closed     bool
	closeOnce  sync.Once
	closeDone  chan struct{}
	closeErr   error
	closers    []io.Closer
	addresses  []BoundAddress
	wg         sync.WaitGroup
	done       chan struct{}
	firstErr   error
	connection atomic.Uint64
	responseMu sync.Mutex
	responses  map[string]int
}

func newServerLifecycle() serverLifecycle {
	return serverLifecycle{
		done:      make(chan struct{}),
		closeDone: make(chan struct{}),
		responses: make(map[string]int),
	}
}

// authenticationState owns every host-bound login capability and its mutex.
// It is intentionally separate from live game sessions: district navigation
// is authenticated account state, not online world membership.
type authenticationState struct {
	authMu                sync.Mutex
	pendingAuth           map[accountAuthGrant]time.Time
	navigationAuth        map[accountAuthGrant]time.Time
	authenticatedSessions map[accountAuthGrant]authenticatedSessionLease
}

func newAuthenticationState() authenticationState {
	return authenticationState{
		pendingAuth:           make(map[accountAuthGrant]time.Time),
		navigationAuth:        make(map[accountAuthGrant]time.Time),
		authenticatedSessions: make(map[accountAuthGrant]authenticatedSessionLease),
	}
}

type accountAuthGrant struct {
	RemoteHost string
	UIN        uint32
}

type authenticatedSessionLease struct {
	Expires            time.Time
	Profile            game.PlayerProfile
	SelectedRoleID     byte
	CurrentMapID       uint32
	CurrentGameID      uint32
	CurrentStageGameID uint32
	RoomID             uint16
}

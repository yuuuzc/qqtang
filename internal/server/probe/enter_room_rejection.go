package probe

import (
	"errors"
	"fmt"

	"qqtang/internal/protocol/game"
)

// enterRoomRejection keeps user-visible legacy result semantics separate from
// diagnostic errors. The transport adapter can answer the client immediately
// while logs retain the concrete room-state failure.
type enterRoomRejection struct {
	resultID game.EnterRoomResultID
	cause    error
}

func (rejection *enterRoomRejection) Error() string { return rejection.cause.Error() }
func (rejection *enterRoomRejection) Unwrap() error { return rejection.cause }

func rejectEnterRoom(resultID game.EnterRoomResultID, format string, arguments ...any) error {
	return &enterRoomRejection{resultID: resultID, cause: fmt.Errorf(format, arguments...)}
}

func enterRoomRejectionResult(err error) game.EnterRoomResultID {
	var rejection *enterRoomRejection
	if errors.As(err, &rejection) {
		return rejection.resultID
	}
	return game.EnterRoomResultRoomUnavailable
}

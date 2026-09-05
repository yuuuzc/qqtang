//go:build windows

package winlaunch

import (
	"fmt"
	"syscall"
	"time"
)

// ExceptionTraceSession is a diagnostic-only, transparent exception observer.
// The injected VEH always returns EXCEPTION_CONTINUE_SEARCH; the legacy
// client's own exception handlers retain complete control of the process.
type ExceptionTraceSession struct {
	process syscall.Handle
	counter uintptr
	trace   uintptr
}

// AttachExceptionTrace installs the existing transparent ring observer in an
// already-running 32-bit client. It is intentionally not part of the normal
// launcher path.
func AttachExceptionTrace(pid uint32, timeout time.Duration) (*ExceptionTraceSession, error) {
	if pid == 0 {
		return nil, fmt.Errorf("pid must be non-zero")
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return nil, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	counter, trace, err := installExceptionTraceVEH(process, pid, timeout)
	if err != nil {
		syscall.CloseHandle(process)
		return nil, err
	}
	return &ExceptionTraceSession{process: process, counter: counter, trace: trace}, nil
}

// Snapshot returns the total observed exception count and the retained ring.
func (session *ExceptionTraceSession) Snapshot() (uint32, []ExceptionCapture) {
	if session == nil || session.process == 0 {
		return 0, nil
	}
	return readExceptionTrace(session.process, session.counter, session.trace)
}

func (session *ExceptionTraceSession) Close() error {
	if session == nil || session.process == 0 {
		return nil
	}
	err := syscall.CloseHandle(session.process)
	session.process = 0
	return err
}

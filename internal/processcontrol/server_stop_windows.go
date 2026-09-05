//go:build windows

package processcontrol

import (
	"fmt"
	"sync"

	"golang.org/x/sys/windows"
)

func serverStopEventName(pid int) string {
	return fmt.Sprintf(`Local\QQTangLocalServerStop-%d`, pid)
}

type ServerStopListener struct {
	handle windows.Handle
	done   chan struct{}
	waited chan struct{}
	once   sync.Once
}

func NewServerStopListener(pid int) (*ServerStopListener, error) {
	name, err := windows.UTF16PtrFromString(serverStopEventName(pid))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateEvent(nil, 0, 0, name)
	if err != nil {
		return nil, fmt.Errorf("create server stop event: %w", err)
	}
	listener := &ServerStopListener{handle: handle, done: make(chan struct{}), waited: make(chan struct{})}
	go func() {
		result, _ := windows.WaitForSingleObject(handle, windows.INFINITE)
		if result == windows.WAIT_OBJECT_0 {
			close(listener.done)
		}
		close(listener.waited)
	}()
	return listener, nil
}

func (listener *ServerStopListener) Done() <-chan struct{} {
	return listener.done
}

func (listener *ServerStopListener) Close() error {
	if listener == nil || listener.handle == 0 {
		return nil
	}
	var closeErr error
	listener.once.Do(func() {
		if err := windows.SetEvent(listener.handle); err != nil {
			closeErr = err
		}
		<-listener.waited
		if err := windows.CloseHandle(listener.handle); err != nil && closeErr == nil {
			closeErr = err
		}
		listener.handle = 0
	})
	return closeErr
}

func RequestServerStop(pid int) error {
	name, err := windows.UTF16PtrFromString(serverStopEventName(pid))
	if err != nil {
		return err
	}
	handle, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, name)
	if err != nil {
		return fmt.Errorf("open server stop event: %w", err)
	}
	defer windows.CloseHandle(handle)
	if err := windows.SetEvent(handle); err != nil {
		return fmt.Errorf("signal server stop event: %w", err)
	}
	return nil
}

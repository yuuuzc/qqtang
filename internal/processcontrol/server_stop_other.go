//go:build !windows

package processcontrol

type ServerStopListener struct {
	done chan struct{}
}

func NewServerStopListener(_ int) (*ServerStopListener, error) {
	return &ServerStopListener{done: make(chan struct{})}, nil
}

func (listener *ServerStopListener) Done() <-chan struct{} {
	return listener.done
}

func (listener *ServerStopListener) Close() error { return nil }

func RequestServerStop(_ int) error { return nil }

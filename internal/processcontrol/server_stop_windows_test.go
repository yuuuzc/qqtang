//go:build windows

package processcontrol

import (
	"os"
	"testing"
	"time"
)

func TestServerStopEventRoundTrip(t *testing.T) {
	listener, err := NewServerStopListener(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err = RequestServerStop(os.Getpid()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-listener.Done():
	case <-time.After(time.Second):
		t.Fatal("server stop event was not delivered")
	}
}

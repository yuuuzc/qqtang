package winlaunch

import (
	"fmt"
	"syscall"
	"time"
)

// WaitForProcessModule waits for a DLL to become visible in an existing
// process without modifying it. It is used to detect the legacy SSO module's
// lazy load after the user clicks Login.
func WaitForProcessModule(pid uint32, moduleName string, timeout time.Duration) error {
	if pid == 0 {
		return fmt.Errorf("pid must be non-zero")
	}
	if moduleName == "" {
		return fmt.Errorf("module name must not be empty")
	}
	if timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	process, err := syscall.OpenProcess(0x0400|0x0010, false, pid)
	if err != nil {
		return fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	_, err = waitForModule(process, pid, moduleName, timeout)
	return err
}

package winlaunch

import (
	"fmt"
	"strings"
	"syscall"
	"time"
)

// WaitClientModuleBase resolves an ASLR module base from a running client.
// It is intended for diagnostic tools that must arm a module-relative probe
// immediately after a DLL is mapped during a legacy transition.
func WaitClientModuleBase(pid uint32, moduleName string, timeout time.Duration) (uintptr, error) {
	if pid == 0 {
		return 0, fmt.Errorf("pid must be non-zero")
	}
	if strings.TrimSpace(moduleName) == "" {
		return 0, fmt.Errorf("module name must not be empty")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	process, err := syscall.OpenProcess(attachedProcessAccess, false, pid)
	if err != nil {
		return 0, fmt.Errorf("OpenProcess pid %d: %w", pid, err)
	}
	defer syscall.CloseHandle(process)
	deadline := time.Now().Add(timeout)
	for {
		for _, module := range snapshotRemoteModules(process) {
			if strings.EqualFold(module.name, moduleName) {
				return uintptr(module.base), nil
			}
		}
		waitResult, _, _ := procWaitForSingle.Call(uintptr(process), 0)
		if waitResult == waitObject0 {
			return 0, fmt.Errorf("Client.exe exited before %s loaded", moduleName)
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("timed out waiting for %s in Client PID %d", moduleName, pid)
		}
		time.Sleep(time.Millisecond)
	}
}

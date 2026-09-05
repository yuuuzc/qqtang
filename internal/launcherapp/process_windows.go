//go:build windows

package launcherapp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"qqtang/internal/processcontrol"
)

func processAlive(pid int) bool {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(handle)
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}
	return exitCode == 259
}

func processPath(pid int) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		return "", err
	}
	return filepath.Clean(windows.UTF16ToString(buffer[:size])), nil
}

func processMatches(pid int, expected string) bool {
	actual, err := processPath(pid)
	return err == nil && strings.EqualFold(filepath.Clean(actual), filepath.Clean(expected))
}

func processesByPath(expected string) []int {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil
	}
	var result []int
	for {
		pid := int(entry.ProcessID)
		if pid > 0 && processMatches(pid, expected) {
			result = append(result, pid)
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			break
		}
	}
	return result
}

func terminateExactProcess(pid int, expected string) error {
	if !processMatches(pid, expected) {
		return fmt.Errorf("进程路径不匹配，拒绝停止")
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Kill(); err != nil && !errorsIsProcessDone(err) {
		return err
	}
	handle, openErr := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if openErr == nil {
		defer windows.CloseHandle(handle)
		_, _ = windows.WaitForSingleObject(handle, 3000)
	}
	return nil
}

func stopExactServerProcess(pid int, expected string, timeout time.Duration) (bool, error) {
	if !processMatches(pid, expected) {
		return false, fmt.Errorf("进程路径不匹配，拒绝停止")
	}
	if err := processcontrol.RequestServerStop(pid); err != nil {
		return false, err
	}
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		if errorsIsProcessDone(err) {
			return true, nil
		}
		return false, fmt.Errorf("打开服务端进程等待句柄：%w", err)
	}
	defer windows.CloseHandle(handle)
	result, waitErr := windows.WaitForSingleObject(handle, uint32(timeout.Milliseconds()))
	if waitErr == nil && result == windows.WAIT_OBJECT_0 {
		return true, nil
	}
	if waitErr != nil {
		return false, fmt.Errorf("等待服务端优雅关闭：%w", waitErr)
	}
	return false, fmt.Errorf("服务端在 %s 内未完成优雅关闭", timeout)
}

func errorsIsProcessDone(err error) bool {
	return err == os.ErrProcessDone || err == windows.ERROR_INVALID_PARAMETER
}

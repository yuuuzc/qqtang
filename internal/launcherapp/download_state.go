package launcherapp

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// DLScript.xml is a transient download queue written by the legacy client.
// This is the exact empty queue shipped by the verified original client.
var baselineClientDownloadScript = []byte("<?xml version=\"1.0\"?>\r\n<Command/>\r\n")

func restoreClientDownloadScript(clientRoot string, liveClientCount int) error {
	path := filepath.Join(clientRoot, "config", "DLScript.xml")
	current, readErr := os.ReadFile(path)
	if readErr == nil && bytes.Equal(current, baselineClientDownloadScript) {
		return nil
	}
	if readErr != nil && !os.IsNotExist(readErr) {
		return fmt.Errorf("读取客户端下载队列：%w", readErr)
	}
	if liveClientCount != 0 {
		return fmt.Errorf("客户端下载队列在另一个客户端运行期间发生变化；请先停止全部客户端再启动新实例：%s", path)
	}
	if readErr == nil {
		// An original client tree may be read-only. The runtime copy is mutable,
		// but make that explicit before the atomic replacement on Windows.
		if err := os.Chmod(path, 0o666); err != nil {
			return fmt.Errorf("恢复客户端下载队列写权限：%w", err)
		}
	}
	if err := atomicWrite(path, baselineClientDownloadScript); err != nil {
		return fmt.Errorf("恢复客户端下载队列：%w", err)
	}
	restored, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("复核客户端下载队列：%w", err)
	}
	if !bytes.Equal(restored, baselineClientDownloadScript) {
		return fmt.Errorf("客户端下载队列未能恢复到原版基线：%s", path)
	}
	return nil
}

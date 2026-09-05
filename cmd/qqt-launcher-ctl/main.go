// qqt-launcher-ctl is the native controller used by the WinForms launcher.
// It deliberately has no UI dependencies so process supervision remains
// testable and reliable even if the shell is closed or restarted.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"qqtang/internal/launcherapp"
	"qqtang/internal/server/networksetup"
)

type response struct {
	OK             bool               `json:"ok"`
	Message        string             `json:"message,omitempty"`
	Status         launcherapp.Status `json:"status"`
	IPCandidates   []string           `json:"ip_candidates,omitempty"`
	DiagnosticPath string             `json:"diagnostic_path,omitempty"`
}

func main() {
	root := flag.String("root", "", "QQTang-Local root")
	action := flag.String("action", "status", "status, start-server, stop-server, start-client, stop-clients, or save-gm")
	mode := flag.String("mode", "", "local, lan-host, or remote-host")
	serverIP := flag.String("server-ip", "", "published server IPv4 address or DNS name")
	clientIP := flag.String("client-ip", "", "client target IPv4 address or DNS name")
	maximumFPS := flag.Int("max-fps", 144, "client frame-rate limit: 45, 60, 144, or 300")
	showFPS := flag.Bool("show-fps", false, "show the client's FPS counter")
	gmRemote := flag.Bool("gm-remote", false, "allow remote GM access")
	username := flag.String("username", "", "GM username")
	passwordStdin := flag.Bool("password-stdin", false, "read the GM password from stdin")
	flag.Parse()

	if *root == "" {
		located, err := launcherapp.LocateRoot()
		if err != nil {
			fatal("", err)
		}
		*root = located
	}
	manager, err := launcherapp.New(*root)
	if err != nil {
		fatal(*root, err)
	}

	var status launcherapp.Status
	var message string
	switch *action {
	case "status":
		status = manager.Status()
	case "start-server":
		settings, loadErr := manager.LoadSettings()
		if loadErr != nil {
			settings = networksetup.Default()
		}
		if *mode != "" {
			settings.Mode = networksetup.Mode(*mode)
		}
		if *serverIP != "" {
			settings.ServerIP = strings.TrimSpace(*serverIP)
		}
		if *clientIP != "" {
			settings.ClientServerIP = strings.TrimSpace(*clientIP)
		} else {
			settings.ClientServerIP = settings.ServerIP
		}
		settings.GMRemote = *gmRemote
		status, err = manager.StartServer(settings)
		message = "服务端已启动"
	case "stop-server":
		status, err = manager.StopServer()
		message = "服务端已停止"
	case "start-client":
		target := strings.TrimSpace(*serverIP)
		if target == "" {
			target = "127.0.0.1"
		}
		status, err = manager.StartClient(target, *maximumFPS, *showFPS)
		message = "客户端已启动"
	case "stop-clients":
		status, err = manager.StopClients()
		message = "客户端已全部停止"
	case "save-gm":
		if !*passwordStdin {
			err = fmt.Errorf("save-gm requires -password-stdin")
			break
		}
		password, readErr := io.ReadAll(io.LimitReader(os.Stdin, 1024))
		if readErr != nil {
			err = fmt.Errorf("读取 GM 密码：%w", readErr)
			break
		}
		err = manager.SaveGMCredentials(*username, strings.TrimRight(string(password), "\r\n"))
		status = manager.Status()
		message = "远程 GM 密码已保存"
	default:
		err = fmt.Errorf("unknown action %q", *action)
	}
	if err != nil {
		manager.RecordDiagnostic("控制器 action=%s 失败：%v", *action, err)
		fatal(*root, err)
	}
	emit(response{
		OK:             true,
		Message:        message,
		Status:         status,
		IPCandidates:   networksetup.PrivateIPv4Candidates(),
		DiagnosticPath: manager.DiagnosticPath(),
	})
}

func fatal(root string, err error) {
	diagnostic := ""
	if root != "" {
		diagnostic = root + "\\runtime\\logs\\launcher-ui.log"
	}
	emit(response{OK: false, Message: err.Error(), DiagnosticPath: diagnostic})
	os.Exit(1)
}

func emit(value response) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
}

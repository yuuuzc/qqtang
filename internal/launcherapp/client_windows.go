//go:build windows

package launcherapp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"

	"qqtang/internal/server/networksetup"
	"qqtang/internal/tooling/clientpatch"
)

type launchResult struct {
	PID      uint32 `json:"pid"`
	Running  bool   `json:"running"`
	ExitCode *int32 `json:"exit_code,omitempty"`
}

type serverConfigSummary struct {
	DirectoryHall struct {
		ShopServerID uint32 `json:"shop_server_id"`
	} `json:"directory_hall"`
}

func (manager *Manager) StartClient(serverIP string, maximumFPS int, showFPS bool) (Status, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	paths := manager.paths()
	settings, err := networksetup.Load(paths.network)
	if err != nil {
		return Status{}, fmt.Errorf("读取联机设置：%w", err)
	}
	settings.ClientServerIP = serverIP
	if err := networksetup.Save(paths.network, settings); err != nil {
		return Status{}, fmt.Errorf("保存客户端地址：%w", err)
	}
	if err := networksetup.ApplyClientTarget(paths.clientRoot, serverIP); err != nil {
		return Status{}, fmt.Errorf("写入客户端服务器地址：%w", err)
	}
	if err := ensureClientAliases(paths.clientRoot); err != nil {
		return Status{}, err
	}
	if err := serverReachable(serverIP); err != nil {
		return Status{}, fmt.Errorf("无法连接 QQ堂服务器 %s:18080。请确认服务端已启动、地址正确，并检查主机入站防火墙或云安全组：%w", serverIP, err)
	}
	for _, required := range []string{paths.client, paths.launcher, paths.loginHelper, paths.trace} {
		if info, statErr := os.Stat(required); statErr != nil || info.IsDir() {
			return Status{}, fmt.Errorf("客户端启动依赖缺失：%s", required)
		}
	}
	if err := os.MkdirAll(paths.logs, 0o755); err != nil {
		return Status{}, fmt.Errorf("创建日志目录：%w", err)
	}
	configData, err := os.ReadFile(paths.config)
	if err != nil {
		return Status{}, fmt.Errorf("读取服务端配置：%w", err)
	}
	var config serverConfigSummary
	if err := json.Unmarshal(configData, &config); err != nil || config.DirectoryHall.ShopServerID == 0 {
		return Status{}, errors.New("服务端配置缺少有效的 shop_server_id")
	}
	if _, err := clientpatch.CheckStaticShopServer(filepath.Join(paths.clientRoot, "QQTDir.dll"), config.DirectoryHall.ShopServerID); err != nil {
		return Status{}, fmt.Errorf("客户端商城静态兼容校验失败：%w；请重新运行完整构建或重新解压发布包", err)
	}
	if _, err := clientpatch.CheckStaticMultiClient(filepath.Join(paths.clientRoot, "Core.dll")); err != nil {
		return Status{}, fmt.Errorf("客户端多开静态兼容校验失败：%w；请重新运行完整构建或重新解压发布包", err)
	}
	if _, err := clientpatch.CheckStaticFrameRate(paths.client); err != nil {
		return Status{}, fmt.Errorf("客户端高帧率静态兼容校验失败：%w；请重新运行完整构建或重新解压发布包", err)
	}
	if err := applyClientFrameSettings(paths.clientRoot, maximumFPS, showFPS); err != nil {
		return Status{}, err
	}
	state, _ := manager.loadState()
	state = manager.reconcileClients(state)
	if err := restoreClientDownloadScript(paths.clientRoot, len(state.Clients)); err != nil {
		return Status{}, err
	}
	manager.stopOrphanHelpers(state)
	liveInstances := make(map[int]struct{}, len(state.Clients))
	for _, entry := range state.Clients {
		liveInstances[entry.Instance] = struct{}{}
	}
	instance := 1
	for {
		if _, used := liveInstances[instance]; !used {
			break
		}
		instance++
	}
	launcherStdout := instanceFile(paths.logs, "local-launcher.stdout.log", instance)
	launcherStderr := instanceFile(paths.logs, "local-launcher.stderr.log", instance)
	launchResultPath := instanceFile(paths.logs, "local-client-launch.json", instance)
	loginStdout := instanceFile(paths.logs, "local-login-immediate.stdout.log", instance)
	loginStderr := instanceFile(paths.logs, "local-login-immediate.stderr.log", instance)
	loginResultPath := instanceFile(paths.logs, "local-login-immediate.json", instance)
	loginReadyPath := instanceFile(paths.logs, "local-login-immediate.ready.json", instance)
	for _, file := range []string{launcherStdout, launcherStderr, launchResultPath, loginStdout, loginStderr, loginResultPath, loginReadyPath} {
		_ = os.Remove(file)
	}

	started := make([]ProcessEntry, 0, 2)
	cleanup := func() {
		for index := len(started) - 1; index >= 0; index-- {
			_ = terminateExactProcess(started[index].PID, started[index].Path)
		}
	}
	launcherArguments := []string{"-exe", paths.client, "-cwd", paths.clientRoot, "-tp-free", "-high-resolution-timer", "-wait", "1s", "-out", launchResultPath}
	launchContext, cancelLaunch := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancelLaunch()
	if err := runHiddenCommand(launchContext, manager.root, paths.launcher, launcherArguments, launcherStdout, launcherStderr); err != nil {
		return Status{}, fmt.Errorf("客户端启动器失败：%w；日志：%s", err, tailFile(launcherStderr, 20))
	}
	data, err := os.ReadFile(launchResultPath)
	if err != nil {
		return Status{}, fmt.Errorf("客户端启动器没有生成结果：%w；日志：%s", err, tailFile(launcherStderr, 20))
	}
	var result launchResult
	if err := json.Unmarshal(data, &result); err != nil {
		return Status{}, fmt.Errorf("客户端启动结果损坏：%w", err)
	}
	if !result.Running || result.PID == 0 || !processMatches(int(result.PID), paths.client) {
		exit := "未记录"
		if result.ExitCode != nil {
			exit = strconv.FormatInt(int64(*result.ExitCode), 10)
		}
		return Status{}, fmt.Errorf(
			"客户端在启动检查期间退出（代码 %s）。启动结果：%s；启动器日志：%s；详情：%s",
			exit, launchResultPath, launcherStderr, tailFile(launcherStderr, 30),
		)
	}
	clientEntry := ProcessEntry{PID: int(result.PID), Path: paths.client, Instance: instance, StartTimeUTC: time.Now().UTC().Format(time.RFC3339Nano), LaunchResult: launchResultPath}
	started = append(started, clientEntry)

	loginArguments := []string{"-pid", strconv.Itoa(clientEntry.PID), "-trace", paths.trace, "-auth-address", net.JoinHostPort(serverIP, "18000"), "-attach-timeout", "5m", "-click-timeout", "24h", "-lobby-timeout", "24h", "-duration", "0s", "-keep-until-client-exit", "-out", loginResultPath, "-ready", loginReadyPath}
	loginPID, err := startHiddenCommand(manager.root, paths.loginHelper, loginArguments, loginStdout, loginStderr)
	if err != nil {
		cleanup()
		return Status{}, fmt.Errorf("启动登录辅助进程：%w", err)
	}
	loginEntry := ProcessEntry{PID: loginPID, Path: paths.loginHelper, Instance: instance, StartTimeUTC: time.Now().UTC().Format(time.RFC3339Nano), Ready: loginReadyPath, Result: loginResultPath}
	started = append(started, loginEntry)
	loginContext, cancelLogin := context.WithTimeout(context.Background(), 45*time.Second)
	err = waitForFile(loginContext, loginReadyPath, func(data []byte) (bool, error) {
		var ready struct {
			Stage string `json:"stage"`
			Error string `json:"error"`
		}
		if err := json.Unmarshal(data, &ready); err != nil {
			return false, nil
		}
		if ready.Stage == "failed" {
			return false, fmt.Errorf("%s", ready.Error)
		}
		switch ready.Stage {
		case "armed", "auth_accepted", "login_replayed", "lobby_prepared":
			return true, nil
		}
		if !processMatches(loginPID, paths.loginHelper) {
			return false, fmt.Errorf("登录辅助进程提前退出：%s", tailFile(loginStderr, 30))
		}
		return false, nil
	})
	cancelLogin()
	if err != nil {
		cleanup()
		return Status{}, fmt.Errorf("登录辅助进程未能就绪：%w", err)
	}

	state.SchemaVersion = stateSchemaVersion
	state.Network = settings
	state.Clients = append(state.Clients, clientEntry)
	state.LoginHelpers = append(state.LoginHelpers, loginEntry)
	state.ShopServerHelpers = []ProcessEntry{}
	// The launcher does not assign accounts to client instances. The login
	// helper obtains the authenticated account profile from the Go server.
	sort.Slice(state.Clients, func(i, j int) bool { return state.Clients[i].Instance < state.Clients[j].Instance })
	sort.Slice(state.LoginHelpers, func(i, j int) bool { return state.LoginHelpers[i].Instance < state.LoginHelpers[j].Instance })
	state.ClientCount = len(state.Clients)
	state.Client = &state.Clients[0]
	state.LoginHelper = &state.LoginHelpers[0]
	if err := manager.saveState(state); err != nil {
		cleanup()
		return Status{}, fmt.Errorf("客户端已启动，但保存运行状态失败：%w", err)
	}
	manager.appendLog("客户端实例 %d 已启动 PID=%d server=%s max_fps=%d show_fps=%t", instance, clientEntry.PID, serverIP, maximumFPS, showFPS)
	return manager.statusLocked(settings), nil
}

func (manager *Manager) StopClients() (Status, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	paths := manager.paths()
	expected := []string{paths.loginHelper, paths.shopHelper, paths.client, paths.launcher}
	var failures []string
	stopped := 0
	for _, path := range expected {
		for _, pid := range processesByPath(path) {
			if err := terminateExactProcess(pid, path); err != nil {
				failures = append(failures, fmt.Sprintf("PID %d: %v", pid, err))
			} else {
				stopped++
			}
		}
	}
	if len(failures) != 0 {
		return Status{}, fmt.Errorf("停止客户端失败：%v", failures)
	}
	state, _ := manager.loadState()
	state.Clients = []ProcessEntry{}
	state.LoginHelpers = []ProcessEntry{}
	state.ShopServerHelpers = []ProcessEntry{}
	state.Client, state.LoginHelper, state.ClientCount = nil, nil, 0
	if err := manager.saveState(state); err != nil {
		return Status{}, fmt.Errorf("客户端已停止，但保存运行状态失败：%w", err)
	}
	settings, _ := networksetup.Load(paths.network)
	manager.appendLog("停止全部客户端完成（共 %d 个相关进程）", stopped)
	return manager.statusLocked(settings), nil
}

func (manager *Manager) stopOrphanHelpers(state RunState) {
	live := make(map[int]struct{}, len(state.Clients))
	for _, entry := range state.Clients {
		live[entry.Instance] = struct{}{}
	}
	for _, group := range []struct {
		entries []ProcessEntry
		path    string
	}{{state.LoginHelpers, manager.paths().loginHelper}, {state.ShopServerHelpers, manager.paths().shopHelper}} {
		for _, entry := range group.entries {
			if _, ok := live[entry.Instance]; !ok && processMatches(entry.PID, group.path) {
				_ = terminateExactProcess(entry.PID, group.path)
			}
		}
	}
}

func runHiddenCommand(ctx context.Context, directory, executable string, arguments []string, stdoutPath, stderrPath string) error {
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer stdout.Close()
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer stderr.Close()
	command := exec.CommandContext(ctx, executable, arguments...)
	command.Dir = directory
	command.Stdout, command.Stderr = stdout, stderr
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return command.Run()
}

func startHiddenCommand(directory, executable string, arguments []string, stdoutPath, stderrPath string) (int, error) {
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return 0, err
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		stdout.Close()
		return 0, err
	}
	command := exec.Command(executable, arguments...)
	command.Dir = directory
	command.Stdout, command.Stderr = stdout, stderr
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if err := command.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		return 0, err
	}
	pid := command.Process.Pid
	_ = command.Process.Release()
	stdout.Close()
	stderr.Close()
	return pid, nil
}

func ensureClientAliases(clientRoot string) error {
	source := filepath.Join(clientRoot, "config", "combineforge.ini")
	target := filepath.Join(clientRoot, "config", "combineforge.xml")
	sourceHash, err := fileHash(source)
	if err != nil {
		return fmt.Errorf("合成配置缺失：%w", err)
	}
	targetHash, targetErr := fileHash(target)
	if targetErr == nil && sourceHash == targetHash {
		return nil
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary, err := os.CreateTemp(filepath.Dir(target), ".combineforge-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err = io.Copy(temporary, input); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		_ = os.Remove(target)
		if err = os.Rename(temporaryPath, target); err != nil {
			return err
		}
	}
	verified, err := fileHash(target)
	if err != nil || verified != sourceHash {
		return errors.New("合成配置别名校验失败")
	}
	return nil
}

func fileHash(path string) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	file, err := os.Open(path)
	if err != nil {
		return result, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return result, err
	}
	copy(result[:], hash.Sum(nil))
	return result, nil
}

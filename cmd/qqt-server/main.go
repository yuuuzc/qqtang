package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"qqtang/internal/processcontrol"
	gmserver "qqtang/internal/server/gm"
	"qqtang/internal/server/probe"
)

func main() {
	configPath := flag.String("config", "configs/server.json", "server configuration")
	gmUsername := flag.String("gm-username", "", "GM username to save before startup (requires -gm-password-stdin)")
	gmPasswordStdin := flag.Bool("gm-password-stdin", false, "read the GM password from stdin and save its salted verifier")
	flag.Parse()
	if err := saveRequestedGMCredentials(*configPath, *gmUsername, *gmPasswordStdin, os.Stdin); err != nil {
		fatal(err)
	}
	config, err := probe.LoadConfig(*configPath)
	if err != nil {
		fatal(err)
	}
	server, err := probe.New(config)
	if err != nil {
		fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	launcherStop, err := processcontrol.NewServerStopListener(os.Getpid())
	if err != nil {
		fatal(err)
	}
	defer launcherStop.Close()
	go func() {
		select {
		case <-launcherStop.Done():
			stop()
		case <-ctx.Done():
		}
	}()
	if err := server.Start(ctx); err != nil {
		fatal(err)
	}
	ready := struct {
		Event       string               `json:"event"`
		SessionPath string               `json:"session_path"`
		Addresses   []probe.BoundAddress `json:"addresses"`
	}{Event: "server_ready", SessionPath: server.SessionPath(), Addresses: server.Addresses()}
	_ = json.NewEncoder(os.Stdout).Encode(ready)
	waitErr := server.Wait()
	closeErr := server.Close()
	if waitErr != nil {
		fatal(waitErr)
	}
	if closeErr != nil {
		fatal(closeErr)
	}
}

func saveRequestedGMCredentials(configPath, username string, passwordStdin bool, input io.Reader) error {
	username = strings.TrimSpace(username)
	if username == "" && !passwordStdin {
		return nil
	}
	if username == "" || !passwordStdin {
		return fmt.Errorf("GM credential setup requires both -gm-username and -gm-password-stdin")
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("read server configuration for GM credentials: %w", err)
	}
	var paths struct {
		GMAuthPath string `json:"gm_auth_path"`
	}
	if err := json.Unmarshal(data, &paths); err != nil {
		return fmt.Errorf("decode server configuration for GM credentials: %w", err)
	}
	if strings.TrimSpace(paths.GMAuthPath) == "" {
		return fmt.Errorf("server configuration has no gm_auth_path")
	}
	authPath := paths.GMAuthPath
	if !filepath.IsAbs(authPath) {
		authPath = filepath.Join(filepath.Dir(configPath), authPath)
	}
	password, err := io.ReadAll(io.LimitReader(input, 1025))
	if err != nil {
		return fmt.Errorf("read GM password from stdin: %w", err)
	}
	if len(password) > 1024 {
		return fmt.Errorf("GM password from stdin exceeds 1024 bytes")
	}
	credentials, err := gmserver.NewCredentials(username, strings.TrimRight(string(password), "\r\n"))
	if err != nil {
		return err
	}
	return gmserver.SaveCredentials(filepath.Clean(authPath), credentials)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "qqt-server:", err)
	os.Exit(1)
}

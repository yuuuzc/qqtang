package networksetup

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyClientTarget(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"DirCfg.ini":     "[Dir0]\r\nDirIP=127.0.0.1\r\n",
		"p2psvrInfo.ini": "tcpip=127.0.0.1\r\nudpip=127.0.0.1\r\nstunip=127.0.0.1\r\n",
		"caserver.ini":   "ip=127.0.0.1\r\nip2=127.0.0.1\r\n",
		"webserver.ini":  "ip=127.0.0.1\r\nip2=127.0.0.1\r\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := ApplyClientTarget(root, "192.168.10.12"); err != nil {
		t.Fatal(err)
	}
	for name := range files {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || !strings.Contains(string(data), "192.168.10.12") || strings.Contains(string(data), "127.0.0.1") {
			t.Fatalf("%s = %q, %v", name, data, err)
		}
	}
}

func TestApplyClientTargetAcceptsDNSName(t *testing.T) {
	root := t.TempDir()
	for _, endpointFile := range clientEndpointFiles {
		var contents strings.Builder
		for _, key := range endpointFile.keys {
			contents.WriteString(key + "=127.0.0.1\r\n")
		}
		if err := os.WriteFile(filepath.Join(root, endpointFile.name), []byte(contents.String()), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := applyClientTarget(context.Background(), root, "Game.Example.COM.",
		func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("203.0.113.25")}, nil
		}); err != nil {
		t.Fatal(err)
	}
	for _, endpointFile := range clientEndpointFiles {
		data, err := os.ReadFile(filepath.Join(root, endpointFile.name))
		if err != nil || !strings.Contains(string(data), "203.0.113.25") || strings.Contains(string(data), "game.example.com") {
			t.Fatalf("%s DNS target = %q, %v", endpointFile.name, data, err)
		}
	}
}

func TestApplyClientTargetAcceptsPublicIPv4(t *testing.T) {
	root := t.TempDir()
	for _, endpointFile := range clientEndpointFiles {
		var lines []string
		for _, key := range endpointFile.keys {
			lines = append(lines, key+"=127.0.0.1")
		}
		if err := os.WriteFile(filepath.Join(root, endpointFile.name), []byte(strings.Join(lines, "\r\n")+"\r\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := ApplyClientTarget(root, "203.0.113.8"); err != nil {
		t.Fatal(err)
	}
}

func TestApplyClientTargetValidatesEveryFileBeforeReplacingAny(t *testing.T) {
	root := t.TempDir()
	originals := make(map[string][]byte, len(clientEndpointFiles))
	for _, endpointFile := range clientEndpointFiles {
		var lines []string
		for _, key := range endpointFile.keys {
			if endpointFile.name == "caserver.ini" && key == "ip2" {
				continue
			}
			lines = append(lines, key+"=127.0.0.1")
		}
		data := []byte(strings.Join(lines, "\r\n") + "\r\n")
		originals[endpointFile.name] = append([]byte(nil), data...)
		if err := os.WriteFile(filepath.Join(root, endpointFile.name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := ApplyClientTarget(root, "192.168.10.12"); err == nil || !strings.Contains(err.Error(), "caserver.ini has no ip2 key") {
		t.Fatalf("ApplyClientTarget error = %v, want missing caserver ip2", err)
	}
	for name, original := range originals {
		got, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(original) {
			t.Fatalf("%s changed after validation failure: got %q, want %q", name, got, original)
		}
	}
}

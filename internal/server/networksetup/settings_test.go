package networksetup

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
)

func TestSettingsRoundTripAndModeValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "network.json")
	want := Settings{SchemaVersion: SchemaVersion, Mode: ModeLANHost, ServerIP: "192.168.1.20", ClientServerIP: "192.168.1.20"}
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil || got != want {
		t.Fatalf("Load = %+v, %v", got, err)
	}
	invalid := []Settings{
		{SchemaVersion: SchemaVersion, Mode: ModeLocal, ServerIP: "192.168.1.2", ClientServerIP: "127.0.0.1"},
		{SchemaVersion: SchemaVersion, Mode: ModeLANHost, ServerIP: "8.8.8.8", ClientServerIP: "127.0.0.1"},
		{SchemaVersion: SchemaVersion, Mode: "public", ServerIP: "127.0.0.1", ClientServerIP: "127.0.0.1"},
		{SchemaVersion: SchemaVersion, Mode: ModeLocal, ServerIP: "127.0.0.1", ClientServerIP: "not-an-ip"},
	}
	for _, settings := range invalid {
		if err := settings.Validate(); err == nil {
			t.Fatalf("invalid settings accepted: %+v", settings)
		}
	}
}

func TestRemoteHostAcceptsDNSAndResolvesPublicIPv4(t *testing.T) {
	settings := Settings{SchemaVersion: SchemaVersion, Mode: ModeRemoteHost, ServerIP: "Game.Example.COM.", ClientServerIP: "game.example.com"}
	if err := settings.Validate(); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveServerIPv4(context.Background(), settings.ServerIP,
		func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("203.0.113.9"), netip.MustParseAddr("198.51.100.8")}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.String() != "198.51.100.8" {
		t.Fatalf("resolved public IPv4 = %s", resolved)
	}
}

func TestRemoteHostDNSRejectsPrivateOnlyResolution(t *testing.T) {
	_, err := resolveServerIPv4(context.Background(), "game.example.com",
		func(context.Context, string, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr("192.168.1.10")}, nil
		})
	if err == nil {
		t.Fatal("private-only DNS result was accepted as a public endpoint")
	}
}

func TestLoadRejectsOldSchemaAndRemoteHostBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "network.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":1,"mode":"local","server_ip":"127.0.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("old network schema was accepted")
	}
	remote := Settings{SchemaVersion: SchemaVersion, Mode: ModeRemoteHost, ServerIP: "203.0.113.8", ClientServerIP: "203.0.113.8"}
	if err := remote.Validate(); err != nil || remote.BindIP() != "0.0.0.0" || !remote.RunsServer() {
		t.Fatalf("remote host = %+v, %v", remote, err)
	}
}

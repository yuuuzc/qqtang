package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	gmserver "qqtang/internal/server/gm"
)

func TestSaveRequestedGMCredentialsUsesConfiguredRelativePath(t *testing.T) {
	directory := t.TempDir()
	configPath := filepath.Join(directory, "server.json")
	if err := os.WriteFile(configPath, []byte(`{"gm_auth_path":"private/gm.json"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveRequestedGMCredentials(configPath, "operator", true, strings.NewReader("strong password\n")); err != nil {
		t.Fatal(err)
	}
	credentials, err := gmserver.LoadCredentials(filepath.Join(directory, "private", "gm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !credentials.Verify("operator", "strong password") {
		t.Fatal("saved GM credentials do not verify")
	}
}

func TestSaveRequestedGMCredentialsRequiresPairedFlags(t *testing.T) {
	for _, test := range []struct {
		username      string
		passwordStdin bool
	}{
		{username: "operator"},
		{passwordStdin: true},
	} {
		if err := saveRequestedGMCredentials("unused.json", test.username, test.passwordStdin, strings.NewReader("password")); err == nil {
			t.Fatalf("unpaired GM options were accepted: %+v", test)
		}
	}
}

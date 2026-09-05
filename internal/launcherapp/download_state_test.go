package launcherapp

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRestoreClientDownloadScriptUsesExactOriginalBaseline(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config", "DLScript.xml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupt download queue"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := restoreClientDownloadScript(root, 0); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, baselineClientDownloadScript) {
		t.Fatalf("DLScript.xml = %q, want exact original baseline %q", got, baselineClientDownloadScript)
	}
}

func TestRestoreClientDownloadScriptRejectsMutationWhileClientIsLive(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config", "DLScript.xml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte("<?xml version=\"1.0\"?><Command><Group/></Command>")
	if err := os.WriteFile(path, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := restoreClientDownloadScript(root, 1); err == nil {
		t.Fatal("expected a changed live download queue to be rejected")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, corrupt) {
		t.Fatalf("live download queue was modified: %q", got)
	}
}

func TestRestoreClientDownloadScriptAllowsCleanQueueWhileClientIsLive(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config", "DLScript.xml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, baselineClientDownloadScript, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := restoreClientDownloadScript(root, 2); err != nil {
		t.Fatal(err)
	}
}

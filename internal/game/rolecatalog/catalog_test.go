package rolecatalog

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSeparatesRoleResourcesFromItemIDs(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "object", "player")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "[walk]\ncloth=11001\ncap = 500\n[stand]\ncloth=11001\n"
	if err := os.WriteFile(filepath.Join(directory, "Player10.ini"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Definitions) != 1 || catalog.Definitions[0].RoleID != 10 {
		t.Fatalf("definitions = %+v", catalog.Definitions)
	}
	walk := catalog.Definitions[0].Actions[0]
	if walk.Name != "walk" || walk.Components["cloth"] != 11001 || walk.Components["cap"] != 500 {
		t.Fatalf("walk = %+v", walk)
	}
}

func TestLoadRejectsDuplicateCaseVariants(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "object", "player")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"player10.ini", "player010.ini"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("[stand]\ncloth=11001\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Load(root); err == nil {
		t.Fatal("duplicate RoleID was accepted")
	}
}

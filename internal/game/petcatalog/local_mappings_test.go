package petcatalog

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func TestInstallLocalTypeMappingsIsValidatedAndIdempotent(t *testing.T) {
	clientRoot := t.TempDir()
	configDir := filepath.Join(clientRoot, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	original := "; starter\r\n[PetExperience]\r\nExpTable = 0,180\r\n\r\n; 普通的酷比\r\n[Pet25001]\r\nLevel1 = 100\r\nLevel4 = 101\r\nLevel7 = 102\r\n"
	encoded, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(original))
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "PetCfg.ini")
	if err := os.WriteFile(configPath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := InstallLocalTypeMappings(clientRoot)
	if err != nil || !changed {
		t.Fatalf("first install changed=%t err=%v", changed, err)
	}
	changed, err = InstallLocalTypeMappings(clientRoot)
	if err != nil || changed {
		t.Fatalf("second install changed=%t err=%v", changed, err)
	}

	contents, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := simplifiedchinese.GBK.NewDecoder().Bytes(contents)
	if err != nil {
		t.Fatal(err)
	}
	definitions, _, err := parse(string(decoded))
	if err != nil {
		t.Fatal(err)
	}
	localCount := 0
	for _, definition := range definitions {
		if definition.Source == SourceLocalExtension {
			localCount++
		}
	}
	if localCount != 13 {
		t.Fatalf("local definition count = %d, want 13", localCount)
	}
}

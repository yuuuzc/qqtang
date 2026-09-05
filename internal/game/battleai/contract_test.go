package battleai

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadContractDoesNotPinReplaceableONNXBytes(t *testing.T) {
	directory := t.TempDir()
	modelPath := filepath.Join(directory, "actor.onnx")
	metadataPath := filepath.Join(directory, "actor.onnx.json")
	if err := os.WriteFile(modelPath, []byte("replacement model bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	metadata := `{"format":"qqtang-decentralized-actor-onnx","contract_version":1,"model_architecture_version":2,"model_variant":"routed-multiplayer-context-v1","tensor_version":10,"channels":82,"scalars":84,"actions":45,"height":13,"width":15,"batch_dynamic":true,"onnx_sha256":"stale provenance only"}`
	if err := os.WriteFile(metadataPath, []byte(metadata), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadContract(modelPath, metadataPath); err != nil {
		t.Fatal(err)
	}
}

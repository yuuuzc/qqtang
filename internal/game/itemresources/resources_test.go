package itemresources

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"qqtang/internal/game/itemcatalog"
)

func TestBuildSpecsKeepsCommodityIdentitySeparate(t *testing.T) {
	specs := BuildSpecs(
		[]itemcatalog.RegistryEntry{{ItemID: 4696, Category: "cladorn", ResourceID: 551, Name: "蓝色忍者衫"}},
		[]itemcatalog.CommodityEntry{{CommodityID: 90001, Category: "cladorn", ResourceID: 551, Name: "商城商品"}, {CommodityID: 7, Category: "cap", ResourceID: 4}},
	)
	if len(specs) != 2 || specs[1].ItemIDs[0] != 4696 || specs[1].CommodityIDs[0] != 90001 {
		t.Fatalf("specs = %+v", specs)
	}
	if !specs[0].CommodityOnly || len(specs[0].ItemIDs) != 0 {
		t.Fatalf("commodity-only resource = %+v", specs[0])
	}
}

func TestInspectAndInstallZipUsesFilePathManifest(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "cap4.zip")
	data := makeResourceZip(t, "cap4", map[string][]byte{
		"cap4.img":       []byte("icon"),
		"cap4_stand.img": []byte("stand"),
	}, "[batch]\r\nsrc1=cap4.img\r\ndst1=res/uiRes/icon/cap/cap4.img\r\nsrc2=cap4_stand.img\r\ndst2=object/cap/cap4_stand.img\r\ncount=2\r\n")
	if err := os.WriteFile(zipPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := InspectZip(zipPath)
	if err != nil || len(info.Files) != 2 || info.SHA256 == "" {
		t.Fatalf("InspectZip = %+v, %v", info, err)
	}
	client := t.TempDir()
	result, err := InstallZip(zipPath, client)
	if err != nil || len(result.Installed) != 2 || len(result.Conflicts) != 0 {
		t.Fatalf("InstallZip = %+v, %v", result, err)
	}
	got, err := os.ReadFile(filepath.Join(client, "object", "cap", "cap4_stand.img"))
	if err != nil || string(got) != "stand" {
		t.Fatalf("installed stand = %q, %v", got, err)
	}
}

func TestInstallZipRejectsTraversal(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "bad.zip")
	data := makeResourceZip(t, "bad", map[string][]byte{"x": []byte("x")}, "[batch]\nsrc1=x\ndst1=../escape\ncount=1\n")
	if err := os.WriteFile(zipPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := InspectZip(zipPath); err == nil {
		t.Fatal("traversal manifest was accepted")
	}
}

func TestManifestlessOfficialZipIsCachedButNotInstalled(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "bomb218.zip")
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	entry, err := writer.Create("bomb218/bomb218.img")
	if err != nil {
		t.Fatal(err)
	}
	entry.Write([]byte("icon"))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(zipPath, output.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := InspectZip(zipPath)
	if err != nil || len(info.Files) != 0 {
		t.Fatalf("manifestless InspectZip = %+v, %v", info, err)
	}
	if _, err := InstallZip(zipPath, t.TempDir()); err == nil {
		t.Fatal("manifestless ZIP was installed by guessing destinations")
	}
	override := &ManifestOverride{
		ResourceKey: ResourceKey{Category: "bomb", ResourceID: 218}, Confidence: "test", Evidence: []string{"adjacent"},
		Files: []ManifestFile{{Source: "bomb218.img", ArchivePath: "bomb218/bomb218.img", Destination: "object/bomb/bomb218.img"}},
	}
	client := t.TempDir()
	result, err := InstallZipWithOverride(zipPath, client, override)
	if err != nil || len(result.Installed) != 1 {
		t.Fatalf("override install = %+v, %v", result, err)
	}
}

func TestInspectResourceCanPreferAuditedLocalVariant(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "item20043.zip")
	data := makeResourceZip(t, "item20043", map[string][]byte{"item20043.img": []byte("cdn")},
		"[batch]\nsrc1=item20043.img\ndst1=res/uiRes/icon/item/item20043.img\ncount=1\n")
	if err := os.WriteFile(zipPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	client := t.TempDir()
	target := filepath.Join(client, "res", "uiRes", "icon", "item", "item20043.img")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("complete-client"), 0o600); err != nil {
		t.Fatal(err)
	}
	preference := &VariantPreference{
		ResourceKey: ResourceKey{Category: "item", ResourceID: 20043},
		Decision:    "prefer-local", Evidence: []string{"audited visual variant"},
	}
	state := inspectResource(client, zipPath, nil, preference)
	if state.Status != "local-preferred" || len(state.Installed) != 1 || len(state.Variants) != 1 || len(state.Conflicts) != 0 {
		t.Fatalf("state = %+v", state)
	}
}

func TestDownloadAllRejectsHTMLAndKeepsOnlyValidZip(t *testing.T) {
	valid := makeResourceZip(t, "cap4", map[string][]byte{"cap4.img": []byte("icon")}, "[batch]\nsrc1=cap4.img\ndst1=res/cap4.img\ncount=1\n")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/cap/cap4.zip" {
			writer.Header().Set("Content-Type", "application/zip")
			writer.Write(valid)
			return
		}
		writer.Write([]byte("<!DOCTYPE html>"))
	}))
	defer server.Close()
	cache := t.TempDir()
	results := DownloadAll(context.Background(), []ResourceSpec{{ResourceKey: ResourceKey{Category: "cap", ResourceID: 4}}, {ResourceKey: ResourceKey{Category: "cap", ResourceID: 5}}}, DownloadOptions{
		BaseURL: server.URL, CacheRoot: cache, Concurrency: 2, Retries: 1,
	})
	if len(results) != 2 || results[0].Status != "downloaded" || results[1].Status != "error" {
		t.Fatalf("download results = %+v", results)
	}
	if _, err := InspectZip(CachePath(cache, ResourceKey{Category: "cap", ResourceID: 4})); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(CachePath(cache, ResourceKey{Category: "cap", ResourceID: 5})); !os.IsNotExist(err) {
		t.Fatalf("invalid payload was retained: %v", err)
	}
}

func makeResourceZip(t *testing.T, root string, files map[string][]byte, manifest string) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for name, data := range files {
		entry, err := writer.Create(root + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	entry, err := writer.Create(root + "/FilePath.ini")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(manifest)); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

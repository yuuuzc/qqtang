package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"qqtang/internal/game/itemresources"
	"qqtang/internal/game/petcatalog"
)

const petModelInstallEvidence = "config/Dltype.xml QQT_avatar_zip: *_*.img -> object/ZIP_FOLDER/../"

type restorationResult struct {
	ModelID       uint32   `json:"model_id"`
	SourceZip     string   `json:"source_zip"`
	DownloadState string   `json:"download_state"`
	InstallState  string   `json:"install_state"`
	SHA256        string   `json:"sha256,omitempty"`
	Installed     []string `json:"installed,omitempty"`
	Existing      []string `json:"existing,omitempty"`
	Error         string   `json:"error,omitempty"`
	Evidence      string   `json:"installation_evidence"`
}

func main() {
	clientRoot := flag.String("client-root", "runtime/client-patched", "installed QQTang client root")
	cacheRoot := flag.String("cache-root", "runtime/resource-cache/pet-model-zips", "verified original pet-model ZIP cache")
	resultPath := flag.String("json", "data/qqt_pet_resource_download_results.json", "restoration result JSON")
	baseURL := flag.String("base-url", itemresources.DefaultCDNBaseURL, "original resource CDN base URL")
	download := flag.Bool("download", false, "download missing official pet-model ZIPs")
	install := flag.Bool("install", false, "install verified stand/walk model files using Dltype.xml destination rule")
	concurrency := flag.Int("concurrency", 6, "download concurrency")
	retries := flag.Int("retries", 3, "download attempts per model")
	flag.Parse()

	if *install {
		changed, err := petcatalog.InstallLocalTypeMappings(*clientRoot)
		if err != nil {
			fatal(err)
		}
		if changed {
			fmt.Println("installed 13 reserved local PetTypeID mappings")
		}
	}
	catalog, err := petcatalog.Load(*clientRoot)
	if err != nil {
		fatal(err)
	}
	modelIDs := referencedModelIDs(catalog)
	specs := make([]itemresources.ResourceSpec, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		specs = append(specs, itemresources.ResourceSpec{ResourceKey: itemresources.ResourceKey{Category: "pet", ResourceID: modelID}})
	}

	downloadByID := make(map[uint32]itemresources.DownloadResult, len(specs))
	if *download {
		lastProgress := time.Now()
		results := itemresources.DownloadAll(context.Background(), specs, itemresources.DownloadOptions{
			BaseURL: *baseURL, CacheRoot: *cacheRoot, Concurrency: *concurrency, Retries: *retries,
			Progress: func(completed, total int, result itemresources.DownloadResult) {
				if completed == total || completed%10 == 0 || time.Since(lastProgress) >= 10*time.Second {
					fmt.Printf("download %d/%d model=%d status=%s\n", completed, total, result.Resource.ResourceID, result.Status)
					lastProgress = time.Now()
				}
			},
		})
		for _, result := range results {
			downloadByID[result.Resource.ResourceID] = result
		}
	}

	results := make([]restorationResult, 0, len(modelIDs))
	for _, modelID := range modelIDs {
		zipPath := itemresources.CachePath(*cacheRoot, itemresources.ResourceKey{Category: "pet", ResourceID: modelID})
		result := restorationResult{
			ModelID: modelID, SourceZip: filepath.ToSlash(filepath.Join("pet", fmt.Sprintf("pet%d.zip", modelID))),
			DownloadState: "not-requested", InstallState: "not-requested", Evidence: petModelInstallEvidence,
		}
		if downloaded, ok := downloadByID[modelID]; ok {
			result.DownloadState = downloaded.Status
			result.Error = downloaded.Error
		} else if _, statErr := os.Stat(zipPath); statErr == nil {
			result.DownloadState = "cached"
		} else {
			result.DownloadState = "missing"
		}
		if *install && result.DownloadState != "missing" && result.Error == "" {
			installed, existing, digest, installErr := installModelZip(zipPath, *clientRoot, modelID)
			result.SHA256 = digest
			result.Installed = installed
			result.Existing = existing
			if installErr != nil {
				result.InstallState = "invalid"
				result.Error = installErr.Error()
			} else if len(installed) > 0 {
				result.InstallState = "installed"
			} else {
				result.InstallState = "existing"
			}
		}
		results = append(results, result)
	}
	if err := writeJSON(*resultPath, map[string]any{
		"schema_version": 1, "generated_utc": time.Now().UTC(), "cdn_base_url": *baseURL,
		"model_count": len(modelIDs), "results": results,
	}); err != nil {
		fatal(err)
	}

	counts := make(map[string]int)
	for _, result := range results {
		counts[result.DownloadState+"/"+result.InstallState]++
	}
	fmt.Printf("pet models=%d result=%v\n", len(results), counts)
}

func referencedModelIDs(catalog petcatalog.Catalog) []uint32 {
	set := make(map[uint32]struct{})
	add := func(ids ...uint32) {
		for _, id := range ids {
			if id != 0 {
				set[id] = struct{}{}
			}
		}
	}
	for _, definition := range catalog.Definitions {
		add(definition.Level1ResourceID, definition.Level4ResourceID, definition.Level7ResourceID)
	}
	for _, model := range catalog.UnmappedModels {
		add(model.JuvenileResourceID, model.AdultResourceID)
	}
	ids := make([]uint32, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func installModelZip(zipPath, clientRoot string, modelID uint32) ([]string, []string, string, error) {
	encoded, err := os.ReadFile(zipPath)
	if err != nil {
		return nil, nil, "", err
	}
	digestBytes := sha256.Sum256(encoded)
	digest := strings.ToUpper(hex.EncodeToString(digestBytes[:]))
	reader, err := zip.NewReader(bytes.NewReader(encoded), int64(len(encoded)))
	if err != nil {
		return nil, nil, digest, fmt.Errorf("open official model ZIP: %w", err)
	}
	standName := fmt.Sprintf("pet%d_stand.img", modelID)
	walkName := fmt.Sprintf("pet%d_walk.img", modelID)
	iconName := fmt.Sprintf("pet%d.img", modelID)
	destinations := map[string]string{
		standName: filepath.ToSlash(filepath.Join("object", "pet", standName)),
		walkName:  filepath.ToSlash(filepath.Join("object", "pet", walkName)),
		// Dltype.xml maps a package's bare pet<ID>.img to the pet icon
		// directory, while *_*.img is installed as an object animation.
		iconName: filepath.ToSlash(filepath.Join("res", "uiRes", "icon", "pet", iconName)),
	}
	files := make(map[string][]byte, len(destinations))
	root := fmt.Sprintf("pet%d/", modelID)
	for _, entry := range reader.File {
		name := strings.ReplaceAll(entry.Name, "\\", "/")
		if entry.FileInfo().IsDir() {
			continue
		}
		if !strings.HasPrefix(name, root) || strings.Contains(strings.TrimPrefix(name, root), "/") {
			return nil, nil, digest, fmt.Errorf("unexpected archive path %q", entry.Name)
		}
		base := strings.TrimPrefix(name, root)
		if strings.EqualFold(base, "FilePath.ini") || strings.EqualFold(base, "Thumbs.db") {
			continue
		}
		canonicalName := ""
		for candidate := range destinations {
			if strings.EqualFold(candidate, base) {
				canonicalName = candidate
				break
			}
		}
		if canonicalName == "" {
			return nil, nil, digest, fmt.Errorf("unexpected model file %q", entry.Name)
		}
		if entry.UncompressedSize64 == 0 || entry.UncompressedSize64 > 32<<20 {
			return nil, nil, digest, fmt.Errorf("model file %q has invalid size %d", entry.Name, entry.UncompressedSize64)
		}
		stream, openErr := entry.Open()
		if openErr != nil {
			return nil, nil, digest, openErr
		}
		data, readErr := io.ReadAll(io.LimitReader(stream, 32<<20+1))
		stream.Close()
		if readErr != nil {
			return nil, nil, digest, fmt.Errorf("read model file %q: %w", entry.Name, readErr)
		}
		if len(data) > 32<<20 {
			return nil, nil, digest, fmt.Errorf("model file %q exceeds 32 MiB", entry.Name)
		}
		files[canonicalName] = data
	}
	for _, name := range []string{standName, walkName} {
		if len(files[name]) == 0 {
			return nil, nil, digest, fmt.Errorf("official model ZIP lacks %s", name)
		}
	}
	var installed, existing []string
	for name, data := range files {
		relative := destinations[name]
		destination := filepath.Join(clientRoot, filepath.FromSlash(relative))
		destinationRoot := filepath.Dir(destination)
		if err := os.MkdirAll(destinationRoot, 0o755); err != nil {
			return installed, existing, digest, err
		}
		if current, readErr := os.ReadFile(destination); readErr == nil {
			if !bytes.Equal(current, data) {
				return installed, existing, digest, fmt.Errorf("refusing to overwrite conflicting local file %s", relative)
			}
			existing = append(existing, relative)
			continue
		} else if !os.IsNotExist(readErr) {
			return installed, existing, digest, readErr
		}
		temporary, createErr := os.CreateTemp(destinationRoot, ".pet-model-*.tmp")
		if createErr != nil {
			return installed, existing, digest, createErr
		}
		temporaryPath := temporary.Name()
		if _, createErr = temporary.Write(data); createErr == nil {
			createErr = temporary.Close()
		} else {
			temporary.Close()
		}
		if createErr == nil {
			createErr = os.Rename(temporaryPath, destination)
		}
		if createErr != nil {
			os.Remove(temporaryPath)
			return installed, existing, digest, createErr
		}
		installed = append(installed, relative)
	}
	sort.Strings(installed)
	sort.Strings(existing)
	return installed, existing, digest, nil
}

func writeJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o644)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

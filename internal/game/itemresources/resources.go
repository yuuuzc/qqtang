package itemresources

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"qqtang/internal/game/itemcatalog"
)

const (
	DefaultCDNBaseURL = "http://qqt-img.qq.com/item/ItemZips"
	maxArchiveFiles   = 10_000
	maxArchiveFile    = 64 << 20
	maxArchiveTotal   = 512 << 20
	maxDownloadBytes  = 128 << 20
)

var categoryPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type ResourceKey struct {
	Category   string `json:"category"`
	ResourceID uint32 `json:"resource_id"`
}

type ResourceSpec struct {
	ResourceKey
	Names         []string `json:"names,omitempty"`
	ItemIDs       []uint32 `json:"item_ids,omitempty"`
	CommodityIDs  []uint32 `json:"commodity_ids,omitempty"`
	CommodityOnly bool     `json:"commodity_only"`
}

type ManifestFile struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ArchivePath string `json:"archive_path"`
}

type ManifestOverride struct {
	ResourceKey
	Confidence string         `json:"confidence"`
	Evidence   []string       `json:"evidence"`
	Files      []ManifestFile `json:"files"`
}

type ManifestOverrides struct {
	SchemaVersion int                `json:"schema_version"`
	Overrides     []ManifestOverride `json:"overrides"`
}

type VariantPreference struct {
	ResourceKey
	Decision string   `json:"decision"`
	Evidence []string `json:"evidence"`
}

type VariantPreferences struct {
	SchemaVersion int                 `json:"schema_version"`
	Preferences   []VariantPreference `json:"preferences"`
}

type ZipInfo struct {
	Path      string         `json:"path"`
	SHA256    string         `json:"sha256"`
	Size      int64          `json:"size"`
	Files     []ManifestFile `json:"files"`
	Root      string         `json:"root,omitempty"`
	FileCount int            `json:"file_count"`
}

type InstallResult struct {
	Installed []string `json:"installed,omitempty"`
	Existing  []string `json:"existing,omitempty"`
	Conflicts []string `json:"conflicts,omitempty"`
}

type DownloadResult struct {
	Resource ResourceKey `json:"resource"`
	Path     string      `json:"path,omitempty"`
	Status   string      `json:"status"`
	Error    string      `json:"error,omitempty"`
}

type DownloadOptions struct {
	BaseURL     string
	CacheRoot   string
	Concurrency int
	Retries     int
	Client      *http.Client
	Progress    func(completed, total int, result DownloadResult)
}

type ItemResource struct {
	ItemID           uint32   `json:"item_id"`
	Category         string   `json:"category"`
	ResourceID       uint32   `json:"resource_id"`
	Name             string   `json:"name"`
	Description      string   `json:"description,omitempty"`
	CommodityIDs     []uint32 `json:"commodity_ids,omitempty"`
	SourceZip        string   `json:"source_zip"`
	ResourceStatus   string   `json:"resource_status"`
	ZipSHA256        string   `json:"zip_sha256,omitempty"`
	ZipBytes         int64    `json:"zip_bytes,omitempty"`
	InstalledFiles   []string `json:"installed_files"`
	MissingFiles     []string `json:"missing_files,omitempty"`
	ConflictFiles    []string `json:"conflict_files,omitempty"`
	VariantFiles     []string `json:"variant_files,omitempty"`
	RestorationBasis string   `json:"restoration_basis"`
}

type Summary struct {
	ItemCount                  int            `json:"item_count"`
	CosmeticItemCount          int            `json:"cosmetic_item_count"`
	UniqueResourceCount        int            `json:"unique_resource_count"`
	CommodityOnlyResourceCount int            `json:"commodity_only_resource_count"`
	ResourceStatusCounts       map[string]int `json:"resource_status_counts"`
}

type Index struct {
	SchemaVersion          int            `json:"schema_version"`
	GeneratedUTC           string         `json:"generated_utc"`
	ItemConfigVersion      uint32         `json:"item_config_version"`
	CommodityConfigVersion uint32         `json:"commodity_config_version"`
	CDNBaseURL             string         `json:"cdn_base_url"`
	Summary                Summary        `json:"summary"`
	Items                  []ItemResource `json:"items"`
	CommodityOnlyResources []ResourceSpec `json:"commodity_only_resources,omitempty"`
}

func BuildSpecs(items []itemcatalog.RegistryEntry, commodities []itemcatalog.CommodityEntry) []ResourceSpec {
	byKey := make(map[ResourceKey]*ResourceSpec)
	get := func(key ResourceKey) *ResourceSpec {
		entry := byKey[key]
		if entry == nil {
			entry = &ResourceSpec{ResourceKey: key}
			byKey[key] = entry
		}
		return entry
	}
	for _, item := range items {
		entry := get(ResourceKey{Category: item.Category, ResourceID: item.ResourceID})
		entry.ItemIDs = appendUniqueUint32(entry.ItemIDs, item.ItemID)
		entry.Names = appendUniqueString(entry.Names, item.Name)
	}
	for _, commodity := range commodities {
		entry := get(ResourceKey{Category: commodity.Category, ResourceID: commodity.ResourceID})
		entry.CommodityIDs = appendUniqueUint32(entry.CommodityIDs, commodity.CommodityID)
		entry.Names = appendUniqueString(entry.Names, commodity.Name)
	}
	result := make([]ResourceSpec, 0, len(byKey))
	for _, entry := range byKey {
		sort.Slice(entry.ItemIDs, func(i, j int) bool { return entry.ItemIDs[i] < entry.ItemIDs[j] })
		sort.Slice(entry.CommodityIDs, func(i, j int) bool { return entry.CommodityIDs[i] < entry.CommodityIDs[j] })
		sort.Strings(entry.Names)
		entry.CommodityOnly = len(entry.ItemIDs) == 0
		result = append(result, *entry)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Category != result[j].Category {
			return result[i].Category < result[j].Category
		}
		return result[i].ResourceID < result[j].ResourceID
	})
	return result
}

func SourceZipRelative(key ResourceKey) string {
	return path.Join(key.Category, key.Category+strconv.FormatUint(uint64(key.ResourceID), 10)+".zip")
}

func CachePath(cacheRoot string, key ResourceKey) string {
	return filepath.Join(cacheRoot, key.Category, key.Category+strconv.FormatUint(uint64(key.ResourceID), 10)+".zip")
}

func InspectZip(zipPath string) (ZipInfo, error) {
	stat, err := os.Stat(zipPath)
	if err != nil {
		return ZipInfo{}, err
	}
	if stat.Size() < 4 || stat.Size() > maxDownloadBytes {
		return ZipInfo{}, fmt.Errorf("archive size %d is outside 4..%d", stat.Size(), maxDownloadBytes)
	}
	data, err := os.ReadFile(zipPath)
	if err != nil {
		return ZipInfo{}, err
	}
	if !bytes.HasPrefix(data, []byte{'P', 'K'}) {
		return ZipInfo{}, fmt.Errorf("archive does not start with PK")
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ZipInfo{}, err
	}
	if len(reader.File) == 0 || len(reader.File) > maxArchiveFiles {
		return ZipInfo{}, fmt.Errorf("archive contains %d files", len(reader.File))
	}
	files := make(map[string]*zip.File, len(reader.File))
	var manifest *zip.File
	var expanded uint64
	for _, file := range reader.File {
		clean, cleanErr := cleanArchiveName(file.Name)
		if cleanErr != nil {
			return ZipInfo{}, cleanErr
		}
		if file.UncompressedSize64 > maxArchiveFile {
			return ZipInfo{}, fmt.Errorf("archive member %q is too large", file.Name)
		}
		expanded += file.UncompressedSize64
		if expanded > maxArchiveTotal {
			return ZipInfo{}, fmt.Errorf("archive expands beyond %d bytes", maxArchiveTotal)
		}
		files[strings.ToLower(clean)] = file
		if strings.EqualFold(path.Base(clean), "FilePath.ini") {
			if manifest != nil {
				return ZipInfo{}, fmt.Errorf("archive has multiple FilePath.ini files")
			}
			manifest = file
		}
	}
	var root string
	var mappings []ManifestFile
	if manifest != nil {
		manifestData, err := readZipMember(manifest)
		if err != nil {
			return ZipInfo{}, fmt.Errorf("read FilePath.ini: %w", err)
		}
		manifestName, _ := cleanArchiveName(manifest.Name)
		root = path.Dir(manifestName)
		if root == "." {
			root = ""
		}
		mappings, err = parseManifest(manifestData)
		if err != nil {
			return ZipInfo{}, err
		}
		for index := range mappings {
			candidates := []string{path.Join(root, mappings[index].Source), mappings[index].Source}
			for _, candidate := range candidates {
				if file := files[strings.ToLower(candidate)]; file != nil && !file.FileInfo().IsDir() {
					mappings[index].ArchivePath = candidate
					break
				}
			}
			if mappings[index].ArchivePath == "" {
				return ZipInfo{}, fmt.Errorf("manifest source %q does not exist", mappings[index].Source)
			}
		}
	}
	digest := sha256.Sum256(data)
	return ZipInfo{
		Path: zipPath, SHA256: strings.ToUpper(hex.EncodeToString(digest[:])), Size: int64(len(data)),
		Files: mappings, Root: root, FileCount: len(reader.File),
	}, nil
}

func InstallZip(zipPath, clientRoot string) (InstallResult, error) {
	info, err := InspectZip(zipPath)
	if err != nil {
		return InstallResult{}, err
	}
	return installZipInfo(zipPath, clientRoot, info)
}

func InstallZipWithOverride(zipPath, clientRoot string, override *ManifestOverride) (InstallResult, error) {
	info, err := inspectZipWithOverride(zipPath, override)
	if err != nil {
		return InstallResult{}, err
	}
	return installZipInfo(zipPath, clientRoot, info)
}

func installZipInfo(zipPath, clientRoot string, info ZipInfo) (InstallResult, error) {
	if len(info.Files) == 0 {
		return InstallResult{}, fmt.Errorf("archive has no FilePath.ini and cannot be installed safely")
	}
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return InstallResult{}, err
	}
	defer reader.Close()
	files := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		clean, _ := cleanArchiveName(file.Name)
		files[strings.ToLower(clean)] = file
	}
	rootAbsolute, err := filepath.Abs(clientRoot)
	if err != nil {
		return InstallResult{}, err
	}
	result := InstallResult{}
	for _, mapping := range info.Files {
		file := files[strings.ToLower(mapping.ArchivePath)]
		if file == nil {
			return result, fmt.Errorf("archive member %q disappeared", mapping.ArchivePath)
		}
		data, readErr := readZipMember(file)
		if readErr != nil {
			return result, readErr
		}
		destination, rel, destinationErr := safeDestination(rootAbsolute, mapping.Destination)
		if destinationErr != nil {
			return result, destinationErr
		}
		if existing, readErr := os.ReadFile(destination); readErr == nil {
			if bytes.Equal(existing, data) {
				result.Existing = append(result.Existing, rel)
				continue
			}
			result.Conflicts = append(result.Conflicts, rel)
			continue
		} else if !os.IsNotExist(readErr) {
			return result, readErr
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return result, err
		}
		temporary, err := os.CreateTemp(filepath.Dir(destination), ".qqt-resource-*.tmp")
		if err != nil {
			return result, err
		}
		temporaryPath := temporary.Name()
		ok := false
		if _, err = temporary.Write(data); err == nil {
			err = temporary.Close()
		}
		if err == nil {
			err = os.Rename(temporaryPath, destination)
		}
		if err == nil {
			ok = true
			result.Installed = append(result.Installed, rel)
		}
		if !ok {
			temporary.Close()
			os.Remove(temporaryPath)
			return result, err
		}
	}
	sort.Strings(result.Installed)
	sort.Strings(result.Existing)
	sort.Strings(result.Conflicts)
	return result, nil
}

func LoadManifestOverrides(configPath string) (map[ResourceKey]ManifestOverride, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var config ManifestOverrides
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if config.SchemaVersion != 1 {
		return nil, fmt.Errorf("manifest override schema %d, want 1", config.SchemaVersion)
	}
	result := make(map[ResourceKey]ManifestOverride, len(config.Overrides))
	for index, override := range config.Overrides {
		if !categoryPattern.MatchString(override.Category) || override.ResourceID == 0 || override.Confidence == "" || len(override.Evidence) == 0 || len(override.Files) == 0 {
			return nil, fmt.Errorf("manifest override %d is incomplete", index)
		}
		if _, duplicate := result[override.ResourceKey]; duplicate {
			return nil, fmt.Errorf("manifest override repeats %s/%d", override.Category, override.ResourceID)
		}
		for fileIndex := range override.Files {
			file := &override.Files[fileIndex]
			var cleanErr error
			file.Source, cleanErr = cleanArchiveName(file.Source)
			if cleanErr != nil {
				return nil, fmt.Errorf("manifest override %s/%d source: %w", override.Category, override.ResourceID, cleanErr)
			}
			file.ArchivePath, cleanErr = cleanArchiveName(file.ArchivePath)
			if cleanErr != nil {
				return nil, fmt.Errorf("manifest override %s/%d archive path: %w", override.Category, override.ResourceID, cleanErr)
			}
			file.Destination, cleanErr = cleanDestination(file.Destination)
			if cleanErr != nil {
				return nil, fmt.Errorf("manifest override %s/%d destination: %w", override.Category, override.ResourceID, cleanErr)
			}
		}
		result[override.ResourceKey] = override
	}
	return result, nil
}

func LoadVariantPreferences(configPath string) (map[ResourceKey]VariantPreference, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var config VariantPreferences
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	if config.SchemaVersion != 1 {
		return nil, fmt.Errorf("variant preference schema %d, want 1", config.SchemaVersion)
	}
	result := make(map[ResourceKey]VariantPreference, len(config.Preferences))
	for index, preference := range config.Preferences {
		if !categoryPattern.MatchString(preference.Category) || preference.ResourceID == 0 ||
			preference.Decision != "prefer-local" || len(preference.Evidence) == 0 {
			return nil, fmt.Errorf("variant preference %d is incomplete", index)
		}
		if _, duplicate := result[preference.ResourceKey]; duplicate {
			return nil, fmt.Errorf("variant preference repeats %s/%d", preference.Category, preference.ResourceID)
		}
		result[preference.ResourceKey] = preference
	}
	return result, nil
}

func inspectZipWithOverride(zipPath string, override *ManifestOverride) (ZipInfo, error) {
	info, err := InspectZip(zipPath)
	if err != nil || len(info.Files) > 0 || override == nil {
		return info, err
	}
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return ZipInfo{}, err
	}
	defer reader.Close()
	files := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		clean, cleanErr := cleanArchiveName(file.Name)
		if cleanErr != nil {
			return ZipInfo{}, cleanErr
		}
		files[strings.ToLower(clean)] = file
	}
	for _, mapping := range override.Files {
		if file := files[strings.ToLower(mapping.ArchivePath)]; file == nil || file.FileInfo().IsDir() {
			return ZipInfo{}, fmt.Errorf("override archive path %q is absent", mapping.ArchivePath)
		}
	}
	info.Files = append([]ManifestFile(nil), override.Files...)
	return info, nil
}

func DownloadAll(ctx context.Context, specs []ResourceSpec, options DownloadOptions) []DownloadResult {
	if options.BaseURL == "" {
		options.BaseURL = DefaultCDNBaseURL
	}
	if options.Concurrency <= 0 {
		options.Concurrency = 6
	}
	if options.Retries <= 0 {
		options.Retries = 3
	}
	if options.Client == nil {
		options.Client = &http.Client{Timeout: 60 * time.Second}
	}
	jobs := make(chan ResourceSpec)
	results := make(chan DownloadResult)
	var workers sync.WaitGroup
	for index := 0; index < options.Concurrency; index++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for spec := range jobs {
				results <- downloadOne(ctx, spec.ResourceKey, options)
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, spec := range specs {
			select {
			case jobs <- spec:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()
	output := make([]DownloadResult, 0, len(specs))
	for result := range results {
		output = append(output, result)
		if options.Progress != nil {
			options.Progress(len(output), len(specs), result)
		}
	}
	sort.Slice(output, func(i, j int) bool {
		if output[i].Resource.Category != output[j].Resource.Category {
			return output[i].Resource.Category < output[j].Resource.Category
		}
		return output[i].Resource.ResourceID < output[j].Resource.ResourceID
	})
	return output
}

func BuildIndexWithPolicies(clientRoot, cacheRoot, cdnBaseURL string, generated time.Time, overrides map[ResourceKey]ManifestOverride, preferences map[ResourceKey]VariantPreference) (Index, error) {
	itemVersion, items, err := itemcatalog.LoadItemRegistry(clientRoot)
	if err != nil {
		return Index{}, err
	}
	commodityVersion, commodities, err := itemcatalog.LoadCommodityRegistry(clientRoot)
	if err != nil {
		return Index{}, err
	}
	specs := BuildSpecs(items, commodities)
	specByKey := make(map[ResourceKey]ResourceSpec, len(specs))
	for _, spec := range specs {
		specByKey[spec.ResourceKey] = spec
	}
	index := Index{
		SchemaVersion: 1, GeneratedUTC: generated.UTC().Format(time.RFC3339),
		ItemConfigVersion: itemVersion, CommodityConfigVersion: commodityVersion,
		CDNBaseURL: strings.TrimRight(cdnBaseURL, "/"),
		Summary:    Summary{ResourceStatusCounts: make(map[string]int)},
	}
	resourceStates := make(map[ResourceKey]resourceState, len(specs))
	for _, spec := range specs {
		override, hasOverride := overrides[spec.ResourceKey]
		var overridePointer *ManifestOverride
		if hasOverride {
			overridePointer = &override
		}
		preference, hasPreference := preferences[spec.ResourceKey]
		var preferencePointer *VariantPreference
		if hasPreference {
			preferencePointer = &preference
		}
		state := inspectResource(clientRoot, CachePath(cacheRoot, spec.ResourceKey), overridePointer, preferencePointer)
		resourceStates[spec.ResourceKey] = state
		index.Summary.ResourceStatusCounts[state.Status]++
		if spec.CommodityOnly {
			index.CommodityOnlyResources = append(index.CommodityOnlyResources, spec)
			index.Summary.CommodityOnlyResourceCount++
		}
	}
	for _, item := range items {
		key := ResourceKey{Category: item.Category, ResourceID: item.ResourceID}
		state := resourceStates[key]
		spec := specByKey[key]
		index.Items = append(index.Items, ItemResource{
			ItemID: item.ItemID, Category: item.Category, ResourceID: item.ResourceID,
			Name: item.Name, Description: item.Description, CommodityIDs: spec.CommodityIDs,
			SourceZip: SourceZipRelative(key), ResourceStatus: state.Status,
			ZipSHA256: state.ZipSHA256, ZipBytes: state.ZipBytes,
			InstalledFiles: state.Installed, MissingFiles: state.Missing,
			ConflictFiles: state.Conflicts, VariantFiles: state.Variants, RestorationBasis: state.Basis,
		})
		if IsAppearanceCategory(item.Category) {
			index.Summary.CosmeticItemCount++
		}
	}
	index.Summary.ItemCount = len(index.Items)
	index.Summary.UniqueResourceCount = len(specs)
	return index, nil
}

func IsAppearanceCategory(category string) bool {
	switch category {
	case "thadorn", "cap", "hair", "eye", "mouth", "fpack", "npack", "ear", "cladorn",
		"bomb", "huanying", "footprint", "bg", "frame", "enter", "namecard", "namecardbound":
		return true
	default:
		return false
	}
}

type resourceState struct {
	Status, Basis, ZipSHA256 string
	ZipBytes                 int64
	Installed, Missing       []string
	Conflicts, Variants      []string
}

func inspectResource(clientRoot, zipPath string, override *ManifestOverride, preference *VariantPreference) resourceState {
	info, err := inspectZipWithOverride(zipPath, override)
	if err != nil {
		key := resourceKeyFromCachePath(zipPath)
		local := exactLocalCandidates(clientRoot, key)
		if len(local) > 0 {
			return resourceState{Status: "local-only", Basis: "exact-local-resource-no-source-zip", Installed: local}
		}
		return resourceState{Status: "missing", Basis: "no-valid-original-zip"}
	}
	state := resourceState{Status: "available", Basis: "verified-original-zip", ZipSHA256: info.SHA256, ZipBytes: info.Size}
	if override != nil && len(info.Files) > 0 {
		state.Basis = "verified-explicit-manifest-override"
	}
	if len(info.Files) == 0 {
		state.Status = "unmapped"
		state.Basis = "verified-original-zip-without-filepath"
		return state
	}
	rootAbsolute, err := filepath.Abs(clientRoot)
	if err != nil {
		return state
	}
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return state
	}
	defer reader.Close()
	files := make(map[string]*zip.File, len(reader.File))
	for _, file := range reader.File {
		clean, _ := cleanArchiveName(file.Name)
		files[strings.ToLower(clean)] = file
	}
	for _, mapping := range info.Files {
		_, rel, destinationErr := safeDestination(rootAbsolute, mapping.Destination)
		if destinationErr != nil {
			state.Conflicts = append(state.Conflicts, mapping.Destination)
			continue
		}
		destination, _, _ := safeDestination(rootAbsolute, mapping.Destination)
		existing, readErr := os.ReadFile(destination)
		if readErr != nil {
			state.Missing = append(state.Missing, rel)
			continue
		}
		archiveData, readErr := readZipMember(files[strings.ToLower(mapping.ArchivePath)])
		if readErr != nil || !bytes.Equal(existing, archiveData) {
			state.Conflicts = append(state.Conflicts, rel)
			continue
		}
		state.Installed = append(state.Installed, rel)
	}
	sort.Strings(state.Installed)
	sort.Strings(state.Missing)
	sort.Strings(state.Conflicts)
	if preference != nil && preference.Decision == "prefer-local" && len(state.Missing) == 0 &&
		len(state.Conflicts) > 0 && len(state.Installed)+len(state.Conflicts) == len(info.Files) {
		state.Variants = append(state.Variants, state.Conflicts...)
		state.Installed = append(state.Installed, state.Conflicts...)
		sort.Strings(state.Installed)
		state.Conflicts = nil
		state.Status = "local-preferred"
		state.Basis = "audited-local-client-variant-preferred-over-cdn"
		return state
	}
	switch {
	case len(info.Files) > 0 && len(state.Installed) == len(info.Files):
		state.Status = "restored"
		if override != nil {
			state.Basis = "verified-explicit-manifest-override-installed"
		} else {
			state.Basis = "verified-filepath-installed"
		}
	case len(state.Installed) > 0 || len(state.Conflicts) > 0:
		state.Status = "partial"
		state.Basis = "verified-filepath-partial"
	}
	return state
}

func resourceKeyFromCachePath(zipPath string) ResourceKey {
	category := filepath.Base(filepath.Dir(zipPath))
	base := strings.TrimSuffix(filepath.Base(zipPath), filepath.Ext(zipPath))
	idText := strings.TrimPrefix(base, category)
	id, _ := strconv.ParseUint(idText, 10, 32)
	return ResourceKey{Category: category, ResourceID: uint32(id)}
}

func exactLocalCandidates(clientRoot string, key ResourceKey) []string {
	stem := key.Category + strconv.FormatUint(uint64(key.ResourceID), 10)
	candidates := []string{
		path.Join("res", "uiRes", "icon", key.Category, stem+".img"),
		path.Join("object", key.Category, stem+".img"),
		path.Join("object", key.Category, stem+"_stand.img"),
		path.Join("object", key.Category, stem+"_walk.img"),
	}
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		info, err := os.Stat(filepath.Join(clientRoot, filepath.FromSlash(candidate)))
		if err == nil && !info.IsDir() {
			result = append(result, candidate)
		}
	}
	return result
}

func downloadOne(ctx context.Context, key ResourceKey, options DownloadOptions) DownloadResult {
	result := DownloadResult{Resource: key, Path: CachePath(options.CacheRoot, key)}
	if !categoryPattern.MatchString(key.Category) {
		result.Status = "invalid-category"
		result.Error = "category contains unsafe characters"
		return result
	}
	if _, err := InspectZip(result.Path); err == nil {
		result.Status = "cached"
		return result
	}
	if err := os.MkdirAll(filepath.Dir(result.Path), 0o755); err != nil {
		result.Status, result.Error = "error", err.Error()
		return result
	}
	resourceURL := strings.TrimRight(options.BaseURL, "/") + "/" + url.PathEscape(key.Category) + "/" +
		url.PathEscape(key.Category+strconv.FormatUint(uint64(key.ResourceID), 10)+".zip")
	var lastErr error
	for attempt := 1; attempt <= options.Retries; attempt++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, resourceURL, nil)
		if err != nil {
			lastErr = err
			break
		}
		request.Header.Set("User-Agent", "QQTang-LocalRestore/1.0 (preservation; original-client-resource-recovery)")
		response, err := options.Client.Do(request)
		if err != nil {
			lastErr = err
			continue
		}
		if response.StatusCode == http.StatusNotFound {
			response.Body.Close()
			result.Status = "missing"
			result.Error = "HTTP 404"
			return result
		}
		if response.StatusCode != http.StatusOK {
			io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			response.Body.Close()
			lastErr = fmt.Errorf("HTTP %d", response.StatusCode)
			continue
		}
		temporary, err := os.CreateTemp(filepath.Dir(result.Path), ".qqt-download-*.part")
		if err != nil {
			response.Body.Close()
			lastErr = err
			continue
		}
		temporaryPath := temporary.Name()
		written, copyErr := io.Copy(temporary, io.LimitReader(response.Body, maxDownloadBytes+1))
		closeErr := temporary.Close()
		response.Body.Close()
		if copyErr != nil || closeErr != nil || written > maxDownloadBytes {
			os.Remove(temporaryPath)
			if copyErr != nil {
				lastErr = copyErr
			} else if closeErr != nil {
				lastErr = closeErr
			} else {
				lastErr = fmt.Errorf("download exceeds %d bytes", maxDownloadBytes)
			}
			continue
		}
		if _, err := InspectZip(temporaryPath); err != nil {
			os.Remove(temporaryPath)
			lastErr = fmt.Errorf("downloaded payload is not a valid QQTang resource ZIP: %w", err)
			continue
		}
		if err := replaceValidated(temporaryPath, result.Path); err != nil {
			os.Remove(temporaryPath)
			lastErr = err
			continue
		}
		result.Status = "downloaded"
		return result
	}
	result.Status = "error"
	if lastErr != nil {
		result.Error = lastErr.Error()
	}
	return result
}

func parseManifest(data []byte) ([]ManifestFile, error) {
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("FilePath.ini exceeds 1 MiB")
	}
	values := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	sectionSeen := false
	for scanner.Scan() {
		line := strings.TrimSpace(strings.TrimPrefix(scanner.Text(), "\ufeff"))
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if !sectionSeen {
			if !strings.EqualFold(line, "[batch]") {
				return nil, fmt.Errorf("FilePath.ini does not start with [batch]")
			}
			sectionSeen = true
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid FilePath.ini line %q", line)
		}
		values[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	count, err := strconv.Atoi(values["count"])
	if err != nil || count <= 0 || count > maxArchiveFiles {
		return nil, fmt.Errorf("invalid FilePath.ini count %q", values["count"])
	}
	result := make([]ManifestFile, 0, count)
	for index := 1; index <= count; index++ {
		source, err := cleanArchiveName(values["src"+strconv.Itoa(index)])
		if err != nil {
			return nil, fmt.Errorf("invalid src%d: %w", index, err)
		}
		destination, err := cleanDestination(values["dst"+strconv.Itoa(index)])
		if err != nil {
			return nil, fmt.Errorf("invalid dst%d: %w", index, err)
		}
		result = append(result, ManifestFile{Source: source, Destination: destination})
	}
	return result, nil
}

func cleanArchiveName(name string) (string, error) {
	name = strings.ReplaceAll(strings.TrimSpace(name), "\\", "/")
	clean := path.Clean(name)
	if clean == "." || clean == "" || strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") || strings.ContainsRune(clean, '\x00') {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	return clean, nil
}

func cleanDestination(destination string) (string, error) {
	destination = strings.ReplaceAll(strings.TrimSpace(destination), "\\", "/")
	clean := path.Clean(destination)
	if clean == "." || clean == "" || strings.HasPrefix(clean, "/") || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, ":") || strings.ContainsRune(clean, '\x00') {
		return "", fmt.Errorf("unsafe destination %q", destination)
	}
	return clean, nil
}

func safeDestination(rootAbsolute, destination string) (string, string, error) {
	rel, err := cleanDestination(destination)
	if err != nil {
		return "", "", err
	}
	joined := filepath.Join(rootAbsolute, filepath.FromSlash(rel))
	absolute, err := filepath.Abs(joined)
	if err != nil {
		return "", "", err
	}
	prefix := rootAbsolute + string(filepath.Separator)
	if !strings.HasPrefix(strings.ToLower(absolute), strings.ToLower(prefix)) {
		return "", "", fmt.Errorf("destination escapes client root: %q", destination)
	}
	return absolute, filepath.ToSlash(rel), nil
}

func readZipMember(file *zip.File) ([]byte, error) {
	if file == nil {
		return nil, os.ErrNotExist
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxArchiveFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxArchiveFile {
		return nil, fmt.Errorf("archive member exceeds %d bytes", maxArchiveFile)
	}
	return data, nil
}

func replaceValidated(temporaryPath, destination string) error {
	if err := os.Rename(temporaryPath, destination); err == nil {
		return nil
	}
	if err := os.Remove(destination); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(temporaryPath, destination)
}

func appendUniqueUint32(values []uint32, value uint32) []uint32 {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func appendUniqueString(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

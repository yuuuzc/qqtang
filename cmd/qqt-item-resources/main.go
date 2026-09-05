package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"qqtang/internal/game/itemcatalog"
	"qqtang/internal/game/itemresources"
)

func main() {
	clientRoot := flag.String("client-root", "runtime/client-patched", "installed QQTang client root")
	cacheRoot := flag.String("cache-root", "runtime/resource-cache/item-zips", "verified original ZIP cache")
	jsonPath := flag.String("json", "data/qqt_item_resources.json", "canonical item-to-resource JSON")
	reportPath := flag.String("report", "docs/item-resource-restoration.md", "restoration report")
	downloadLogPath := flag.String("download-log", "data/qqt_resource_download_results.json", "download result JSON")
	baseURL := flag.String("base-url", itemresources.DefaultCDNBaseURL, "original resource CDN base URL")
	overridePath := flag.String("manifest-overrides", "configs/item-resource-manifest-overrides.json", "audited FilePath.ini overrides")
	preferencePath := flag.String("variant-preferences", "configs/item-resource-variant-preferences.json", "audited local/CDN variant decisions")
	download := flag.Bool("download", false, "download missing original ZIPs")
	install := flag.Bool("install", false, "install verified ZIPs according to FilePath.ini")
	appearanceOnly := flag.Bool("appearance-only", false, "limit download/install to appearance categories")
	categories := flag.String("categories", "", "optional comma-separated category allowlist")
	concurrency := flag.Int("concurrency", 6, "download concurrency")
	retries := flag.Int("retries", 3, "download attempts per resource")
	flag.Parse()

	itemVersion, items, err := itemcatalog.LoadItemRegistry(*clientRoot)
	if err != nil {
		fatal(err)
	}
	commodityVersion, commodities, err := itemcatalog.LoadCommodityRegistry(*clientRoot)
	if err != nil {
		fatal(err)
	}
	if itemVersion != commodityVersion {
		fatal(fmt.Errorf("itemCFG version %d does not match commodityCFG version %d", itemVersion, commodityVersion))
	}
	allSpecs := itemresources.BuildSpecs(items, commodities)
	overrides, err := itemresources.LoadManifestOverrides(*overridePath)
	if err != nil {
		fatal(err)
	}
	preferences, err := itemresources.LoadVariantPreferences(*preferencePath)
	if err != nil {
		fatal(err)
	}
	selected := filterSpecs(allSpecs, *appearanceOnly, *categories)
	fmt.Printf("config version=%d canonical_items=%d commodities=%d unique_resources=%d selected=%d\n",
		itemVersion, len(items), len(commodities), len(allSpecs), len(selected))

	var downloads []itemresources.DownloadResult
	if *download {
		lastProgress := time.Now()
		downloads = itemresources.DownloadAll(context.Background(), selected, itemresources.DownloadOptions{
			BaseURL: *baseURL, CacheRoot: *cacheRoot, Concurrency: *concurrency, Retries: *retries,
			Progress: func(completed, total int, result itemresources.DownloadResult) {
				if completed == total || completed%25 == 0 || time.Since(lastProgress) >= 10*time.Second {
					fmt.Printf("download %d/%d last=%s/%d status=%s\n", completed, total,
						result.Resource.Category, result.Resource.ResourceID, result.Status)
					lastProgress = time.Now()
				}
			},
		})
		if err := writeJSON(*downloadLogPath, downloads); err != nil {
			fatal(err)
		}
		fmt.Printf("download summary: %s\n", summarizeDownloads(downloads))
	}

	if *install {
		installedFiles, existingFiles, conflicts, invalid := 0, 0, 0, 0
		for index, spec := range selected {
			zipPath := itemresources.CachePath(*cacheRoot, spec.ResourceKey)
			override, hasOverride := overrides[spec.ResourceKey]
			var overridePointer *itemresources.ManifestOverride
			if hasOverride {
				overridePointer = &override
			}
			result, installErr := itemresources.InstallZipWithOverride(zipPath, *clientRoot, overridePointer)
			if installErr != nil {
				invalid++
				continue
			}
			installedFiles += len(result.Installed)
			existingFiles += len(result.Existing)
			conflicts += len(result.Conflicts)
			if (index+1)%100 == 0 || index+1 == len(selected) {
				fmt.Printf("install %d/%d new_files=%d existing=%d conflicts=%d invalid_or_missing=%d\n",
					index+1, len(selected), installedFiles, existingFiles, conflicts, invalid)
			}
		}
	}

	index, err := itemresources.BuildIndexWithPolicies(*clientRoot, *cacheRoot, *baseURL, time.Now(), overrides, preferences)
	if err != nil {
		fatal(err)
	}
	if err := writeJSON(*jsonPath, index); err != nil {
		fatal(err)
	}
	if err := writeReport(*reportPath, index); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote canonical index %s and report %s\n", *jsonPath, *reportPath)
	fmt.Printf("summary: items=%d cosmetics=%d unique=%d commodity_only=%d status=%v\n",
		index.Summary.ItemCount, index.Summary.CosmeticItemCount, index.Summary.UniqueResourceCount,
		index.Summary.CommodityOnlyResourceCount, index.Summary.ResourceStatusCounts)
}

func filterSpecs(specs []itemresources.ResourceSpec, appearanceOnly bool, categories string) []itemresources.ResourceSpec {
	allowed := make(map[string]bool)
	for _, category := range strings.Split(categories, ",") {
		category = strings.TrimSpace(category)
		if category != "" {
			allowed[category] = true
		}
	}
	result := make([]itemresources.ResourceSpec, 0, len(specs))
	for _, spec := range specs {
		if appearanceOnly && !itemresources.IsAppearanceCategory(spec.Category) {
			continue
		}
		if len(allowed) > 0 && !allowed[spec.Category] {
			continue
		}
		result = append(result, spec)
	}
	return result
}

func summarizeDownloads(results []itemresources.DownloadResult) string {
	counts := make(map[string]int)
	for _, result := range results {
		counts[result.Status]++
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
	}
	return strings.Join(parts, " ")
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data)
}

func writeReport(reportPath string, index itemresources.Index) error {
	var report strings.Builder
	fmt.Fprintf(&report, "# QQ堂 5.2 道具资源恢复报告\n\n")
	fmt.Fprintf(&report, "生成时间：%s\n\n", index.GeneratedUTC)
	fmt.Fprintf(&report, "- itemCFG 版本：%d\n", index.ItemConfigVersion)
	fmt.Fprintf(&report, "- commodityCFG 版本：%d\n", index.CommodityConfigVersion)
	fmt.Fprintf(&report, "- itemCFG canonical 道具数：%d\n", index.Summary.ItemCount)
	fmt.Fprintf(&report, "- 外观道具数：%d\n", index.Summary.CosmeticItemCount)
	fmt.Fprintf(&report, "- unique category/resource 数：%d\n", index.Summary.UniqueResourceCount)
	fmt.Fprintf(&report, "- commodity-only resource 数：%d\n", index.Summary.CommodityOnlyResourceCount)
	statusKeys := make([]string, 0, len(index.Summary.ResourceStatusCounts))
	for status := range index.Summary.ResourceStatusCounts {
		statusKeys = append(statusKeys, status)
	}
	sort.Strings(statusKeys)
	for _, status := range statusKeys {
		fmt.Fprintf(&report, "- %s unique resource：%d\n", status, index.Summary.ResourceStatusCounts[status])
	}
	report.WriteString("\n状态定义：`restored` 表示原始 ZIP 有效且 `FilePath.ini` 全部目标文件与 ZIP 一致；`local-preferred` 表示 CDN ZIP 与完整 5.2 客户端是两个已验证变体，并按审计决策保留客户端版本；`available` 表示 ZIP 已缓存但尚未完整安装；`partial` 表示尚未裁决的冲突；`local-only` 表示 CDN ZIP 缺失但完整客户端已有精确资源；`unmapped` 表示官方 ZIP 有效但缺少 `FilePath.ini`，因此未猜测安装路径；`missing` 表示 ZIP 与精确本地资源均不存在。\n")
	report.WriteString("\n## 尚未由 FilePath 完整闭合的 canonical item 清单\n\n")
	report.WriteString("| item_id | category | resource_id | name | status | basis | source_zip |\n|---:|---|---:|---|---|---|---|\n")
	for _, item := range index.Items {
		if item.ResourceStatus == "restored" {
			continue
		}
		fmt.Fprintf(&report, "| %d | %s | %d | %s | %s | %s | %s |\n", item.ItemID,
			escapeMarkdown(item.Category), item.ResourceID, escapeMarkdown(item.Name), item.ResourceStatus,
			escapeMarkdown(item.RestorationBasis), escapeMarkdown(item.SourceZip))
	}
	return writeAtomic(reportPath, []byte(report.String()))
}

func escapeMarkdown(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "|", "\\|"), "\n", " ")
}

func writeAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".qqt-item-resources-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	ok := false
	defer func() {
		temporary.Close()
		if !ok {
			os.Remove(temporaryPath)
		}
	}()
	if _, err := temporary.Write(data); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
			return removeErr
		}
		if err := os.Rename(temporaryPath, path); err != nil {
			return err
		}
	}
	ok = true
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

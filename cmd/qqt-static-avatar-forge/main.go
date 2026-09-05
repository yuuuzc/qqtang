package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"qqtang/internal/tooling/clientpatch"
)

func main() {
	path := flag.String("file", "", "prepared Client.exe path")
	metadata := flag.String("metadata", "", "optional Client.tp-free.json path")
	check := flag.Bool("check", false, "verify the static patch without changing files")
	output := flag.String("out", "", "optional JSON report path")
	flag.Parse()
	if *path == "" {
		fatalf("-file is required")
	}

	var (
		result clientpatch.AvatarForgeBoundaryPatchResult
		err    error
	)
	if *check {
		result, err = clientpatch.CheckStaticAvatarForgeBoundary(*path)
	} else {
		result, err = clientpatch.PatchStaticAvatarForgeBoundary(*path)
	}
	if err != nil {
		fatalf("%v", err)
	}
	if *metadata != "" {
		if err := syncMetadata(*metadata, result.AfterSHA256, *check); err != nil {
			fatalf("%v", err)
		}
		result.MetadataPath = *metadata
		result.MetadataSHA256 = result.AfterSHA256
	}

	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fatalf("encode result: %v", err)
	}
	data = append(data, '\n')
	if *output == "" {
		_, _ = os.Stdout.Write(data)
		return
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		fatalf("write result: %v", err)
	}
}

func syncMetadata(path, digest string, check bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read TP-free metadata: %w", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		return fmt.Errorf("decode TP-free metadata: %w", err)
	}
	current, ok := metadata["sha256"].(string)
	if !ok || current == "" {
		return fmt.Errorf("TP-free metadata has no sha256 string")
	}
	if check {
		if !strings.EqualFold(current, digest) {
			return fmt.Errorf("Client.tp-free.json sha256 %s does not match patched Client.exe %s", current, digest)
		}
		return nil
	}
	metadata["sha256"] = strings.ToLower(digest)
	updated, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("encode TP-free metadata: %w", err)
	}
	updated = append(updated, '\n')
	if err := os.WriteFile(path, updated, 0o644); err != nil {
		return fmt.Errorf("write TP-free metadata: %w", err)
	}
	return nil
}

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}

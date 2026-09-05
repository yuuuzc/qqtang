package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"qqtang/internal/tooling/clientpatch"
)

func main() {
	file := flag.String("file", filepath.Join("runtime", "client-patched", "QQTSection.dll"), "prepared QQTang QQTSection.dll")
	check := flag.Bool("check", false, "verify without changing files")
	output := flag.String("out", "", "optional JSON evidence output")
	flag.Parse()

	var (
		result clientpatch.StartRejectionPromptPatchResult
		err    error
	)
	if *check {
		result, err = clientpatch.CheckStaticStartRejectionPrompt(*file)
	} else {
		result, err = clientpatch.PatchStaticStartRejectionPrompt(*file)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "qqt-static-start-rejection:", err)
		os.Exit(1)
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "qqt-static-start-rejection:", err)
		os.Exit(1)
	}
	encoded = append(encoded, '\n')
	if *output != "" {
		if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "qqt-static-start-rejection:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*output, encoded, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "qqt-static-start-rejection:", err)
			os.Exit(1)
		}
	}
	_, _ = os.Stdout.Write(encoded)
}

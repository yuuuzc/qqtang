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
	clientRoot := flag.String("client-root", filepath.Join("runtime", "client-patched"), "prepared QQTang client root")
	check := flag.Bool("check", false, "verify without changing files")
	output := flag.String("out", "", "optional JSON evidence output")
	flag.Parse()

	var (
		result clientpatch.SoloBossCardPatchResult
		err    error
	)
	if *check {
		result, err = clientpatch.CheckStaticSoloBossCard(*clientRoot)
	} else {
		result, err = clientpatch.PatchStaticSoloBossCard(*clientRoot)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "qqt-static-solo-boss-card:", err)
		os.Exit(1)
	}
	encoded, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "qqt-static-solo-boss-card:", err)
		os.Exit(1)
	}
	encoded = append(encoded, '\n')
	if *output != "" {
		if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "qqt-static-solo-boss-card:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(*output, encoded, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "qqt-static-solo-boss-card:", err)
			os.Exit(1)
		}
	}
	_, _ = os.Stdout.Write(encoded)
}

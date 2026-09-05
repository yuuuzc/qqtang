package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"qqtang/internal/tooling/clientpatch"
)

func main() {
	path := flag.String("file", "", "prepared QQTDir.dll path")
	serverID := flag.Uint("server-id", 2, "local shop server ID")
	check := flag.Bool("check", false, "verify the static patch without changing the file")
	output := flag.String("out", "", "optional JSON report path")
	flag.Parse()
	if *path == "" || *serverID == 0 || *serverID > uint(^uint32(0)) {
		fatalf("-file and a non-zero 32-bit -server-id are required")
	}
	var (
		result clientpatch.ShopServerPatchResult
		err    error
	)
	if *check {
		result, err = clientpatch.CheckStaticShopServer(*path, uint32(*serverID))
	} else {
		result, err = clientpatch.PatchStaticShopServer(*path, uint32(*serverID))
	}
	if err != nil {
		fatalf("%v", err)
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

func fatalf(format string, values ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", values...)
	os.Exit(1)
}

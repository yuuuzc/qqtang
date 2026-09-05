package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"qqtang/internal/game/itemcatalog"
	"qqtang/internal/game/shopcatalog"
)

func main() {
	clientRoot := flag.String("client-root", filepath.Join("runtime", "client-patched"), "QQTang client root")
	output := flag.String("out", "", "output Commodity.ini (defaults to <client-root>/config/Commodity.ini)")
	maximum := flag.Int("max-commodities", shopcatalog.DefaultCommodityLimit, "maximum storefront rows; zero keeps the complete registry")
	flag.Parse()
	if *output == "" {
		*output = filepath.Join(*clientRoot, "config", "Commodity.ini")
	}
	itemVersion, items, err := itemcatalog.LoadItemRegistry(*clientRoot)
	if err != nil {
		fatal(err)
	}
	commodityVersion, commodities, err := itemcatalog.LoadCommodityRegistry(*clientRoot)
	if err != nil {
		fatal(err)
	}
	if itemVersion != commodityVersion {
		fatal(fmt.Errorf("itemCFG version %d differs from commodityCFG version %d", itemVersion, commodityVersion))
	}
	data, summary, err := shopcatalog.BuildCommodityINIWithLimit(commodityVersion, items, commodities, *maximum)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*output), 0o755); err != nil {
		fatal(err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(*output), ".Commodity-*.ini")
	if err != nil {
		fatal(err)
	}
	temporaryPath := temporary.Name()
	ok := false
	defer func() {
		temporary.Close()
		if !ok {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err = temporary.Write(data); err != nil {
		fatal(err)
	}
	if err = temporary.Close(); err != nil {
		fatal(err)
	}
	_ = os.Remove(*output)
	if err = os.Rename(temporaryPath, *output); err != nil {
		fatal(err)
	}
	ok = true
	fmt.Printf("Commodity.ini ready: %s; version=%d; commodities=%d; canonical_items=%d\n", *output, summary.Version, summary.CommodityCount, summary.ItemCount)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "qqt-commodity-config:", err)
	os.Exit(1)
}

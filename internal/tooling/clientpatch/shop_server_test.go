package clientpatch

import (
	"encoding/hex"
	"testing"
)

func TestStaticShopServerPatch(t *testing.T) {
	patch := staticShopServerPatch(2)
	if got := hex.EncodeToString(patch); got != "8b442408c7000200000033c0c20800" {
		t.Fatalf("patch = %s", got)
	}
}

package clientpatch

import (
	"bytes"
	"testing"
)

func TestAvatarForgeBoundaryContexts(t *testing.T) {
	for _, site := range avatarForgeBoundarySites {
		patched, already, err := patchAvatarForgeContext(site, site.context)
		if err != nil {
			t.Fatalf("patch %s: %v", site.name, err)
		}
		if already {
			t.Fatalf("unpatched %s reported as patched", site.name)
		}
		offset := site.rva - site.contextRVA
		if got, want := patched[offset:offset+5], []byte{0x68, 0xFF, 0, 0, 0}; !bytes.Equal(got, want) {
			t.Fatalf("%s instruction = % X, want % X", site.name, got, want)
		}
		if _, already, err = patchAvatarForgeContext(site, patched); err != nil || !already {
			t.Fatalf("idempotent check %s: already=%v err=%v", site.name, already, err)
		}
	}
}

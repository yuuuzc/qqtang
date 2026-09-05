package winlaunch

import (
	"bytes"
	"testing"
)

func TestShopResponseTraceStubsReplayOriginalPrologues(t *testing.T) {
	for _, test := range []struct {
		name     string
		stub     uintptr
		patch    uintptr
		record   uintptr
		original []byte
		entry    shopResponseTraceEntry
	}{
		{"decode", 0x20000000, 0x1000A515, 0x20000500, qqtShopBuyDecodeSignature, shopResponseTraceDecode},
		{"result", 0x20000180, 0x1000B412, 0x20000500, qqtShopBuyResultSignature, shopResponseTraceResult},
		{"dispatch", 0x20000300, 0x1000A43C, 0x20000500, qqtShopResponseDispatchSignature, shopResponseTraceDispatch},
	} {
		t.Run(test.name, func(t *testing.T) {
			stub := buildShopResponseEntryStub(test.stub, test.record, test.patch, test.original, test.entry)
			if !bytes.Contains(stub, test.original) {
				t.Fatalf("stub does not replay original prologue: %X", stub)
			}
			if len(stub) < 5 || stub[len(stub)-5] != 0xE9 {
				t.Fatalf("stub has no tail jump: %X", stub)
			}
		})
	}
}

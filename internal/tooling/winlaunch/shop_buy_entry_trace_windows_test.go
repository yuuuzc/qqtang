package winlaunch

import (
	"bytes"
	"testing"
)

func TestShopBuyEntryTraceStubsReplayOriginalInstructions(t *testing.T) {
	tests := []struct {
		name     string
		original []byte
		build    func(uintptr, uintptr, uintptr, []byte) []byte
	}{
		{"entry", qqtShopBuyEntrySignature, buildShopBuyEntryStub},
		{"interfaces", qqtShopBuyInterfacesSignature, buildShopBuyInterfacesStub},
		{"dispatch", qqtShopBuyDispatchSignature, buildShopBuyDispatchStub},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub := test.build(0x20000000, 0x20000600, 0x10000000, test.original)
			if !bytes.Contains(stub, test.original) {
				t.Fatalf("stub does not replay original bytes %X", test.original)
			}
			if got := stub[len(stub)-5]; got != 0xE9 {
				t.Fatalf("final transfer opcode = 0x%02X, want near jump", got)
			}
		})
	}
}

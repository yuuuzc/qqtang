package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestCompatibilityDLLIsPE32WithSafeValueCreateObj(t *testing.T) {
	image := buildCompatibilityDLL()
	path := filepath.Join(t.TempDir(), "TerSafe.dll")
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := pe.Open(path)
	if err != nil {
		t.Fatalf("open generated PE: %v", err)
	}
	defer file.Close()
	if file.FileHeader.Machine != pe.IMAGE_FILE_MACHINE_I386 {
		t.Fatalf("machine = 0x%X", file.FileHeader.Machine)
	}
	if file.FileHeader.Characteristics&pe.IMAGE_FILE_DLL == 0 {
		t.Fatal("generated PE is not a DLL")
	}
	optional, ok := file.OptionalHeader.(*pe.OptionalHeader32)
	if !ok {
		t.Fatalf("optional header = %T", file.OptionalHeader)
	}
	if optional.AddressOfEntryPoint != textRVA {
		t.Fatalf("entry RVA = 0x%X", optional.AddressOfEntryPoint)
	}
	if optional.DataDirectory[0].VirtualAddress != textRVA+exportOffset {
		t.Fatalf("export RVA = 0x%X", optional.DataDirectory[0].VirtualAddress)
	}
	if optional.DataDirectory[1].VirtualAddress != textRVA+importOffset || optional.DataDirectory[1].Size != 40 {
		t.Fatalf("import directory = RVA 0x%X size %d", optional.DataDirectory[1].VirtualAddress, optional.DataDirectory[1].Size)
	}
	if optional.DataDirectory[5].VirtualAddress != relocRVA || optional.DataDirectory[5].Size != 36 {
		t.Fatalf("relocation directory = RVA 0x%X size %d",
			optional.DataDirectory[5].VirtualAddress, optional.DataDirectory[5].Size)
	}
	text := image[0x200:0x600]
	if !bytes.Equal(text[createObjOffset:createObjOffset+4], []byte{0x8B, 0x44, 0x24, 0x04}) {
		t.Fatalf("CreateObj prologue = %X", text[createObjOffset:createObjOffset+4])
	}
	if !bytes.Equal(text[createObjOffset+4:createObjOffset+16], []byte{
		0x83, 0xF8, 0x09, 0x74, 0x11, 0x83, 0xF8, 0x03, 0x75, 0x18, 0xE8, 0x00,
	}) {
		t.Fatalf("CreateObj type dispatch = %X", text[createObjOffset+4:createObjOffset+16])
	}
	if !bytes.Equal(text[bindOffset:bindOffset+3], []byte{0xC2, 0x04, 0x00}) {
		t.Fatalf("safe-value bind is not a one-argument no-op = %X", text[bindOffset:bindOffset+3])
	}
	if text[copyOffset] != 0xC3 {
		t.Fatalf("safe-value validation callback is not a no-op = 0x%X", text[copyOffset])
	}
	if !bytes.Equal(text[bootstrapInit:bootstrapInit+5], []byte{0x31, 0xC0, 0xC2, 0x0C, 0x00}) {
		t.Fatalf("type-3 initialization ABI is incorrect = %X", text[bootstrapInit:bootstrapInit+5])
	}
	for name, offset := range map[string]int{"encoder": bootstrapRoute, "decoder": bootstrapData} {
		if !bytes.Contains(text[offset:offset+0x60], []byte{0xFC, 0xF3, 0xA4}) {
			t.Fatalf("type-3 %s does not make an identity copy = %X", name, text[offset:offset+0x60])
		}
		if !bytes.Contains(text[offset:offset+0x60], []byte{0xFF, 0x90}) {
			t.Fatalf("type-3 %s does not call the imported allocator", name)
		}
	}
	if got := text[bootstrapRoute+len(buildIdentityTransform(0x18, 0x1C, 0x20, 0x24, 0, bootstrapRoute, 0))-1]; got != 0xC3 {
		t.Fatalf("type-3 encoder cleanup opcode = 0x%X", got)
	}
	decoder := buildIdentityTransform(0x14, 0x18, 0x1C, 0x20, 0x1C, bootstrapData, 1)
	if !bytes.Equal(decoder[len(decoder)-3:], []byte{0xC2, 0x1C, 0x00}) {
		t.Fatalf("type-3 decoder cleanup = %X", decoder[len(decoder)-3:])
	}
	if !bytes.Contains(decoder, []byte{0xB8, 0x01, 0x00, 0x00, 0x00, 0xEB, 0x05, 0xB8, 0x00, 0x00, 0x00, 0x00}) {
		t.Fatalf("type-3 decoder success/failure convention = %X", decoder)
	}
	imports, err := file.ImportedSymbols()
	if err != nil {
		t.Fatalf("read generated imports: %v", err)
	}
	if !containsString(imports, "malloc:MSVCRT.dll") {
		t.Fatalf("generated imports = %v", imports)
	}
	if got := binary.LittleEndian.Uint32(text[0x100:0x104]); got != textRVA+createObjOffset {
		t.Fatalf("function RVA = 0x%X", got)
	}
	if got := string(bytes.TrimRight(text[0x110:0x120], "\x00")); got != "CreateObj" {
		t.Fatalf("export name = %q", got)
	}
	const imageBase = uint32(0x10000000)
	if got := binary.LittleEndian.Uint32(text[objectOffset:]); got != imageBase+textRVA+vtableOffset {
		t.Fatalf("object vtable = 0x%X", got)
	}
	if got := binary.LittleEndian.Uint32(text[bootstrapObject:]); got != imageBase+textRVA+bootstrapVTable {
		t.Fatalf("bootstrap object vtable = 0x%X", got)
	}
	for slot, offset := range map[int]int{
		0: bootstrapInit,
		1: bootstrapStop,
		2: bootstrapFinish,
		3: bootstrapClose,
		6: bootstrapEnter,
		7: bootstrapLeave,
		9: bootstrapRoute,
	} {
		if got, want := binary.LittleEndian.Uint32(text[bootstrapVTable+slot*4:]), imageBase+textRVA+uint32(offset); got != want {
			t.Fatalf("bootstrap vtable[%d] = 0x%X, want 0x%X", slot, got, want)
		}
	}
	if got := binary.LittleEndian.Uint32(text[bootstrapVTable+11*4:]); got != imageBase+textRVA+bootstrapData {
		t.Fatalf("bootstrap vtable[11] = 0x%X", got)
	}
	for index, want := range []uint32{
		imageBase + textRVA + bindOffset,
		imageBase + textRVA + keyOffset,
		imageBase + textRVA + callbackOffset,
	} {
		if got := binary.LittleEndian.Uint32(text[vtableOffset+index*4:]); got != want {
			t.Fatalf("vtable[%d] = 0x%X, want 0x%X", index, got, want)
		}
	}
	relocation := image[0x600:0x624]
	if page, size := binary.LittleEndian.Uint32(relocation), binary.LittleEndian.Uint32(relocation[4:]); page != textRVA || size != 36 {
		t.Fatalf("relocation block = page 0x%X size %d", page, size)
	}
	for index, offset := range []uint16{
		vtableOffset, vtableOffset + 4, vtableOffset + 8,
		bootstrapVTable, bootstrapVTable + 4, bootstrapVTable + 8, bootstrapVTable + 3*4,
		bootstrapVTable + 6*4, bootstrapVTable + 7*4,
		bootstrapVTable + 9*4, bootstrapVTable + 11*4, objectOffset, bootstrapObject,
	} {
		if got, want := binary.LittleEndian.Uint16(relocation[8+index*2:]), uint16(0x3000|offset); got != want {
			t.Fatalf("relocation[%d] = 0x%X, want 0x%X", index, got, want)
		}
	}
	if got := binary.LittleEndian.Uint16(relocation[8+13*2:]); got != 0 {
		t.Fatalf("relocation padding = 0x%X", got)
	}
}

func containsString(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

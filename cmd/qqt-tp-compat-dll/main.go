package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

const (
	fileAlignment    = 0x200
	sectionAlignment = 0x1000
	textRVA          = 0x1000
	relocRVA         = 0x2000
	createObjOffset  = 0x10
	bindOffset       = 0x40
	keyOffset        = 0x50
	callbackOffset   = 0x60
	copyOffset       = 0x80
	bootstrapRoute   = 0x200
	bootstrapInit    = 0x90
	bootstrapStop    = 0x98
	bootstrapFinish  = 0xA0
	bootstrapEnter   = 0xA8
	bootstrapLeave   = 0xB0
	bootstrapClose   = 0xB8
	bootstrapData    = 0x260
	exportOffset     = 0xC0
	vtableOffset     = 0x140
	bootstrapVTable  = 0x150
	objectOffset     = 0x180
	bootstrapObject  = 0x184
	importOffset     = 0x300
	importLookup     = 0x330
	importIAT        = 0x340
	importDLLName    = 0x350
	importMallocName = 0x360
)

func main() {
	output := flag.String("out", "", "output path for the 32-bit TerSafe compatibility DLL")
	flag.Parse()
	if *output == "" {
		fatalf("-out is required")
	}
	if directory := filepath.Dir(*output); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			fatalf("create output directory: %v", err)
		}
	}
	image := buildCompatibilityDLL()
	if err := os.WriteFile(*output, image, 0o644); err != nil {
		fatalf("write %s: %v", *output, err)
	}
	fmt.Printf("wrote TP-free TerSafe compatibility DLL: %s (%d bytes)\n", *output, len(image))
}

// buildCompatibilityDLL emits a dependency-free PE32 DLL. CreateObj is the
// sole public contract used by this QQTang build. QQTModules requests type 3
// for its obsolete protection bootstrap. Live fault evidence proves that its
// bootstrap interface is not a one-method marker. Runtime fault capture plus
// the unpacked QQTModules consumer prove that this client calls slots 0, 1 and
// 2 during initialization, slot 3 while closing the section, and slots 6 and
// 7 for lifecycle notifications. QQTModules' section-close wrapper calls slot
// 3 with ECX only and no explicit arguments, so it is a plain thiscall no-op in
// the protection-free implementation.
// Post-return traces of the QQTModules transport wrapper prove that slot 9 is
// the eight-argument Type-2 encoder and slot 11 is the seven-argument Type-2
// decoder. A loader-synchronized execution trace
// proves slot 0 receives three explicit DWORD arguments and the caller performs
// no stack adjustment before invoking slot 1, so slot 0 is a thiscall callee
// with 12-byte cleanup. Slots 1/2/6/7 have no confirmed explicit arguments.
// Slot 9's QQTModules wrapper restores ESP from its frame, while slot 11 uses
// the normal seven-argument thiscall cleanup. Both methods must allocate and
// return an output buffer: returning success with a null output silently drops
// every movement and bubble packet. The protection-free transform is an
// identity copy allocated by MSVCRT malloc. QQTPPP releases both outputs through
// MFC42 operator delete, which this client build forwards directly to the same
// MSVCRT free, so allocation ownership remains exact. The two underlying
// methods deliberately use different success conventions. Runtime basic-block
// tracing proves QQTPPP treats a non-zero outer decode result as the immediate
// release/return path; QQTModules slot 14 returns one exactly when underlying
// Type-3 slot 11 returns zero. The decoder must therefore return one on success
// (and zero on failure). The already-working encoder retains zero on success.
// Type 3 therefore
// receives its own side-effect-free interface instead of NULL or a type-9
// object. Client.exe requests type 9 for the safe-value
// wrapper used by battle state. Type 9's verified ABI is:
//
//	vtable[0](storage)                 bind optional protected shadow storage
//	vtable[1]()                        return an opaque one-byte key
//	vtable[2](key) -> callback         select the read/write callback
//	callback(storage, write, value, n) validate/update the protected shadow
//
// The wrapper itself always keeps the canonical plain value at offset zero:
// getters copy it to the output before invoking the callback, and setters update
// it before invoking the callback. Therefore the correct protection-free
// equivalent is a no-op bind and a no-op callback. Copying the uninitialized
// shadow back into the canonical value corrupts coordinates and timers. The DLL
// contains no TP initialization, device access, scanning, network activity or
// kernel driver dependency. The object is stateless, so one immutable singleton
// safely serves every safe-value wrapper in the process.
func buildCompatibilityDLL() []byte {
	image := make([]byte, 0x800)
	copy(image[0:], []byte{'M', 'Z'})
	binary.LittleEndian.PutUint32(image[0x3c:], 0x80)
	copy(image[0x80:], []byte{'P', 'E', 0, 0})
	coff := image[0x84:]
	binary.LittleEndian.PutUint16(coff[0:], 0x014c) // IMAGE_FILE_MACHINE_I386
	binary.LittleEndian.PutUint16(coff[2:], 2)
	binary.LittleEndian.PutUint16(coff[16:], 0x00e0)
	binary.LittleEndian.PutUint16(coff[18:], 0x2102) // executable, 32-bit, DLL

	optional := image[0x98:]
	binary.LittleEndian.PutUint16(optional[0:], 0x010b) // PE32
	binary.LittleEndian.PutUint32(optional[4:], 0x400)  // SizeOfCode
	binary.LittleEndian.PutUint32(optional[8:], 0x200)  // SizeOfInitializedData
	binary.LittleEndian.PutUint32(optional[16:], textRVA)
	binary.LittleEndian.PutUint32(optional[20:], textRVA)
	binary.LittleEndian.PutUint32(optional[24:], relocRVA)
	binary.LittleEndian.PutUint32(optional[28:], 0x10000000)
	binary.LittleEndian.PutUint32(optional[32:], sectionAlignment)
	binary.LittleEndian.PutUint32(optional[36:], fileAlignment)
	binary.LittleEndian.PutUint16(optional[40:], 5)
	binary.LittleEndian.PutUint16(optional[48:], 5)
	binary.LittleEndian.PutUint32(optional[56:], 0x3000)
	binary.LittleEndian.PutUint32(optional[60:], 0x200)
	binary.LittleEndian.PutUint16(optional[68:], 2)      // Windows GUI
	binary.LittleEndian.PutUint16(optional[70:], 0x0140) // DYNAMIC_BASE | NX_COMPAT
	binary.LittleEndian.PutUint32(optional[72:], 0x100000)
	binary.LittleEndian.PutUint32(optional[76:], 0x1000)
	binary.LittleEndian.PutUint32(optional[80:], 0x100000)
	binary.LittleEndian.PutUint32(optional[84:], 0x1000)
	binary.LittleEndian.PutUint32(optional[92:], 16)
	// Export, import, base-relocation and IAT data directories.
	binary.LittleEndian.PutUint32(optional[96:], textRVA+exportOffset)
	binary.LittleEndian.PutUint32(optional[100:], 0x70)
	binary.LittleEndian.PutUint32(optional[96+1*8:], textRVA+importOffset)
	binary.LittleEndian.PutUint32(optional[100+1*8:], 40)
	binary.LittleEndian.PutUint32(optional[96+5*8:], relocRVA)
	// A DLL is normally rebased because the historical 0x10000000 image base is
	// already occupied by boost_python.dll in the final client.  Windows 11
	// tolerated the former zero-entry relocation block, while Windows 10
	// rejected the image at LoadLibraryW with ERROR_BAD_EXE_FORMAT. The singleton
	// object and its vtable contain four absolute pointers and therefore have
	// matching IMAGE_REL_BASED_HIGHLOW entries.
	binary.LittleEndian.PutUint32(optional[100+5*8:], 36)
	binary.LittleEndian.PutUint32(optional[96+12*8:], textRVA+importIAT)
	binary.LittleEndian.PutUint32(optional[100+12*8:], 8)

	sectionTable := image[0x178:]
	writeSectionHeader(sectionTable[0:40], ".text", 0x400, textRVA, 0x400, 0x200, 0x60000020)
	writeSectionHeader(sectionTable[40:80], ".reloc", 36, relocRVA, 0x200, 0x600, 0x42000040)

	text := image[0x200:0x600]
	// BOOL WINAPI DllMain(HINSTANCE, DWORD, LPVOID) { return TRUE; }
	copy(text[0x00:], []byte{0xB8, 0x01, 0x00, 0x00, 0x00, 0xC2, 0x0C, 0x00})
	// void* __cdecl CreateObj(unsigned int type) {
	//   if (type == 3) return &bootstrapObject;
	//   if (type == 9) return &safeValueObject;
	//   return NULL;
	// }
	// The call/pop sequence keeps the function position-independent.
	copy(text[createObjOffset:], []byte{
		0x8B, 0x44, 0x24, 0x04, // mov eax,[esp+4]
		0x83, 0xF8, 0x09, // cmp eax,9
		0x74, 0x11, // je type9
		0x83, 0xF8, 0x03, // cmp eax,3
		0x75, 0x18, // jne unsupported
		0xE8, 0x00, 0x00, 0x00, 0x00, // call next
		0x58,                         // pop eax
		0x05, 0x61, 0x01, 0x00, 0x00, // add eax,bootstrapObject-(return address)
		0xC3, // ret
		// type9:
		0xE8, 0x00, 0x00, 0x00, 0x00, // call next
		0x58,                         // pop eax
		0x05, 0x51, 0x01, 0x00, 0x00, // add eax,object-(return address)
		0xC3,       // ret
		0x31, 0xC0, // unsupported: xor eax,eax
		0xC3, // ret
	})
	// void __thiscall Bind(void* storage) { }
	copy(text[bindOffset:], []byte{0xC2, 0x04, 0x00})
	// unsigned char __thiscall Key() { return 0; }
	copy(text[keyOffset:], []byte{0x31, 0xC0, 0xC3})
	// CopyFn __thiscall Callback(unsigned char) { return &CopyValue; }
	copy(text[callbackOffset:], []byte{
		0xE8, 0x00, 0x00, 0x00, 0x00, // call next
		0x58,             // pop eax
		0x83, 0xC0, 0x1B, // add eax,CopyValue-(return address)
		0xC2, 0x04, 0x00, // ret 4
	})
	// void __cdecl ValidateValue(void* storage, int write, void* value, size_t n) { }
	copy(text[copyOffset:], []byte{0xC3})
	// uintptr_t __thiscall BootstrapInit(callback, account, owner) { return 0; }
	copy(text[bootstrapInit:], []byte{0x31, 0xC0, 0xC2, 0x0C, 0x00})
	// uintptr_t __thiscall BootstrapStop/Finish/Close/Enter/Leave() { return 0; }
	// Keep separate addresses so loader-synchronized probes can distinguish the
	// remaining methods while their side-effect-free compatibility is validated.
	for _, offset := range []int{bootstrapStop, bootstrapFinish, bootstrapClose, bootstrapEnter, bootstrapLeave} {
		copy(text[offset:], []byte{0x31, 0xC0, 0xC3})
	}
	// int __thiscall Encode(type, protocol, routeBytes, routes, input, inputLen,
	//                       output, outputLen)
	// The obfuscated QQTModules wrapper restores ESP with LEAVE, matching the
	// previously verified plain RET behavior of this slot.
	copy(text[bootstrapRoute:], buildIdentityTransform(0x18, 0x1C, 0x20, 0x24, 0, bootstrapRoute, 0))
	// int __thiscall Decode(sender, type, protocol, input, inputLen,
	//                       output, outputLen)
	copy(text[bootstrapData:], buildIdentityTransform(0x14, 0x18, 0x1C, 0x20, 0x1C, bootstrapData, 1))

	exports := text[exportOffset:]
	binary.LittleEndian.PutUint32(exports[12:], textRVA+0xF0) // DLL name
	binary.LittleEndian.PutUint32(exports[16:], 1)            // ordinal base
	binary.LittleEndian.PutUint32(exports[20:], 1)            // functions
	binary.LittleEndian.PutUint32(exports[24:], 1)            // names
	binary.LittleEndian.PutUint32(exports[28:], textRVA+0x100)
	binary.LittleEndian.PutUint32(exports[32:], textRVA+0x104)
	binary.LittleEndian.PutUint32(exports[36:], textRVA+0x108)
	copy(text[0xF0:], "TerSafe.dll\x00")
	binary.LittleEndian.PutUint32(text[0x100:], textRVA+createObjOffset)
	binary.LittleEndian.PutUint32(text[0x104:], textRVA+0x110)
	binary.LittleEndian.PutUint16(text[0x108:], 0)
	copy(text[0x110:], "CreateObj\x00")

	// The sole runtime dependency is the allocator paired with QQTPPP's existing
	// MFC42 operator-delete/free path. The transform copies bytes itself, so no
	// CRT memcpy dependency is needed.
	binary.LittleEndian.PutUint32(text[importOffset+0:], textRVA+importLookup)
	binary.LittleEndian.PutUint32(text[importOffset+12:], textRVA+importDLLName)
	binary.LittleEndian.PutUint32(text[importOffset+16:], textRVA+importIAT)
	binary.LittleEndian.PutUint32(text[importLookup:], textRVA+importMallocName)
	binary.LittleEndian.PutUint32(text[importIAT:], textRVA+importMallocName)
	copy(text[importDLLName:], "MSVCRT.dll\x00")
	copy(text[importMallocName+2:], "malloc\x00")

	// Stateless safe-value singleton and vtable. These absolute addresses are
	// the only values the loader must relocate when the preferred image base is
	// occupied.
	const imageBase = uint32(0x10000000)
	binary.LittleEndian.PutUint32(text[vtableOffset+0:], imageBase+textRVA+bindOffset)
	binary.LittleEndian.PutUint32(text[vtableOffset+4:], imageBase+textRVA+keyOffset)
	binary.LittleEndian.PutUint32(text[vtableOffset+8:], imageBase+textRVA+callbackOffset)
	binary.LittleEndian.PutUint32(text[objectOffset:], imageBase+textRVA+vtableOffset)
	for slot, offset := range map[int]int{
		0: bootstrapInit,
		1: bootstrapStop,
		2: bootstrapFinish,
		3: bootstrapClose,
		6: bootstrapEnter,
		7: bootstrapLeave,
	} {
		binary.LittleEndian.PutUint32(text[bootstrapVTable+slot*4:], imageBase+textRVA+uint32(offset))
	}
	binary.LittleEndian.PutUint32(text[bootstrapVTable+9*4:], imageBase+textRVA+bootstrapRoute)
	binary.LittleEndian.PutUint32(text[bootstrapVTable+11*4:], imageBase+textRVA+bootstrapData)
	binary.LittleEndian.PutUint32(text[bootstrapObject:], imageBase+textRVA+bootstrapVTable)

	// One valid relocation block. The preferred image base in the PE header is
	// handled by the loader independently. The final ABSOLUTE entry pads the
	// block to a DWORD boundary.
	binary.LittleEndian.PutUint32(image[0x600:], textRVA)
	binary.LittleEndian.PutUint32(image[0x604:], 36)
	for index, offset := range []uint16{
		vtableOffset, vtableOffset + 4, vtableOffset + 8,
		bootstrapVTable, bootstrapVTable + 4, bootstrapVTable + 8, bootstrapVTable + 3*4,
		bootstrapVTable + 6*4, bootstrapVTable + 7*4,
		bootstrapVTable + 9*4, bootstrapVTable + 11*4, objectOffset, bootstrapObject,
	} {
		binary.LittleEndian.PutUint16(image[0x608+index*2:], 0x3000|offset)
	}
	// IMAGE_BASE_RELOCATION blocks are DWORD-aligned. The trailing ABSOLUTE
	// entry is padding and deliberately requires no loader action.
	binary.LittleEndian.PutUint16(image[0x608+13*2:], 0)
	return image
}

// buildIdentityTransform emits a small x86 thiscall method. EBP-relative
// offsets include the return address and explicit arguments; ECX contains the
// compatibility singleton and is deliberately unused. A one-byte short-jump
// fixup keeps every failure path centralized and auditable.
func buildIdentityTransform(inputOffset, lengthOffset, outputOffset, outputLengthOffset byte, cleanup uint16, methodOffset int, successValue uint32) []byte {
	failureValue := uint32(1)
	if successValue != 0 {
		failureValue = 0
	}
	code := []byte{
		0x55, 0x8B, 0xEC, 0x53, 0x56, 0x57, // frame; preserve EBX/ESI/EDI
		0x8B, 0x75, inputOffset, // mov esi,[ebp+input]
		0x8B, 0x5D, lengthOffset, // mov ebx,[ebp+length]
		0x8B, 0x7D, outputOffset, // mov edi,[ebp+output]
		0x8B, 0x55, outputLengthOffset, // mov edx,[ebp+outputLength]
		0x31, 0xC0, // xor eax,eax
	}
	failureJumps := make([]int, 0, 5)
	appendFailureJump := func() {
		code = append(code, 0x74, 0x00) // jz failure
		failureJumps = append(failureJumps, len(code)-1)
	}
	code = append(code, 0x85, 0xFF) // test edi,edi
	appendFailureJump()
	code = append(code, 0x89, 0x07) // mov [edi],eax
	code = append(code, 0x85, 0xD2) // test edx,edx
	appendFailureJump()
	code = append(code, 0x89, 0x02) // mov [edx],eax
	code = append(code, 0x85, 0xF6) // test esi,esi
	appendFailureJump()
	code = append(code, 0x85, 0xDB) // test ebx,ebx
	appendFailureJump()
	code = append(code, 0x52, 0x53) // preserve outputLength; push malloc size
	callPosition := len(code)
	code = append(code,
		0xE8, 0x00, 0x00, 0x00, 0x00, // call next
		0x58,                   // pop eax (position-independent base)
		0xFF, 0x90, 0, 0, 0, 0, // call dword ptr [eax+mallocIAT]
		0x83, 0xC4, 0x04, // discard malloc argument
		0x5A,       // restore outputLength pointer
		0x85, 0xC0, // test eax,eax
	)
	appendFailureJump()
	displacement := int32(importIAT - (methodOffset + callPosition + 5))
	binary.LittleEndian.PutUint32(code[callPosition+8:], uint32(displacement))
	code = append(code,
		0x89, 0x07, // mov [output],eax
		0x89, 0x1A, // mov [outputLength],ebx
		0x89, 0xC7, // mov edi,eax
		0x89, 0xD9, // mov ecx,ebx
		0xFC, 0xF3, 0xA4, // cld; rep movsb
		0xB8, 0x00, 0x00, 0x00, 0x00, // mov eax,successValue
		0xEB, 0x05, // jmp epilogue
	)
	binary.LittleEndian.PutUint32(code[len(code)-6:len(code)-2], successValue)
	failure := len(code)
	code = append(code, 0xB8, 0x00, 0x00, 0x00, 0x00) // mov eax,failureValue
	binary.LittleEndian.PutUint32(code[len(code)-4:], failureValue)
	for _, fixup := range failureJumps {
		delta := failure - (fixup + 1)
		if delta < -128 || delta > 127 {
			panic("identity transform failure jump exceeds int8")
		}
		code[fixup] = byte(int8(delta))
	}
	code = append(code, 0x5F, 0x5E, 0x5B, 0xC9) // epilogue
	if cleanup == 0 {
		return append(code, 0xC3)
	}
	return append(code, 0xC2, byte(cleanup), byte(cleanup>>8))
}

func writeSectionHeader(header []byte, name string, virtualSize, virtualAddress, rawSize, rawOffset, characteristics uint32) {
	copy(header[0:8], name)
	binary.LittleEndian.PutUint32(header[8:], virtualSize)
	binary.LittleEndian.PutUint32(header[12:], virtualAddress)
	binary.LittleEndian.PutUint32(header[16:], rawSize)
	binary.LittleEndian.PutUint32(header[20:], rawOffset)
	binary.LittleEndian.PutUint32(header[36:], characteristics)
}

func fatalf(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "qqt-tp-compat-dll: "+format+"\n", arguments...)
	os.Exit(1)
}

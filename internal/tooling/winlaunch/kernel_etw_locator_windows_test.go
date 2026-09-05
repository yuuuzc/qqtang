package winlaunch

import (
	"encoding/binary"
	"strings"
	"testing"
)

func TestLocateKernelETWWrapperUsesCompleteUniqueShape(t *testing.T) {
	image := testETWImage(0x1019F, 0x10118)
	candidate, err := locateKernelETWWrapper(image)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.rva != 0x1019F || candidate.helperRVA != 0x10118 {
		t.Fatalf("candidate = RVA 0x%X helper 0x%X", candidate.rva, candidate.helperRVA)
	}
	if candidate.address != 0x7001019F || candidate.helper != 0x70010118 {
		t.Fatalf("addresses = 0x%X helper 0x%X", candidate.address, candidate.helper)
	}
}

func TestLocateKernelETWWrapperAcceptsBuildSpecificStackCleanupTail(t *testing.T) {
	image := testETWImage(0x10106, 0x10080)
	offset := int(uint32(0x10106) - image.sections[0].virtualAddress)
	copy(image.sections[0].data[offset+9:offset+13], []byte{0x83, 0xC4, 0x04, 0xC3})
	candidate, err := locateKernelETWWrapper(image)
	if err != nil {
		t.Fatal(err)
	}
	if got := candidate.tail; len(got) != 4 || got[0] != 0x83 || got[3] != 0xC3 {
		t.Fatalf("tail = %X", got)
	}
	if candidate.continuationRVA != 0x1010F {
		t.Fatalf("continuation RVA = 0x%X", candidate.continuationRVA)
	}
}

func TestLocateKernelETWWrapperDoesNotDependOnKnownRVA(t *testing.T) {
	image := testETWImage(0x10106, 0x10080)
	candidate, err := locateKernelETWWrapper(image)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.rva != 0x10106 || candidate.helperRVA != 0x10080 {
		t.Fatalf("candidate = RVA 0x%X helper 0x%X", candidate.rva, candidate.helperRVA)
	}
}

func TestLocateKernelETWWrapperRejectsBranchedTail(t *testing.T) {
	image := testETWImage(0x10106, 0x10080)
	sectionOffset := int(uint32(0x10106) - image.sections[0].virtualAddress)
	image.sections[0].data[sectionOffset+9] = 0x75
	if _, err := locateKernelETWWrapper(image); err == nil || !strings.Contains(err.Error(), "no bounded") {
		t.Fatalf("error = %v", err)
	}
}

func TestLocateKernelETWWrapperRejectsMissingFunctionBoundary(t *testing.T) {
	image := testETWImage(0x10106, 0x10080)
	sectionOffset := int(uint32(0x10106) - image.sections[0].virtualAddress)
	image.sections[0].data[sectionOffset-1] = 0x41
	if _, err := locateKernelETWWrapper(image); err == nil || !strings.Contains(err.Error(), "no bounded") {
		t.Fatalf("error = %v", err)
	}
}

func TestLocateKernelETWWrapperRejectsAmbiguousMatches(t *testing.T) {
	image := testETWImage(0x10106, 0x10080)
	writeTestETWWrapper(image.sections[0].data, image.sections[0].virtualAddress, 0x10200, 0x10080)
	if _, err := locateKernelETWWrapper(image); err == nil || !strings.Contains(err.Error(), "not unique") {
		t.Fatalf("error = %v", err)
	}
}

func TestLocateKernelETWWrapperRejectsNonExecutableHelper(t *testing.T) {
	image := testETWImage(0x10106, 0x18000)
	if _, err := locateKernelETWWrapper(image); err == nil || !strings.Contains(err.Error(), "no bounded") {
		t.Fatalf("error = %v", err)
	}
}

func testETWImage(wrapperRVA, helperRVA uint32) remoteExecutableImage {
	const sectionRVA = uint32(0x10000)
	data := make([]byte, 0x400)
	for index := range data {
		data[index] = 0xCC
	}
	writeTestETWWrapper(data, sectionRVA, wrapperRVA, helperRVA)
	return remoteExecutableImage{
		base: 0x70000000,
		size: 0x20000,
		path: `C:\Windows\SysWOW64\kernel32.dll`,
		sections: []remoteExecutableSection{{
			name:            ".text",
			virtualAddress:  sectionRVA,
			virtualSize:     uint32(len(data)),
			characteristics: imageSectionMemExecute,
			data:            data,
		}},
	}
}

func writeTestETWWrapper(data []byte, sectionRVA, wrapperRVA, helperRVA uint32) {
	offset := int(wrapperRVA - sectionRVA)
	copy(data[offset:offset+11], []byte{0x8B, 0xFF, 0x51, 0x51, 0xE8, 0, 0, 0, 0, 0x59, 0xC3})
	displacement := int32(helperRVA - (wrapperRVA + 9))
	binary.LittleEndian.PutUint32(data[offset+5:offset+9], uint32(displacement))
}

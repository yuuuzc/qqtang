package winlaunch

import (
	"encoding/binary"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

type TPEarlySystemHeaderReport struct {
	Module              string   `json:"module"`
	HeaderAddress       string   `json:"header_address"`
	OriginalValue       string   `json:"original_value"`
	ObservedValues      []string `json:"observed_values,omitempty"`
	ValueAfterETWRepair string   `json:"value_after_etw_repair,omitempty"`
	OriginalProtect     uint32   `json:"original_protect"`
	Restored            bool     `json:"restored"`
}

type tpEarlySystemHeaderCompatibility struct {
	process         syscall.Handle
	headerAddress   uintptr
	headerPage      uintptr
	original        []byte
	originalProtect uint32
	report          *TPEarlySystemHeaderReport
	restored        bool
}

// prepareTPEarlySystemHeaderCompatibility supports one obsolete ClientBase
// bootstrap convention without allowing it to corrupt a system image for the
// lifetime of the game. Old TP temporarily uses KERNELBASE's PE
// SizeOfUninitializedData field as private scratch storage. Windows 10 19045 still permits the write;
// newer builds map the same header read-only and raise C0000005. Resolve the
// PE field from the live image (never from a Windows-private RVA), keep its
// page writable only during TP bootstrap, and restore it after the gated TP
// worker completes its verified ETW registration boundary.
func prepareTPEarlySystemHeaderCompatibility(
	process syscall.Handle,
	pid uint32,
	timeout time.Duration,
) (*tpEarlySystemHeaderCompatibility, error) {
	_, headerAddress, err := resolveTPEarlySystemHeaderField(process, pid, timeout)
	if err != nil {
		return nil, err
	}
	original, ok := readRemote(process, headerAddress, 4)
	if !ok {
		return nil, fmt.Errorf("read KERNELBASE SizeOfUninitializedData at 0x%X", headerAddress)
	}
	originalValue := binary.LittleEndian.Uint32(original)
	page := headerAddress &^ 0xFFF
	var oldProtect uint32
	changed, _, protectErr := procVirtualProtectEx.Call(
		uintptr(process), page, 0x1000, pageReadWrite, uintptr(unsafe.Pointer(&oldProtect)),
	)
	if changed == 0 {
		return nil, fmt.Errorf("make KERNELBASE header writable at 0x%X: %w", page, protectErr)
	}
	report := &TPEarlySystemHeaderReport{
		Module: "KERNELBASE.dll", HeaderAddress: fmt.Sprintf("0x%08X", headerAddress),
		OriginalValue: fmt.Sprintf("0x%08X", originalValue), OriginalProtect: oldProtect,
	}
	return &tpEarlySystemHeaderCompatibility{
		process: process, headerAddress: headerAddress, headerPage: page,
		original: append([]byte(nil), original...), originalProtect: oldProtect, report: report,
	}, nil
}

func resolveTPEarlySystemHeaderField(
	process syscall.Handle,
	pid uint32,
	timeout time.Duration,
) (uintptr, uintptr, error) {
	base, err := waitForModule(process, pid, "KERNELBASE.dll", timeout)
	if err != nil {
		return 0, 0, err
	}
	dos, ok := readRemote(process, base, 0x40)
	if !ok || len(dos) < 0x40 || dos[0] != 'M' || dos[1] != 'Z' {
		return 0, 0, fmt.Errorf("KERNELBASE has no readable DOS header")
	}
	peOffset := uintptr(binary.LittleEndian.Uint32(dos[0x3C:0x40]))
	if peOffset < 0x40 || peOffset > 0x1000 {
		return 0, 0, fmt.Errorf("KERNELBASE PE offset is invalid: 0x%X", peOffset)
	}
	ntHeader, ok := readRemote(process, base+peOffset, 0x40)
	if !ok || len(ntHeader) < 0x40 || string(ntHeader[:4]) != "PE\x00\x00" {
		return 0, 0, fmt.Errorf("KERNELBASE has no readable PE header")
	}
	optionalHeader := 4 + 20
	magic := binary.LittleEndian.Uint16(ntHeader[optionalHeader : optionalHeader+2])
	if magic != 0x10B && magic != 0x20B {
		return 0, 0, fmt.Errorf("KERNELBASE optional-header magic is 0x%X", magic)
	}
	// IMAGE_OPTIONAL_HEADER.SizeOfUninitializedData is DWORD offset 0x0C.
	return base, base + peOffset + uintptr(optionalHeader) + 0x0C, nil
}

func (compatibility *tpEarlySystemHeaderCompatibility) restoreAfterETWRepair() error {
	value, ok := readRemote(compatibility.process, compatibility.headerAddress, 4)
	if !ok {
		return fmt.Errorf("read TP KERNELBASE scratch field at 0x%X", compatibility.headerAddress)
	}
	current := binary.LittleEndian.Uint32(value)
	compatibility.report.ObservedValues = append(
		compatibility.report.ObservedValues, fmt.Sprintf("0x%08X", current),
	)
	compatibility.report.ValueAfterETWRepair = fmt.Sprintf("0x%08X", current)
	return compatibility.restore()
}

func (compatibility *tpEarlySystemHeaderCompatibility) restore() error {
	if compatibility == nil || compatibility.restored {
		return nil
	}
	if err := writeRemote(compatibility.process, compatibility.headerAddress, compatibility.original); err != nil {
		return fmt.Errorf("restore KERNELBASE SizeOfUninitializedData: %w", err)
	}
	var ignored uint32
	restored, _, protectErr := procVirtualProtectEx.Call(
		uintptr(compatibility.process), compatibility.headerPage, 0x1000,
		uintptr(compatibility.originalProtect), uintptr(unsafe.Pointer(&ignored)),
	)
	if restored == 0 {
		return fmt.Errorf("restore KERNELBASE header protection: %w", protectErr)
	}
	compatibility.restored = true
	compatibility.report.Restored = true
	return nil
}

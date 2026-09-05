package wininspect

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

const (
	processQueryInformation = 0x0400
	processVMRead           = 0x0010
	processVMWrite          = 0x0020
	processVMOperation      = 0x0008
	processDuplicateHandle  = 0x0040
	threadSuspendResume     = 0x0002
	threadGetContext        = 0x0008
	threadQueryInformation  = 0x0040
	wow64ContextControl     = 0x00010001
	wow64ContextInteger     = 0x00010002
	listModulesAll          = 0x03
)

var (
	kernel32                   = syscall.NewLazyDLL("kernel32.dll")
	user32                     = syscall.NewLazyDLL("user32.dll")
	procOpenProcess            = kernel32.NewProc("OpenProcess")
	procReadProcessMemory      = kernel32.NewProc("ReadProcessMemory")
	procWriteProcessMemory     = kernel32.NewProc("WriteProcessMemory")
	procVirtualQueryEx         = kernel32.NewProc("VirtualQueryEx")
	procOpenThread             = kernel32.NewProc("OpenThread")
	procSuspendThread          = kernel32.NewProc("SuspendThread")
	procResumeThread           = kernel32.NewProc("ResumeThread")
	procWow64GetThreadContext  = kernel32.NewProc("Wow64GetThreadContext")
	procEnumProcessModulesEx   = kernel32.NewProc("K32EnumProcessModulesEx")
	procGetModuleFileNameEx    = kernel32.NewProc("K32GetModuleFileNameExW")
	procGetModuleInformation   = kernel32.NewProc("K32GetModuleInformation")
	procDuplicateHandle        = kernel32.NewProc("DuplicateHandle")
	procGetCurrentProcess      = kernel32.NewProc("GetCurrentProcess")
	procEnumWindows            = user32.NewProc("EnumWindows")
	procEnumChildWindows       = user32.NewProc("EnumChildWindows")
	procIsWindowVisible        = user32.NewProc("IsWindowVisible")
	procIsWindowEnabled        = user32.NewProc("IsWindowEnabled")
	procGetWindowThreadProcess = user32.NewProc("GetWindowThreadProcessId")
	procGetWindowText          = user32.NewProc("GetWindowTextW")
	procGetClassName           = user32.NewProc("GetClassNameW")
	procGetWindowRect          = user32.NewProc("GetWindowRect")
	procPostMessage            = user32.NewProc("PostMessageW")
)

type Result struct {
	PID              uint32        `json:"pid"`
	ThreadID         uint32        `json:"thread_id"`
	Window           string        `json:"window"`
	WindowTitle      string        `json:"window_title"`
	WindowClass      string        `json:"window_class,omitempty"`
	WindowRect       *WindowRect   `json:"window_rect,omitempty"`
	ChildWindows     []WindowInfo  `json:"child_windows,omitempty"`
	Registers        Registers     `json:"registers"`
	Frames           []StackEntry  `json:"frames"`
	StackScan        []StackEntry  `json:"stack_scan"`
	StackWords       []StackWord   `json:"stack_words,omitempty"`
	Modules          []Module      `json:"modules"`
	MemoryMatches    []MemoryMatch `json:"memory_matches,omitempty"`
	TPErrorCandidate []MemoryMatch `json:"tp_error_candidate_matches,omitempty"`
	PointerRefs      []PointerRef  `json:"pointer_references,omitempty"`
	Handle           *HandleInfo   `json:"handle,omitempty"`
}

type WindowRect struct {
	Left   int32 `json:"left"`
	Top    int32 `json:"top"`
	Right  int32 `json:"right"`
	Bottom int32 `json:"bottom"`
}

type WindowInfo struct {
	Window   string     `json:"window"`
	Title    string     `json:"title,omitempty"`
	Class    string     `json:"class,omitempty"`
	Rect     WindowRect `json:"rect"`
	Visible  bool       `json:"visible"`
	Enabled  bool       `json:"enabled"`
	ThreadID uint32     `json:"thread_id"`
}

type Registers struct {
	EAX    uint32 `json:"eax"`
	EBX    uint32 `json:"ebx"`
	ECX    uint32 `json:"ecx"`
	EDX    uint32 `json:"edx"`
	ESI    uint32 `json:"esi"`
	EDI    uint32 `json:"edi"`
	EBP    uint32 `json:"ebp"`
	ESP    uint32 `json:"esp"`
	EIP    uint32 `json:"eip"`
	EFlags uint32 `json:"eflags"`
}

type StackEntry struct {
	Index          int    `json:"index"`
	Offset         uint32 `json:"offset,omitempty"`
	Address        uint32 `json:"address"`
	Module         string `json:"module,omitempty"`
	RVA            uint32 `json:"rva,omitempty"`
	Source         string `json:"source"`
	Context        string `json:"context_bytes,omitempty"`
	RegionBase     uint32 `json:"region_base,omitempty"`
	AllocationBase uint32 `json:"allocation_base,omitempty"`
	RegionSize     uint64 `json:"region_size,omitempty"`
	Protect        string `json:"protect,omitempty"`
	MemoryType     string `json:"memory_type,omitempty"`
}

type StackWord struct {
	Offset  uint32 `json:"offset"`
	Address uint32 `json:"address"`
	Value   uint32 `json:"value"`
	Module  string `json:"module,omitempty"`
	RVA     uint32 `json:"rva,omitempty"`
}

type Module struct {
	Base uint32 `json:"base"`
	Size uint32 `json:"size"`
	Path string `json:"path"`
}

type MemoryMatch struct {
	Pattern     string `json:"pattern"`
	Encoding    string `json:"encoding"`
	Address     uint32 `json:"address"`
	RegionBase  uint32 `json:"region_base"`
	RegionSize  uint64 `json:"region_size"`
	Protect     string `json:"protect"`
	ContextBase uint32 `json:"context_base"`
	Context     string `json:"context_bytes"`
}

type PointerRef struct {
	TargetPattern  string `json:"target_pattern"`
	TargetEncoding string `json:"target_encoding"`
	TargetAddress  uint32 `json:"target_address"`
	Address        uint32 `json:"address"`
	RegionBase     uint32 `json:"region_base"`
	RegionSize     uint64 `json:"region_size"`
	Protect        string `json:"protect"`
}

type HandleInfo struct {
	Value      uint32      `json:"value"`
	TypeName   string      `json:"type_name"`
	ObjectName string      `json:"object_name,omitempty"`
	ThreadID   uint32      `json:"thread_id,omitempty"`
	Mutant     *MutantInfo `json:"mutant,omitempty"`
}

type MutantInfo struct {
	CurrentCount  int32 `json:"current_count"`
	OwnedByCaller bool  `json:"owned_by_caller"`
	Abandoned     bool  `json:"abandoned"`
}

type moduleInformation struct {
	Base       uintptr
	Size       uint32
	_          uint32
	EntryPoint uintptr
}

type memoryBasicInformation struct {
	BaseAddress       uintptr
	AllocationBase    uintptr
	AllocationProtect uint32
	_                 uint32
	RegionSize        uintptr
	State             uint32
	Protect           uint32
	Type              uint32
	_                 uint32
}

func SnapshotThread(pid, requestedThreadID uint32) (Result, error) {
	processValue, _, openErr := procOpenProcess.Call(processQueryInformation|processVMRead, 0, uintptr(pid))
	if processValue == 0 {
		return Result{}, fmt.Errorf("OpenProcess: %w", openErr)
	}
	process := syscall.Handle(processValue)
	defer syscall.CloseHandle(process)

	modules, err := enumerateModules(process)
	if err != nil {
		return Result{}, err
	}
	hwnd, threadID := findWindow(pid)
	if requestedThreadID != 0 {
		threadID = requestedThreadID
	}
	if threadID == 0 {
		return Result{}, fmt.Errorf("no visible top-level window for pid %d", pid)
	}
	memoryMatches := scanWarningStrings(process)
	pointerRefs := scanPointers(process, memoryMatches)
	threadValue, _, threadErr := procOpenThread.Call(threadSuspendResume|threadGetContext|threadQueryInformation, 0, uintptr(threadID))
	if threadValue == 0 {
		return Result{}, fmt.Errorf("OpenThread: %w", threadErr)
	}
	thread := syscall.Handle(threadValue)
	defer syscall.CloseHandle(thread)
	previous, _, suspendErr := procSuspendThread.Call(uintptr(thread))
	if previous == ^uintptr(0) {
		return Result{}, fmt.Errorf("SuspendThread: %w", suspendErr)
	}
	defer procResumeThread.Call(uintptr(thread))

	context := make([]byte, 716)
	binary.LittleEndian.PutUint32(context[0:4], wow64ContextControl|wow64ContextInteger)
	success, _, contextErr := procWow64GetThreadContext.Call(uintptr(thread), uintptr(unsafe.Pointer(&context[0])))
	if success == 0 {
		return Result{}, fmt.Errorf("Wow64GetThreadContext: %w", contextErr)
	}
	registers := Registers{
		EDI: u32(context, 156), ESI: u32(context, 160), EBX: u32(context, 164),
		EDX: u32(context, 168), ECX: u32(context, 172), EAX: u32(context, 176),
		EBP: u32(context, 180), EIP: u32(context, 184), EFlags: u32(context, 192), ESP: u32(context, 196),
	}

	frames := []StackEntry{describeEntry(0, 0, registers.EIP, "eip", modules)}
	for ebp, index := registers.EBP, 1; ebp != 0 && index < 64; index++ {
		data, ok := readMemory(process, uintptr(ebp), 8)
		if !ok {
			break
		}
		next := binary.LittleEndian.Uint32(data[0:4])
		address := binary.LittleEndian.Uint32(data[4:8])
		frames = append(frames, describeEntry(index, 0, address, "ebp-chain", modules))
		if next <= ebp || next-ebp > 8*1024*1024 {
			break
		}
		ebp = next
	}
	annotateEntries(process, frames)

	var scan []StackEntry
	var stackWords []StackWord
	if data, ok := readMemory(process, uintptr(registers.ESP), 4096); ok {
		seen := make(map[uint32]bool)
		for offset := 0; offset+4 <= len(data); offset += 4 {
			address := binary.LittleEndian.Uint32(data[offset : offset+4])
			name, rva := moduleAndRVA(address, modules)
			stackWords = append(stackWords, StackWord{
				Offset: uint32(offset), Address: registers.ESP + uint32(offset), Value: address,
				Module: name, RVA: rva,
			})
			if name == "" || seen[address] {
				continue
			}
			seen[address] = true
			scan = append(scan, describeEntry(len(scan), uint32(offset), address, "esp-scan", modules))
			if len(scan) >= 512 {
				break
			}
		}
	}
	annotateEntries(process, scan)
	title := ""
	class := ""
	var rect *WindowRect
	var children []WindowInfo
	if hwnd != 0 {
		title = windowText(hwnd)
		class = windowClass(hwnd)
		if value, ok := readWindowRect(hwnd); ok {
			rect = &value
		}
		children = enumerateChildWindows(hwnd)
	}
	return Result{
		PID: pid, ThreadID: threadID, Window: fmt.Sprintf("0x%X", hwnd), WindowTitle: title,
		WindowClass: class, WindowRect: rect, ChildWindows: children,
		Registers: registers, Frames: frames, StackScan: scan, StackWords: stackWords, Modules: modules,
		MemoryMatches: memoryMatches, PointerRefs: pointerRefs,
	}, nil
}

func scanWarningStrings(process syscall.Handle) []MemoryMatch {
	type searchPattern struct {
		label    string
		encoding string
		data     []byte
	}
	patterns := []searchPattern{
		{label: "警告码", encoding: "utf-8", data: []byte("警告码")},
		{label: "警告码", encoding: "utf-16le", data: utf16LE("警告码")},
		{label: "警告码", encoding: "cp936", data: []byte{0xBE, 0xAF, 0xB8, 0xE6, 0xC2, 0xEB}},
		{label: "(0, 5, 540)", encoding: "ascii", data: []byte("(0, 5, 540)")},
		{label: "(0, 5, 540)", encoding: "utf-16le", data: utf16LE("(0, 5, 540)")},
		{label: "tp-error-3-1008-31000", encoding: "u32le", data: dwordsLE(3, 1008, 31000)},
		{label: "tp-error-31000-1008-3", encoding: "u32le", data: dwordsLE(31000, 1008, 3)},
		{label: "wsprintfA-prologue", encoding: "x86", data: []byte{0x8B, 0xFF, 0x55, 0x8B, 0xEC, 0x8D, 0x45, 0x10, 0x50, 0xFF, 0x75, 0x0C, 0xFF, 0x75, 0x08}},
	}

	const (
		memCommit    = 0x1000
		pageNoAccess = 0x01
		pageGuard    = 0x100
		chunkSize    = 1024 * 1024
		overlapSize  = 64
		maxMatches   = 256
	)
	var matches []MemoryMatch
	seen := make(map[string]bool)
	for address := uintptr(0x10000); address < uintptr(0x80000000); {
		var info memoryBasicInformation
		queried, _, _ := procVirtualQueryEx.Call(
			uintptr(process), address, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
		)
		if queried == 0 || info.RegionSize == 0 {
			break
		}
		next := info.BaseAddress + info.RegionSize
		if next <= address {
			break
		}
		if info.State == memCommit && info.Protect&pageNoAccess == 0 && info.Protect&pageGuard == 0 && info.BaseAddress < uintptr(0x80000000) {
			regionEnd := next
			if regionEnd > uintptr(0x80000000) {
				regionEnd = uintptr(0x80000000)
			}
			var overlap []byte
			for chunkAddress := info.BaseAddress; chunkAddress < regionEnd; {
				size := chunkSize
				if remaining := regionEnd - chunkAddress; remaining < uintptr(size) {
					size = int(remaining)
				}
				data, ok := readMemory(process, chunkAddress, size)
				if !ok {
					break
				}
				combined := make([]byte, 0, len(overlap)+len(data))
				combined = append(combined, overlap...)
				combined = append(combined, data...)
				combinedBase := chunkAddress - uintptr(len(overlap))
				for _, pattern := range patterns {
					for searchAt := 0; searchAt+len(pattern.data) <= len(combined); {
						relative := bytes.Index(combined[searchAt:], pattern.data)
						if relative < 0 {
							break
						}
						relative += searchAt
						matchAddress := combinedBase + uintptr(relative)
						key := fmt.Sprintf("%X/%s/%s", matchAddress, pattern.label, pattern.encoding)
						if !seen[key] {
							seen[key] = true
							contextBase := matchAddress
							if matchAddress >= info.BaseAddress+64 {
								contextBase = matchAddress - 64
							}
							contextSize := 256
							if remaining := regionEnd - contextBase; remaining < uintptr(contextSize) {
								contextSize = int(remaining)
							}
							context, _ := readMemory(process, contextBase, contextSize)
							matches = append(matches, MemoryMatch{
								Pattern: pattern.label, Encoding: pattern.encoding, Address: uint32(matchAddress),
								RegionBase: uint32(info.BaseAddress), RegionSize: uint64(info.RegionSize),
								Protect: fmt.Sprintf("0x%X", info.Protect), ContextBase: uint32(contextBase),
								Context: hex.EncodeToString(context),
							})
							if len(matches) >= maxMatches {
								return matches
							}
						}
						searchAt = relative + 1
					}
				}
				keep := overlapSize
				if len(combined) < keep {
					keep = len(combined)
				}
				overlap = append(overlap[:0], combined[len(combined)-keep:]...)
				chunkAddress += uintptr(len(data))
			}
		}
		address = next
	}
	return matches
}

func dwordsLE(values ...uint32) []byte {
	data := make([]byte, len(values)*4)
	for index, value := range values {
		binary.LittleEndian.PutUint32(data[index*4:], value)
	}
	return data
}

func scanPointers(process syscall.Handle, targets []MemoryMatch) []PointerRef {
	if len(targets) == 0 {
		return nil
	}
	const (
		memCommit    = 0x1000
		pageNoAccess = 0x01
		pageGuard    = 0x100
		chunkSize    = 1024 * 1024
		maxRefs      = 1024
	)
	type pointerPattern struct {
		target MemoryMatch
		data   [4]byte
	}
	patterns := make([]pointerPattern, 0, len(targets))
	for _, target := range targets {
		var pattern pointerPattern
		pattern.target = target
		binary.LittleEndian.PutUint32(pattern.data[:], target.Address)
		patterns = append(patterns, pattern)
	}

	var refs []PointerRef
	seen := make(map[string]bool)
	for address := uintptr(0x10000); address < uintptr(0x80000000); {
		var info memoryBasicInformation
		queried, _, _ := procVirtualQueryEx.Call(
			uintptr(process), address, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
		)
		if queried == 0 || info.RegionSize == 0 {
			break
		}
		next := info.BaseAddress + info.RegionSize
		if next <= address {
			break
		}
		if info.State == memCommit && info.Protect&pageNoAccess == 0 && info.Protect&pageGuard == 0 && info.BaseAddress < uintptr(0x80000000) {
			regionEnd := next
			if regionEnd > uintptr(0x80000000) {
				regionEnd = uintptr(0x80000000)
			}
			var overlap []byte
			for chunkAddress := info.BaseAddress; chunkAddress < regionEnd; {
				size := chunkSize
				if remaining := regionEnd - chunkAddress; remaining < uintptr(size) {
					size = int(remaining)
				}
				data, ok := readMemory(process, chunkAddress, size)
				if !ok {
					break
				}
				combined := make([]byte, 0, len(overlap)+len(data))
				combined = append(combined, overlap...)
				combined = append(combined, data...)
				combinedBase := chunkAddress - uintptr(len(overlap))
				for _, pattern := range patterns {
					for searchAt := 0; searchAt+len(pattern.data) <= len(combined); {
						relative := bytes.Index(combined[searchAt:], pattern.data[:])
						if relative < 0 {
							break
						}
						relative += searchAt
						refAddress := combinedBase + uintptr(relative)
						key := fmt.Sprintf("%X/%X", refAddress, pattern.target.Address)
						if !seen[key] {
							seen[key] = true
							refs = append(refs, PointerRef{
								TargetPattern: pattern.target.Pattern, TargetEncoding: pattern.target.Encoding,
								TargetAddress: pattern.target.Address, Address: uint32(refAddress),
								RegionBase: uint32(info.BaseAddress), RegionSize: uint64(info.RegionSize),
								Protect: fmt.Sprintf("0x%X", info.Protect),
							})
							if len(refs) >= maxRefs {
								return refs
							}
						}
						searchAt = relative + 1
					}
				}
				keep := 3
				if len(combined) < keep {
					keep = len(combined)
				}
				overlap = append(overlap[:0], combined[len(combined)-keep:]...)
				chunkAddress += uintptr(len(data))
			}
		}
		address = next
	}
	return refs
}

// FindPointerReferences scans the readable 32-bit address space for little-endian
// references to address. It is intended for focused runtime-object analysis and
// does not modify the target process.
func FindPointerReferences(pid uint32, address uint32) ([]PointerRef, error) {
	processValue, _, openErr := procOpenProcess.Call(processQueryInformation|processVMRead, 0, uintptr(pid))
	if processValue == 0 {
		return nil, fmt.Errorf("OpenProcess: %w", openErr)
	}
	process := syscall.Handle(processValue)
	defer syscall.CloseHandle(process)
	target := MemoryMatch{
		Pattern:  "explicit-target",
		Encoding: "address",
		Address:  address,
	}
	return scanPointers(process, []MemoryMatch{target}), nil
}

// FindTPErrorCandidate locates the disproved three-DWORD candidate without
// modifying the target. Synchronized evidence identified this byte sequence as
// python23 Unicode-error metadata, not as the TP warning's writable state.
func FindTPErrorCandidate(pid uint32) ([]MemoryMatch, error) {
	processValue, _, openErr := procOpenProcess.Call(processQueryInformation|processVMRead, 0, uintptr(pid))
	if processValue == 0 {
		return nil, fmt.Errorf("OpenProcess: %w", openErr)
	}
	process := syscall.Handle(processValue)
	defer syscall.CloseHandle(process)
	pattern := dwordsLE(3, 1008, 31000)
	const (
		memCommit    = 0x1000
		pageNoAccess = 0x01
		pageGuard    = 0x100
		chunkSize    = 1024 * 1024
	)
	var matches []MemoryMatch
	for address := uintptr(0x10000); address < uintptr(0x80000000); {
		var info memoryBasicInformation
		queried, _, _ := procVirtualQueryEx.Call(
			uintptr(process), address, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
		)
		if queried == 0 || info.RegionSize == 0 {
			break
		}
		next := info.BaseAddress + info.RegionSize
		if next <= address {
			break
		}
		if info.State == memCommit && info.Protect&pageNoAccess == 0 && info.Protect&pageGuard == 0 {
			var overlap []byte
			for chunkAddress := info.BaseAddress; chunkAddress < next; {
				size := chunkSize
				if remaining := next - chunkAddress; remaining < uintptr(size) {
					size = int(remaining)
				}
				data, ok := readMemory(process, chunkAddress, size)
				if !ok {
					break
				}
				combined := append(append(make([]byte, 0, len(overlap)+len(data)), overlap...), data...)
				combinedBase := chunkAddress - uintptr(len(overlap))
				for searchAt := 0; searchAt+len(pattern) <= len(combined); {
					relative := bytes.Index(combined[searchAt:], pattern)
					if relative < 0 {
						break
					}
					relative += searchAt
					matchAddress := combinedBase + uintptr(relative)
					contextBase := matchAddress
					if matchAddress >= info.BaseAddress+64 {
						contextBase = matchAddress - 64
					}
					contextSize := 320
					if remaining := next - contextBase; remaining < uintptr(contextSize) {
						contextSize = int(remaining)
					}
					context, _ := readMemory(process, contextBase, contextSize)
					matches = append(matches, MemoryMatch{
						Pattern: "disproved candidate tuple (3,1008,31000)", Encoding: "dword-le",
						Address: uint32(matchAddress), RegionBase: uint32(info.BaseAddress),
						RegionSize: uint64(info.RegionSize), Protect: fmt.Sprintf("0x%X", info.Protect),
						ContextBase: uint32(contextBase), Context: fmt.Sprintf("%x", context),
					})
					searchAt = relative + len(pattern)
				}
				keep := len(pattern) - 1
				if len(combined) < keep {
					keep = len(combined)
				}
				overlap = append(overlap[:0], combined[len(combined)-keep:]...)
				chunkAddress += uintptr(len(data))
			}
		}
		address = next
	}
	return matches, nil
}

func ClearTPError1008(pid uint32) ([]uint32, error) {
	return nil, fmt.Errorf("disabled: synchronized evidence identifies this signature as python23 metadata, not a TP error record")
}

func DismissVisibleDialog(pid uint32) (string, error) {
	hwnd, _ := findWindow(pid)
	if hwnd == 0 {
		return "", fmt.Errorf("no visible top-level window for pid %d", pid)
	}
	title := windowText(hwnd)
	const (
		wmCommand = 0x0111
		idOK      = 1
	)
	success, _, postErr := procPostMessage.Call(hwnd, wmCommand, idOK, 0)
	if success == 0 {
		return title, fmt.Errorf("PostMessageW WM_COMMAND/IDOK: %w", postErr)
	}
	return title, nil
}

func utf16LE(value string) []byte {
	words := utf16.Encode([]rune(value))
	data := make([]byte, len(words)*2)
	for index, word := range words {
		binary.LittleEndian.PutUint16(data[index*2:], word)
	}
	return data
}

func DumpModule(pid uint32, moduleName, outputPath string) error {
	processValue, _, openErr := procOpenProcess.Call(processQueryInformation|processVMRead, 0, uintptr(pid))
	if processValue == 0 {
		return fmt.Errorf("OpenProcess: %w", openErr)
	}
	process := syscall.Handle(processValue)
	defer syscall.CloseHandle(process)
	modules, err := enumerateModules(process)
	if err != nil {
		return err
	}
	var target *Module
	for index := range modules {
		if strings.EqualFold(filepath.Base(modules[index].Path), moduleName) {
			target = &modules[index]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("module not loaded: %s", moduleName)
	}
	file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Truncate(int64(target.Size)); err != nil {
		return err
	}
	const chunk = 64 * 1024
	for offset := uint32(0); offset < target.Size; offset += chunk {
		size := uint32(chunk)
		if target.Size-offset < size {
			size = target.Size - offset
		}
		data, ok := readMemory(process, uintptr(target.Base+offset), int(size))
		if !ok || len(data) != int(size) {
			return fmt.Errorf("ReadProcessMemory %s+0x%X size 0x%X", moduleName, offset, size)
		}
		if _, err := file.WriteAt(data, int64(offset)); err != nil {
			return err
		}
	}
	return file.Sync()
}

func DumpRegion(pid uint32, address uintptr, outputPath string) (uintptr, uintptr, error) {
	processValue, _, openErr := procOpenProcess.Call(processQueryInformation|processVMRead, 0, uintptr(pid))
	if processValue == 0 {
		return 0, 0, fmt.Errorf("OpenProcess: %w", openErr)
	}
	process := syscall.Handle(processValue)
	defer syscall.CloseHandle(process)
	var info memoryBasicInformation
	queried, _, queryErr := procVirtualQueryEx.Call(
		uintptr(process), address, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
	)
	if queried == 0 || info.RegionSize == 0 {
		return 0, 0, fmt.Errorf("VirtualQueryEx 0x%X: %w", address, queryErr)
	}
	if info.State != 0x1000 || info.Protect&0x101 != 0 {
		return 0, 0, fmt.Errorf("region 0x%X size 0x%X is not readable: state 0x%X protect 0x%X", info.BaseAddress, info.RegionSize, info.State, info.Protect)
	}
	if info.RegionSize > 128*1024*1024 {
		return 0, 0, fmt.Errorf("region 0x%X is unexpectedly large: 0x%X", info.BaseAddress, info.RegionSize)
	}
	data, ok := readMemory(process, info.BaseAddress, int(info.RegionSize))
	if !ok || len(data) != int(info.RegionSize) {
		return 0, 0, fmt.Errorf("ReadProcessMemory region 0x%X size 0x%X", info.BaseAddress, info.RegionSize)
	}
	if err := os.WriteFile(outputPath, data, 0o600); err != nil {
		return 0, 0, err
	}
	return info.BaseAddress, info.RegionSize, nil
}

func enumerateModules(process syscall.Handle) ([]Module, error) {
	handles := make([]uintptr, 1024)
	var needed uint32
	success, _, callErr := procEnumProcessModulesEx.Call(
		uintptr(process), uintptr(unsafe.Pointer(&handles[0])), uintptr(len(handles))*unsafe.Sizeof(handles[0]),
		uintptr(unsafe.Pointer(&needed)), listModulesAll,
	)
	if success == 0 {
		return nil, fmt.Errorf("K32EnumProcessModulesEx: %w", callErr)
	}
	count := int(needed / uint32(unsafe.Sizeof(handles[0])))
	if count > len(handles) {
		count = len(handles)
	}
	modules := make([]Module, 0, count)
	for _, handle := range handles[:count] {
		var info moduleInformation
		success, _, _ := procGetModuleInformation.Call(uintptr(process), handle, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
		if success == 0 || info.Base > 0xFFFFFFFF {
			continue
		}
		buffer := make([]uint16, 32768)
		length, _, _ := procGetModuleFileNameEx.Call(uintptr(process), handle, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
		path := ""
		if length != 0 {
			path = syscall.UTF16ToString(buffer[:length])
		}
		modules = append(modules, Module{Base: uint32(info.Base), Size: info.Size, Path: path})
	}
	sort.Slice(modules, func(i, j int) bool { return modules[i].Base < modules[j].Base })
	return modules, nil
}

func findWindow(pid uint32) (uintptr, uint32) {
	var found uintptr
	var foundThread uint32
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var windowPID uint32
		thread, _, _ := procGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&windowPID)))
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if windowPID == pid && visible != 0 && windowText(hwnd) != "" {
			found = hwnd
			foundThread = uint32(thread)
			return 0
		}
		return 1
	})
	procEnumWindows.Call(callback, 0)
	return found, foundThread
}

func windowText(hwnd uintptr) string {
	buffer := make([]uint16, 2048)
	length, _, _ := procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:length])
}

func windowClass(hwnd uintptr) string {
	buffer := make([]uint16, 512)
	length, _, _ := procGetClassName.Call(hwnd, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if length == 0 {
		return ""
	}
	return syscall.UTF16ToString(buffer[:length])
}

func readWindowRect(hwnd uintptr) (WindowRect, bool) {
	var rect WindowRect
	success, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	return rect, success != 0
}

func enumerateChildWindows(parent uintptr) []WindowInfo {
	var children []WindowInfo
	callback := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var windowPID uint32
		thread, _, _ := procGetWindowThreadProcess.Call(hwnd, uintptr(unsafe.Pointer(&windowPID)))
		rect, _ := readWindowRect(hwnd)
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		enabled, _, _ := procIsWindowEnabled.Call(hwnd)
		children = append(children, WindowInfo{
			Window: fmt.Sprintf("0x%X", hwnd), Title: windowText(hwnd), Class: windowClass(hwnd), Rect: rect,
			Visible: visible != 0, Enabled: enabled != 0, ThreadID: uint32(thread),
		})
		return 1
	})
	procEnumChildWindows.Call(parent, callback, 0)
	return children
}

func readMemory(process syscall.Handle, address uintptr, size int) ([]byte, bool) {
	if address == 0 || size <= 0 {
		return nil, false
	}
	buffer := make([]byte, size)
	var read uintptr
	success, _, _ := procReadProcessMemory.Call(uintptr(process), address, uintptr(unsafe.Pointer(&buffer[0])), uintptr(size), uintptr(unsafe.Pointer(&read)))
	if success == 0 || read == 0 {
		return nil, false
	}
	return buffer[:read], true
}

func describeEntry(index int, offset, address uint32, source string, modules []Module) StackEntry {
	name, rva := moduleAndRVA(address, modules)
	return StackEntry{Index: index, Offset: offset, Address: address, Module: name, RVA: rva, Source: source}
}

func annotateEntries(process syscall.Handle, entries []StackEntry) {
	for index := range entries {
		address := entries[index].Address
		if address < 32 {
			continue
		}
		var info memoryBasicInformation
		queried, _, _ := procVirtualQueryEx.Call(
			uintptr(process), uintptr(address), uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
		)
		if queried != 0 {
			entries[index].RegionBase = uint32(info.BaseAddress)
			entries[index].AllocationBase = uint32(info.AllocationBase)
			entries[index].RegionSize = uint64(info.RegionSize)
			entries[index].Protect = fmt.Sprintf("0x%X", info.Protect)
			switch info.Type {
			case 0x1000000:
				entries[index].MemoryType = "image"
			case 0x40000:
				entries[index].MemoryType = "mapped"
			case 0x20000:
				entries[index].MemoryType = "private"
			default:
				entries[index].MemoryType = fmt.Sprintf("0x%X", info.Type)
			}
		}
		if data, ok := readMemory(process, uintptr(address-32), 64); ok {
			entries[index].Context = hex.EncodeToString(data)
		}
	}
}

func moduleAndRVA(address uint32, modules []Module) (string, uint32) {
	for _, module := range modules {
		if address >= module.Base && address < module.Base+module.Size {
			name := filepath.Base(module.Path)
			if name == "." || name == "" {
				name = module.Path
			}
			return name, address - module.Base
		}
	}
	return "", 0
}

func u32(data []byte, offset int) uint32 { return binary.LittleEndian.Uint32(data[offset : offset+4]) }

package inspect

import (
	"crypto/sha256"
	"debug/pe"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	Root      string
	OutputDir string
}

type FileTypeReport struct {
	GeneratedAt   time.Time        `json:"generated_at"`
	Root          string           `json:"root"`
	FileCount     int              `json:"file_count"`
	TotalBytes    int64            `json:"total_bytes"`
	Extensions    []ExtensionCount `json:"extensions"`
	LargestFiles  []FileSummary    `json:"largest_files"`
	EmptyFiles    []string         `json:"empty_files"`
	DuplicateSets []DuplicateSet   `json:"duplicate_sets"`
}

type ExtensionCount struct {
	Extension  string `json:"extension"`
	Count      int    `json:"count"`
	TotalBytes int64  `json:"total_bytes"`
}

type FileSummary struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Kind   string `json:"kind"`
}

type DuplicateSet struct {
	SHA256 string   `json:"sha256"`
	Size   int64    `json:"size"`
	Paths  []string `json:"paths"`
}

type PEReport struct {
	GeneratedAt time.Time `json:"generated_at"`
	Files       []PEFile  `json:"files"`
}

type PEFile struct {
	Path                 string      `json:"path"`
	Size                 int64       `json:"size"`
	SHA256               string      `json:"sha256"`
	Machine              string      `json:"machine"`
	MachineValue         string      `json:"machine_value"`
	Architecture         string      `json:"architecture"`
	CompileTimestamp     time.Time   `json:"compile_timestamp"`
	OptionalHeaderMagic  string      `json:"optional_header_magic"`
	EntryPoint           uint32      `json:"entry_point_rva"`
	Subsystem            string      `json:"subsystem"`
	CLRDirectoryPresent  bool        `json:"clr_directory_present"`
	ImportedLibraries    []string    `json:"imported_libraries"`
	ImportedSymbols      []string    `json:"imported_symbols"`
	NetworkImports       []string    `json:"network_imports"`
	Sections             []PESection `json:"sections"`
	ProtectionIndicators []string    `json:"protection_indicators"`
	ParseError           string      `json:"parse_error,omitempty"`
}

type PESection struct {
	Name            string  `json:"name"`
	VirtualAddress  uint32  `json:"virtual_address"`
	VirtualSize     uint32  `json:"virtual_size"`
	RawOffset       uint32  `json:"raw_offset"`
	RawSize         uint32  `json:"raw_size"`
	Characteristics string  `json:"characteristics"`
	Entropy         float64 `json:"entropy"`
}

type NetworkReport struct {
	GeneratedAt    time.Time       `json:"generated_at"`
	Endpoints      []StringFinding `json:"endpoints"`
	PortSettings   []StringFinding `json:"port_settings"`
	KeywordHits    []StringFinding `json:"keyword_hits"`
	NetworkImports []NetworkImport `json:"network_imports"`
	Limitations    []string        `json:"limitations"`
}

type StringFinding struct {
	Path     string `json:"path"`
	Offset   int    `json:"offset"`
	Encoding string `json:"encoding"`
	Kind     string `json:"kind"`
	Value    string `json:"value"`
}

type NetworkImport struct {
	Path   string `json:"path"`
	Symbol string `json:"symbol"`
}

type ResourceReport struct {
	GeneratedAt time.Time        `json:"generated_at"`
	FileCount   int              `json:"file_count"`
	TotalBytes  int64            `json:"total_bytes"`
	Extensions  []ExtensionCount `json:"extensions"`
	Largest     []FileSummary    `json:"largest"`
	Files       []FileSummary    `json:"files"`
}

type fileRecord struct {
	path string
	size int64
	hash string
	kind string
}

var (
	urlRE        = regexp.MustCompile(`(?i)https?://[^\x00-\x20\"'<>]{4,}`)
	domainRE     = regexp.MustCompile(`(?i)(?:[a-z0-9](?:[a-z0-9-]{0,62})\.)+(?:com|net|org|cn|qq|info|biz|io)(?::[0-9]{1,5})?`)
	ipv4RE       = regexp.MustCompile(`(?:[0-9]{1,3}\.){3}[0-9]{1,3}`)
	portRE       = regexp.MustCompile(`(?i)(?:tcpport|udpport|stunport|dirport|port2?|serverport)\s*=\s*([0-9]{1,5})`)
	networkNames = []string{
		"connect", "wsaconnect", "send", "recv", "wsasend", "wsarecv",
		"gethostbyname", "getaddrinfo", "internetconnect", "httpsendrequest",
		"winhttpconnect", "winhttpsendrequest", "socket", "select", "ioctlsocket",
	}
	keywords = []string{"login", "auth", "server", "gateway", "zone", "channel", "room", "battle", "version", "patch", "update", "tcp", "udp"}
)

func Run(opts Options) error {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return fmt.Errorf("resolve root: %w", err)
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return fmt.Errorf("inspect root is not a directory: %s", root)
	}
	if err := os.MkdirAll(opts.OutputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	files, peFiles, network, resources, err := scan(root)
	if err != nil {
		return err
	}
	generated := time.Now().UTC()
	fileReport := buildFileReport(root, generated, files)
	peReport := PEReport{GeneratedAt: generated, Files: peFiles}
	resourceReport := buildResourceReport(generated, resources)
	network.GeneratedAt = generated
	network.Limitations = []string{
		"String offsets are file offsets, not code cross-references.",
		"Imported APIs prove dependency only; call sites require disassembly or runtime tracing.",
		"Ports are reported only when attached to a named configuration key.",
	}

	outputs := []struct {
		name  string
		value any
	}{
		{"client-file-types.json", fileReport},
		{"pe-inventory.json", peReport},
		{"network-strings.json", network},
		{"resources.json", resourceReport},
	}
	for _, output := range outputs {
		if err := writeJSON(filepath.Join(opts.OutputDir, output.name), output.value); err != nil {
			return err
		}
	}
	return nil
}

func scan(root string) ([]fileRecord, []PEFile, NetworkReport, []fileRecord, error) {
	var files []fileRecord
	var peFiles []PEFile
	var resources []fileRecord
	var network NetworkReport
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		sum := sha256.Sum256(data)
		record := fileRecord{path: rel, size: info.Size(), hash: hex.EncodeToString(sum[:]), kind: detectKind(data, filepath.Ext(path))}
		files = append(files, record)
		if isResource(rel, filepath.Ext(path)) {
			resources = append(resources, record)
		}

		stringsFound := extractStrings(data)
		scanStrings(rel, stringsFound, &network)
		if len(data) >= 2 && data[0] == 'M' && data[1] == 'Z' {
			peInfo := inspectPE(path, rel, info.Size(), record.hash)
			for _, symbol := range peInfo.NetworkImports {
				network.NetworkImports = append(network.NetworkImports, NetworkImport{Path: rel, Symbol: symbol})
			}
			peFiles = append(peFiles, peInfo)
		}
		return nil
	})
	if err != nil {
		return nil, nil, NetworkReport{}, nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	sort.Slice(peFiles, func(i, j int) bool { return peFiles[i].Path < peFiles[j].Path })
	sort.Slice(resources, func(i, j int) bool { return resources[i].path < resources[j].path })
	sortFindings(network.Endpoints)
	sortFindings(network.PortSettings)
	sortFindings(network.KeywordHits)
	sort.Slice(network.NetworkImports, func(i, j int) bool {
		if network.NetworkImports[i].Path == network.NetworkImports[j].Path {
			return network.NetworkImports[i].Symbol < network.NetworkImports[j].Symbol
		}
		return network.NetworkImports[i].Path < network.NetworkImports[j].Path
	})
	return files, peFiles, network, resources, nil
}

func inspectPE(path, rel string, size int64, hash string) PEFile {
	result := PEFile{Path: rel, Size: size, SHA256: hash}
	f, err := pe.Open(path)
	if err != nil {
		result.ParseError = err.Error()
		return result
	}
	defer f.Close()
	result.MachineValue = fmt.Sprintf("0x%04x", f.FileHeader.Machine)
	result.Machine, result.Architecture = machineName(f.FileHeader.Machine)
	result.CompileTimestamp = time.Unix(int64(f.FileHeader.TimeDateStamp), 0).UTC()
	result.ImportedLibraries, result.ImportedSymbols, err = parseImportTable(path, f)
	if err != nil {
		result.ProtectionIndicators = append(result.ProtectionIndicators, "import table parse incomplete: "+err.Error())
	}
	sort.Strings(result.ImportedLibraries)
	sort.Strings(result.ImportedSymbols)
	for _, symbol := range result.ImportedSymbols {
		if isNetworkName(symbol) {
			result.NetworkImports = append(result.NetworkImports, symbol)
		}
	}
	result.NetworkImports = uniqueSorted(result.NetworkImports)

	switch h := f.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		result.OptionalHeaderMagic = "PE32"
		result.EntryPoint = h.AddressOfEntryPoint
		result.Subsystem = subsystemName(h.Subsystem)
		result.CLRDirectoryPresent = len(h.DataDirectory) > 14 && h.DataDirectory[14].VirtualAddress != 0
	case *pe.OptionalHeader64:
		result.OptionalHeaderMagic = "PE32+"
		result.EntryPoint = h.AddressOfEntryPoint
		result.Subsystem = subsystemName(h.Subsystem)
		result.CLRDirectoryPresent = len(h.DataDirectory) > 14 && h.DataDirectory[14].VirtualAddress != 0
	default:
		result.OptionalHeaderMagic = "unknown"
	}
	for _, section := range f.Sections {
		data, readErr := section.Data()
		entropy := 0.0
		if readErr == nil {
			entropy = shannonEntropy(data)
		}
		result.Sections = append(result.Sections, PESection{
			Name: strings.TrimRight(section.Name, "\x00"), VirtualAddress: section.VirtualAddress, VirtualSize: section.VirtualSize, RawOffset: section.Offset,
			RawSize: section.Size, Characteristics: fmt.Sprintf("0x%08x", section.Characteristics), Entropy: math.Round(entropy*1000) / 1000,
		})
		upper := strings.ToUpper(section.Name)
		if strings.Contains(upper, "UPX") || strings.Contains(upper, "PACK") || strings.Contains(upper, "ASPACK") {
			result.ProtectionIndicators = append(result.ProtectionIndicators, "packer-like section name: "+section.Name)
		}
		const memExecute = 0x20000000
		const memWrite = 0x80000000
		if section.Characteristics&memExecute != 0 && section.Characteristics&memWrite != 0 {
			result.ProtectionIndicators = append(result.ProtectionIndicators, "writable+executable section: "+section.Name)
		}
		if len(data) >= 64*1024 && entropy >= 7.5 {
			result.ProtectionIndicators = append(result.ProtectionIndicators, fmt.Sprintf("high-entropy section: %s (%.3f)", section.Name, entropy))
		}
	}
	result.ProtectionIndicators = uniqueSorted(result.ProtectionIndicators)
	return result
}

type extractedString struct {
	offset   int
	encoding string
	value    string
}

func extractStrings(data []byte) []extractedString {
	const minLength = 4
	var out []extractedString
	for i := 0; i < len(data); {
		start := i
		for i < len(data) && data[i] >= 0x20 && data[i] <= 0x7e {
			i++
		}
		if i-start >= minLength {
			out = append(out, extractedString{offset: start, encoding: "ascii", value: string(data[start:i])})
		}
		i++
	}
	for parity := 0; parity < 2; parity++ {
		for i := parity; i+1 < len(data); {
			start := i
			var b strings.Builder
			for i+1 < len(data) && data[i] >= 0x20 && data[i] <= 0x7e && data[i+1] == 0 {
				b.WriteByte(data[i])
				i += 2
			}
			if b.Len() >= minLength {
				out = append(out, extractedString{offset: start, encoding: "utf16le-ascii", value: b.String()})
			}
			i += 2
		}
	}
	return out
}

func scanStrings(path string, stringsFound []extractedString, report *NetworkReport) {
	keywordSeen := make(map[string]int)
	for _, found := range stringsFound {
		matches := make([]struct{ kind, value string }, 0)
		for _, value := range urlRE.FindAllString(found.value, -1) {
			matches = append(matches, struct{ kind, value string }{"url", value})
		}
		for _, value := range domainRE.FindAllString(found.value, -1) {
			matches = append(matches, struct{ kind, value string }{"domain", value})
		}
		for _, value := range ipv4RE.FindAllString(found.value, -1) {
			if ip := net.ParseIP(value); ip != nil && ip.To4() != nil {
				matches = append(matches, struct{ kind, value string }{"ipv4", value})
			}
		}
		for _, match := range matches {
			report.Endpoints = append(report.Endpoints, StringFinding{Path: path, Offset: found.offset + strings.Index(found.value, match.value), Encoding: found.encoding, Kind: match.kind, Value: match.value})
		}
		for _, match := range portRE.FindAllStringSubmatchIndex(found.value, -1) {
			value := found.value[match[0]:match[1]]
			portText := found.value[match[2]:match[3]]
			port, _ := strconv.Atoi(portText)
			if port <= 65535 {
				report.PortSettings = append(report.PortSettings, StringFinding{Path: path, Offset: found.offset + match[0], Encoding: found.encoding, Kind: "named-port", Value: value})
			}
		}
		lower := strings.ToLower(found.value)
		for _, keyword := range keywords {
			if keywordSeen[keyword] >= 20 {
				continue
			}
			if index := strings.Index(lower, keyword); index >= 0 {
				value := found.value
				if len(value) > 240 {
					left := index - 80
					if left < 0 {
						left = 0
					}
					right := left + 240
					if right > len(value) {
						right = len(value)
					}
					value = value[left:right]
				}
				report.KeywordHits = append(report.KeywordHits, StringFinding{Path: path, Offset: found.offset + index, Encoding: found.encoding, Kind: keyword, Value: value})
				keywordSeen[keyword]++
			}
		}
	}
	report.Endpoints = dedupeFindings(report.Endpoints)
	report.PortSettings = dedupeFindings(report.PortSettings)
}

func buildFileReport(root string, generated time.Time, files []fileRecord) FileTypeReport {
	report := FileTypeReport{GeneratedAt: generated, Root: root, FileCount: len(files)}
	extensions := make(map[string]*ExtensionCount)
	duplicates := make(map[string][]fileRecord)
	for _, file := range files {
		report.TotalBytes += file.size
		ext := strings.ToLower(filepath.Ext(file.path))
		if ext == "" {
			ext = "[none]"
		}
		entry := extensions[ext]
		if entry == nil {
			entry = &ExtensionCount{Extension: ext}
			extensions[ext] = entry
		}
		entry.Count++
		entry.TotalBytes += file.size
		if file.size == 0 {
			report.EmptyFiles = append(report.EmptyFiles, file.path)
		}
		duplicates[file.hash] = append(duplicates[file.hash], file)
	}
	for _, entry := range extensions {
		report.Extensions = append(report.Extensions, *entry)
	}
	sort.Slice(report.Extensions, func(i, j int) bool {
		return report.Extensions[i].Count > report.Extensions[j].Count || (report.Extensions[i].Count == report.Extensions[j].Count && report.Extensions[i].Extension < report.Extensions[j].Extension)
	})
	largest := append([]fileRecord(nil), files...)
	sort.Slice(largest, func(i, j int) bool {
		return largest[i].size > largest[j].size || (largest[i].size == largest[j].size && largest[i].path < largest[j].path)
	})
	for i := 0; i < len(largest) && i < 30; i++ {
		report.LargestFiles = append(report.LargestFiles, summary(largest[i]))
	}
	for hash, group := range duplicates {
		if len(group) < 2 {
			continue
		}
		paths := make([]string, 0, len(group))
		for _, file := range group {
			paths = append(paths, file.path)
		}
		sort.Strings(paths)
		report.DuplicateSets = append(report.DuplicateSets, DuplicateSet{SHA256: hash, Size: group[0].size, Paths: paths})
	}
	sort.Slice(report.DuplicateSets, func(i, j int) bool { return report.DuplicateSets[i].Size > report.DuplicateSets[j].Size })
	sort.Strings(report.EmptyFiles)
	return report
}

func buildResourceReport(generated time.Time, files []fileRecord) ResourceReport {
	report := ResourceReport{GeneratedAt: generated, FileCount: len(files)}
	extensions := make(map[string]*ExtensionCount)
	for _, file := range files {
		report.TotalBytes += file.size
		report.Files = append(report.Files, summary(file))
		ext := strings.ToLower(filepath.Ext(file.path))
		if ext == "" {
			ext = "[none]"
		}
		entry := extensions[ext]
		if entry == nil {
			entry = &ExtensionCount{Extension: ext}
			extensions[ext] = entry
		}
		entry.Count++
		entry.TotalBytes += file.size
	}
	for _, entry := range extensions {
		report.Extensions = append(report.Extensions, *entry)
	}
	sort.Slice(report.Extensions, func(i, j int) bool {
		return report.Extensions[i].Count > report.Extensions[j].Count || (report.Extensions[i].Count == report.Extensions[j].Count && report.Extensions[i].Extension < report.Extensions[j].Extension)
	})
	largest := append([]fileRecord(nil), files...)
	sort.Slice(largest, func(i, j int) bool { return largest[i].size > largest[j].size })
	for i := 0; i < len(largest) && i < 50; i++ {
		report.Largest = append(report.Largest, summary(largest[i]))
	}
	return report
}

func summary(file fileRecord) FileSummary {
	return FileSummary{Path: file.path, Size: file.size, SHA256: file.hash, Kind: file.kind}
}

func isResource(path, ext string) bool {
	top := strings.ToLower(strings.Split(filepath.ToSlash(path), "/")[0])
	switch top {
	case "config", "data", "effect", "icon", "map", "music", "object", "profile", "qqshow", "res", "screen", "sound", "ui":
		return true
	}
	switch strings.ToLower(ext) {
	case ".qbv", ".swf", ".wmv", ".wav", ".ogg", ".mp3", ".png", ".jpg", ".jpeg", ".bmp", ".gif", ".dds", ".tga":
		return true
	}
	return false
}

func detectKind(data []byte, ext string) string {
	if len(data) >= 2 && data[0] == 'M' && data[1] == 'Z' {
		return "windows-pe"
	}
	if len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n" {
		return "png"
	}
	if len(data) >= 3 && string(data[:3]) == "ID3" {
		return "mp3"
	}
	if len(data) >= 4 && string(data[:4]) == "RIFF" {
		return "riff"
	}
	if len(data) >= 4 && string(data[:4]) == "OggS" {
		return "ogg"
	}
	if len(data) >= 2 && data[0] == 'P' && (data[1] == 'K') {
		return "zip"
	}
	if len(data) == 0 {
		return "empty"
	}
	if isMostlyText(data) {
		return "text"
	}
	if ext != "" {
		return "binary-" + strings.TrimPrefix(strings.ToLower(ext), ".")
	}
	return "binary"
}

func isMostlyText(data []byte) bool {
	if len(data) > 32*1024 {
		data = data[:32*1024]
	}
	if len(data) == 0 {
		return true
	}
	printable := 0
	for _, b := range data {
		if b == '\r' || b == '\n' || b == '\t' || (b >= 0x20 && b != 0x7f) {
			printable++
		}
	}
	return float64(printable)/float64(len(data)) >= 0.85
}

func machineName(machine uint16) (string, string) {
	switch machine {
	case pe.IMAGE_FILE_MACHINE_I386:
		return "IMAGE_FILE_MACHINE_I386", "x86 (32-bit)"
	case pe.IMAGE_FILE_MACHINE_AMD64:
		return "IMAGE_FILE_MACHINE_AMD64", "x86-64 (64-bit)"
	case pe.IMAGE_FILE_MACHINE_ARM64:
		return "IMAGE_FILE_MACHINE_ARM64", "ARM64 (64-bit)"
	default:
		return "unknown", "unknown"
	}
}

func subsystemName(value uint16) string {
	switch value {
	case 2:
		return "windows-gui"
	case 3:
		return "windows-console"
	case 9:
		return "windows-ce-gui"
	case 10:
		return "efi-application"
	default:
		return fmt.Sprintf("unknown-%d", value)
	}
}

func isNetworkName(value string) bool {
	name := value
	if colon := strings.IndexByte(name, ':'); colon >= 0 {
		name = name[:colon]
	}
	lower := strings.ToLower(name)
	for _, name := range networkNames {
		if lower == name || lower == name+"a" || lower == name+"w" || strings.HasPrefix(lower, "?"+name+"@") {
			return true
		}
	}
	return false
}

func parseImportTable(path string, file *pe.File) ([]string, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var directory pe.DataDirectory
	var thunkSize uint32
	switch header := file.OptionalHeader.(type) {
	case *pe.OptionalHeader32:
		if len(header.DataDirectory) <= 1 || header.DataDirectory[1].VirtualAddress == 0 {
			return nil, nil, nil
		}
		directory = header.DataDirectory[1]
		thunkSize = 4
	case *pe.OptionalHeader64:
		if len(header.DataDirectory) <= 1 || header.DataDirectory[1].VirtualAddress == 0 {
			return nil, nil, nil
		}
		directory = header.DataDirectory[1]
		thunkSize = 8
	default:
		return nil, nil, fmt.Errorf("unknown optional header")
	}
	baseOffset, ok := rvaToOffset(file, directory.VirtualAddress, len(data))
	if !ok {
		return nil, nil, fmt.Errorf("import directory RVA 0x%x is not file-backed", directory.VirtualAddress)
	}
	var libraries []string
	var symbols []string
	var firstError error
	const descriptorSize = 20
	for descriptorIndex := 0; descriptorIndex < 4096; descriptorIndex++ {
		offset := baseOffset + descriptorIndex*descriptorSize
		if offset < 0 || offset+descriptorSize > len(data) {
			if firstError == nil {
				firstError = fmt.Errorf("import descriptor %d exceeds file", descriptorIndex)
			}
			break
		}
		descriptor := data[offset : offset+descriptorSize]
		originalFirstThunk := binary.LittleEndian.Uint32(descriptor[0:4])
		nameRVA := binary.LittleEndian.Uint32(descriptor[12:16])
		firstThunk := binary.LittleEndian.Uint32(descriptor[16:20])
		if originalFirstThunk == 0 && nameRVA == 0 && firstThunk == 0 {
			break
		}
		dll, ok := cStringAtRVA(file, data, nameRVA, 512)
		if !ok {
			if firstError == nil {
				firstError = fmt.Errorf("descriptor %d DLL name RVA 0x%x is invalid", descriptorIndex, nameRVA)
			}
			continue
		}
		libraries = append(libraries, dll)
		thunkRVA := originalFirstThunk
		if thunkRVA == 0 {
			thunkRVA = firstThunk
		}
		for thunkIndex := 0; thunkIndex < 65536; thunkIndex++ {
			thunkOffset, ok := rvaToOffset(file, thunkRVA+uint32(thunkIndex)*thunkSize, len(data))
			if !ok || thunkOffset+int(thunkSize) > len(data) {
				if firstError == nil {
					firstError = fmt.Errorf("%s thunk %d is not file-backed", dll, thunkIndex)
				}
				break
			}
			var value uint64
			if thunkSize == 4 {
				value = uint64(binary.LittleEndian.Uint32(data[thunkOffset : thunkOffset+4]))
			} else {
				value = binary.LittleEndian.Uint64(data[thunkOffset : thunkOffset+8])
			}
			if value == 0 {
				break
			}
			ordinalMask := uint64(0x80000000)
			if thunkSize == 8 {
				ordinalMask = uint64(0x8000000000000000)
			}
			if value&ordinalMask != 0 {
				ordinal := uint16(value & 0xffff)
				if resolved := resolveWinsockOrdinal(dll, ordinal); resolved != "" {
					symbols = append(symbols, resolved+":"+dll)
				} else {
					symbols = append(symbols, fmt.Sprintf("ordinal_%d:%s", ordinal, dll))
				}
				continue
			}
			if value > math.MaxUint32 {
				if firstError == nil {
					firstError = fmt.Errorf("%s thunk %d has invalid name RVA 0x%x", dll, thunkIndex, value)
				}
				break
			}
			name, ok := cStringAtRVA(file, data, uint32(value)+2, 2048)
			if !ok {
				if firstError == nil {
					firstError = fmt.Errorf("%s thunk %d name RVA 0x%x is invalid", dll, thunkIndex, value)
				}
				break
			}
			symbols = append(symbols, name+":"+dll)
		}
	}
	return uniqueSorted(libraries), uniqueSorted(symbols), firstError
}

func resolveWinsockOrdinal(library string, ordinal uint16) string {
	upper := strings.ToUpper(library)
	if upper != "WS2_32.DLL" && upper != "WSOCK32.DLL" {
		return ""
	}
	names := map[uint16]string{
		1: "accept", 2: "bind", 3: "closesocket", 4: "connect", 5: "getpeername",
		6: "getsockname", 7: "getsockopt", 8: "htonl", 9: "htons", 10: "ioctlsocket",
		11: "inet_addr", 12: "inet_ntoa", 13: "listen", 14: "ntohl", 15: "ntohs",
		16: "recv", 17: "recvfrom", 18: "select", 19: "send", 20: "sendto",
		21: "setsockopt", 22: "shutdown", 23: "socket", 51: "gethostbyaddr",
		52: "gethostbyname", 53: "getprotobyname", 54: "getprotobynumber", 55: "getservbyname",
		56: "getservbyport", 57: "gethostname", 101: "WSAAsyncSelect", 102: "WSAAsyncGetHostByAddr",
		103: "WSAAsyncGetHostByName", 104: "WSAAsyncGetProtoByNumber", 105: "WSAAsyncGetProtoByName",
		106: "WSAAsyncGetServByPort", 107: "WSAAsyncGetServByName", 108: "WSACancelAsyncRequest",
		109: "WSASetBlockingHook", 110: "WSAUnhookBlockingHook", 111: "WSAGetLastError",
		112: "WSASetLastError", 113: "WSACancelBlockingCall", 114: "WSAIsBlocking",
		115: "WSAStartup", 116: "WSACleanup", 151: "__WSAFDIsSet",
	}
	return names[ordinal]
}

func rvaToOffset(file *pe.File, rva uint32, fileLength int) (int, bool) {
	for _, section := range file.Sections {
		size := section.VirtualSize
		if section.Size > size {
			size = section.Size
		}
		if rva < section.VirtualAddress || rva-section.VirtualAddress >= size {
			continue
		}
		offset := uint64(section.Offset) + uint64(rva-section.VirtualAddress)
		if offset >= uint64(fileLength) {
			return 0, false
		}
		return int(offset), true
	}
	return 0, false
}

func cStringAtRVA(file *pe.File, data []byte, rva uint32, maximum int) (string, bool) {
	offset, ok := rvaToOffset(file, rva, len(data))
	if !ok {
		return "", false
	}
	end := offset
	limit := offset + maximum
	if limit > len(data) {
		limit = len(data)
	}
	for end < limit && data[end] != 0 {
		if data[end] < 0x20 || data[end] > 0x7e {
			return "", false
		}
		end++
	}
	if end == offset || end == limit {
		return "", false
	}
	return string(data[offset:end]), true
}

func shannonEntropy(data []byte) float64 {
	if len(data) == 0 {
		return 0
	}
	var counts [256]int
	for _, b := range data {
		counts[b]++
	}
	var result float64
	for _, count := range counts {
		if count > 0 {
			p := float64(count) / float64(len(data))
			result -= p * math.Log2(p)
		}
	}
	return result
}

func uniqueSorted(values []string) []string {
	sort.Strings(values)
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}

func dedupeFindings(values []StringFinding) []StringFinding {
	seen := make(map[string]struct{})
	result := values[:0]
	for _, value := range values {
		key := fmt.Sprintf("%s\x00%d\x00%s\x00%s", value.Path, value.Offset, value.Kind, value.Value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func sortFindings(values []StringFinding) {
	sort.Slice(values, func(i, j int) bool {
		if values[i].Path == values[j].Path {
			return values[i].Offset < values[j].Offset
		}
		return values[i].Path < values[j].Path
	})
}

func writeJSON(path string, value any) error {
	temporary := path + ".tmp"
	f, err := os.Create(temporary)
	if err != nil {
		return fmt.Errorf("create %s: %w", temporary, err)
	}
	encoder := json.NewEncoder(f)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		f.Close()
		return fmt.Errorf("encode %s: %w", temporary, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", temporary, err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

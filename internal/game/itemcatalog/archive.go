package itemcatalog

import (
	"bufio"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	objectArchiveVersion     = 100
	objectArchiveHeaderSize  = 16
	objectArchiveMaxPathSize = 4096
	objectArchiveMaxFileSize = 64 << 20
)

// Archive is the read-only index for the original client's data/object.pkg.
// The package itself is never extracted or modified; individual resources are
// decompressed on demand for GM previews and analysis tools.
type Archive struct {
	packagePath string
	entries     map[string]ArchiveEntry
	paths       []string
}

type ArchiveEntry struct {
	Path           string
	PackageIndex   uint32
	PackageOffset  uint32
	ExpandedSize   uint32
	CompressedSize uint32
}

func OpenObjectArchive(clientRoot string) (*Archive, error) {
	indexPath := filepath.Join(clientRoot, "data", "object.idx")
	packagePath := filepath.Join(clientRoot, "data", "object.pkg")
	file, err := os.Open(indexPath)
	if err != nil {
		return nil, fmt.Errorf("open object archive index: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat object archive index: %w", err)
	}
	if info.Size() < objectArchiveHeaderSize {
		return nil, fmt.Errorf("object archive index is only %d bytes", info.Size())
	}
	reader := bufio.NewReader(file)
	var header [objectArchiveHeaderSize]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, fmt.Errorf("read object archive header: %w", err)
	}
	version := binary.LittleEndian.Uint32(header[0:4])
	count := binary.LittleEndian.Uint32(header[4:8])
	indexOffset := binary.LittleEndian.Uint32(header[8:12])
	indexSize := binary.LittleEndian.Uint32(header[12:16])
	if version != objectArchiveVersion {
		return nil, fmt.Errorf("object archive index version %d, want %d", version, objectArchiveVersion)
	}
	if indexOffset != objectArchiveHeaderSize || uint64(indexOffset)+uint64(indexSize) != uint64(info.Size()) {
		return nil, fmt.Errorf("invalid object archive index range %d+%d for %d-byte file", indexOffset, indexSize, info.Size())
	}
	if count > 1_000_000 {
		return nil, fmt.Errorf("object archive entry count %d exceeds safety limit", count)
	}
	entries := make(map[string]ArchiveEntry, count)
	paths := make([]string, 0, count)
	consumed := uint32(0)
	for entryIndex := uint32(0); entryIndex < count; entryIndex++ {
		var lengthBytes [2]byte
		if _, err := io.ReadFull(reader, lengthBytes[:]); err != nil {
			return nil, fmt.Errorf("read object archive entry %d path length: %w", entryIndex, err)
		}
		pathLength := uint32(binary.LittleEndian.Uint16(lengthBytes[:]))
		consumed += 2
		if pathLength == 0 || pathLength > objectArchiveMaxPathSize || consumed+pathLength+16 > indexSize {
			return nil, fmt.Errorf("object archive entry %d has invalid path length %d", entryIndex, pathLength)
		}
		pathBytes := make([]byte, pathLength)
		if _, err := io.ReadFull(reader, pathBytes); err != nil {
			return nil, fmt.Errorf("read object archive entry %d path: %w", entryIndex, err)
		}
		var metadata [16]byte
		if _, err := io.ReadFull(reader, metadata[:]); err != nil {
			return nil, fmt.Errorf("read object archive entry %d metadata: %w", entryIndex, err)
		}
		consumed += pathLength + uint32(len(metadata))
		path := normalizeArchivePath(string(pathBytes))
		entry := ArchiveEntry{
			Path: string(pathBytes), PackageIndex: binary.LittleEndian.Uint32(metadata[0:4]),
			PackageOffset: binary.LittleEndian.Uint32(metadata[4:8]), ExpandedSize: binary.LittleEndian.Uint32(metadata[8:12]),
			CompressedSize: binary.LittleEndian.Uint32(metadata[12:16]),
		}
		if path == "" || entry.PackageIndex != 0 || entry.ExpandedSize == 0 || entry.ExpandedSize > objectArchiveMaxFileSize || entry.CompressedSize == 0 {
			return nil, fmt.Errorf("object archive entry %d (%q) has unsupported metadata %+v", entryIndex, entry.Path, entry)
		}
		if _, duplicate := entries[path]; duplicate {
			return nil, fmt.Errorf("object archive repeats path %q", entry.Path)
		}
		entries[path] = entry
		paths = append(paths, path)
	}
	if consumed != indexSize {
		return nil, fmt.Errorf("object archive parsed %d index bytes, want %d", consumed, indexSize)
	}
	packageInfo, err := os.Stat(packagePath)
	if err != nil {
		return nil, fmt.Errorf("stat object archive package: %w", err)
	}
	for _, entry := range entries {
		if uint64(entry.PackageOffset)+uint64(entry.CompressedSize) > uint64(packageInfo.Size()) {
			return nil, fmt.Errorf("object archive entry %q exceeds %d-byte package", entry.Path, packageInfo.Size())
		}
	}
	sort.Strings(paths)
	return &Archive{packagePath: packagePath, entries: entries, paths: paths}, nil
}

func (archive *Archive) Has(path string) bool {
	if archive == nil {
		return false
	}
	_, ok := archive.entries[normalizeArchivePath(path)]
	return ok
}

func (archive *Archive) Paths() []string {
	if archive == nil {
		return nil
	}
	return append([]string(nil), archive.paths...)
}

func (archive *Archive) Read(path string) ([]byte, error) {
	if archive == nil {
		return nil, fmt.Errorf("object archive is unavailable")
	}
	entry, ok := archive.entries[normalizeArchivePath(path)]
	if !ok {
		return nil, os.ErrNotExist
	}
	file, err := os.Open(archive.packagePath)
	if err != nil {
		return nil, fmt.Errorf("open object archive package: %w", err)
	}
	defer file.Close()
	compressed := io.NewSectionReader(file, int64(entry.PackageOffset), int64(entry.CompressedSize))
	reader, err := zlib.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("open compressed object resource %q: %w", entry.Path, err)
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, int64(entry.ExpandedSize)+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, fmt.Errorf("decompress object resource %q: %w", entry.Path, readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close object resource %q: %w", entry.Path, closeErr)
	}
	if len(data) != int(entry.ExpandedSize) {
		return nil, fmt.Errorf("object resource %q expanded to %d bytes, want %d", entry.Path, len(data), entry.ExpandedSize)
	}
	return data, nil
}

func normalizeArchivePath(path string) string {
	path = strings.ReplaceAll(strings.TrimSpace(path), "/", "\\")
	path = strings.TrimLeft(path, "\\")
	return strings.ToLower(path)
}

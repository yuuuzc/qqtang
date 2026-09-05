package itemcatalog

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenObjectArchiveReadsCompressedResource(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("QQF resource payload")
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDir, "object.pkg"), compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	path := []byte(`object\cap\cap104_stand.img`)
	indexSize := 2 + len(path) + 16
	index := make([]byte, 16+indexSize)
	binary.LittleEndian.PutUint32(index[0:4], objectArchiveVersion)
	binary.LittleEndian.PutUint32(index[4:8], 1)
	binary.LittleEndian.PutUint32(index[8:12], 16)
	binary.LittleEndian.PutUint32(index[12:16], uint32(indexSize))
	position := 16
	binary.LittleEndian.PutUint16(index[position:position+2], uint16(len(path)))
	position += 2
	copy(index[position:], path)
	position += len(path)
	binary.LittleEndian.PutUint32(index[position+8:position+12], uint32(len(payload)))
	binary.LittleEndian.PutUint32(index[position+12:position+16], uint32(compressed.Len()))
	if err := os.WriteFile(filepath.Join(dataDir, "object.idx"), index, 0o600); err != nil {
		t.Fatal(err)
	}
	archive, err := OpenObjectArchive(root)
	if err != nil {
		t.Fatal(err)
	}
	if !archive.Has("OBJECT/cap/cap104_stand.img") {
		t.Fatal("archive path normalization did not find resource")
	}
	got, err := archive.Read(`object\cap\cap104_stand.img`)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("Read() = %q, %v", got, err)
	}

}

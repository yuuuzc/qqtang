package capture

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type Writer struct {
	mu     sync.Mutex
	file   *os.File
	buffer *bufio.Writer
}

func Open(path string) (*Writer, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open capture %s: %w", path, err)
	}
	return &Writer{file: file, buffer: bufio.NewWriter(file)}, nil
}

func (w *Writer) Write(record Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return fmt.Errorf("capture writer is closed")
	}
	if err := json.NewEncoder(w.buffer).Encode(record); err != nil {
		return fmt.Errorf("encode capture: %w", err)
	}
	if err := w.buffer.Flush(); err != nil {
		return fmt.Errorf("flush capture: %w", err)
	}
	return nil
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	flushErr := w.buffer.Flush()
	closeErr := w.file.Close()
	w.file = nil
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

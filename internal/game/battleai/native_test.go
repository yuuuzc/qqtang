package battleai

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeRunnerLoadsAndMasksActorLogits(t *testing.T) {
	payload := tinyNativeModel(t)
	runner, err := loadNativeRunner(payload)
	if err != nil {
		t.Fatal(err)
	}
	plane := runner.Height * runner.Width
	spatial := make([]float32, runner.Channels*plane)
	for index := 0; index < plane; index++ {
		spatial[index] = 1
	}
	spatial[7*plane+1] = 1
	legal := make([]uint8, runner.Actions)
	for index := range legal {
		legal[index] = 1
	}
	legal[44] = 0
	logits, err := runner.RunActor(spatial, make([]float32, runner.Scalars), legal)
	if err != nil {
		t.Fatal(err)
	}
	if logits[43] != 43 || logits[44] != -math.MaxFloat32 {
		t.Fatalf("native actor logits 43/44 = %v/%v", logits[43], logits[44])
	}
}

func TestNativeRunnerRejectsCorruptChecksum(t *testing.T) {
	payload := tinyNativeModel(t)
	payload[30] ^= 0x80
	if _, err := loadNativeRunner(payload); err == nil {
		t.Fatal("corrupt native model checksum was accepted")
	}
}

func TestLoadNativePolicyRejectsStaleTensorContractAtStartup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stale.qtai")
	if err := os.WriteFile(path, tinyNativeModel(t), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadNativePolicy(path, NativePolicyConfig{}); err == nil {
		t.Fatal("server policy accepted a stale 48-scalar artifact")
	}
}

func tinyNativeModel(t *testing.T) []byte {
	t.Helper()
	type tensor struct {
		name  string
		shape []uint32
		data  []float32
	}
	zeros := func(count int) []float32 { return make([]float32, count) }
	tensors := []tensor{
		{nativeTensorOrder[0], []uint32{1, 28, 3, 3}, zeros(252)},
		{nativeTensorOrder[1], []uint32{1}, zeros(1)},
		{nativeTensorOrder[2], []uint32{1, 1, 3, 3}, zeros(9)},
		{nativeTensorOrder[3], []uint32{1}, zeros(1)},
		{nativeTensorOrder[4], []uint32{1, 1, 3, 3}, zeros(9)},
		{nativeTensorOrder[5], []uint32{1}, zeros(1)},
		{nativeTensorOrder[6], []uint32{1, 55}, zeros(55)},
		{nativeTensorOrder[7], []uint32{1}, zeros(1)},
		{nativeTensorOrder[8], []uint32{1}, []float32{1}},
		{nativeTensorOrder[9], []uint32{1}, zeros(1)},
		{nativeTensorOrder[10], []uint32{1, 1}, zeros(1)},
		{nativeTensorOrder[11], []uint32{1}, zeros(1)},
		{nativeTensorOrder[12], []uint32{45, 1}, zeros(45)},
		{nativeTensorOrder[13], []uint32{45}, make([]float32, 45)},
	}
	for index := range tensors[13].data {
		tensors[13].data[index] = float32(index)
	}
	var body bytes.Buffer
	if err := binary.Write(&body, binary.LittleEndian, [4]byte{'Q', 'T', 'A', 'I'}); err != nil {
		t.Fatal(err)
	}
	for _, value := range []uint16{1, 2, 2, 14, 26, 48, 45, 3, 5, 0} {
		if err := binary.Write(&body, binary.LittleEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range tensors {
		if err := binary.Write(&body, binary.LittleEndian, uint16(len(entry.name))); err != nil {
			t.Fatal(err)
		}
		body.WriteByte(byte(len(entry.shape)))
		body.WriteByte(nativeActorDTypeF32)
		if err := binary.Write(&body, binary.LittleEndian, uint32(len(entry.data))); err != nil {
			t.Fatal(err)
		}
		body.WriteString(entry.name)
		for _, size := range entry.shape {
			if err := binary.Write(&body, binary.LittleEndian, size); err != nil {
				t.Fatal(err)
			}
		}
		for _, value := range entry.data {
			if err := binary.Write(&body, binary.LittleEndian, math.Float32bits(value)); err != nil {
				t.Fatal(err)
			}
		}
	}
	payload := body.Bytes()
	digest := sha256.Sum256(payload)
	return append(append([]byte(nil), payload...), digest[:]...)
}

package launcherapp

import (
	"bytes"
	"testing"
)

func TestReplaceScalarSettingPreservesSurroundingConfiguration(t *testing.T) {
	input := []byte("[options]\r\nlimitfps=60\r\nnext=1\r\n")
	got, err := replaceScalarSetting(input, "limitfps", 144)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("[options]\r\nlimitfps=144\r\nnext=1\r\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("updated config = %q, want %q", got, want)
	}

	device := []byte("class device:\r\n  fps= 0\r\n  openCount= 1\r\n")
	got, err = replaceScalarSetting(device, "fps", 1)
	if err != nil {
		t.Fatal(err)
	}
	want = []byte("class device:\r\n  fps= 1\r\n  openCount= 1\r\n")
	if !bytes.Equal(got, want) {
		t.Fatalf("updated device config = %q, want %q", got, want)
	}
}

package itemcatalog

import (
	"bytes"
	"encoding/binary"
	"image/gif"
	"image/png"
	"testing"
)

func TestDecodeDIMGFormat3RGB565AndAlpha(t *testing.T) {
	data := testDIMGResource(3, 2, 1, []testDIMGFrame{{x: 1, y: 1, mode: 3, width: 2, height: 1, pixels: []byte{0x00, 0xF8, 0x1F, 0x00, 32, 16}}})
	decoded, err := DecodeDIMG(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.RGBAAt(1, 1); got.R != 255 || got.G != 0 || got.B != 0 || got.A != 255 {
		t.Fatalf("red pixel = %+v", got)
	}
	if got := decoded.RGBAAt(2, 1); got.R != 0 || got.G != 0 || got.B != 255 || got.A < 127 || got.A > 128 {
		t.Fatalf("blue pixel = %+v", got)
	}
	if pngData, err := DIMGToPNG(data); err != nil || len(pngData) < 8 || string(pngData[1:4]) != "PNG" {
		t.Fatalf("DIMGToPNG length=%d err=%v", len(pngData), err)
	}
}

func TestDecodeDIMGRejectsInvalidFrameBounds(t *testing.T) {
	data := testDIMGResource(1, 1, 1, []testDIMGFrame{{x: 1, mode: 3, width: 1, height: 1, pixels: []byte{0, 0, 32}}})
	if _, err := DecodeDIMG(data); err == nil {
		t.Fatal("out-of-bounds DIMG frame was accepted")
	}
}

func TestDecodeDIMGFrameAcceptsNegativeCompositionOrigin(t *testing.T) {
	data := testDIMGResource(100, 100, 1, []testDIMGFrame{{x: -5, y: 25, mode: 3, width: 1, height: 1, pixels: []byte{0xE0, 0x07, 32}}})
	if _, err := DecodeDIMG(data); err == nil {
		t.Fatal("strict canvas decoder accepted negative origin")
	}
	frame, err := DecodeDIMGFrame(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := frame.RGBAAt(0, 0); got.G != 255 || got.A != 255 {
		t.Fatalf("frame pixel = %+v", got)
	}
}

func TestDIMGToPreviewUsesFrameWhenDeclaredCanvasDoesNotContainIt(t *testing.T) {
	// Original UI icons can report a canvas equal to the bitmap while also
	// carrying non-zero frame_info_cx/cy. Those fields are animation anchors,
	// not a reason to reject the visible frame.
	pixels := make([]byte, 64*57*3)
	for index := 64 * 57 * 2; index < len(pixels); index++ {
		pixels[index] = 32
	}
	data := testDIMGResource(64, 57, 1, []testDIMGFrame{{x: 1, y: 3, mode: 3, width: 64, height: 57, pixels: pixels}})
	if _, err := DIMGToPNG(data); err == nil {
		t.Fatal("strict canvas decoder unexpectedly accepted overflowing frame")
	}
	preview, err := DIMGToPreview(data)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(preview.Data))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 64 || decoded.Bounds().Dy() != 57 || preview.ContentType != "image/png" {
		t.Fatalf("preview bounds/type = %v/%s", decoded.Bounds(), preview.ContentType)
	}
}

func TestDIMGToPreviewAcceptsVersion65537ContainerSuffix(t *testing.T) {
	data := testDIMGResource(1, 1, 1, []testDIMGFrame{{mode: 8, width: 1, height: 1, pixels: []byte{0, 0, 255, 255}}})
	binary.LittleEndian.PutUint32(data[8:12], 65537)
	data = append(data, 0x11, 0x22, 0x33, 0x44)
	preview, err := DIMGToPreview(data)
	if err != nil {
		t.Fatalf("version 65537 preview with official-style suffix: %v", err)
	}
	if preview.ContentType != "image/png" || preview.FrameCount != 1 {
		t.Fatalf("preview=%+v", preview)
	}

	legacy := append([]byte(nil), data...)
	binary.LittleEndian.PutUint32(legacy[8:12], 65536)
	if _, err = DIMGToPreview(legacy); err == nil {
		t.Fatal("version 65536 resource with a suffix was accepted")
	}
}

func TestDIMGToPreviewAcceptsZeroSizedEmptyDirection(t *testing.T) {
	data := testDIMGResource(100, 100, 2, []testDIMGFrame{
		{mode: 8, width: 1, height: 1, pixels: []byte{0, 0, 255, 255}},
		{mode: 8, width: 0, height: 0},
	})
	binary.LittleEndian.PutUint32(data[8:12], 65537)
	preview, err := DIMGToPreview(data)
	if err != nil {
		t.Fatalf("preview with an official-style empty direction: %v", err)
	}
	if preview.ContentType != "image/png" || preview.FrameCount != 1 {
		t.Fatalf("preview=%+v", preview)
	}
}

func TestDIMGToPreviewEncodesFirstDirectionAsAnimatedGIF(t *testing.T) {
	red := []byte{0x00, 0xF8, 32}
	blue := []byte{0x1F, 0x00, 32}
	data := testDIMGResource(1, 1, 2, []testDIMGFrame{
		{mode: 3, width: 1, height: 1, pixels: red},
		{mode: 3, width: 1, height: 1, pixels: blue},
		{mode: 3, width: 1, height: 1, pixels: blue},
		{mode: 3, width: 1, height: 1, pixels: red},
	})
	preview, err := DIMGToPreview(data)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ContentType != "image/gif" || preview.FrameCount != 2 {
		t.Fatalf("preview = %+v", preview)
	}
	decoded, err := gif.DecodeAll(bytes.NewReader(preview.Data))
	if err != nil || len(decoded.Image) != 2 || decoded.Delay[0] != 10 {
		t.Fatalf("GIF frames/delay = %d/%v, err=%v", len(decoded.Image), decoded.Delay, err)
	}
}

func TestDecodeDIMGPixelModes8And16(t *testing.T) {
	for _, test := range []struct {
		name   string
		mode   uint32
		pixels []byte
	}{
		{name: "BGRA", mode: 8, pixels: []byte{3, 2, 1, 4}},
		{name: "BGR", mode: 16, pixels: []byte{3, 2, 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := testDIMGResource(1, 1, 1, []testDIMGFrame{{mode: test.mode, width: 1, height: 1, pixels: test.pixels}})
			frame, err := DecodeDIMGFrame(data)
			if err != nil {
				t.Fatal(err)
			}
			got := frame.RGBAAt(0, 0)
			if got.R != 1 || got.G != 2 || got.B != 3 || test.mode == 8 && got.A != 4 || test.mode == 16 && got.A != 255 {
				t.Fatalf("pixel = %+v", got)
			}
		})
	}
}

type testDIMGFrame struct {
	x, y          int32
	mode          uint32
	width, height uint32
	pixels        []byte
}

func testDIMGResource(canvasWidth, canvasHeight uint32, directions uint32, frames []testDIMGFrame) []byte {
	var output bytes.Buffer
	header := make([]byte, dimgSpriteHeaderSize)
	copy(header, dimgMagic)
	binary.LittleEndian.PutUint32(header[8:12], 65536)
	binary.LittleEndian.PutUint32(header[12:16], 24)
	binary.LittleEndian.PutUint32(header[16:20], uint32(len(frames)))
	binary.LittleEndian.PutUint32(header[20:24], directions)
	binary.LittleEndian.PutUint32(header[32:36], canvasWidth)
	binary.LittleEndian.PutUint32(header[36:40], canvasHeight)
	output.Write(header)
	for _, frame := range frames {
		frameHeader := make([]byte, dimgFrameHeaderSize)
		binary.LittleEndian.PutUint32(frameHeader[4:8], uint32(frame.x))
		binary.LittleEndian.PutUint32(frameHeader[8:12], uint32(frame.y))
		binary.LittleEndian.PutUint32(frameHeader[12:16], frame.mode)
		output.Write(frameHeader)
		if frame.mode == 0 {
			continue
		}
		imageHeader := make([]byte, dimgImageHeaderSize)
		binary.LittleEndian.PutUint32(imageHeader[0:4], frame.width)
		binary.LittleEndian.PutUint32(imageHeader[4:8], frame.height)
		output.Write(imageHeader)
		output.Write(frame.pixels)
	}
	return output.Bytes()
}

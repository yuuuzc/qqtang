package itemcatalog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
)

const (
	dimgSpriteHeaderSize = 40
	dimgFrameHeaderSize  = 16
	dimgImageHeaderSize  = 12
	dimgMaxDimension     = 4096
	dimgMaxFrames        = 1024
)

var dimgMagic = []byte{'Q', 'Q', 'F', 0x1A, 'D', 'I', 'M', 'G'}

type dimgSprite struct {
	width, height int
	directions    int
	frames        []dimgFrame
}

type dimgFrame struct {
	x, y  int
	image *image.RGBA
}

// DIMGPreview is a browser-ready rendering of one DIMG direction. Animated
// resources are returned as GIF; a single visible frame is returned as PNG.
type DIMGPreview struct {
	Data        []byte
	ContentType string
	FrameCount  int
}

// ValidateDIMG verifies every declared frame and the bounds needed by the
// selected client rendering path without paying the cost of PNG/GIF encoding.
// The GM uses it to audit the restored catalog during service startup.
func ValidateDIMG(data []byte, tightFrame bool) error {
	sprite, err := parseDIMG(data)
	if err != nil {
		return err
	}
	if !tightFrame {
		frame, err := firstVisibleFrame(sprite.frames)
		if err != nil {
			return err
		}
		if frame.x < 0 || frame.y < 0 || frame.x+frame.image.Bounds().Dx() > sprite.width || frame.y+frame.image.Bounds().Dy() > sprite.height {
			return fmt.Errorf("invalid QQF/DIMG canvas %dx%d frame %d,%d %dx%d", sprite.width, sprite.height, frame.x, frame.y, frame.image.Bounds().Dx(), frame.image.Bounds().Dy())
		}
		return nil
	}
	framesPerDirection := len(sprite.frames) / sprite.directions
	bounds, visible := unionFrameBounds(sprite.frames[:framesPerDirection])
	if visible == 0 {
		return fmt.Errorf("QQF/DIMG direction has no visible frames")
	}
	if bounds.Dx() > dimgMaxDimension || bounds.Dy() > dimgMaxDimension {
		return fmt.Errorf("QQF/DIMG preview bounds %dx%d exceed limit", bounds.Dx(), bounds.Dy())
	}
	return nil
}

// DecodeDIMG converts the first visible frame into the resource's declared
// canvas. It is intentionally strict and is used for standalone UI icons.
func DecodeDIMG(data []byte) (*image.RGBA, error) {
	sprite, err := parseDIMG(data)
	if err != nil {
		return nil, err
	}
	frame, err := firstVisibleFrame(sprite.frames)
	if err != nil {
		return nil, err
	}
	if frame.x < 0 || frame.y < 0 || frame.x+frame.image.Bounds().Dx() > sprite.width || frame.y+frame.image.Bounds().Dy() > sprite.height {
		return nil, fmt.Errorf("invalid QQF/DIMG canvas %dx%d frame %d,%d %dx%d", sprite.width, sprite.height, frame.x, frame.y, frame.image.Bounds().Dx(), frame.image.Bounds().Dy())
	}
	result := image.NewRGBA(image.Rect(0, 0, sprite.width, sprite.height))
	draw.Draw(result, frame.image.Bounds().Add(image.Pt(frame.x, frame.y)), frame.image, frame.image.Bounds().Min, draw.Over)
	return result, nil
}

// DecodeDIMGFrame returns the first visible frame without applying the client
// composition origin. Avatar layers often have negative origins.
func DecodeDIMGFrame(data []byte) (*image.RGBA, error) {
	sprite, err := parseDIMG(data)
	if err != nil {
		return nil, err
	}
	frame, err := firstVisibleFrame(sprite.frames)
	if err != nil {
		return nil, err
	}
	return frame.image, nil
}

// DIMGToPreview renders only the first direction. Cycling all four character
// directions would look like an animation but would actually rotate the item.
// DIMG has no per-frame duration, so the GM preview uses a documented 100 ms
// display interval.
func DIMGToPreview(data []byte) (DIMGPreview, error) {
	sprite, err := parseDIMG(data)
	if err != nil {
		return DIMGPreview{}, err
	}
	framesPerDirection := len(sprite.frames) / sprite.directions
	frames := sprite.frames[:framesPerDirection]
	bounds, visible := unionFrameBounds(frames)
	if visible == 0 {
		return DIMGPreview{}, fmt.Errorf("QQF/DIMG direction has no visible frames")
	}
	if bounds.Dx() > dimgMaxDimension || bounds.Dy() > dimgMaxDimension {
		return DIMGPreview{}, fmt.Errorf("QQF/DIMG preview bounds %dx%d exceed limit", bounds.Dx(), bounds.Dy())
	}
	if len(frames) == 1 {
		canvas := composeDIMGFrame(frames[0], bounds)
		data, err := encodePNG(canvas)
		return DIMGPreview{Data: data, ContentType: "image/png", FrameCount: 1}, err
	}

	palette := make(color.Palette, 0, 256)
	palette = append(palette, color.RGBA{})
	for r := 0; r < 6; r++ {
		for g := 0; g < 6; g++ {
			for b := 0; b < 6; b++ {
				palette = append(palette, color.RGBA{uint8(r * 51), uint8(g * 51), uint8(b * 51), 255})
			}
		}
	}
	animation := &gif.GIF{LoopCount: 0}
	for _, frame := range frames {
		canvas := composeDIMGFrame(frame, bounds)
		paletted := image.NewPaletted(canvas.Bounds(), palette)
		draw.FloydSteinberg.Draw(paletted, canvas.Bounds(), canvas, image.Point{})
		animation.Image = append(animation.Image, paletted)
		animation.Delay = append(animation.Delay, 10)
		animation.Disposal = append(animation.Disposal, gif.DisposalBackground)
	}
	var output bytes.Buffer
	if err := gif.EncodeAll(&output, animation); err != nil {
		return DIMGPreview{}, fmt.Errorf("encode QQF/DIMG GIF: %w", err)
	}
	return DIMGPreview{Data: output.Bytes(), ContentType: "image/gif", FrameCount: len(frames)}, nil
}

func parseDIMG(data []byte) (dimgSprite, error) {
	if len(data) < dimgSpriteHeaderSize || !bytes.Equal(data[:len(dimgMagic)], dimgMagic) {
		return dimgSprite{}, fmt.Errorf("item icon is not a QQF/DIMG resource")
	}
	version := binary.LittleEndian.Uint32(data[8:12])
	frameInfoSize := binary.LittleEndian.Uint32(data[12:16])
	frameCount := int(binary.LittleEndian.Uint32(data[16:20]))
	directions := int(binary.LittleEndian.Uint32(data[20:24]))
	width := int(binary.LittleEndian.Uint32(data[32:36]))
	height := int(binary.LittleEndian.Uint32(data[36:40]))
	if version != 65536 && version != 65537 {
		return dimgSprite{}, fmt.Errorf("QQF/DIMG version %d is unsupported", version)
	}
	if frameInfoSize != 24 {
		return dimgSprite{}, fmt.Errorf("QQF/DIMG frame-info size %d is unsupported", frameInfoSize)
	}
	if frameCount < 1 || frameCount > dimgMaxFrames || directions < 1 || directions > 32 || frameCount%directions != 0 {
		return dimgSprite{}, fmt.Errorf("invalid QQF/DIMG frame/direction count %d/%d", frameCount, directions)
	}
	if width < 1 || height < 1 || width > dimgMaxDimension || height > dimgMaxDimension {
		return dimgSprite{}, fmt.Errorf("invalid QQF/DIMG canvas %dx%d", width, height)
	}

	offset := dimgSpriteHeaderSize
	frames := make([]dimgFrame, 0, frameCount)
	for index := 0; index < frameCount; index++ {
		if len(data)-offset < dimgFrameHeaderSize {
			return dimgSprite{}, fmt.Errorf("QQF/DIMG frame %d header is truncated", index)
		}
		if magic := binary.LittleEndian.Uint32(data[offset : offset+4]); magic != 0 {
			return dimgSprite{}, fmt.Errorf("QQF/DIMG frame %d magic %#x is unsupported", index, magic)
		}
		x := int(int32(binary.LittleEndian.Uint32(data[offset+4 : offset+8])))
		y := int(int32(binary.LittleEndian.Uint32(data[offset+8 : offset+12])))
		mode := binary.LittleEndian.Uint32(data[offset+12 : offset+16])
		offset += dimgFrameHeaderSize
		if mode == 0 {
			frames = append(frames, dimgFrame{x: x, y: y})
			continue
		}
		if len(data)-offset < dimgImageHeaderSize {
			return dimgSprite{}, fmt.Errorf("QQF/DIMG frame %d image header is truncated", index)
		}
		frameWidth := int(binary.LittleEndian.Uint32(data[offset : offset+4]))
		frameHeight := int(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		offset += dimgImageHeaderSize
		// Some official four-direction layers encode an absent direction as a
		// non-zero pixel mode followed by a 0x0 image header. The client treats
		// that direction as empty; it is distinct from a partially-zero or
		// truncated image, which remains invalid below.
		if frameWidth == 0 && frameHeight == 0 {
			frames = append(frames, dimgFrame{x: x, y: y})
			continue
		}
		if frameWidth < 1 || frameHeight < 1 || frameWidth > dimgMaxDimension || frameHeight > dimgMaxDimension {
			return dimgSprite{}, fmt.Errorf("invalid QQF/DIMG frame %d size %dx%d", index, frameWidth, frameHeight)
		}
		bytesPerPixel := 0
		switch mode {
		case 3, 0x11000000:
			bytesPerPixel = 3
		case 8:
			bytesPerPixel = 4
		case 16:
			bytesPerPixel = 3
		default:
			return dimgSprite{}, fmt.Errorf("QQF/DIMG frame %d pixel mode %#x is unsupported", index, mode)
		}
		pixels := frameWidth * frameHeight
		byteCount := pixels * bytesPerPixel
		if pixels/frameWidth != frameHeight || byteCount/bytesPerPixel != pixels || len(data)-offset < byteCount {
			return dimgSprite{}, fmt.Errorf("QQF/DIMG frame %d pixel data is truncated", index)
		}
		decoded := decodeDIMGPixels(data[offset:offset+byteCount], mode, frameWidth, frameHeight)
		offset += byteCount
		frames = append(frames, dimgFrame{x: x, y: y, image: decoded})
	}
	if offset != len(data) && version != 65537 {
		return dimgSprite{}, fmt.Errorf("QQF/DIMG has %d trailing bytes", len(data)-offset)
	}
	// Two verified official ItemZips contain version 65537, single-frame BGRA
	// icons whose declared frame closes before the file does. Client widgets
	// address the declared frame table and render those resources successfully;
	// the suffix is therefore a versioned container extension, not another
	// inferred frame. Version 65536 remains exact-length so corrupt legacy
	// resources are still rejected instead of silently accepted.
	return dimgSprite{width: width, height: height, directions: directions, frames: frames}, nil
}

func decodeDIMGPixels(data []byte, mode uint32, width, height int) *image.RGBA {
	result := image.NewRGBA(image.Rect(0, 0, width, height))
	pixels := width * height
	for index := 0; index < pixels; index++ {
		var pixel color.RGBA
		switch mode {
		case 3, 0x11000000:
			value := binary.LittleEndian.Uint16(data[index*2 : index*2+2])
			r5, g6, b5 := uint8(value>>11), uint8((value>>5)&0x3F), uint8(value&0x1F)
			a5 := data[pixels*2+index]
			if a5 > 32 {
				a5 = 32
			}
			pixel = color.RGBA{uint8(uint16(r5) * 255 / 31), uint8(uint16(g6) * 255 / 63), uint8(uint16(b5) * 255 / 31), uint8((uint16(a5)*255 + 16) / 32)}
		case 8:
			pixel = color.RGBA{data[index*4+2], data[index*4+1], data[index*4], data[index*4+3]}
		case 16:
			pixel = color.RGBA{data[index*3+2], data[index*3+1], data[index*3], 255}
		}
		result.SetRGBA(index%width, index/width, pixel)
	}
	return result
}

func firstVisibleFrame(frames []dimgFrame) (dimgFrame, error) {
	for _, frame := range frames {
		if frame.image != nil {
			return frame, nil
		}
	}
	return dimgFrame{}, fmt.Errorf("QQF/DIMG has no visible frames")
}

func unionFrameBounds(frames []dimgFrame) (image.Rectangle, int) {
	var result image.Rectangle
	visible := 0
	for _, frame := range frames {
		if frame.image == nil {
			continue
		}
		bounds := frame.image.Bounds().Add(image.Pt(frame.x, frame.y))
		if visible == 0 {
			result = bounds
		} else {
			result = result.Union(bounds)
		}
		visible++
	}
	return result, visible
}

func composeDIMGFrame(frame dimgFrame, bounds image.Rectangle) *image.RGBA {
	result := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	if frame.image != nil {
		target := frame.image.Bounds().Add(image.Pt(frame.x-bounds.Min.X, frame.y-bounds.Min.Y))
		draw.Draw(result, target, frame.image, frame.image.Bounds().Min, draw.Over)
	}
	return result
}

func encodePNG(value image.Image) ([]byte, error) {
	var output bytes.Buffer
	if err := png.Encode(&output, value); err != nil {
		return nil, fmt.Errorf("encode QQF/DIMG PNG: %w", err)
	}
	return output.Bytes(), nil
}

func DIMGToPNG(data []byte) ([]byte, error) {
	decoded, err := DecodeDIMG(data)
	if err != nil {
		return nil, err
	}
	return encodePNG(decoded)
}

func DIMGFrameToPNG(data []byte) ([]byte, error) {
	decoded, err := DecodeDIMGFrame(data)
	if err != nil {
		return nil, err
	}
	return encodePNG(decoded)
}

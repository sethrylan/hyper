package main

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestRenderIsDeterministic(t *testing.T) {
	frameDirectory := t.TempDir()
	for index, frame := range frames {
		frameImage := image.NewRGBA(image.Rect(0, 0, 4, 3))
		for y := range frameImage.Bounds().Dy() {
			for x := range frameImage.Bounds().Dx() {
				frameImage.Set(x, y, color.RGBA{R: uint8(index * 20), G: uint8(x * 30), B: uint8(y * 40), A: 255})
			}
		}
		writePNG(t, filepath.Join(frameDirectory, frame.name), frameImage)
	}

	firstPath := filepath.Join(t.TempDir(), "first.gif")
	secondPath := filepath.Join(t.TempDir(), "second.gif")
	if err := render(firstPath, frameDirectory); err != nil {
		t.Fatal(err)
	}
	if err := render(secondPath, frameDirectory); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(secondPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("identical frames produced different GIF bytes")
	}

	decoded, err := gif.DecodeAll(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Image) != len(frames) {
		t.Fatalf("frame count = %d, want %d", len(decoded.Image), len(frames))
	}
	for index, frame := range frames {
		if decoded.Delay[index] != frame.delay {
			t.Fatalf("frame %d delay = %d, want %d", index, decoded.Delay[index], frame.delay)
		}
	}
}

func writePNG(t *testing.T, path string, value image.Image) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(file, value); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

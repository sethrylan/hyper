package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"sort"
)

var frames = []struct {
	delay int
	name  string
}{
	{name: "demo-frame-01.png", delay: 150},
	{name: "demo-frame-02.png", delay: 150},
	{name: "demo-frame-03.png", delay: 100},
	{name: "demo-frame-04.png", delay: 150},
	{name: "demo-frame-05.png", delay: 200},
	{name: "demo-frame-06.png", delay: 200},
	{name: "demo-frame-07.png", delay: 250},
	{name: "demo-frame-08.png", delay: 150},
}

func main() {
	if err := render("docs/demo.gif", "docs"); err != nil {
		fmt.Fprintf(os.Stderr, "render demo GIF: %v\n", err)
		os.Exit(1)
	}
}

func render(outputPath, frameDirectory string) error {
	sources := make([]image.Image, 0, len(frames))
	var bounds image.Rectangle
	for index, frame := range frames {
		framePath := filepath.Join(frameDirectory, frame.name)
		source, err := decodePNG(framePath)
		if err != nil {
			return err
		}
		if index == 0 {
			bounds = source.Bounds()
		} else if source.Bounds() != bounds {
			return fmt.Errorf("frame %s has bounds %v, want %v", framePath, source.Bounds(), bounds)
		}
		sources = append(sources, source)
	}

	animation := gif.GIF{LoopCount: 0}
	framePalette := frequentColorPalette(sources, 256)
	for index, source := range sources {
		paletted := image.NewPaletted(bounds, framePalette)
		draw.Draw(paletted, bounds, source, bounds.Min, draw.Src)
		animation.Image = append(animation.Image, paletted)
		animation.Delay = append(animation.Delay, frames[index].delay)
	}

	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, &animation); err != nil {
		return fmt.Errorf("encode animation: %w", err)
	}
	if err := os.WriteFile(outputPath, encoded.Bytes(), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", outputPath, err)
	}
	return nil
}

func frequentColorPalette(images []image.Image, limit int) color.Palette {
	counts := map[uint32]int{}
	for _, source := range images {
		bounds := source.Bounds()
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				r, g, b, a := source.At(x, y).RGBA()
				key := (r>>8)<<24 | (g>>8)<<16 | (b>>8)<<8 | a>>8
				counts[key]++
			}
		}
	}

	type colorCount struct {
		count int
		value uint32
	}
	values := make([]colorCount, 0, len(counts))
	for value, count := range counts {
		values = append(values, colorCount{count: count, value: value})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].count == values[j].count {
			return values[i].value < values[j].value
		}
		return values[i].count > values[j].count
	})

	selected := make([]colorCount, 0, min(limit, len(values)))
	seen := map[uint32]struct{}{}
	for _, value := range values {
		if len(selected) == min(128, limit) {
			break
		}
		if colorChroma(value.value) < 24 {
			continue
		}
		selected = append(selected, value)
		seen[value.value] = struct{}{}
	}
	for _, value := range values {
		if len(selected) == limit {
			break
		}
		if _, ok := seen[value.value]; ok {
			continue
		}
		selected = append(selected, value)
	}

	result := make(color.Palette, 0, max(2, len(selected)))
	for _, value := range selected {
		var channels [4]byte
		binary.BigEndian.PutUint32(channels[:], value.value)
		result = append(result, color.RGBA{
			R: channels[0],
			G: channels[1],
			B: channels[2],
			A: channels[3],
		})
	}
	for len(result) < 2 {
		result = append(result, color.RGBA{A: 255})
	}
	return result
}

func colorChroma(value uint32) uint8 {
	var channels [4]byte
	binary.BigEndian.PutUint32(channels[:], value)
	return max(channels[0], channels[1], channels[2]) - min(channels[0], channels[1], channels[2])
}

func decodePNG(path string) (image.Image, error) {
	file, err := os.Open(path) //nolint:gosec // Paths come from the checked-in frame list and trusted render script.
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = file.Close() }()

	decoded, err := png.Decode(file)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}
	return decoded, nil
}

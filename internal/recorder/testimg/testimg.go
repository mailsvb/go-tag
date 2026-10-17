// Package testimg makes tiny cover images for tests.
package testimg

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
)

func img(c color.Color) image.Image {
	m := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			m.Set(x, y, c)
		}
	}
	return m
}

// JPEG returns an 8x8 JPEG.
func JPEG() []byte {
	var b bytes.Buffer
	jpeg.Encode(&b, img(color.RGBA{200, 30, 30, 255}), nil)
	return b.Bytes()
}

// PNG returns an 8x8 PNG.
func PNG() []byte {
	var b bytes.Buffer
	png.Encode(&b, img(color.RGBA{30, 30, 200, 255}))
	return b.Bytes()
}

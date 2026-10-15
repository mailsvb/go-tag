package library

import (
	"bytes"
	"image"
	_ "image/gif"  // register decoder
	_ "image/jpeg" // register decoder
	_ "image/png"  // register decoder

	"golang.org/x/image/draw"

	"github.com/mailsvb/go-tag/internal/tags"
)

// ThumbSize is the edge length in pixels of cover thumbnails.
const ThumbSize = 96

// Thumbnail decodes a cover and scales it to fit into size×size pixels.
// It returns nil for a missing or undecodable cover.
func Thumbnail(p *tags.Picture, size int) image.Image {
	if p == nil || len(p.Data) == 0 {
		return nil
	}
	src, _, err := image.Decode(bytes.NewReader(p.Data))
	if err != nil {
		return nil
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return nil
	}
	if w > h {
		w, h = size, max(1, h*size/w)
	} else {
		w, h = max(1, w*size/h), size
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

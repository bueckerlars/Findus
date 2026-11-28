package service

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"

	nativewebp "github.com/HugoSmits86/nativewebp"
	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"
)

const (
	maxImageWidth = 1200
	thumbSize     = 480 // largest square slot in the UI (~180 CSS px) on a high-DPI screen
)

const maxImagePixels = 50_000_000 // about 200 MB decoded

var allowedImageFormats = map[string]bool{"jpeg": true, "png": true, "webp": true, "gif": true}

// decodeSlots bounds how many uploads are decoded at once, and with that the memory they need.
var decodeSlots = make(chan struct{}, 2)

// ItemThumbnailPath is where the cached thumbnail of an item photo lives.
// It is a subdirectory of images/, which backups skip: thumbnails can be regenerated.
func ItemThumbnailPath(dataDir, itemID string) string {
	return filepath.Join(dataDir, "images", "thumbs", itemID+".jpg")
}

// EnsureThumbnail writes a square JPEG thumbnail of src to dst unless it is already current.
// The thumbnail carries the mtime of its source, so any replacement of the photo invalidates it.
func EnsureThumbnail(src, dst string) error {
	si, err := os.Stat(src)
	if err != nil {
		return err
	}
	if di, err := os.Stat(dst); err == nil && di.ModTime().Equal(si.ModTime()) {
		return nil
	}
	img, err := imaging.Open(src)
	if err != nil {
		return err
	}
	side := min(thumbSize, img.Bounds().Dx(), img.Bounds().Dy())
	thumb := imaging.Fill(img, side, side, imaging.Center, imaging.Lanczos)
	// JPEG has no alpha: put transparent photos on white instead of black.
	thumb = imaging.Overlay(imaging.New(side, side, color.White), thumb, image.Point{}, 1)

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// Unique temp file: parallel requests may build the same thumbnail.
	tmp, err := os.CreateTemp(filepath.Dir(dst), "*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	err = jpeg.Encode(tmp, thumb, &jpeg.Options{Quality: 78})
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if err := os.Chtimes(tmp.Name(), si.ModTime(), si.ModTime()); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

// EncodeWebPFromImage reads an image stream, resizes to max width, and encodes WebP.
func EncodeWebPFromImage(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	// The imaging library also decodes TIFF and BMP; a crafted TIFF panics in one of its goroutines,
	// which no recover can catch. Check the format and size before decoding.
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if !allowedImageFormats[format] {
		return nil, fmt.Errorf("unsupported image format %q (use JPEG, PNG, WebP or GIF)", format)
	}
	if int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return nil, fmt.Errorf("image too large (max %d megapixels)", maxImagePixels/1_000_000)
	}
	decodeSlots <- struct{}{}
	defer func() { <-decodeSlots }()
	img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return nil, err
	}
	if img.Bounds().Dx() > maxImageWidth {
		img = imaging.Resize(img, maxImageWidth, 0, imaging.Lanczos)
	}
	var buf bytes.Buffer
	if err := nativewebp.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

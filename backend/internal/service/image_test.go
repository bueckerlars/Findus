package service

import (
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	nativewebp "github.com/HugoSmits86/nativewebp"
	"github.com/disintegration/imaging"
	"github.com/stretchr/testify/require"
)

func TestEnsureThumbnail(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "a.webp"), ItemThumbnailPath(dir, "a")
	write := func(w, h int, mtime time.Time) {
		f, err := os.Create(src)
		require.NoError(t, err)
		require.NoError(t, nativewebp.Encode(f, image.NewNRGBA(image.Rect(0, 0, w, h))))
		require.NoError(t, f.Close())
		require.NoError(t, os.Chtimes(src, mtime, mtime))
	}
	size := func() image.Point {
		img, err := imaging.Open(dst)
		require.NoError(t, err)
		return img.Bounds().Size()
	}

	write(1200, 800, time.Unix(2000, 0))
	require.NoError(t, EnsureThumbnail(src, dst))
	require.Equal(t, image.Pt(thumbSize, thumbSize), size())

	// Fully transparent input must come out white, not black.
	img, err := imaging.Open(dst)
	require.NoError(t, err)
	r, _, _, _ := img.At(0, 0).RGBA()
	require.Greater(t, r, uint32(0xf000))

	// A replaced photo (even with an older mtime) rebuilds it; small photos are not upscaled.
	write(100, 200, time.Unix(1000, 0))
	require.NoError(t, EnsureThumbnail(src, dst))
	require.Equal(t, image.Pt(100, 100), size())

	require.Error(t, EnsureThumbnail(filepath.Join(dir, "missing.webp"), dst))
}

package core

// User-chosen background images. Imported images are validated, shrunk to at most
// 2560x1440 and stored as JPEG in Java4U's own folder, so they load fast and never
// depend on the original file staying where it was.

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	// Formats accepted for import.
	_ "image/gif"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	maxSourceBytes  = 64 << 20
	maxSourcePixels = 100_000_000
	importMaxW      = 2560
	importMaxH      = 1440
	jpegQuality     = 90
)

func validWallpaperName(name string) bool {
	return name != "" &&
		!strings.HasPrefix(name, ".") &&
		!strings.ContainsAny(name, `/\`) &&
		!strings.Contains(name, "..") &&
		strings.HasSuffix(name, ".jpg")
}

// ListWallpapers returns the names of all imported wallpapers, sorted.
func ListWallpapers(dirs Dirs) []string {
	out := []string{}
	entries, err := os.ReadDir(dirs.WallpapersDir())
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() && validWallpaperName(e.Name()) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// WallpaperPath is the full path of a stored wallpaper; it rejects names that could
// point outside the folder.
func WallpaperPath(dirs Dirs, name string) (string, error) {
	if !validWallpaperName(name) {
		return "", errors.New("invalid wallpaper name")
	}
	return filepath.Join(dirs.WallpapersDir(), name), nil
}

// ImportWallpaperFile imports a png/jpg/webp/gif/bmp file. Returns the stored file name.
func ImportWallpaperFile(dirs Dirs, src string) (string, error) {
	st, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", errors.New("not a file")
	}
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return ImportWallpaper(dirs, filepath.Base(src), f)
}

// ImportWallpaper imports image data read from r. filename is only used to pick a name.
func ImportWallpaper(dirs Dirs, filename string, r io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxSourceBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > maxSourceBytes {
		return "", errors.New("image is larger than 64 MB")
	}
	// Check the pixel count before decoding, so a tiny file cannot claim a huge image.
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("image error: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxSourcePixels {
		return "", errors.New("image dimensions are too large")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("image error: %w", err)
	}

	b := src.Bounds()
	w, h := fitWithin(b.Dx(), b.Dy(), importMaxW, importMaxH)
	var img image.Image = src
	if w != b.Dx() || h != b.Dy() {
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
		img = dst
	}

	stem := sanitizeStem(strings.TrimSuffix(filename, filepath.Ext(filename)))
	if err := os.MkdirAll(dirs.WallpapersDir(), 0o755); err != nil {
		return "", err
	}
	var name, dest string
	for n := 0; ; n++ {
		name = stem + ".jpg"
		if n > 0 {
			name = fmt.Sprintf("%s-%d.jpg", stem, n)
		}
		dest = filepath.Join(dirs.WallpapersDir(), name)
		if !exists(dest) {
			break
		}
	}

	out, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	if err := jpeg.Encode(out, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		out.Close()
		os.Remove(dest)
		return "", fmt.Errorf("image error: %w", err)
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	return name, nil
}

func RemoveWallpaper(dirs Dirs, name string) error {
	p, err := WallpaperPath(dirs, name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// fitWithin scales (w, h) down, keeping the aspect ratio, so it fits in (maxW, maxH).
func fitWithin(w, h, maxW, maxH int) (int, int) {
	if w <= maxW && h <= maxH {
		return w, h
	}
	s := math.Min(float64(maxW)/float64(w), float64(maxH)/float64(h))
	return max(1, int(math.Round(float64(w)*s))), max(1, int(math.Round(float64(h)*s)))
}

func sanitizeStem(s string) string {
	var b strings.Builder
	for _, c := range s {
		if b.Len() >= 40 {
			break
		}
		if c == '-' || c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "wallpaper"
	}
	return b.String()
}

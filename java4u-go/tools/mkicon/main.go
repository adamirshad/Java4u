// Command mkicon draws Java4U's app icon (the pixel goose from the web UI) as
// build/java4u.ico (16-256 px, for Windows), build/java4u-256.png and
// build/java4u.iconset (16-1024 px, for the macOS app).
//
//	go run ./tools/mkicon
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
)

// The goose SVG is a grid of 1x1 rects; its viewBox is "-1 -1 18 20". The icon is a
// 20x20-unit square from (-2,-1) with a rounded dark-green tile behind the goose.
const (
	minX, minY, span = -2.0, -1.0, 20.0
	radius           = 3.5
)

var tile = color.NRGBA{0x1d, 0x4a, 0x25, 0xff}

type pixel struct {
	x, y int
	c    color.NRGBA
}

func main() {
	svg, err := os.ReadFile(filepath.Join("internal", "gui", "web", "goose.svg"))
	if err != nil {
		log.Fatal(err)
	}
	re := regexp.MustCompile(`<rect x="(-?\d+)" y="(-?\d+)" width="1" height="1" fill="#([0-9a-f]{6})"/>`)
	var goose []pixel
	for _, m := range re.FindAllStringSubmatch(string(svg), -1) {
		x, _ := strconv.Atoi(m[1])
		y, _ := strconv.Atoi(m[2])
		v, _ := strconv.ParseUint(m[3], 16, 32)
		goose = append(goose, pixel{x, y, color.NRGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xff}})
	}
	if len(goose) < 50 {
		log.Fatalf("found only %d goose pixels in goose.svg", len(goose))
	}
	grid := map[[2]int]color.NRGBA{}
	for _, p := range goose {
		grid[[2]int{p.x, p.y}] = p.c
	}

	sizes := []int{16, 24, 32, 48, 64, 128, 256}
	var pngs [][]byte
	for _, s := range sizes {
		var buf bytes.Buffer
		img := render(s, grid)
		if err := png.Encode(&buf, img); err != nil {
			log.Fatal(err)
		}
		pngs = append(pngs, buf.Bytes())
	}
	os.MkdirAll("build", 0o755)
	if err := os.WriteFile(filepath.Join("build", "java4u.ico"), ico(sizes, pngs), 0o644); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("build", "java4u-256.png"), pngs[len(pngs)-1], 0o644); err != nil {
		log.Fatal(err)
	}

	// macOS: an .iconset folder, which `iconutil -c icns` turns into java4u.icns.
	set := filepath.Join("build", "java4u.iconset")
	os.MkdirAll(set, 0o755)
	for _, s := range []int{16, 32, 128, 256, 512} {
		for _, scale := range []int{1, 2} {
			name := fmt.Sprintf("icon_%dx%d.png", s, s)
			if scale == 2 {
				name = fmt.Sprintf("icon_%dx%d@2x.png", s, s)
			}
			var buf bytes.Buffer
			if err := png.Encode(&buf, render(s*scale, grid)); err != nil {
				log.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(set, name), buf.Bytes(), 0o644); err != nil {
				log.Fatal(err)
			}
		}
	}
	fmt.Println("wrote build/java4u.ico, build/java4u-256.png and build/java4u.iconset")
}

// render draws the icon at size px, sampling 4x4 points per pixel for smooth tile corners.
func render(size int, grid map[[2]int]color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 4
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					u := minX + (float64(px)+(float64(sx)+0.5)/ss)*span/float64(size)
					v := minY + (float64(py)+(float64(sy)+0.5)/ss)*span/float64(size)
					c, ok := grid[[2]int{int(math.Floor(u)), int(math.Floor(v))}]
					if !ok {
						if !inTile(u, v) {
							continue
						}
						c = tile
					}
					r += float64(c.R)
					g += float64(c.G)
					b += float64(c.B)
					a++
				}
			}
			if a == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(255 * a / ss / ss)})
		}
	}
	return img
}

// inTile reports whether (u, v) is inside the rounded square.
func inTile(u, v float64) bool {
	x, y := u-minX, v-minY
	cx := math.Max(radius-x, math.Max(0, x-(span-radius)))
	cy := math.Max(radius-y, math.Max(0, y-(span-radius)))
	if x < radius || x > span-radius {
		if y < radius || y > span-radius {
			return cx*cx+cy*cy <= radius*radius
		}
	}
	return x >= 0 && y >= 0 && x <= span && y <= span
}

// ico packs PNG images into a Windows .ico file.
func ico(sizes []int, pngs [][]byte) []byte {
	var buf bytes.Buffer
	binary.Write(&buf, binary.LittleEndian, [3]uint16{0, 1, uint16(len(pngs))})
	offset := 6 + 16*len(pngs)
	for i, data := range pngs {
		dim := uint8(sizes[i])
		if sizes[i] >= 256 {
			dim = 0 // 0 means 256
		}
		buf.Write([]byte{dim, dim, 0, 0})
		binary.Write(&buf, binary.LittleEndian, uint16(1))  // planes
		binary.Write(&buf, binary.LittleEndian, uint16(32)) // bits per pixel
		binary.Write(&buf, binary.LittleEndian, uint32(len(data)))
		binary.Write(&buf, binary.LittleEndian, uint32(offset))
		offset += len(data)
	}
	for _, data := range pngs {
		buf.Write(data)
	}
	return buf.Bytes()
}

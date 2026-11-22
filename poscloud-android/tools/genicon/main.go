// Command genicon draws Icon.png — the POSCloud app icon for Android
// (a blue gradient square with a white cloud and sync arrows).
// The repository stores only text files, so instead of a PNG this file holds
// the icon's vector geometry traced from the 1024x1024 design mockup;
// the image is reproduced deterministically with 4x supersampling.
// Usage: go run ./tools/genicon [path/Icon.png].
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

const (
	size = 1024 // image size
	ss   = 4    // supersampling: 4x4 samples per pixel
)

// Geometry from the mockup: background gradient, rounded square, white shapes.
var (
	geoTop    = [3]float64{59, 130, 245}
	geoBottom = [3]float64{37, 99, 235}
	geoRect   = [4]float64{0, 0, 1023, 1023} // x0, y0, x1, y1
	geoRadius = float64(176)

	// geoPoly — outlines of the white shapes: the cloud body with sync arcs
	// and two arrows. Nesting (if any appears) is resolved with the even-odd
	// rule, as in SVG fill-rule="evenodd".
	geoPoly = [][][2]float64{
		{{252, 757}, {125, 784}, {204, 872}},
		{{215, 342}, {191, 393}, {182, 421}, {173, 465}, {170, 498}, {170, 525}, {174, 565}, {182, 602}, {194, 638}, {213, 678}, {243, 723}, {260, 743}, {289, 771}, {312, 789}, {340, 807}, {362, 768}, {340, 754}, {317, 736}, {278, 695}, {252, 656}, {239, 630}, {226, 594}, {219, 564}, {215, 532}, {216, 480}, {227, 426}, {240, 391}, {254, 364}},
		{{827, 380}, {803, 333}, {771, 289}, {728, 247}, {683, 216}, {661, 255}, {683, 269}, {707, 288}, {735, 316}, {754, 340}, {770, 365}, {784, 393}, {799, 436}, {798, 440}, {780, 430}, {766, 425}, {747, 421}, {725, 420}, {714, 389}, {696, 359}, {671, 333}, {649, 318}, {627, 308}, {604, 302}, {588, 300}, {555, 302}, {520, 313}, {505, 321}, {487, 334}, {461, 362}, {442, 396}, {422, 388}, {397, 382}, {360, 380}, {342, 382}, {314, 389}, {293, 398}, {275, 409}, {260, 421}, {247, 435}, {236, 451}, {227, 470}, {220, 502}, {220, 517}, {224, 540}, {232, 561}, {240, 574}, {243, 599}, {250, 613}, {266, 629}, {287, 638}, {778, 639}, {779, 641}, {769, 659}, {808, 681}, {819, 661}, {836, 617}, {847, 596}, {853, 528}, {853, 495}, {849, 457}, {840, 417}},
		{{898, 239}, {820, 151}, {771, 267}},
	}
)

// polygon bounding box for quick clipping.
type bbox struct{ x0, y0, x1, y1 float64 }

var boxes []bbox

func init() {
	for _, poly := range geoPoly {
		b := bbox{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		for _, p := range poly {
			b.x0 = math.Min(b.x0, p[0])
			b.y0 = math.Min(b.y0, p[1])
			b.x1 = math.Max(b.x1, p[0])
			b.y1 = math.Max(b.y1, p[1])
		}
		boxes = append(boxes, b)
	}
}

// inRoundRect reports whether the point lies inside the background's rounded square.
func inRoundRect(px, py float64) bool {
	x0, y0, x1, y1 := geoRect[0], geoRect[1], geoRect[2], geoRect[3]
	r := geoRadius
	cx := math.Min(math.Max(px, x0+r), x1-r)
	cy := math.Min(math.Max(py, y0+r), y1-r)
	dx, dy := px-cx, py-cy
	return dx*dx+dy*dy <= r*r
}

// inShapes checks the even-odd rule for all outlines at once.
func inShapes(px, py float64) bool {
	cross := 0
	for i, poly := range geoPoly {
		b := boxes[i]
		if px < b.x0 || px > b.x1 || py < b.y0 || py > b.y1 {
			continue
		}
		n := len(poly)
		for cur, prev := 0, n-1; cur < n; prev, cur = cur, cur+1 {
			xi, yi := poly[cur][0], poly[cur][1]
			xj, yj := poly[prev][0], poly[prev][1]
			if (yi > py) != (yj > py) {
				if px < (xj-xi)*(py-yi)/(yj-yi)+xi {
					cross++
				}
			}
		}
	}
	return cross%2 == 1
}

func main() {
	out := "Icon.png"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var rs, gs, bs, cnt uint32
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/ss
					py := float64(y) + (float64(sy)+0.5)/ss
					if !inRoundRect(px, py) {
						continue // transparent corner
					}
					cnt++
					if inShapes(px, py) {
						rs, gs, bs = rs+255, gs+255, bs+255
					} else {
						t := py / (size - 1)
						rs += uint32(geoTop[0] + (geoBottom[0]-geoTop[0])*t)
						gs += uint32(geoTop[1] + (geoBottom[1]-geoTop[1])*t)
						bs += uint32(geoTop[2] + (geoBottom[2]-geoTop[2])*t)
					}
				}
			}
			if cnt == 0 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(rs / cnt),
				G: uint8(gs / cnt),
				B: uint8(bs / cnt),
				A: uint8(cnt * 255 / (ss * ss)),
			})
		}
	}
	f, err := os.Create(out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to create", out+":", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fmt.Fprintln(os.Stderr, "failed to write PNG:", err)
		os.Exit(1)
	}
	fmt.Println("icon written:", out)
}

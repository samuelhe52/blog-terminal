package main

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var mathInk = color.NRGBA{0xeb, 0xbc, 0xba, 0xff}

func TestMathImage(t *testing.T) {
	img, err := renderMathImage(`\frac{-b \pm \sqrt{b^2 - 4ac}}{2a}`, mathInk, 20)
	if err != nil {
		t.Fatal(err)
	}
	b := img.Bounds()
	if b.Min != (image.Point{}) || b.Dx() < 60 || b.Dy() < 30 || b.Dx() > 200 || b.Dy() > 100 {
		t.Fatalf("unexpected size %v", b)
	}
	// Tightly cropped: every edge touches ink.
	inkAt := func(x, y int) bool { _, _, _, a := img.At(x, y).RGBA(); return a != 0 }
	edges := map[string]bool{}
	solid := 0
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			if !inkAt(x, y) {
				continue
			}
			c := img.At(x, y).(color.NRGBA)
			if c.R != mathInk.R || c.G != mathInk.G || c.B != mathInk.B {
				t.Fatalf("pixel %d,%d is %v, not the ink color", x, y, c)
			}
			if c.A == 0xff {
				solid++
			}
			edges["left"] = edges["left"] || x == 0
			edges["right"] = edges["right"] || x == b.Dx()-1
			edges["top"] = edges["top"] || y == 0
			edges["bottom"] = edges["bottom"] || y == b.Dy()-1
		}
	}
	if len(edges) != 4 || !edges["left"] || !edges["right"] || !edges["top"] || !edges["bottom"] {
		t.Fatalf("not cropped to the ink: %v", edges)
	}
	if solid == 0 || solid == b.Dx()*b.Dy() {
		t.Fatalf("%d of %d pixels solid", solid, b.Dx()*b.Dy())
	}

	// A larger em gives a proportionally larger image.
	big, err := renderMathImage(`\frac{-b \pm \sqrt{b^2 - 4ac}}{2a}`, mathInk, 40)
	if err != nil {
		t.Fatal(err)
	}
	if ratio := float64(big.Bounds().Dx()) / float64(b.Dx()); ratio < 1.8 || ratio > 2.2 {
		t.Fatalf("doubling the em scaled the width by %.2f", ratio)
	}
}

func TestMathImageErrors(t *testing.T) {
	for name, tt := range map[string]struct {
		tex string
		px  float64
	}{
		"parse error": {`\frac{a`, 20},
		"empty":       {"  ", 20},
		"no ink":      {`\quad`, 20},
		"too long":    {strings.Repeat("x", maxMathBytes+1), 20},
		"too deep":    {strings.Repeat("{", maxMathDepth+1) + "x" + strings.Repeat("}", maxMathDepth+1), 20},
		"tiny em":     {"x", 1},
		"huge em":     {"x", 10000},
		"too large":   {`\begin{matrix}` + strings.Repeat(`x\\`, 300) + `\end{matrix}`, 256},
	} {
		if img, err := renderMathImage(tt.tex, mathInk, tt.px); err == nil {
			t.Errorf("%s: got a %v image, want an error", name, img.Bounds())
		}
	}
}

func TestMathImageSVGSubset(t *testing.T) {
	for _, svg := range []string{
		`<svg width="10" height="10"><circle r="3"/></svg>`,
		`<svg width="10" height="10"><g transform="scale(2)"><path d="M0 0L1 1Z"/></g></svg>`,
		`<svg width="10" height="10"><path d="m0 0l1 1z"/></svg>`,
		`<svg width="10" height="10"><path d="L1 1Z"/></svg>`,
		`<path d="M0 0L1 1Z"/>`,
	} {
		if _, err := rasterizeMathSVG(svg, 1); err == nil {
			t.Errorf("accepted %s", svg)
		}
	}
	mask, err := rasterizeMathSVG(`<svg width="4" height="4"><g transform="translate(1,1)"><path d="M0 0L2 0Q2 2 2 2C2 2 0 2 0 2"/><rect x="0" y="0" width="1" height="1"/></g></svg>`, 0)
	if err != nil {
		t.Fatal(err)
	}
	if mask.AlphaAt(0, 0).A != 0 || mask.AlphaAt(1, 1).A != 0xff || mask.AlphaAt(2, 2).A != 0xff || mask.AlphaAt(3, 3).A != 0 {
		t.Fatal("shapes drawn in the wrong place")
	}
}

func TestMathImageConcurrentAndDeterministic(t *testing.T) {
	tex := `\Lambda = \begin{bmatrix} \lambda_1 & 0 \\ 0 & \lambda_2 \end{bmatrix}`
	want, err := renderMathImage(tex, mathInk, 24)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := renderMathImage(tex, mathInk, 24)
			if err != nil {
				t.Error(err)
				return
			}
			if string(got.(*image.NRGBA).Pix) != string(want.(*image.NRGBA).Pix) {
				t.Error("rendering is not deterministic")
			}
		}()
	}
	wg.Wait()
}

func TestMathImageTallDelimiters(t *testing.T) {
	if _, err := renderMathImage(`\begin{pmatrix} 1 \\ 2 \\ 3 \end{pmatrix} \left\| \frac{a}{b} \right\|`, mathInk, 24); err != nil {
		t.Fatalf("delimiters with a ready-made size: %v", err)
	}
	for _, px := range []float64{12, 24, 48} {
		if _, err := renderMathImage(`\begin{bmatrix} 1 \\ 2 \\ 3 \\ 4 \\ 5 \end{bmatrix}`, mathInk, px); !errors.Is(err, errTallDelimiter) {
			t.Fatalf("assembled delimiter at %v px: got %v", px, err)
		}
	}
}

// TestCorpusMathImages draws every display formula in the fixture and, when
// available, the live posts. Formulas with a delimiter too tall to draw are
// logged; any other error fails. Set MATH_IMAGE_DIR to keep the PNGs.
func TestCorpusMathImages(t *testing.T) {
	dir := os.Getenv("MATH_IMAGE_DIR")
	for name, c := range corpora(t) {
		seen := map[string]bool{}
		var total, tall int
		var elapsed, slowest time.Duration
		for _, p := range c.Posts {
			for _, tex := range displayFormulas(preprocess(p.Body, p.URL)) {
				if seen[tex] {
					continue
				}
				seen[tex] = true
				total++
				var img image.Image
				for _, px := range []float64{32, 16} {
					start := time.Now()
					var err error
					img, err = renderMathImage(tex, mathInk, px)
					took := time.Since(start)
					elapsed += took
					slowest = max(slowest, took)
					if errors.Is(err, errTallDelimiter) {
						if px == 32 {
							tall++
						}
						t.Logf("%s %s/%s at %v px: %v: %.50q", name, p.Lang, p.Slug, px, err, tex)
					} else if err != nil {
						t.Errorf("%s %s/%s at %v px: %v\n%s", name, p.Lang, p.Slug, px, err, tex)
					}
				}
				if dir != "" && img != nil {
					f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s-%02d.png", name, total)))
					if err != nil {
						t.Fatal(err)
					}
					if err := png.Encode(f, img); err != nil {
						t.Fatal(err)
					}
					f.Close()
				}
			}
		}
		t.Logf("%s: %d distinct display formulas drawn at 16 and 32 px/em in %v (slowest %v), %d too tall to draw", name, total, elapsed, slowest, tall)
	}
}

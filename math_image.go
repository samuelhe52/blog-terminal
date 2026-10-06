package main

import (
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"strconv"
	"strings"
	"sync"

	"github.com/go-opentype/opentype"
	texmath "github.com/go-tex/math"
	"golang.org/x/image/vector"
)

// Display math can also be drawn as an image, for terminals that show
// graphics. go-tex/math typesets TeX with the OpenType MATH table of an
// embedded STIX Two Math font and returns SVG made of filled glyph outlines
// and rules; renderMathImage rasterizes that SVG itself. Everything is pure
// Go, so it needs no TeX installation.

// The renderer only holds the parsed font and a glyph-outline cache guarded
// by its own mutex, so one is shared by every session. The cache keeps one
// entry per glyph and pixel size used.
var mathImageRenderer = sync.OnceValues(func() (*texmath.Renderer, error) {
	return texmath.New(texmath.DefaultFont())
})

// errTallDelimiter reports a formula with a delimiter taller than the
// font's largest ready-made size, as around a matrix of four or more rows.
// go-tex/math v0.50.0 builds those from the font's glyph-assembly parts but
// stacks the parts in the wrong places, so the caller should show the
// formula as text instead.
var errTallDelimiter = errors.New("math image: delimiter too tall to draw")

var mathFont = sync.OnceValues(func() (*opentype.Font, error) {
	return opentype.Parse(texmath.DefaultFont())
})

// assemblyPaths caches, per pixel size, the outlines of the glyphs that only
// appear in an assembled delimiter (parts of a vertical assembly that aren't
// also a base glyph or one of its ready-made sizes), written as go-tex/math
// writes them into its SVG. Sizes are limited to minMathImageEm…
// maxMathImageEm, so the cache stays small.
var assemblyPaths sync.Map // int → []string

// assembledDelimiter reports whether svg contains an assembled delimiter.
func assembledDelimiter(svg string, px int) (bool, error) {
	paths, ok := assemblyPaths.Load(px)
	if !ok {
		font, err := mathFont()
		if err != nil {
			return false, err
		}
		face := font.NewFace(px)
		parts, sized := map[opentype.GlyphIndex]bool{}, map[opentype.GlyphIndex]bool{}
		for gid := range opentype.GlyphIndex(font.NumGlyphs()) {
			variants, asm := face.MathVariants(gid, true)
			if asm == nil {
				continue
			}
			sized[gid] = true
			for _, v := range variants {
				sized[v.Glyph] = true
			}
			for _, p := range asm.Parts {
				parts[p.Glyph] = true
			}
		}
		var list []string
		for gid := range parts {
			if d, ok := face.GlyphSVGPath(gid); ok && d != "" && !sized[gid] {
				list = append(list, `d="`+d+`"`)
			}
		}
		paths, _ = assemblyPaths.LoadOrStore(px, list)
	}
	for _, d := range paths.([]string) {
		if strings.Contains(svg, d) {
			return true, nil
		}
	}
	return false, nil
}

const (
	minMathImageEm     = 4
	maxMathImageEm     = 256
	maxMathImagePixels = 16 << 20
)

// renderMathImage typesets a display formula at pxHeightPerEm pixels per em
// (rounded to a whole pixel) and returns it cropped to its ink, with glyphs in
// fg on a transparent background. The image's origin is (0, 0). The result
// depends only on the arguments, and the function is safe for concurrent use.
func renderMathImage(tex string, fg color.Color, pxHeightPerEm float64) (img image.Image, err error) {
	px := int(math.Round(pxHeightPerEm))
	if px < minMathImageEm || px > maxMathImageEm {
		return nil, fmt.Errorf("math image: %v pixels per em is out of range", pxHeightPerEm)
	}
	if strings.TrimSpace(tex) == "" || len(tex) > maxMathBytes || mathDepth(tex) > maxMathDepth {
		return nil, errors.New("math image: formula is empty, too long, or nested too deeply")
	}
	r, err := mathImageRenderer()
	if err != nil {
		return nil, err
	}
	defer func() {
		if p := recover(); p != nil {
			img, err = nil, fmt.Errorf("math image: %v", p)
		}
	}()
	svg, _, err := r.RenderDisplaySVGMetrics(tex, px)
	if err != nil {
		return nil, fmt.Errorf("math image: %w", err)
	}
	if tall, err := assembledDelimiter(svg, px); err != nil {
		return nil, err
	} else if tall {
		return nil, errTallDelimiter
	}
	mask, err := rasterizeMathSVG(svg, px)
	if err != nil {
		return nil, err
	}
	return tint(mask, fg)
}

// rasterizeMathSVG draws the subset of SVG that go-tex/math writes: nested
// groups moved by translate(), filled paths of absolute M, L, Q, C, and Z
// commands, and filled rects. Anything else is an error rather than a
// silently wrong picture. Ink may overhang the typeset box (italic
// corrections, accents), so the canvas has a margin of one em on each side.
func rasterizeMathSVG(svg string, margin int) (*image.Alpha, error) {
	dec := xml.NewDecoder(strings.NewReader(svg))
	var z *vector.Rasterizer
	type offset struct{ x, y float64 }
	stack := []offset{{float64(margin), float64(margin)}}
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("math image: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			attr := map[string]string{}
			for _, a := range t.Attr {
				attr[a.Name.Local] = a.Value
			}
			at := stack[len(stack)-1]
			switch t.Name.Local {
			case "svg":
				if z != nil {
					return nil, errors.New("math image: nested svg")
				}
				w, errW := strconv.ParseFloat(attr["width"], 64)
				h, errH := strconv.ParseFloat(attr["height"], 64)
				if errW != nil || errH != nil || w < 0 || h < 0 {
					return nil, errors.New("math image: svg without a size")
				}
				cw, ch := int(math.Ceil(w))+2*margin, int(math.Ceil(h))+2*margin
				if cw*ch > maxMathImagePixels {
					return nil, errors.New("math image: formula is too large")
				}
				z = vector.NewRasterizer(cw, ch)
			case "g":
				if tf, ok := attr["transform"]; ok {
					dx, dy, err := parseTranslate(tf)
					if err != nil {
						return nil, err
					}
					at = offset{at.x + dx, at.y + dy}
				}
			case "path":
				if z == nil {
					return nil, errors.New("math image: path outside svg")
				}
				if err := addSVGPath(z, attr["d"], at.x, at.y); err != nil {
					return nil, err
				}
			case "rect":
				if z == nil {
					return nil, errors.New("math image: rect outside svg")
				}
				var v [4]float64
				for i, name := range []string{"x", "y", "width", "height"} {
					if v[i], err = strconv.ParseFloat(attr[name], 64); err != nil {
						return nil, fmt.Errorf("math image: rect %s: %w", name, err)
					}
				}
				x0, y0 := float32(at.x+v[0]), float32(at.y+v[1])
				x1, y1 := x0+float32(v[2]), y0+float32(v[3])
				z.MoveTo(x0, y0)
				z.LineTo(x1, y0)
				z.LineTo(x1, y1)
				z.LineTo(x0, y1)
				z.ClosePath()
			default:
				return nil, fmt.Errorf("math image: unexpected <%s>", t.Name.Local)
			}
			stack = append(stack, at)
		case xml.EndElement:
			if len(stack) == 1 {
				return nil, errors.New("math image: unbalanced svg")
			}
			stack = stack[:len(stack)-1]
		}
	}
	if z == nil {
		return nil, errors.New("math image: no svg")
	}
	mask := image.NewAlpha(z.Bounds())
	z.DrawOp = draw.Src
	z.Draw(mask, mask.Bounds(), image.Opaque, image.Point{})
	return mask, nil
}

func parseTranslate(tf string) (float64, float64, error) {
	args, ok := strings.CutPrefix(strings.TrimSpace(tf), "translate(")
	if args, ok = strings.CutSuffix(args, ")"); !ok {
		return 0, 0, fmt.Errorf("math image: unexpected transform %q", tf)
	}
	parts := strings.FieldsFunc(args, func(r rune) bool { return r == ',' || r == ' ' })
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("math image: unexpected transform %q", tf)
	}
	dx, errX := strconv.ParseFloat(parts[0], 64)
	dy, errY := strconv.ParseFloat(parts[1], 64)
	if errX != nil || errY != nil {
		return 0, 0, fmt.Errorf("math image: unexpected transform %q", tf)
	}
	return dx, dy, nil
}

// addSVGPath adds a path's subpaths to z, moved by (dx, dy). Like SVG's
// fill, every subpath is closed.
func addSVGPath(z *vector.Rasterizer, d string, dx, dy float64) error {
	points := map[byte]int{'M': 1, 'L': 1, 'Q': 2, 'C': 3, 'Z': 0}
	open := false
	for i := 0; i < len(d); {
		c := d[i]
		if c == ' ' || c == ',' || c == '\n' || c == '\t' {
			i++
			continue
		}
		n, ok := points[c]
		if !ok {
			return fmt.Errorf("math image: unexpected path command %q", c)
		}
		i++
		var p [6]float32
		for k := 0; k < 2*n; k++ {
			for i < len(d) && (d[i] == ' ' || d[i] == ',') {
				i++
			}
			end := i
			for end < len(d) && (strings.IndexByte("0123456789.eE", d[end]) >= 0 || ((d[end] == '-' || d[end] == '+') && (end == i || d[end-1] == 'e' || d[end-1] == 'E'))) {
				end++
			}
			v, err := strconv.ParseFloat(d[i:end], 64)
			if err != nil {
				return fmt.Errorf("math image: bad path number %q", d[i:end])
			}
			if k%2 == 0 {
				v += dx
			} else {
				v += dy
			}
			p[k] = float32(v)
			i = end
		}
		if c != 'M' && c != 'Z' && !open {
			return errors.New("math image: path segment before a move")
		}
		switch c {
		case 'M':
			if open {
				z.ClosePath()
			}
			z.MoveTo(p[0], p[1])
			open = true
		case 'L':
			z.LineTo(p[0], p[1])
		case 'Q':
			z.QuadTo(p[0], p[1], p[2], p[3])
		case 'C':
			z.CubeTo(p[0], p[1], p[2], p[3], p[4], p[5])
		case 'Z':
			if open {
				z.ClosePath()
			}
			open = false
		}
	}
	if open {
		z.ClosePath()
	}
	return nil
}

// tint crops the mask to its ink and paints it in fg, keeping fg's own
// transparency.
func tint(mask *image.Alpha, fg color.Color) (image.Image, error) {
	b := mask.Bounds()
	ink := image.Rectangle{Min: b.Max, Max: b.Min}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if mask.AlphaAt(x, y).A != 0 {
				ink.Min.X, ink.Max.X = min(ink.Min.X, x), max(ink.Max.X, x+1)
				ink.Min.Y, ink.Max.Y = min(ink.Min.Y, y), max(ink.Max.Y, y+1)
			}
		}
	}
	if ink.Empty() {
		return nil, errors.New("math image: formula has no ink")
	}
	c := color.NRGBAModel.Convert(fg).(color.NRGBA)
	out := image.NewNRGBA(image.Rect(0, 0, ink.Dx(), ink.Dy()))
	for y := 0; y < ink.Dy(); y++ {
		for x := 0; x < ink.Dx(); x++ {
			a := mask.AlphaAt(ink.Min.X+x, ink.Min.Y+y).A
			if a != 0 {
				out.SetNRGBA(x, y, color.NRGBA{c.R, c.G, c.B, uint8((uint32(a)*uint32(c.A) + 127) / 255)})
			}
		}
	}
	return out, nil
}

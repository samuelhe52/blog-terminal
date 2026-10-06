package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // first frame only
	_ "image/jpeg"
	"image/png"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Images are loaded from disk once at startup, downscaled, encoded as PNG, and
// shared read-only by every session. Nothing is fetched over the network.
const (
	maxImageFileBytes = 32 << 20   // larger files are skipped
	maxImagePixels    = 40_000_000 // checked before decoding
	maxImageSide      = 1280       // longest side kept after downscaling
	maxStoredImages   = 64 << 20   // base64 PNG kept for all images together
	svgTimeout        = 10 * time.Second
	svgZoom           = 2 // render SVGs at twice their size, for sharper text
)

// Layout. The pixel size of a cell is not queried: the terminal fits the
// image into its box keeping the aspect ratio, so assuming a 1:2 cell only
// risks a small margin, and the size depends on nothing but the width.
const (
	cellPixels   = 8  // assumed cell width in pixels, for an image's natural size
	cellAspect   = 2  // assumed cell height / width
	maxImageRows = 20 // so a tall image never fills more than a screen
	minImageCols = 8  // below this a caption is more useful than a picture
)

// sessionImageBudget bounds the decoded pixel data one session keeps in the
// terminal. Kitty's quota is 320 MB per screen; staying well under it means
// the terminal never evicts an image this session still shows.
const sessionImageBudget = 128 << 20

// imageAsset is a picture ready to send: a downscaled PNG in base64. Assets
// are immutable once registered.
type imageAsset struct {
	id            int // registry index, carried by markers in rendered text
	width, height int // pixels, as encoded
	cols          int // columns at the image's natural size
	data          string
}

// The registry only grows, and only during startup in practice. Sessions read
// it to resolve markers.
var imageRegistry struct {
	sync.RWMutex
	assets []*imageAsset
	bytes  int
}

var errImageBudget = errors.New("image memory budget exhausted")

// newImageAsset prepares img for display and registers it. cols is the width
// in cells at which the image looks right; 0 derives it from the pixel width.
// The asset can be placed in any article through imagePass.block, with the
// caller's fallback text after it. Typeset math, for example, can rasterize a
// formula to an image.Image and show it this way on supported terminals.
func newImageAsset(img image.Image, cols int) (*imageAsset, error) {
	b := img.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 {
		return nil, errors.New("empty image")
	}
	if cols <= 0 {
		cols = (b.Dx() + cellPixels - 1) / cellPixels
	}
	if side := max(b.Dx(), b.Dy()); side > maxImageSide {
		w := max(1, b.Dx()*maxImageSide/side)
		h := max(1, b.Dy()*maxImageSide/side)
		scaled := image.NewNRGBA(image.Rect(0, 0, w, h))
		draw.CatmullRom.Scale(scaled, scaled.Bounds(), img, b, draw.Src, nil)
		img = scaled
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		return nil, err
	}
	a := &imageAsset{width: img.Bounds().Dx(), height: img.Bounds().Dy(), cols: cols, data: base64.StdEncoding.EncodeToString(encoded.Bytes())}
	imageRegistry.Lock()
	defer imageRegistry.Unlock()
	if imageRegistry.bytes+len(a.data) > maxStoredImages {
		return nil, errImageBudget
	}
	imageRegistry.bytes += len(a.data)
	a.id = len(imageRegistry.assets)
	imageRegistry.assets = append(imageRegistry.assets, a)
	return a, nil
}

func lookupImage(id int) *imageAsset {
	imageRegistry.RLock()
	defer imageRegistry.RUnlock()
	if id < 0 || id >= len(imageRegistry.assets) {
		return nil
	}
	return imageRegistry.assets[id]
}

// loadImages finds the images each post refers to and loads the local ones.
// Relative sources resolve against the post's directory and must stay inside
// content; site-absolute ones (/images/a.png) resolve against assets, which
// may be empty. Remote URLs are never fetched. A missing or broken image is
// logged with warn and keeps its caption; it never stops startup.
func loadImages(c *catalog, content, assets string, warn func(string, ...any)) {
	contentRoot, err := imageRoot(content)
	if err != nil {
		warn("images: %v", err)
		return
	}
	assetsRoot := ""
	if assets != "" {
		if assetsRoot, err = imageRoot(assets); err != nil {
			warn("images: %v", err)
		}
	}
	rsvg, _ := exec.LookPath("rsvg-convert")
	loaded := make(map[string]*imageAsset)
	failed := make(map[string]bool)
	for _, p := range c.Posts {
		var sources []string
		preprocessWith(p.Body, p.URL, &imagePass{collect: func(src string) { sources = append(sources, src) }})
		for _, src := range sources {
			file, err := resolveImage(src, p.File, contentRoot, assetsRoot)
			if err != nil {
				warn("image %q in %s: %v", src, p.File, err)
				continue
			}
			if file == "" || failed[file] {
				continue
			}
			a := loaded[file]
			if a == nil {
				img, cols, err := decodeImageFile(file, rsvg)
				if err == nil {
					a, err = newImageAsset(img, cols)
				}
				if err != nil {
					warn("image %s: %v", file, err)
					failed[file] = true
					continue
				}
				loaded[file] = a
			}
			if p.Images == nil {
				p.Images = make(map[string]*imageAsset)
			}
			p.Images[src] = a
		}
	}
}

func imageRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(abs)
}

// resolveImage maps an image source to a file. It returns "" without an error
// for sources that are not local files.
func resolveImage(src, postFile, contentRoot, assetsRoot string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(src))
	if err != nil {
		return "", err
	}
	if u.Scheme != "" || u.Host != "" || u.Path == "" {
		return "", nil
	}
	root, file := contentRoot, ""
	if strings.HasPrefix(u.Path, "/") {
		if assetsRoot == "" {
			return "", nil
		}
		root, file = assetsRoot, filepath.Join(assetsRoot, filepath.FromSlash(u.Path))
	} else {
		post, err := filepath.Abs(postFile)
		if err != nil {
			return "", err
		}
		post, err = filepath.EvalSymlinks(post)
		if err != nil {
			return "", err
		}
		file = filepath.Join(filepath.Dir(post), filepath.FromSlash(u.Path))
	}
	// Check after resolving links, so neither .. nor a symlink leaves root.
	real, err := filepath.EvalSymlinks(file)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("outside %s", root)
	}
	return real, nil
}

// decodeImageFile reads a PNG, JPEG, GIF, or WebP file, or an SVG through
// rsvg-convert when it is installed. It returns the natural width in cells.
func decodeImageFile(file, rsvg string) (image.Image, int, error) {
	info, err := os.Stat(file)
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return nil, 0, errors.New("not a regular file")
	}
	if info.Size() > maxImageFileBytes {
		return nil, 0, fmt.Errorf("larger than %d MB", maxImageFileBytes>>20)
	}
	zoom := 1
	var data []byte
	if strings.EqualFold(filepath.Ext(file), ".svg") {
		if rsvg == "" {
			return nil, 0, errors.New("SVG needs rsvg-convert on PATH")
		}
		ctx, cancel := context.WithTimeout(context.Background(), svgTimeout)
		defer cancel()
		cmd := exec.CommandContext(ctx, rsvg, "--zoom", strconv.Itoa(svgZoom), "--format", "png", file)
		out, err := cmd.StdoutPipe()
		if err != nil {
			return nil, 0, err
		}
		if err := cmd.Start(); err != nil {
			return nil, 0, err
		}
		data, err = io.ReadAll(io.LimitReader(out, maxImageFileBytes+1))
		if waitErr := cmd.Wait(); err == nil {
			err = waitErr
		}
		if err != nil {
			return nil, 0, fmt.Errorf("rsvg-convert: %w", err)
		}
		if len(data) > maxImageFileBytes {
			return nil, 0, errors.New("rsvg-convert output too large")
		}
		zoom = svgZoom
	} else if data, err = os.ReadFile(file); err != nil {
		return nil, 0, err
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	if config.Width < 1 || config.Height < 1 || config.Width*config.Height > maxImagePixels {
		return nil, 0, fmt.Errorf("unsupported size %dx%d", config.Width, config.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	return img, (config.Width/zoom + cellPixels - 1) / cellPixels, nil
}

// Markers stand for an image block in rendered text. They survive Glamour as
// a one-line paragraph, and each session replaces them with its own
// placeholder cells after colors have been adapted to the client.
const (
	markerStart = "\uE000"
	markerEnd   = "\uE001"
)

func imageMarker(id int) string {
	return markerStart + strconv.Itoa(id) + markerEnd
}

// imagePass lets preprocess put drawn images in front of their captions. A
// nil pass, used when the terminal can't draw images, changes nothing.
type imagePass struct {
	width   int
	images  map[string]*imageAsset // a post's images, by source as written
	collect func(src string)       // set while loading, to list sources
}

// image returns the block for an image source, or "" to keep only the
// caption.
func (g *imagePass) image(src string) string {
	if g == nil {
		return ""
	}
	if g.collect != nil {
		g.collect(src)
		return ""
	}
	return g.block(g.images[src])
}

// block returns Markdown for a block image, to be followed by the caller's
// fallback text, or "" when the image can't be shown.
func (g *imagePass) block(a *imageAsset) string {
	if g == nil || a == nil || g.width < minImageCols+4 {
		return ""
	}
	return "\n\n" + imageMarker(a.id) + "\n\n"
}

// imageCells sizes an image to at most avail columns and maxImageRows rows,
// never larger than its natural size.
func imageCells(a *imageAsset, avail int) (cols, rows int) {
	cols = min(avail, a.cols, maxPlaceholderCells)
	rows = max(1, (cols*a.height+a.width*cellAspect/2)/(a.width*cellAspect))
	if rows > maxImageRows {
		rows = maxImageRows
		cols = max(1, min(cols, (rows*cellAspect*a.width+a.height/2)/a.height))
	}
	return cols, rows
}

// imageSession tracks the images one session has sent to its terminal. Ids
// are local to the session. When ids or the memory budget run out, the least
// recently shown images not on screen are deleted from the terminal.
type imageSession struct {
	shown   map[*imageAsset]*shownImage
	bytes   int
	clock   int
	pending strings.Builder // graphics commands not yet written
}

type shownImage struct {
	id, cols, rows, used, bytes int
}

// place replaces the markers in rendered text with placeholder cells, and
// queues the commands that send and size the images. Text without markers
// is returned unchanged.
func (s *imageSession) place(content string, width int) string {
	if !strings.Contains(content, markerStart) {
		return content
	}
	type spot struct {
		asset  *imageAsset
		prefix string
		ok     bool
	}
	lines := strings.Split(content, "\n")
	spots := make(map[int]spot)
	var order []*imageAsset
	size := make(map[*imageAsset][2]int)
	for i, line := range lines {
		plain := ansi.Strip(line)
		at := strings.Index(plain, markerStart)
		if at < 0 {
			continue
		}
		end := strings.Index(plain[at:], markerEnd)
		if end < 0 {
			continue
		}
		id, err := strconv.Atoi(plain[at+len(markerStart) : at+end])
		a := lookupImage(id)
		cells := ansi.StringWidth(plain[:at])
		margin := min(2, len(plain)-len(strings.TrimLeft(plain, " ")))
		avail := width - cells - margin
		if err != nil || a == nil || avail < minImageCols {
			spots[i] = spot{}
			continue
		}
		// Keep a quote bar or list indent, without styles leaking past it.
		prefix := strings.Repeat(" ", cells)
		if strings.TrimSpace(plain[:at]) != "" {
			prefix = ansi.Cut(line, 0, cells) + "\x1b[m"
		}
		spots[i] = spot{asset: a, prefix: prefix, ok: true}
		cols, rows := imageCells(a, avail)
		// One placement per image: repeats share the smallest size.
		if prev, seen := size[a]; !seen || cols < prev[0] {
			if !seen {
				order = append(order, a)
			}
			size[a] = [2]int{cols, rows}
		}
	}
	s.clock++
	ids := make(map[*imageAsset]int)
	for _, a := range order {
		ids[a] = s.show(a, size[a][0], size[a][1], size)
	}
	var out []string
	for i, line := range lines {
		sp, marked := spots[i]
		if !marked {
			out = append(out, line)
			continue
		}
		id := ids[sp.asset]
		if !sp.ok || id == 0 {
			continue // the caption that follows stands in for it
		}
		cols, rows := size[sp.asset][0], size[sp.asset][1]
		for r := range rows {
			out = append(out, sp.prefix+placeholderRow(id, r, cols))
		}
	}
	return strings.Join(out, "\n")
}

// show returns the session id for an image, sending it or resizing its
// placement as needed. It returns 0 if no id or budget is left.
func (s *imageSession) show(a *imageAsset, cols, rows int, current map[*imageAsset][2]int) int {
	if s.shown == nil {
		s.shown = make(map[*imageAsset]*shownImage)
	}
	if img := s.shown[a]; img != nil {
		img.used = s.clock
		if img.cols != cols || img.rows != rows {
			img.cols, img.rows = cols, rows
			s.pending.WriteString(placeImage(img.id, cols, rows))
		}
		return img.id
	}
	need := a.width * a.height * 4
	for len(s.shown) > lastImageID-firstImageID || s.bytes+need > sessionImageBudget {
		var victim *imageAsset
		for other, img := range s.shown {
			if _, onScreen := current[other]; !onScreen && (victim == nil || img.used < s.shown[victim].used) {
				victim = other
			}
		}
		if victim == nil {
			return 0
		}
		s.pending.WriteString(deleteImage(s.shown[victim].id))
		s.bytes -= s.shown[victim].bytes
		delete(s.shown, victim)
	}
	taken := make(map[int]bool, len(s.shown))
	for _, img := range s.shown {
		taken[img.id] = true
	}
	id := firstImageID
	for taken[id] {
		id++
	}
	s.shown[a] = &shownImage{id: id, cols: cols, rows: rows, used: s.clock, bytes: need}
	s.bytes += need
	s.pending.WriteString(transmitImage(id, a.data, cols, rows))
	return id
}

// flush returns a command writing the queued graphics commands, or nil.
func (s *imageSession) flush() tea.Cmd {
	if s.pending.Len() == 0 {
		return nil
	}
	raw := s.pending.String()
	s.pending.Reset()
	return tea.Raw(raw)
}

// imageModel adds terminal detection to a session's model. Images stay off
// until the terminal has named itself and accepted a test image, and
// startup never waits for either reply.
type imageModel struct{ model }

func withImages(m model) imageModel { return imageModel{m} }

// XTVERSION is a plain CSI query that terminals without it ignore.
func (m imageModel) Init() tea.Cmd {
	return tea.Batch(m.model.Init(), tea.RequestTerminalVersion)
}

func (m imageModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.TerminalVersionMsg:
		if placeholderTerminal(msg.Name) {
			return m, tea.Raw(graphicsQuery)
		}
		return m, nil
	case uv.KittyGraphicsEvent:
		if msg.Options.ID == probeID && string(msg.Payload) == "OK" && !m.cache.graphics {
			m.cache.graphics = true
			m.renderArticle(true)
		}
		return m, m.images.flush()
	}
	next, cmd := m.model.Update(msg)
	m.model = next.(model)
	return m, tea.Batch(cmd, m.images.flush())
}

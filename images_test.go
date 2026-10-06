package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	gossh "golang.org/x/crypto/ssh"
)

func writePNG(t *testing.T, file string, w, h int) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.NRGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// imageCatalog loads the fixture with its images, collecting warnings.
func imageCatalog(t *testing.T, assets string) (*catalog, []string) {
	t.Helper()
	c := fixtureCatalog(t)
	var warnings []string
	loadImages(c, "testdata/fixture", assets, func(format string, args ...any) {
		warnings = append(warnings, fmt.Sprintf(format, args...))
	})
	return c, warnings
}

func TestTransmitImageChunks(t *testing.T) {
	data := strings.Repeat("A", 2*kitty.MaxChunkSize+4)
	got := transmitImage(20, data, 30, 9)
	want := ansi.KittyGraphics([]byte(data[:kitty.MaxChunkSize]), "a=T", "f=100", "t=d", "i=20", "p=1", "U=1", "c=30", "r=9", "q=2", "m=1") +
		ansi.KittyGraphics([]byte(data[kitty.MaxChunkSize:2*kitty.MaxChunkSize]), "m=1") +
		ansi.KittyGraphics([]byte("AAAA"), "m=0")
	if got != want {
		t.Fatalf("chunks:\n%.200q", got)
	}
}

func TestPlaceholderTerminal(t *testing.T) {
	for name, want := range map[string]bool{
		"kitty(0.28.0)":      true,
		"kitty(0.35.2)":      true,
		"kitty(1.0.0)":       true,
		"kitty(0.27.1)":      false,
		"kitty":              false,
		"ghostty 1.1.3":      true,
		"WezTerm 20240203":   false,
		"tmux 3.4":           false,
		"iTerm2 3.5.0":       false,
		"xterm(390)":         false,
		"":                   false,
		"kitty(0.x)":         false,
		"Konsole 24.02":      false,
		"foot(1.17.2)":       false,
		"ghostty-not-really": true, // the name is the only signal
	} {
		if got := placeholderTerminal(name); got != want {
			t.Errorf("%q: got %v, want %v", name, got, want)
		}
	}
}

func TestResolveImage(t *testing.T) {
	root := t.TempDir()
	content := filepath.Join(root, "content")
	assets := filepath.Join(root, "public")
	writePNG(t, filepath.Join(content, "en", "imgs", "a.png"), 4, 4)
	writePNG(t, filepath.Join(assets, "images", "b.png"), 4, 4)
	writePNG(t, filepath.Join(root, "secret.png"), 4, 4)
	if err := os.Symlink(filepath.Join(root, "secret.png"), filepath.Join(content, "en", "link.png")); err != nil {
		t.Fatal(err)
	}
	contentRoot, _ := imageRoot(content)
	assetsRoot, _ := imageRoot(assets)
	post := filepath.Join(content, "zh", "post.md")
	if err := os.MkdirAll(filepath.Dir(post), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(post, nil, 0600); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		src, assets string
		want        string // file name, "" for none
		err         bool
	}{
		{"../en/imgs/a.png", assetsRoot, "a.png", false},
		{"../en/imgs/a.png?v=2#top", assetsRoot, "a.png", false},
		{"/images/b.png", assetsRoot, "b.png", false},
		{"/images/b.png", "", "", false},
		{"https://example.com/b.png", assetsRoot, "", false},
		{"//example.com/b.png", assetsRoot, "", false},
		{"data:image/png;base64,AAAA", assetsRoot, "", false},
		{"../../secret.png", assetsRoot, "", true},
		{"/../secret.png", assetsRoot, "", true},
		{"../en/link.png", assetsRoot, "", true},
		{"missing.png", assetsRoot, "", true},
	} {
		got, err := resolveImage(tt.src, post, contentRoot, tt.assets)
		if (err != nil) != tt.err || filepath.Base(got) != filepath.Base(tt.want) && tt.want != "" || (tt.want == "" && got != "") {
			t.Errorf("%q: got %q, %v", tt.src, got, err)
		}
	}
}

func TestLoadImages(t *testing.T) {
	c, warnings := imageCatalog(t, "")
	tips, _ := c.resolve("reference/tips-and-tricks", en)
	a := tips.Images["./imgs/pipeline.png"]
	if a == nil || a.width != 320 || a.height != 120 || a.cols != 40 {
		t.Fatalf("fixture image not loaded: %+v", a)
	}
	// Site-absolute images need --assets, so the fixture's SVG is skipped
	// silently without it.
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", warnings)
	}
	attention, _ := c.resolve("attention-notes", en)
	if len(attention.Images) != 0 {
		t.Fatal("site-absolute image loaded without --assets")
	}

	// With assets but no rasterizer, the SVG warns and keeps its caption.
	t.Setenv("PATH", t.TempDir())
	c, warnings = imageCatalog(t, "testdata/assets")
	attention, _ = c.resolve("attention-notes", en)
	if len(attention.Images) != 0 || len(warnings) != 1 || !strings.Contains(warnings[0], "rsvg-convert") {
		t.Fatalf("SVG without rsvg-convert: %v %v", attention.Images, warnings)
	}

	// A stand-in rsvg-convert proves SVGs go through it, and that both
	// translations share one asset.
	bin := t.TempDir()
	out := filepath.Join(bin, "out.png")
	writePNG(t, out, 128, 60)
	script := "#!/bin/sh\nexec /bin/cat '" + out + "'\n"
	if err := os.WriteFile(filepath.Join(bin, "rsvg-convert"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	c, warnings = imageCatalog(t, "testdata/assets")
	en, _ := c.resolve("attention-notes", en)
	zh, _ := c.resolve("attention-notes", zh)
	src := "/images/fixture/linear-attention.svg"
	if len(warnings) != 0 || en.Images[src] == nil || en.Images[src] != zh.Images[src] {
		t.Fatalf("SVG not shared: %v %v", warnings, en.Images)
	}
	if got := en.Images[src].cols; got != 128/svgZoom/cellPixels {
		t.Fatalf("SVG natural width %d", got)
	}
}

func TestLoadImagesSurvivesBrokenFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.CopyFS(root, os.DirFS("testdata/fixture")); err != nil {
		t.Fatal(err)
	}
	post := filepath.Join(root, "en", "broken.md")
	body := "---\ntitle: Broken\ndate: 2026-01-02\nlang: en\ntranslationSlug: broken\n---\n![a](bad.png)\n\n![b](missing.png)\n\n<img src=\"../../outside.png\">\n\n```md\n![c](code.png)\n```\n"
	if err := os.WriteFile(post, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "en", "bad.png"), []byte("not a png"), 0600); err != nil {
		t.Fatal(err)
	}
	writePNG(t, filepath.Join(filepath.Dir(root), "outside.png"), 4, 4)
	c, err := loadCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	var warnings []string
	loadImages(c, root, "", func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) })
	if len(warnings) != 3 {
		t.Fatalf("want warnings for bad, missing, and outside images (not code): %q", warnings)
	}
	p, _ := c.resolve("broken", en)
	if len(p.Images) != 0 {
		t.Fatalf("broken images loaded: %v", p.Images)
	}
}

func TestNewImageAssetDownscales(t *testing.T) {
	a, err := newImageAsset(image.NewNRGBA(image.Rect(0, 0, 4000, 1000)), 0)
	if err != nil {
		t.Fatal(err)
	}
	if a.width != maxImageSide || a.height != maxImageSide/4 || a.cols != 500 || lookupImage(a.id) != a {
		t.Fatalf("got %dx%d, %d cols", a.width, a.height, a.cols)
	}
}

func TestImageCells(t *testing.T) {
	wide := &imageAsset{width: 900, height: 300, cols: 113}
	tall := &imageAsset{width: 300, height: 900, cols: 38}
	small := &imageAsset{width: 64, height: 32, cols: 8}
	for _, tt := range []struct {
		a                 *imageAsset
		avail, cols, rows int
	}{
		{wide, 76, 76, 13},
		{wide, 200, 113, 19},
		{tall, 76, 13, 20},
		{small, 76, 8, 2},
		{wide, 8, 8, 1},
	} {
		cols, rows := imageCells(tt.a, tt.avail)
		if cols != tt.cols || rows != tt.rows {
			t.Errorf("%dx%d in %d: got %dx%d, want %dx%d", tt.a.width, tt.a.height, tt.avail, cols, rows, tt.cols, tt.rows)
		}
	}
}

func TestPreprocessImageBlocks(t *testing.T) {
	a := &imageAsset{id: 7}
	g := &imagePass{width: 80, images: map[string]*imageAsset{"a.png": a, "/b.png": a}}
	for _, tt := range []struct {
		name, source string
		blocks       int
	}{
		{"Markdown line", "![alt](a.png)\n", 1},
		{"Markdown with title", "  ![alt](<a.png> \"t\")\n", 1},
		{"HTML", `<img alt="x" src="/b.png">`, 1},
		{"inline Markdown", "see ![alt](a.png) here\n", 0},
		{"code", "```\n![alt](a.png)\n```\n`<img src=\"/b.png\">`", 0},
		{"unknown", "![alt](c.png)\n", 0},
	} {
		got := preprocessWith(tt.source, defaultSite.URL, g)
		if n := strings.Count(got, imageMarker(7)); n != tt.blocks {
			t.Errorf("%s: %d blocks in %q", tt.name, n, got)
		}
		// The caption and link follow the image, exactly as without one.
		if without := preprocess(tt.source, defaultSite.URL); !strings.Contains(got, strings.TrimSpace(without)) {
			t.Errorf("%s: fallback changed:\n%q\n%q", tt.name, got, without)
		}
	}
	if got := (&imagePass{width: 10, images: g.images}).image("a.png"); got != "" {
		t.Fatal("image block in a very narrow window")
	}
}

// Images draw only after detection, never below 256 colors, and the rest of
// the article stays as it was. Every line still fits.
func TestRenderWithImages(t *testing.T) {
	c, _ := imageCatalog(t, "")
	p, _ := c.resolve("reference/tips-and-tricks", en)
	placeholder := string(kitty.Placeholder)
	for _, style := range []string{"rose-pine", "rose-pine-dawn"} {
		for _, width := range []int{40, 60, 80, 120} {
			plain, err := (&renderCache{}).render(p, width, style, colorprofile.TrueColor)
			if err != nil {
				t.Fatal(err)
			}
			cache := &renderCache{graphics: true}
			marked, err := cache.render(p, width, style, colorprofile.TrueColor)
			if err != nil {
				t.Fatal(err)
			}
			s := &imageSession{}
			placed := s.place(marked, width)
			if !strings.Contains(placed, placeholder) || strings.Contains(placed, markerStart) {
				t.Fatalf("%s/%d: no placeholders", style, width)
			}
			if fitWidth(placed, width) != placed {
				t.Fatalf("%s/%d: placeholders changed by fitWidth", style, width)
			}
			for _, line := range strings.Split(placed, "\n") {
				if ansi.StringWidth(line) > width {
					t.Fatalf("%s/%d: line too wide: %q", style, width, line)
				}
			}
			var kept []string
			for _, line := range strings.Split(placed, "\n") {
				if !strings.Contains(line, placeholder) {
					kept = append(kept, ansi.Strip(line))
				}
			}
			if got := strings.Join(kept, "\n"); !strings.Contains(got, strings.TrimSpace(ansi.Strip(plain))[:40]) || !strings.Contains(got, "Pipeline diagram") {
				t.Fatalf("%s/%d: text around the image changed:\n%s", style, width, got)
			}
			if !strings.Contains(s.pending.String(), "a=T,f=100,t=d,i=16,p=1,U=1") {
				t.Fatalf("%s/%d: image not sent", style, width)
			}
		}
	}
	for _, profile := range []colorprofile.Profile{colorprofile.ANSI, colorprofile.ASCII} {
		out, _ := (&renderCache{graphics: true}).render(p, 80, "rose-pine", profile)
		if strings.Contains(out, markerStart) {
			t.Fatalf("image marker with %v", profile)
		}
	}
}

// The width checks of TestEveryPostWidth and TestEveryPostNeedsNoFinalGuard,
// with every loadable image drawn. Set BLOG_ASSETS_DIR to include the live
// site's /images.
func TestEveryPostWithImages(t *testing.T) {
	for name, c := range corpora(t) {
		dir := "testdata/fixture"
		if name != "fixture" {
			dir = defaultContentDir()
		}
		loadImages(c, dir, os.Getenv("BLOG_ASSETS_DIR"), t.Logf)
		for _, p := range c.Posts {
			if len(p.Images) == 0 {
				continue
			}
			for _, width := range []int{40, 60, 80, 120} {
				for _, style := range []string{"rose-pine", "rose-pine-dawn"} {
					cache := &renderCache{graphics: true}
					out, err := cache.render(p, width, style, colorprofile.TrueColor)
					if err != nil {
						t.Fatal(err)
					}
					out = (&imageSession{}).place(out, width)
					assertWidth(t, out, width)
					if width >= 60 && fitWidth(out, width) != out {
						t.Fatalf("%s/%s/%d: final guard changed an article with images", name, p.Slug, width)
					}
					if !strings.ContainsRune(out, kitty.Placeholder) {
						t.Fatalf("%s/%s/%d: images not drawn", name, p.Slug, width)
					}
				}
			}
		}
	}
}

func TestImageSessionIDs(t *testing.T) {
	asset := func(w, h int) *imageAsset {
		a, err := newImageAsset(image.NewNRGBA(image.Rect(0, 0, w, h)), 0)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	a, b := asset(160, 80), asset(80, 80)
	page := func(assets ...*imageAsset) string {
		var parts []string
		for _, x := range assets {
			parts = append(parts, "  "+imageMarker(x.id))
		}
		return strings.Join(parts, "\n\ntext\n\n")
	}
	s := &imageSession{}
	out := s.place(page(a, b, a), 80)
	sent := s.pending.String()
	s.pending.Reset()
	if strings.Count(sent, "a=T") != 2 || !strings.Contains(sent, "i=16,") || !strings.Contains(sent, "i=17,") {
		t.Fatalf("expected two transmissions: %q", sent)
	}
	if strings.Count(out, "\x1b[38;5;16m") != 2*5 {
		t.Fatalf("repeated image not placed twice: %d rows", strings.Count(out, "\x1b[38;5;16m"))
	}
	// Reopening sends nothing; a narrower window only moves the placement.
	s.place(page(a, b), 80)
	if s.pending.Len() != 0 {
		t.Fatalf("resent: %q", s.pending.String())
	}
	s.place(page(a), 12)
	if got := s.pending.String(); got != placeImage(16, 8, 2) {
		t.Fatalf("resize: %q", got)
	}
	s.pending.Reset()

	// Out of ids, the least recently shown image is deleted first.
	s = &imageSession{}
	s.place(page(a), 80)
	s.place(page(b), 80)
	for range lastImageID - firstImageID - 1 {
		s.place(page(asset(8, 8)), 80)
	}
	s.pending.Reset()
	s.place(page(b, asset(8, 8)), 80)
	if got := s.pending.String(); !strings.HasPrefix(got, deleteImage(16)) || !strings.Contains(got, "i=16,") || strings.Contains(got, deleteImage(17)) {
		t.Fatalf("eviction: %.120q", got)
	}
	if len(s.shown) != lastImageID-firstImageID+1 {
		t.Fatalf("%d ids in use", len(s.shown))
	}
}

func TestImageModelDetection(t *testing.T) {
	c, _ := imageCatalog(t, "")
	m := withImages(newModel(c, en, "rose-pine", colorprofile.TrueColor, 80, 32))
	run := func(msg tea.Msg) tea.Cmd {
		next, cmd := m.Update(msg)
		m = next.(imageModel)
		return cmd
	}
	if cmd := run(tea.TerminalVersionMsg{Name: "WezTerm 20240203"}); cmd != nil {
		t.Fatal("probed an unsupported terminal")
	}
	tips, _ := c.resolve("reference/tips-and-tricks", en)
	m.open(tips)
	if strings.Contains(m.viewport.View(), string(kitty.Placeholder)) {
		t.Fatal("image drawn before detection")
	}
	cmd := run(tea.TerminalVersionMsg{Name: "kitty(0.35.2)"})
	if cmd == nil || fmt.Sprint(cmd().(tea.RawMsg).Msg) != graphicsQuery {
		t.Fatal("no graphics query for kitty")
	}
	if cmd := run(uv.KittyGraphicsEvent{Payload: []byte("OK")}); cmd != nil || m.cache.graphics {
		t.Fatal("a reply to another id enabled images")
	}
	var reply uv.KittyGraphicsEvent
	reply.Options.ID = probeID
	reply.Payload = []byte("OK")
	cmd = run(reply)
	if !m.cache.graphics || cmd == nil || !strings.Contains(fmt.Sprint(cmd().(tea.RawMsg).Msg), "a=T,f=100") {
		t.Fatal("open article not redrawn with its image")
	}
	if !strings.Contains(m.View().Content, string(kitty.Placeholder)) {
		t.Fatal("no placeholders in the view")
	}
	// Another session on the same catalog starts without images.
	other := newModel(c, en, "rose-pine", colorprofile.TrueColor, 80, 32)
	other.open(tips)
	if other.cache.graphics || strings.Contains(other.viewport.View(), string(kitty.Placeholder)) {
		t.Fatal("image state shared between sessions")
	}
}

// TestSSHKittyImages captures the bytes a session sends to a kitty client,
// end to end: the probes, the image, and placeholder cells that keep their
// diacritics and the color carrying the image id.
func TestSSHKittyImages(t *testing.T) {
	c, _ := imageCatalog(t, "")
	cfg := testConfig()
	cfg.HostKey = filepath.Join(t.TempDir(), ".ssh/key")
	srv, err := newServer(cfg, c)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close(); <-done })
	client := dialSSH(t, l.Addr().String())

	open := func(term, version string) (*gossh.Session, io.Writer, *captureBuffer) {
		s, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = s.Close() })
		if err := s.RequestPty(term, 32, 80, gossh.TerminalModes{}); err != nil {
			t.Fatal(err)
		}
		input, err := s.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		b := &captureBuffer{}
		s.Stdout, s.Stderr = b, b
		if err := s.Shell(); err != nil {
			t.Fatal(err)
		}
		waitForRaw(t, b, ansi.RequestNameVersion)
		if version != "" {
			if _, err := io.WriteString(input, "\x1bP>|"+version+"\x1b\\"); err != nil {
				t.Fatal(err)
			}
		}
		return s, input, b
	}
	send := func(w io.Writer, s string) {
		if _, err := io.WriteString(w, s); err != nil {
			t.Fatal(err)
		}
	}

	s, input, screen := open("xterm-kitty", "kitty(0.35.2)")
	waitForRaw(t, screen, graphicsQuery)
	send(input, "\x1b_Gi=31;OK\x1b\\")
	waitFor(t, screen, "blog.konakona.dev")
	send(input, "/Tips\r\r")
	waitFor(t, screen, "Pipeline diagram")
	waitForRaw(t, screen, "\x1b_Ga=T,f=100,t=d,i=16,p=1,U=1,c=40,r=8,q=2,m=0;iVBOR")
	// Bubble Tea redraws each cell itself; the first cell of the first row
	// must arrive with its id color and both diacritics.
	cell := regexp.MustCompile("\x1b\\[[0-9;:]*38[;:]5[;:]16[0-9;:]*m" + string(kitty.Placeholder) + string(kitty.Diacritic(0)) + string(kitty.Diacritic(0)))
	lastRow := string(kitty.Placeholder) + string(kitty.Diacritic(7)) + string(kitty.Diacritic(39))
	waitForRaw(t, screen, lastRow)
	if !cell.MatchString(screen.String()) {
		t.Fatalf("placeholder cell lost its color or diacritics:\n%q", screen.String())
	}
	send(input, "q")
	if strings.Count(screen.String(), "a=T") != 1 {
		t.Fatal("image sent more than once")
	}
	send(input, "\x03")
	waitSession(t, s)

	// A terminal that names itself otherwise, or answers nothing, never
	// receives an APC sequence and keeps the caption.
	for _, version := range []string{"WezTerm 20240203", "tmux 3.4", ""} {
		s, input, screen := open("xterm-kitty", version)
		waitFor(t, screen, "blog.konakona.dev")
		send(input, "/Tips\r\r")
		waitFor(t, screen, "Pipeline diagram")
		if strings.Contains(screen.String(), "\x1b_G") || strings.ContainsRune(screen.String(), kitty.Placeholder) {
			t.Fatalf("%q: graphics sent", version)
		}
		send(input, "\x03")
		waitSession(t, s)
	}
}

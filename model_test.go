package main

import (
	"image/color"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

func press(m model, key string) (model, tea.Cmd) {
	k := tea.KeyPressMsg{}
	switch key {
	case "enter":
		k.Code = tea.KeyEnter
	case "esc":
		k.Code = tea.KeyEscape
	case "ctrl+c":
		k.Code = 'c'
		k.Mod = tea.ModCtrl
	case "down":
		k.Code = tea.KeyDown
	case "up":
		k.Code = tea.KeyUp
	case "space":
		k.Code = ' '
		k.Text = " "
	default:
		if r, ok := strings.CutPrefix(key, "ctrl+"); ok {
			k.Code = []rune(r)[0]
			k.Mod = tea.ModCtrl
			break
		}
		k.Code = []rune(key)[0]
		k.Text = key
	}
	updated, cmd := m.Update(k)
	return updated.(model), cmd
}

func TestModelNavigationFilterTranslationResize(t *testing.T) {
	c := realCatalog(t)
	m := newModel(c, en, "dark", colorprofile.TrueColor, 80, 24)
	m, _ = press(m, "j")
	if m.selected != 1 {
		t.Fatal("j navigation")
	}
	m, _ = press(m, "k")
	if m.selected != 0 {
		t.Fatal("k navigation")
	}
	m, _ = press(m, "enter")
	if m.folder != "cs50-ai-notes" {
		t.Fatal("enter folder")
	}
	m, _ = press(m, "enter")
	if m.article == nil {
		t.Fatal("enter article")
	}
	slug := m.article.Slug
	m, _ = press(m, "ctrl+l")
	if m.lang != zh || m.article.Slug != slug || !m.articleFallback || m.article.Lang != en {
		t.Fatal("missing translation must preserve article and preference")
	}
	if !strings.Contains(m.View().Content, "译文暂缺") {
		t.Fatal("fallback notice missing")
	}
	m, _ = press(m, "G")
	if !m.viewport.AtBottom() {
		t.Fatal("G bottom")
	}
	m, _ = press(m, "g")
	if m.viewport.AtTop() {
		t.Fatal("a single g must wait for the second g")
	}
	m, _ = press(m, "g")
	if !m.viewport.AtTop() {
		t.Fatal("gg top")
	}
	m, _ = press(m, "space")
	if m.viewport.AtTop() {
		t.Fatal("space scroll")
	}
	m, _ = press(m, "ctrl+b")
	if !m.viewport.AtTop() {
		t.Fatal("ctrl+b scroll")
	}
	m, _ = press(m, "q")
	if m.article != nil || m.folder != "cs50-ai-notes" {
		t.Fatal("q reader back")
	}
	m, _ = press(m, "esc")
	if m.folder != "" {
		t.Fatal("back out folder")
	}
	m, _ = press(m, "ctrl+l")
	m, _ = press(m, "/")
	for _, r := range "Test-Time Training" {
		m, _ = press(m, string(r))
	}
	if len(m.items) != 1 {
		t.Fatalf("title filtering: %d", len(m.items))
	}
	m, _ = press(m, "enter")
	m, _ = press(m, "enter")
	if m.article == nil || m.article.Slug != "from-linear-attention-to-test-time-training" {
		t.Fatal("open filter result")
	}
	m, _ = press(m, "ctrl+l")
	if m.article.Lang != zh || m.articleFallback {
		t.Fatal("paired translation switch")
	}
	m, _ = press(m, "G")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m = updated.(model)
	if m.viewport.Width() != 40 || !m.viewport.AtBottom() {
		t.Fatal("resize did not rerender and preserve scroll")
	}
	assertWidth(t, m.viewport.GetContent(), 40)
	assertWidth(t, m.View().Content, 40)
	m, _ = press(m, "t")
	if m.theme != "light" {
		t.Fatal("theme toggle")
	}
	if _, cmd := press(m, "ctrl+c"); cmd == nil {
		t.Fatal("ctrl+c must quit")
	}
}

func TestFilterLifecycleAndGlobalResults(t *testing.T) {
	for _, width := range []int{40, 90} {
		m := newModel(realCatalog(t), en, "dark", colorprofile.TrueColor, width, 32)
		m, _ = press(m, "/")
		for _, r := range "lecture" {
			m, _ = press(m, string(r))
		}
		if !m.filtering || len(m.items) != 7 || !strings.Contains(ansi.Strip(m.View().Content), "cs50-ai-notes/") {
			t.Fatal("root search must expose folder posts and their paths")
		}
		assertWidth(t, m.View().Content, width)
		m, _ = press(m, "esc")
		if m.filtering || m.filter.Focused() || m.filter.Value() != "" {
			t.Fatal("Escape did not clear and close editing")
		}
		var cmd tea.Cmd
		m, cmd = press(m, "esc")
		if cmd != nil {
			t.Fatal("second Escape at root must not quit")
		}
		m, _ = press(m, "/")
		for _, r := range "qwen" {
			m, _ = press(m, string(r))
		}
		m, _ = press(m, "enter")
		if m.filtering || m.filter.Focused() || m.filter.Value() != "qwen" || len(m.items) != 1 {
			t.Fatal("Enter must apply exactly the new query")
		}
		m, _ = press(m, "enter")
		if m.article == nil || m.article.Slug != "qwen38-terminal-bench-21-reproduction" {
			t.Fatal("reported Escape / search sequence opened wrong article")
		}
		m, _ = press(m, "esc")
		m, _ = press(m, "/")
		if !m.filtering || m.filter.Value() != "qwen" {
			t.Fatal("slash must reopen the applied query without appending itself")
		}
		m, _ = press(m, "enter")
		m, _ = press(m, "esc")
		if m.filtering || m.filter.Value() != "" {
			t.Fatal("Escape must clear an applied filter")
		}
	}
}

func TestFilterCoalescedEscapeKeys(t *testing.T) {
	for _, key := range []tea.KeyPressMsg{
		{Code: tea.KeyEscape, Mod: tea.ModAlt},
		{Code: '/', Mod: tea.ModAlt},
	} {
		m := newModel(realCatalog(t), en, "dark", colorprofile.TrueColor, 80, 32)
		m, _ = press(m, "/")
		m, _ = press(m, "lecture")
		updated, _ := m.Update(key)
		m = updated.(model)
		if m.filter.Value() != "" || m.filtering != (key.Code == '/') {
			t.Fatalf("coalesced %s retained old query or wrong editor state", key.String())
		}
	}
}

func TestClientLanguageAndBackground(t *testing.T) {
	for _, tt := range []struct {
		env      []string
		override string
		want     language
	}{
		{[]string{"LANG=zh_CN.UTF-8"}, "", zh}, {[]string{"LANG=zh_CN.UTF-8", "LC_ALL=en_US.UTF-8"}, "", en},
		{[]string{"LANG=fr_FR"}, "", en}, {nil, "", en}, {[]string{"LANG=en_US"}, "zh", zh},
	} {
		if got := initialLanguage(tt.env, tt.override); got != tt.want {
			t.Fatalf("language got %s want %s", got, tt.want)
		}
	}
	m := newModel(realCatalog(t), en, "auto", colorprofile.ANSI256, 80, 24)
	updated, _ := m.Update(tea.BackgroundColorMsg{Color: color.White})
	m = updated.(model)
	if m.theme != "light" {
		t.Fatal("client background ignored")
	}
	m, _ = press(m, "t")
	updated, _ = m.Update(tea.BackgroundColorMsg{Color: color.White})
	m = updated.(model)
	if m.theme != "dark" {
		t.Fatal("background query overrides explicit toggle")
	}
	updated, _ = m.Update(tea.ColorProfileMsg{Profile: colorprofile.ASCII})
	m = updated.(model)
	if strings.Contains(m.View().Content, "38;") {
		t.Fatal("server color leaked to client")
	}
}

func TestModelSmallWindowsAndIndependentSessions(t *testing.T) {
	c := realCatalog(t)
	a := newModel(c, zh, "dark", colorprofile.TrueColor, 80, 24)
	b := newModel(c, en, "light", colorprofile.ANSI, 80, 24)
	p, _ := c.resolve("from-linear-attention-to-test-time-training", zh)
	a.open(p)
	for _, width := range []int{1, 2, 10, 40, 60, 80, 120} {
		for _, height := range []int{1, 8, 24, 50} {
			updated, _ := a.Update(tea.WindowSizeMsg{Width: width, Height: height})
			a = updated.(model)
			assertWidth(t, a.View().Content, width)
			if len(strings.Split(a.View().Content, "\n")) > height {
				t.Fatal("view exceeds height")
			}
		}
	}
	a, _ = press(a, "ctrl+l")
	if b.lang != en || b.theme != "light" || b.article != nil || a.cache == b.cache {
		t.Fatal("shared mutable session state")
	}
}

func TestChromeHeaderAndSingleLineHints(t *testing.T) {
	c := realCatalog(t)
	p, _ := c.resolve("from-linear-attention-to-test-time-training", zh)
	for _, width := range []int{40, 60, 80, 120} {
		for _, lang := range []language{zh, en} {
			m := newModel(c, lang, "dark", colorprofile.TrueColor, width, 32)
			for _, reader := range []bool{false, true} {
				if reader {
					m.open(p)
				}
				h, f := m.chrome()
				assertWidth(t, h, width)
				assertWidth(t, f, width)
				brand := strings.Split(ansi.Strip(h), "\n")[0]
				label := "English"
				if lang == zh {
					label = "中文"
				}
				if !strings.Contains(brand, "konakona · blog.konakona.dev") || !strings.HasSuffix(brand, label) || strings.Contains(brand, "dark") || strings.Contains(brand, "terminal journal") {
					t.Fatalf("header: %q", brand)
				}
				if reader && strings.Contains(ansi.Strip(h), "2026-07-17  ·") {
					t.Fatal("article date repeats the language")
				}
				lines := strings.Split(ansi.Strip(f), "\n")
				hints := lines[len(lines)-1]
				if !strings.Contains(hints, "j/k") || !strings.Contains(hints, "? help") {
					t.Fatalf("hints wrapped/lost at %d: %q", width, hints)
				}
				if hintLine := m.hintLine("  0%  ", reader); strings.Contains(hintLine, "\n") {
					t.Fatal("hint line contains newline")
				}
			}
		}
	}
}

func TestSessionCachesAreIndependent(t *testing.T) {
	c := realCatalog(t)
	p, _ := c.resolve("from-linear-attention-to-test-time-training", en)
	original := p.Body
	a := newModel(c, en, "dark", colorprofile.TrueColor, 80, 32)
	b := newModel(c, en, "dark", colorprofile.TrueColor, 80, 32)
	a.open(p)
	b.open(p)
	a, _ = press(a, "ctrl+l")
	a, _ = press(a, "t")
	a, _ = press(a, "G")
	if b.lang != en || b.theme != "dark" || b.article != p || !b.viewport.AtTop() || len(b.cache.values) != 1 {
		t.Fatal("session state or cache leaked")
	}
	if len(a.cache.values) <= len(b.cache.values) || a.cache == b.cache || p.Body != original {
		t.Fatal("shared mutable cache or catalog content")
	}
}

func TestVimKeys(t *testing.T) {
	c := realCatalog(t)
	m := newModel(c, en, "dark", colorprofile.TrueColor, 80, 30)
	seq := func(keys ...string) {
		t.Helper()
		for _, k := range keys {
			m, _ = press(m, k)
		}
	}
	last := len(m.items) - 1
	seq("G")
	if m.selected != last {
		t.Fatalf("G list: %d", m.selected)
	}
	seq("g", "g")
	if m.selected != 0 {
		t.Fatal("gg list")
	}
	seq("3", "j")
	if m.selected != 3 {
		t.Fatalf("3j: %d", m.selected)
	}
	seq("k", "1", "0", "G")
	if m.selected != min(9, last) {
		t.Fatalf("10G: %d", m.selected)
	}
	seq("2", "g", "g")
	if m.selected != 1 {
		t.Fatalf("2gg: %d", m.selected)
	}
	seq("0", "j")
	if m.selected != 2 {
		t.Fatal("a leading 0 must not start a count")
	}
	seq("5", "esc", "j")
	if m.selected != 3 {
		t.Fatal("esc must cancel a pending count without going back")
	}
	seq("g", "g", "ctrl+d")
	if m.selected == 0 {
		t.Fatal("ctrl+d list")
	}
	seq("ctrl+u")
	if m.selected != 0 {
		t.Fatal("ctrl+u list")
	}

	// Folder: l enters, h leaves; posts open with l and close with h.
	seq("l")
	if m.folder != "cs50-ai-notes" {
		t.Fatalf("l into folder: %q", m.folder)
	}
	seq("l")
	if m.article == nil {
		t.Fatal("l opens post")
	}
	seq("ctrl+d")
	if m.viewport.YOffset() != max(1, m.viewport.Height()/2) {
		t.Fatalf("ctrl+d reader offset %d", m.viewport.YOffset())
	}
	seq("ctrl+u", "4", "ctrl+e")
	if m.viewport.YOffset() != 4 {
		t.Fatalf("4 ctrl+e: %d", m.viewport.YOffset())
	}
	seq("ctrl+y", "1", "2", "G")
	if m.viewport.YOffset() != 11 {
		t.Fatalf("12G reader: %d", m.viewport.YOffset())
	}
	seq("l")
	if m.article == nil || m.viewport.XOffset() != 0 {
		t.Fatal("l in reader must not scroll sideways or close")
	}
	seq("?")
	if !m.help || !strings.Contains(ansi.Strip(m.View().Content), "half page down") {
		t.Fatal("? help")
	}
	seq("j", "?")
	if m.help || m.article == nil {
		t.Fatal("? closes help and keeps the article")
	}
	seq("h")
	if m.article != nil || m.folder != "cs50-ai-notes" {
		t.Fatal("h closes post")
	}
	seq("h")
	if m.folder != "" {
		t.Fatal("h leaves folder")
	}
	seq("ctrl+l")
	if m.lang != zh {
		t.Fatal("ctrl+l language")
	}
	seq("l", "h", "q")
	if _, cmd := press(m, "q"); cmd == nil {
		t.Fatal("q quits at root")
	}
}

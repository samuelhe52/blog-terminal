package main

import (
	"fmt"
	"path"
	"strings"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

type model struct {
	catalog         *catalog
	lang            language
	folder          string
	items           []entry
	selected        int
	folderFallback  bool
	article         *post
	articleFallback bool
	width, height   int
	viewport        viewport.Model
	filter          textinput.Model
	filtering       bool
	help            bool
	count           int
	pendingG        bool
	theme           string
	manualTheme     bool
	profile         colorprofile.Profile
	cache           *renderCache
	err             error
}

func initialLanguage(env []string, override string) language {
	if override != "" {
		if override == "zh" || override == "zh-CN" {
			return zh
		}
		return en
	}
	value := envValue(env, "LC_ALL")
	if value == "" {
		value = envValue(env, "LANG")
	}
	if strings.HasPrefix(strings.ToLower(value), "zh") {
		return zh
	}
	return en
}

func envValue(env []string, key string) string {
	for i := len(env) - 1; i >= 0; i-- {
		if v, ok := strings.CutPrefix(env[i], key+"="); ok {
			return v
		}
	}
	return ""
}

func newModel(c *catalog, lang language, theme string, profile colorprofile.Profile, width, height int) model {
	m := model{catalog: c, lang: lang, theme: theme, profile: profile, cache: &renderCache{}, width: max(1, min(width, 512)), height: max(1, min(height, 256))}
	if theme == "auto" || theme == "" {
		m.theme = "dark"
	} else {
		m.manualTheme = true
	}
	m.viewport = viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(max(1, m.height-8)))
	m.filter = textinput.New()
	m.filter.Prompt, m.filter.Placeholder, m.filter.CharLimit = "/ ", "Search posts…", 120
	m.filter.SetWidth(max(1, m.width-4))
	m.filter.SetVirtualCursor(true)
	m.styleFilter()
	m.refreshListing()
	return m
}

func (m model) Init() tea.Cmd {
	if !m.manualTheme {
		return tea.RequestBackgroundColor
	}
	return nil
}

func (m *model) closeArticle() {
	m.article = nil
	m.err = nil
	m.refreshListing()
}

func (m *model) parentFolder() {
	m.folder = path.Dir(m.folder)
	if m.folder == "." {
		m.folder = ""
	}
	m.selected = 0
	m.refreshListing()
}

func (m *model) refreshListing() {
	m.items, m.folderFallback = m.catalog.listing(m.lang, m.folder, m.filter.Value())
	m.selected = min(max(0, m.selected), max(0, len(m.items)-1))
}

func (m *model) open(p *post) {
	m.article, m.articleFallback = m.catalog.resolve(p.Slug, m.lang)
	m.err = nil
	m.renderArticle(false)
}

func (m *model) renderArticle(preserve bool) {
	if m.article == nil {
		return
	}
	percent := m.viewport.ScrollPercent()
	content, err := m.cache.render(m.article, m.width, m.theme, m.profile)
	m.err = err
	if err != nil {
		content = "Unable to render this article: " + err.Error()
	}
	m.viewport.SetWidth(m.width)
	m.viewport.SetContent(content)
	m.sizeViewport()
	if preserve {
		m.viewport.SetYOffset(int(percent * float64(max(0, m.viewport.TotalLineCount()-m.viewport.Height()))))
	} else {
		m.viewport.GotoTop()
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = max(1, min(msg.Width, 512)), max(1, min(msg.Height, 256))
		m.filter.SetWidth(max(1, m.width-4))
		m.renderArticle(true)
		return m, nil
	case tea.ColorProfileMsg:
		if m.profile != msg.Profile {
			m.profile = msg.Profile
			m.styleFilter()
			m.renderArticle(true)
		}
		return m, nil
	case tea.BackgroundColorMsg:
		if !m.manualTheme {
			m.theme = "light"
			if msg.IsDark() {
				m.theme = "dark"
			}
			m.styleFilter()
			m.renderArticle(true)
		}
		return m, nil
	case tea.KeyPressMsg:
		key := msg.String()
		// Legacy terminals encode two adjacent Escapes as Alt+Escape. Both
		// should cancel editing, regardless of transport packet boundaries.
		if msg.Code == tea.KeyEscape {
			key = "esc"
		}
		// Escape immediately followed by / can similarly become Alt+/. It
		// means cancel the old editor and start a fresh search here.
		if msg.Code == '/' && msg.Mod == tea.ModAlt && m.article == nil {
			m.filter.Reset()
			m.filtering = true
			m.selected = 0
			m.refreshListing()
			return m, m.filter.Focus()
		}
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		if m.filtering {
			switch key {
			case "esc":
				m.filtering = false
				m.filter.Reset()
				m.filter.Blur()
				m.refreshListing()
				return m, nil
			case "enter":
				m.filtering = false
				m.filter.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			m.selected = 0
			m.refreshListing()
			return m, cmd
		}
		if m.help {
			switch key {
			case "?", "q", "esc", "h", "left":
				m.help = false
			}
			return m, nil
		}
		if key == "esc" && (m.pendingG || m.count > 0) {
			m.pendingG, m.count = false, 0
			return m, nil
		}
		// Vim count prefix (5j, 10G) and the two-key gg.
		if t := msg.Text; len(t) == 1 && t[0] >= '0' && t[0] <= '9' && (t[0] != '0' || m.count > 0) {
			m.count = min(m.count*10+int(t[0]-'0'), 9999)
			m.pendingG = false
			return m, nil
		}
		if key == "g" && !m.pendingG {
			m.pendingG = true
			return m, nil
		}
		if key == "g" {
			key = "gg"
		}
		m.pendingG = false
		count, explicit := max(1, m.count), m.count > 0
		m.count = 0
		switch key {
		case "?":
			m.help = true
			return m, nil
		case "ctrl+l":
			m.lang = m.lang.other()
			if m.article != nil {
				m.article, m.articleFallback = m.catalog.resolve(m.article.Slug, m.lang)
				m.renderArticle(true)
			} else {
				m.selected = 0
				m.refreshListing()
			}
			return m, nil
		case "t":
			m.manualTheme = true
			if m.theme == "dark" {
				m.theme = "light"
			} else {
				m.theme = "dark"
			}
			m.styleFilter()
			m.renderArticle(true)
			return m, nil
		case "q", "esc":
			if m.article != nil {
				m.closeArticle()
				return m, nil
			}
			if m.filter.Value() != "" {
				m.filter.Reset()
				m.refreshListing()
				return m, nil
			}
			if m.folder != "" {
				m.parentFolder()
				return m, nil
			}
			if key == "q" {
				return m, tea.Quit
			}
			return m, nil
		}
		if m.article != nil {
			v := &m.viewport
			half := max(1, v.Height()/2)
			switch key {
			case "j", "down", "ctrl+e", "ctrl+n":
				v.ScrollDown(count)
			case "k", "up", "ctrl+y", "ctrl+p":
				v.ScrollUp(count)
			case "ctrl+d":
				v.ScrollDown(count * half)
			case "ctrl+u":
				v.ScrollUp(count * half)
			case "ctrl+f", "space", "pgdown":
				v.ScrollDown(count * v.Height())
			case "ctrl+b", "pgup":
				v.ScrollUp(count * v.Height())
			case "gg", "home":
				v.GotoTop()
				if explicit {
					v.SetYOffset(count - 1)
				}
			case "G", "end":
				v.GotoBottom()
				if explicit {
					v.SetYOffset(count - 1)
				}
			case "h", "left", "backspace":
				m.closeArticle()
			}
			return m, nil
		}
		last := max(0, len(m.items)-1)
		page := m.homeCapacity()
		switch key {
		case "/":
			m.filtering = true
			return m, m.filter.Focus()
		case "j", "down", "ctrl+n", "ctrl+e":
			m.selected = min(m.selected+count, last)
		case "k", "up", "ctrl+p", "ctrl+y":
			m.selected = max(0, m.selected-count)
		case "ctrl+d":
			m.selected = min(m.selected+count*max(1, page/2), last)
		case "ctrl+u":
			m.selected = max(0, m.selected-count*max(1, page/2))
		case "ctrl+f", "space", "pgdown":
			m.selected = min(m.selected+count*page, last)
		case "ctrl+b", "pgup":
			m.selected = max(0, m.selected-count*page)
		case "gg", "home":
			m.selected = 0
			if explicit {
				m.selected = min(count-1, last)
			}
		case "G", "end":
			m.selected = last
			if explicit {
				m.selected = min(count-1, last)
			}
		case "h", "left", "backspace":
			if m.folder != "" {
				m.filter.Reset()
				m.parentFolder()
			}
		case "l", "enter", "right":
			if len(m.items) > 0 {
				e := m.items[m.selected]
				if e.Post != nil {
					m.open(e.Post)
				} else {
					m.folder = e.Folder
					m.selected = 0
					m.filter.Reset()
					m.refreshListing()
				}
			}
		}
		return m, nil
	}
	if m.filtering {
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) styleFilter() {
	s := textinput.DefaultStyles(m.theme == "dark")
	muted := "#908CAA"
	accent := "#C4A7E7"
	if m.theme == "light" {
		muted, accent = "#686477", "#684494"
	}
	s.Focused.Prompt = lipgloss.NewStyle().Foreground(m.profile.Convert(lipgloss.Color(accent)))
	s.Focused.Placeholder = lipgloss.NewStyle().Foreground(m.profile.Convert(lipgloss.Color(muted)))
	s.Blurred = s.Focused
	s.Cursor.Color = m.profile.Convert(lipgloss.Color(accent))
	m.filter.SetStyles(s)
}

func (m model) accent(s string) string {
	color := "#C4A7E7"
	if m.theme == "light" {
		color = "#684494"
	}
	return lipgloss.NewStyle().Bold(true).Foreground(m.profile.Convert(lipgloss.Color(color))).Render(s)
}

func (m model) muted(s string) string {
	color := "#908CAA"
	if m.theme == "light" {
		color = "#686477"
	}
	return lipgloss.NewStyle().Foreground(m.profile.Convert(lipgloss.Color(color))).Render(s)
}

func (m model) notice() string {
	if m.lang == zh {
		return "译文暂缺 · 正在显示英文原文"
	}
	return "Translation unavailable · showing Chinese"
}

func (m model) brand() string {
	label := "English"
	if m.lang == zh {
		label = "中文"
	}
	right := m.accent(label)
	left := m.accent(m.catalog.Site.Title) + m.muted(" · "+m.catalog.Site.host())
	left = ansi.Truncate(left, max(0, m.width-ansi.StringWidth(right)-1), "…")
	gap := max(0, m.width-ansi.StringWidth(left)-ansi.StringWidth(right))
	return ansi.Truncate(left+strings.Repeat(" ", gap)+right, m.width, "")
}

func (m model) hintLine(status string, reader bool) string {
	keys := []string{"j/k", "l open", "? help", "/ filter", "^L lang", "t theme", "q quit"}
	if m.folder != "" {
		keys = []string{"j/k", "l open", "h back", "? help", "/ filter", "^L lang", "t theme"}
	}
	if reader {
		keys = []string{"j/k", "h back", "? help", "^D/^U", "gg/G", "^L lang", "t theme"}
	}
	available := max(0, m.width-ansi.StringWidth(status))
	var hints []string
	for _, hint := range keys {
		candidate := append(append([]string{}, hints...), hint)
		if ansi.StringWidth(strings.Join(candidate, " · ")) <= available {
			hints = candidate
		}
	}
	return ansi.Truncate(status+m.muted(strings.Join(hints, " · ")), m.width, "")
}

func (m model) chrome() (string, string) {
	brand := m.brand()
	rule := m.muted(strings.Repeat("─", m.width))
	if m.article != nil {
		header := brand + "\n\n" + m.accent(cleanText(m.article.Title)) + "\n" + m.muted(m.article.Date.Format("2006-01-02"))
		if m.articleFallback {
			header += "\n" + m.accent(m.notice())
		}
		header += "\n" + rule
		footer := rule + "\n" + m.muted(m.article.URL) + "\n" + m.hintLine(fmt.Sprintf("%3.0f%%  ", m.viewport.ScrollPercent()*100), true)
		return fitWidth(header, m.width), fitWidth(footer, m.width)
	}
	location := "/"
	if m.folder != "" {
		location += m.folder + "/"
	}
	header := brand + "\n\n" + m.accent(location)
	if m.folderFallback {
		header += "\n" + m.accent(m.notice())
	}
	if m.filtering {
		header += "\n" + m.filter.View()
	} else if m.filter.Value() != "" {
		header += "\n" + m.muted("Filter: "+m.filter.Value())
	}
	header += "\n" + rule
	index := 0
	if len(m.items) > 0 {
		index = m.selected + 1
	}
	footer := rule + "\n" + m.hintLine(fmt.Sprintf("%d/%d  ", index, len(m.items)), false)
	return fitWidth(header, m.width), fitWidth(footer, m.width)
}

func (m *model) sizeViewport() {
	h, f := m.chrome()
	m.viewport.SetHeight(max(1, m.height-lipgloss.Height(h)-lipgloss.Height(f)))
}

func (m model) homeCapacity() int {
	h, f := m.chrome()
	return max(1, (m.height-lipgloss.Height(h)-lipgloss.Height(f))/3)
}

func (m model) homeView(height int) string {
	if len(m.items) == 0 {
		return m.muted("No matching posts.")
	}
	capacity := max(1, height/3)
	start := max(0, m.selected-capacity+1)
	var lines []string
	for i := start; i < min(len(m.items), start+capacity); i++ {
		e := m.items[i]
		title, detail := "", ""
		if e.Post == nil {
			title = path.Base(e.Folder) + "/"
			detail = fmt.Sprintf("%d posts", e.Count)
		} else {
			title = e.Post.Title
			detail = e.Post.Date.Format("2006-01-02")
			if m.folder == "" && m.filter.Value() != "" && e.Post.Folder != "" {
				detail = e.Post.Folder + "/  ·  " + detail
			}
			if e.Post.Description != "" {
				detail += "  ·  " + e.Post.Description
			}
		}
		title = cleanText(title)
		marker := "  "
		if i == m.selected {
			marker = "› "
			title = m.accent(title)
		}
		lines = append(lines, ansi.Truncate(marker+title, m.width, "…"), m.muted(ansi.Truncate("  "+cleanText(detail), m.width, "…")), "")
	}
	return strings.TrimSuffix(strings.Join(lines, "\n"), "\n")
}

var helpRows = [][2]string{
	{"j / k", "down / up (also ↓ ↑, ^N ^P, ^E ^Y)"},
	{"l / h", "open / back (also ↵ → / ← ⌫)"},
	{"gg / G", "first / last; with a count, go to line or item N"},
	{"^D / ^U", "half page down / up"},
	{"^F / ^B", "page down / up (also space, PgDn / PgUp)"},
	{"5j, 10G, 3gg", "counts repeat a motion or pick a position"},
	{"/", "filter posts (↵ apply, esc clear)"},
	{"^L", "switch language 中文 / English"},
	{"t", "toggle light / dark"},
	{"q / esc", "back; q quits from the top level"},
	{"?", "close this help"},
	{"^C", "quit"},
}

func (m model) helpView() string {
	keyWidth := 0
	for _, row := range helpRows {
		keyWidth = max(keyWidth, ansi.StringWidth(row[0]))
	}
	lines := []string{m.accent("  Keys"), ""}
	for _, row := range helpRows {
		lines = append(lines, "  "+m.accent(row[0]+strings.Repeat(" ", keyWidth-ansi.StringWidth(row[0])))+"  "+m.muted(row[1]))
	}
	return strings.Join(lines, "\n")
}

func (m model) View() tea.View {
	header, footer := m.chrome()
	available := max(1, m.height-lipgloss.Height(header)-lipgloss.Height(footer))
	content := ""
	if m.help {
		content = m.helpView()
	} else if m.article != nil {
		content = m.viewport.View()
	} else {
		content = m.homeView(available)
	}
	lines := strings.Split(fitWidth(content, m.width), "\n")
	if len(lines) > available {
		lines = lines[:available]
	}
	for len(lines) < available {
		lines = append(lines, "")
	}
	screen := header + "\n" + strings.Join(lines, "\n") + "\n" + footer
	screenLines := strings.Split(screen, "\n")
	if len(screenLines) > m.height {
		screen = strings.Join(screenLines[:m.height], "\n")
	}
	v := tea.NewView(screen)
	v.AltScreen = true
	return v
}

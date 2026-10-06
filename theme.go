package main

import "strings"

// A theme recolors text only. The visitor's terminal background always shows
// through, so Base is used just for text drawn on top of a theme color.
type theme struct {
	Key, Name, Description string
	Dark                   bool
	Base                   string
	Text, Muted, Accent    string
	Secondary, Tertiary    string
	Chroma                 string
}

const autoTheme = "auto"

// The first two double as the auto pair, picked from the terminal background.
var themes = []theme{
	{Key: "rose-pine", Name: "Rosé Pine", Description: "Soho vibes: muted iris, foam, and rose", Dark: true,
		Base: "#191724", Text: "#E0DEF4", Muted: "#908CAA", Accent: "#C4A7E7", Secondary: "#9CCFD8", Tertiary: "#EBBCBA", Chroma: "rose-pine"},
	{Key: "rose-pine-dawn", Name: "Rosé Pine Dawn", Description: "The same palette in morning light",
		Base: "#FAF4ED", Text: "#575279", Muted: "#686477", Accent: "#684494", Secondary: "#286983", Tertiary: "#B4637A", Chroma: "rose-pine-dawn"},
	{Key: "dracula", Name: "Dracula", Description: "High-contrast purple, pink, and cyan", Dark: true,
		Base: "#282A36", Text: "#F8F8F2", Muted: "#7E8AB8", Accent: "#BD93F9", Secondary: "#8BE9FD", Tertiary: "#FF79C6", Chroma: "dracula"},
	{Key: "nord", Name: "Nord", Description: "Cool arctic blues with a warm yellow", Dark: true,
		Base: "#2E3440", Text: "#E5E9F0", Muted: "#8791A7", Accent: "#88C0D0", Secondary: "#81A1C1", Tertiary: "#EBCB8B", Chroma: "nord"},
	{Key: "gruvbox", Name: "Gruvbox", Description: "Retro warm earth tones", Dark: true,
		Base: "#282828", Text: "#EBDBB2", Muted: "#A89984", Accent: "#FABD2F", Secondary: "#8EC07C", Tertiary: "#FE8019", Chroma: "gruvbox"},
	{Key: "paper", Name: "Paper", Description: "Ink on paper: quiet and high-contrast",
		Base: "#F5F5DC", Text: "#2B2B2B", Muted: "#6B6B6B", Accent: "#1F4E8C", Secondary: "#8C3A1F", Tertiary: "#2F6B3A", Chroma: "github"},
}

// themeChoices is the picker order: auto first, then every named theme.
func themeChoices() []string {
	keys := []string{autoTheme}
	for _, t := range themes {
		keys = append(keys, t.Key)
	}
	return keys
}

func validTheme(key string) bool {
	for _, k := range themeChoices() {
		if k == key {
			return true
		}
	}
	return false
}

func lookupTheme(key string) theme {
	for _, t := range themes {
		if t.Key == key {
			return t
		}
	}
	return themes[0]
}

// resolveTheme maps auto to the Rosé Pine pair for the detected background.
func resolveTheme(key string, dark bool) theme {
	if key == autoTheme {
		if dark {
			return themes[0]
		}
		return themes[1]
	}
	return lookupTheme(key)
}

func themeName(key string) string {
	if key == autoTheme {
		return "Auto"
	}
	return lookupTheme(key).Name
}

func themeDescription(key string) string {
	if key == autoTheme {
		return "Follows your terminal: " + themes[0].Name + " or " + themes[1].Name
	}
	return lookupTheme(key).Description
}

func themeList() string {
	return strings.Join(themeChoices(), ", ")
}

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

// Images are drawn with the Kitty graphics protocol's Unicode placeholders:
// the image is sent once out of band, and each cell it covers holds U+10EEEE
// with two diacritics for its row and column. The foreground color carries
// the image id. Placeholder cells are ordinary one-cell text, so they scroll
// in the viewport and diff in Bubble Tea's renderer like any other text.

// Image ids are 256-color indexes, which survive both the TrueColor and the
// ANSI256 profiles unchanged. Indexes below 16 are avoided because renderers
// may write them as the basic SGR 30–37 and 90–97 forms instead of 38;5;N.
const (
	firstImageID = 16
	lastImageID  = 255
	// probeID is only used for the support query; it is never placed.
	probeID = 31
)

// The protocol's diacritic table limits a placement to this many rows and
// columns.
const maxPlaceholderCells = 297

// placeholderTerminal reports whether an XTVERSION reply names a terminal
// that draws Unicode placeholders: kitty 0.28 or later, or Ghostty (all
// public releases). WezTerm, Konsole, and others accept the graphics protocol
// but print the placeholders as text, so they are not listed. tmux answers
// XTVERSION itself, which keeps images off inside it.
func placeholderTerminal(name string) bool {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(strings.ToLower(name), "ghostty") {
		return true
	}
	version, ok := strings.CutPrefix(name, "kitty(")
	if !ok {
		return false
	}
	version, _, _ = strings.Cut(version, ")")
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return major > 0 || minor >= 28
}

// graphicsQuery asks whether the terminal accepts a 1×1 image. The reply is
// an APC carrying i=probeID and OK. It is sent only after XTVERSION has named
// a supported terminal, so other terminals never see an APC sequence.
var graphicsQuery = ansi.KittyGraphics([]byte("AAAA"), "i="+strconv.Itoa(probeID), "s=1", "v=1", "a=q", "t=d", "f=24")

// transmitImage sends a PNG, already base64-encoded, and creates its virtual
// placement of cols×rows cells. The terminal fits the image into that box,
// keeping its aspect ratio. q=2 suppresses replies.
func transmitImage(id int, data string, cols, rows int) string {
	var b strings.Builder
	for start := 0; ; start += kitty.MaxChunkSize {
		end := min(len(data), start+kitty.MaxChunkSize)
		more := 0
		if end < len(data) {
			more = 1
		}
		if start == 0 {
			b.WriteString(ansi.KittyGraphics([]byte(data[start:end]), "a=T", "f=100", "t=d", fmt.Sprintf("i=%d", id), "p=1", "U=1", fmt.Sprintf("c=%d", cols), fmt.Sprintf("r=%d", rows), "q=2", fmt.Sprintf("m=%d", more)))
		} else {
			b.WriteString(ansi.KittyGraphics([]byte(data[start:end]), fmt.Sprintf("m=%d", more)))
		}
		if end == len(data) {
			break
		}
	}
	return b.String()
}

// placeImage replaces the image's virtual placement with one of a new size.
// A placement with the same image and placement id replaces the old one.
func placeImage(id, cols, rows int) string {
	return ansi.KittyGraphics(nil, "a=p", fmt.Sprintf("i=%d", id), "p=1", "U=1", fmt.Sprintf("c=%d", cols), fmt.Sprintf("r=%d", rows), "q=2")
}

// deleteImage frees an image and its data in the terminal.
func deleteImage(id int) string {
	return ansi.KittyGraphics(nil, "a=d", "d=I", fmt.Sprintf("i=%d", id), "q=2")
}

// placeholderRow returns row r of an image's cells, colored with its id.
func placeholderRow(id, row, cols int) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("\x1b[38;5;%dm", id))
	for col := range cols {
		b.WriteRune(kitty.Placeholder)
		b.WriteRune(kitty.Diacritic(row))
		b.WriteRune(kitty.Diacritic(col))
	}
	b.WriteString("\x1b[39m")
	return b.String()
}

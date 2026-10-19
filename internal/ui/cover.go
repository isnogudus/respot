package ui

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg" // album covers are JPEG
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/iterm2"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/isnogudus/respot/internal/api"
)

// CoverMode selects how the album cover is drawn.
type CoverMode int

const (
	// CoverAuto picks CoverITerm2 in iTerm2 and CoverKitty in Ghostty or
	// kitty, both outside tmux, else CoverBlocks.
	CoverAuto CoverMode = iota
	// CoverITerm2 draws the cover image with iTerm2's inline image protocol.
	CoverITerm2
	// CoverKitty draws the cover image with the kitty graphics protocol's
	// Unicode placeholders, as Ghostty and kitty support.
	CoverKitty
	// CoverBlocks draws the cover in coloured half blocks, in any terminal
	// with 24-bit colour.
	CoverBlocks
	// CoverOff never draws a cover.
	CoverOff
)

// ParseCoverMode parses auto, iterm2, kitty, blocks or off.
func ParseCoverMode(s string) (CoverMode, error) {
	switch s {
	case "auto", "":
		return CoverAuto, nil
	case "iterm2":
		return CoverITerm2, nil
	case "kitty":
		return CoverKitty, nil
	case "blocks":
		return CoverBlocks, nil
	case "off":
		return CoverOff, nil
	}
	return CoverAuto, fmt.Errorf("unknown cover mode %q (want auto, iterm2, kitty, blocks or off)", s)
}

// resolve turns CoverAuto into a concrete mode for this terminal.
func (c CoverMode) resolve() CoverMode {
	if c != CoverAuto {
		return c
	}
	if os.Getenv("TMUX") != "" {
		return CoverBlocks
	}
	program, term := os.Getenv("TERM_PROGRAM"), os.Getenv("TERM")
	switch {
	case program == "iTerm.app" || os.Getenv("LC_TERMINAL") == "iTerm2":
		return CoverITerm2
	case program == "ghostty" || term == "xterm-ghostty" || term == "xterm-kitty" || os.Getenv("KITTY_WINDOW_ID") != "":
		return CoverKitty
	}
	return CoverBlocks
}

const (
	// coverRows is the cover's height in cells, as spotify_player's default.
	coverRows = 5
	// defaultCellAspect is a cell's height over its width when the terminal
	// does not report its pixel size.
	defaultCellAspect = 2.0
	// coverCacheSize bounds how many covers are kept.
	coverCacheSize = 16
	// coverMaxBytes bounds a downloaded cover.
	coverMaxBytes = 4 << 20
	// kittyImageID is the one image the kitty protocol shows: a new cover
	// replaces it, so the terminal never holds more than one.
	kittyImageID = 1
	// kittyMaxPixels bounds the side of the image sent to the terminal; the
	// cover is a few cells, so a full size cover would only cost bandwidth.
	kittyMaxPixels = 256
	// coverMinHeight and coverMinWidth hide the cover in smaller windows.
	coverMinHeight = 20
	coverMinWidth  = 60
)

// coverImage is a downloaded cover.
type coverImage struct {
	data []byte // as downloaded, sent to iTerm2 as is
	img  image.Image

	// blocks caches the half block rendering for blocksCols columns.
	blocks     []string
	blocksCols int

	// kitty caches the kitty transmission for kittyCols columns.
	kitty     string
	kittyCols int
}

// coverMsg carries a downloaded cover.
type coverMsg struct {
	url   string
	cover *coverImage
	err   error
}

// covers caches downloaded covers by URL.
type covers struct {
	images  map[string]*coverImage
	order   []string
	pending map[string]bool
	failed  map[string]bool
}

func (c *covers) get(url string) (*coverImage, bool) {
	img, ok := c.images[url]
	return img, ok
}

func (c *covers) put(url string, img *coverImage) {
	if c.images == nil {
		c.images = map[string]*coverImage{}
	}
	if _, ok := c.images[url]; !ok {
		c.order = append(c.order, url)
	}
	c.images[url] = img
	for len(c.order) > coverCacheSize {
		delete(c.images, c.order[0])
		c.order = c.order[1:]
	}
}

// showCover reports whether the player shows the cover.
func (m Model) showCover() bool {
	return m.coverMode != CoverOff && !m.coverOff && m.height >= coverMinHeight && m.width >= coverMinWidth
}

// coverCols is the cover's width in cells, so that coverRows rows make a
// square on this terminal's cells.
func (m Model) coverCols() int {
	aspect := m.cellAspect
	if aspect <= 0 {
		aspect = defaultCellAspect
	}
	return max(4, int(math.Round(coverRows*aspect)))
}

// fetchCovers loads the covers of the playing and the upcoming track that are
// neither cached, loading nor failed before.
func (m *Model) fetchCovers() tea.Cmd {
	if m.coverMode == CoverOff || m.coverOff || m.status == nil {
		return nil
	}
	var cmds []tea.Cmd
	for _, url := range []string{coverURL(m.status.Track), coverURL(m.status.NextTrack)} {
		if url == "" {
			continue
		}
		if _, ok := m.covers.get(url); ok || m.covers.pending[url] || m.covers.failed[url] {
			continue
		}
		if m.covers.pending == nil {
			m.covers.pending = map[string]bool{}
		}
		m.covers.pending[url] = true
		cmds = append(cmds, fetchCover(url))
	}
	return tea.Batch(cmds...)
}

// coverURL is the cover of a track, or "".
func coverURL(t *api.Track) string {
	if t == nil || t.AlbumCoverURL == nil {
		return ""
	}
	return *t.AlbumCoverURL
}

func fetchCover(url string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*requestTimeout)
		defer cancel()
		cover, err := downloadCover(ctx, url)
		return coverMsg{url: url, cover: cover, err: err}
	}
}

func downloadCover(ctx context.Context, url string) (*coverImage, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cover: %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, coverMaxBytes))
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return &coverImage{data: data, img: img}, nil
}

func (m Model) applyCover(msg coverMsg) Model {
	delete(m.covers.pending, msg.url)
	if msg.err != nil {
		if m.covers.failed == nil {
			m.covers.failed = map[string]bool{}
		}
		m.covers.failed[msg.url] = true
		return m
	}
	m.covers.put(msg.url, msg.cover)
	return m
}

// coverCells returns the cover's rows, each exactly coverCols cells wide.
//
// With kitty the first row also carries the image, sent once per cover and
// width, and every row is placeholder text the terminal draws the image
// over; Bubble Tea treats it as any other text. The transmission only
// changes with the cover, so it is resent only then.
//
// With iTerm2 the first row carries the image: it clears the cell box and
// draws the image into it, both with the cursor saved and restored, and every
// row then skips the box with a cursor movement instead of writing into it.
// Bubble Tea rewrites a changed row from its start, so writing spaces there
// would erase the image; a cursor movement leaves the cells alone. The image
// escape only changes with the track, so it is resent only then.
func (m Model) coverCells() []string {
	cols := m.coverCols()
	cover, ok := m.covers.get(coverURL(m.trackOrNil()))
	if !ok {
		return coverPlaceholder(cols)
	}

	if m.coverMode == CoverKitty {
		if cover.kittyCols != cols {
			cover.kitty, cover.kittyCols = kittyTransmit(cover.img, cols, coverRows, os.Getenv("TMUX") != ""), cols
		}
		rows := kittyPlaceholders(kittyImageID, cols, coverRows)
		rows[0] = cover.kitty + rows[0]
		return rows
	}

	if m.coverMode == CoverITerm2 {
		skip := ansi.CursorForward(cols)
		rows := make([]string, coverRows)
		for i := range rows {
			rows[i] = skip
		}
		rows[0] = iterm2Image(cover.data, cols, coverRows) + skip
		return rows
	}

	if cover.blocksCols != cols {
		cover.blocks, cover.blocksCols = renderHalfBlocks(cover.img, cols, coverRows), cols
	}
	return cover.blocks
}

// iterm2Image clears a cols × rows cell box at the cursor and draws data into
// it with iTerm2's inline image protocol, leaving the cursor where it was.
func iterm2Image(data []byte, cols, rows int) string {
	var sb strings.Builder
	sb.WriteString(ansi.SaveCursor)
	for i := range rows {
		if i > 0 {
			sb.WriteString(ansi.CursorDown(1))
		}
		sb.WriteString(ansi.EraseCharacter(cols))
	}
	sb.WriteString(ansi.RestoreCursor + ansi.SaveCursor)
	sb.WriteString(ansi.ITerm2(iterm2.File{
		Size:    int64(len(data)),
		Width:   iterm2.Cells(cols),
		Height:  iterm2.Cells(rows),
		Inline:  true,
		Content: []byte(base64.StdEncoding.EncodeToString(data)),
	}))
	sb.WriteString(ansi.RestoreCursor)
	return sb.String()
}

// kittyTransmit sends img to the terminal as the kitty image kittyImageID,
// with a virtual placement of cols × rows cells that kittyPlaceholders shows.
// The terminal answers nothing (q=2), so no reply lands in respot's input.
// In tmux every chunk is passed through to the outer terminal.
func kittyTransmit(img image.Image, cols, rows int, tmux bool) string {
	opts := &kitty.Options{
		Action:           kitty.TransmitAndPut,
		Quiet:            2,
		ID:               kittyImageID,
		Format:           kitty.PNG,
		Transmission:     kitty.Direct,
		Columns:          cols,
		Rows:             rows,
		VirtualPlacement: true,
		Chunk:            true,
	}
	if tmux {
		opts.ChunkFormatter = ansi.TmuxPassthrough
	}
	var sb strings.Builder
	if err := kitty.EncodeGraphics(&sb, shrink(img, kittyMaxPixels), opts); err != nil {
		return ""
	}
	return sb.String()
}

// kittyPlaceholders returns rows of cols cells that show the image with id
// through its virtual placement: each cell is the placeholder character with
// its row and column as diacritics, coloured with the image id.
func kittyPlaceholders(id, cols, rows int) []string {
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", id>>16&0xff, id>>8&0xff, id&0xff)
	lines := make([]string, rows)
	for y := range rows {
		var sb strings.Builder
		sb.WriteString(fg)
		for x := range cols {
			sb.WriteRune(kitty.Placeholder)
			sb.WriteRune(kitty.Diacritic(y))
			sb.WriteRune(kitty.Diacritic(x))
		}
		sb.WriteString("\x1b[39m")
		lines[y] = sb.String()
	}
	return lines
}

// shrink scales img down so that neither side exceeds limit pixels.
func shrink(img image.Image, limit int) image.Image {
	b := img.Bounds()
	if b.Dx() <= limit && b.Dy() <= limit {
		return img
	}
	w, h := limit, limit
	if b.Dx() > b.Dy() {
		h = max(1, b.Dy()*limit/b.Dx())
	} else {
		w = max(1, b.Dx()*limit/b.Dy())
	}
	px := downscale(img, w, h)
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			p := px[y][x]
			i := out.PixOffset(x, y)
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = p.r, p.g, p.b, 0xff
		}
	}
	return out
}

// rgb is an averaged pixel.
type rgb struct{ r, g, b uint8 }

// downscale averages img into a w × h grid of pixels.
func downscale(img image.Image, w, h int) [][]rgb {
	b := img.Bounds()
	out := make([][]rgb, h)
	for y := range h {
		out[y] = make([]rgb, w)
		y0 := b.Min.Y + y*b.Dy()/h
		y1 := max(y0+1, b.Min.Y+(y+1)*b.Dy()/h)
		for x := range w {
			x0 := b.Min.X + x*b.Dx()/w
			x1 := max(x0+1, b.Min.X+(x+1)*b.Dx()/w)
			var r, g, bl, n uint64
			for py := y0; py < y1; py++ {
				for px := x0; px < x1; px++ {
					cr, cg, cb, _ := img.At(px, py).RGBA()
					r, g, bl, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), n+1
				}
			}
			out[y][x] = rgb{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8)}
		}
	}
	return out
}

// renderHalfBlocks draws img in cols × rows cells: every cell is an upper
// half block whose foreground is the upper pixel and background the lower.
func renderHalfBlocks(img image.Image, cols, rows int) []string {
	px := downscale(img, cols, rows*2)
	lines := make([]string, rows)
	for y := range rows {
		var sb strings.Builder
		for x := range cols {
			top, bottom := px[2*y][x], px[2*y+1][x]
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", top.r, top.g, top.b, bottom.r, bottom.g, bottom.b)
		}
		sb.WriteString("\x1b[0m")
		lines[y] = sb.String()
	}
	return lines
}

// coverPlaceholder fills the cover's cells while there is no cover.
func coverPlaceholder(cols int) []string {
	lines := make([]string, coverRows)
	for i := range lines {
		lines[i] = subtleStyle.Render(strings.Repeat("░", cols))
	}
	return lines
}

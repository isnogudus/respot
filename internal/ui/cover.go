package ui

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg" // album covers are JPEG
	_ "image/png"
	"io"
	"net/http"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jmt/my-spotify-tui/internal/api"
)

const (
	// coverCols × coverRows cells show the cover; each cell stacks two
	// pixels, so the cover is coverCols × 2·coverRows pixels, square on a
	// terminal whose cells are twice as high as wide.
	coverCols = 16
	coverRows = 8
	// coverCacheSize bounds how many rendered covers are kept.
	coverCacheSize = 16
	// coverMaxBytes bounds a downloaded cover.
	coverMaxBytes = 4 << 20
	// coverMinHeight and coverMinWidth hide the cover in smaller windows.
	coverMinHeight = 24
	coverMinWidth  = 60
)

// coverMsg carries a rendered cover.
type coverMsg struct {
	url   string
	lines []string
	err   error
}

// covers caches rendered covers by URL.
type covers struct {
	rendered map[string][]string
	order    []string
	pending  map[string]bool
	failed   map[string]bool
}

func (c *covers) get(url string) ([]string, bool) {
	lines, ok := c.rendered[url]
	return lines, ok
}

func (c *covers) put(url string, lines []string) {
	if c.rendered == nil {
		c.rendered = map[string][]string{}
	}
	if _, ok := c.rendered[url]; !ok {
		c.order = append(c.order, url)
	}
	c.rendered[url] = lines
	for len(c.order) > coverCacheSize {
		delete(c.rendered, c.order[0])
		c.order = c.order[1:]
	}
}

// showCover reports whether the player shows the cover.
func (m Model) showCover() bool {
	return !m.coverOff && m.height >= coverMinHeight && m.width >= coverMinWidth
}

// fetchCovers loads the covers of the playing and the upcoming track that are
// neither cached, loading nor failed before.
func (m *Model) fetchCovers() tea.Cmd {
	if m.coverOff || m.status == nil {
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
		img, err := downloadImage(ctx, url)
		if err != nil {
			return coverMsg{url: url, err: err}
		}
		return coverMsg{url: url, lines: renderHalfBlocks(img, coverCols, coverRows)}
	}
}

func downloadImage(ctx context.Context, url string) (image.Image, error) {
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
	img, _, err := image.Decode(io.LimitReader(resp.Body, coverMaxBytes))
	return img, err
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
	m.covers.put(msg.url, msg.lines)
	return m
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
func coverPlaceholder() []string {
	lines := make([]string, coverRows)
	for i := range lines {
		lines[i] = subtleStyle.Render(strings.Repeat("░", coverCols))
	}
	return lines
}

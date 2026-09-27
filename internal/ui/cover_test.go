package ui

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/jmt/my-spotify-tui/internal/api"
)

// quadrants is a 4×4 image: red top half, blue bottom half.
func quadrants() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			c := color.RGBA{255, 0, 0, 255}
			if y >= 2 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

func TestDownscaleAverages(t *testing.T) {
	px := downscale(quadrants(), 1, 2)
	if px[0][0] != (rgb{255, 0, 0}) || px[1][0] != (rgb{0, 0, 255}) {
		t.Fatalf("downscale = %v, want red over blue", px)
	}
	if avg := downscale(quadrants(), 1, 1)[0][0]; avg.r < 120 || avg.r > 135 || avg.b < 120 || avg.b > 135 {
		t.Fatalf("one pixel must average both halves, got %v", avg)
	}
}

func TestRenderHalfBlocks(t *testing.T) {
	lines := renderHalfBlocks(quadrants(), 2, 1)
	if len(lines) != 1 || ansi.StringWidth(lines[0]) != 2 {
		t.Fatalf("want one line two cells wide, got %q", lines)
	}
	if !strings.Contains(lines[0], "\x1b[38;2;255;0;0m\x1b[48;2;0;0;255m▀") {
		t.Fatalf("a cell must be the upper pixel over the lower: %q", lines[0])
	}
}

func TestCoversCacheIsBounded(t *testing.T) {
	var c covers
	for i := range coverCacheSize + 3 {
		c.put(string(rune('a'+i)), []string{"x"})
	}
	if len(c.rendered) != coverCacheSize {
		t.Fatalf("cache holds %d covers, want %d", len(c.rendered), coverCacheSize)
	}
	if _, ok := c.get("a"); ok {
		t.Fatalf("the oldest cover must be dropped")
	}
}

func TestCoverIsFetchedRenderedAndShown(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, quadrants()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(buf.Bytes()) }))
	defer srv.Close()

	m := newTestModel(t)
	url := srv.URL + "/cover.png"
	st := status(testCtx, 1)
	st.st.Track.AlbumCoverURL = &url
	nm, _ := m.Update(st)
	m = nm.(Model)
	if !m.covers.pending[url] {
		t.Fatalf("a new cover must be fetched")
	}
	if cmd := m.fetchCovers(); cmd != nil {
		t.Fatalf("a pending cover must not be fetched twice")
	}

	m = update(m, fetchCover(url)())
	lines, ok := m.covers.get(url)
	if !ok || len(lines) != coverRows {
		t.Fatalf("cover not rendered: %d lines", len(lines))
	}
	if view := m.View(); !strings.Contains(view, "\x1b[38;2;255;0;0m") {
		t.Fatalf("the player must show the cover")
	}
}

func TestCoverToggleAndSmallWindows(t *testing.T) {
	m := newTestModel(t) // 100 × 30
	if !m.showCover() {
		t.Fatalf("the cover shows in a 100 × 30 window")
	}
	full := m.listHeight()
	m = update(m, key("i"))
	if m.showCover() || m.listHeight() <= full {
		t.Fatalf("i hides the cover and gives the rows to the list")
	}
	m = update(m, key("i"))
	m = update(m, tea.WindowSizeMsg{Width: 100, Height: coverMinHeight - 1})
	if m.showCover() {
		t.Fatalf("a small window hides the cover")
	}
}

func TestFailedCoverIsNotRetried(t *testing.T) {
	m := newTestModel(t)
	url := "http://127.0.0.1:1/cover.jpg"
	m = update(m, coverMsg{url: url, err: api.ErrNoSession})
	st := status(testCtx, 1)
	st.st.Track.AlbumCoverURL = &url
	nm, _ := m.Update(st)
	if nm.(Model).covers.pending[url] {
		t.Fatalf("a failed cover must not be fetched again")
	}
}

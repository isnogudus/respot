package ui

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
		c.put(string(rune('a'+i)), &coverImage{})
	}
	if len(c.images) != coverCacheSize {
		t.Fatalf("cache holds %d covers, want %d", len(c.images), coverCacheSize)
	}
	if _, ok := c.get("a"); ok {
		t.Fatalf("the oldest cover must be dropped")
	}
}

func serveCover(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, quadrants()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(buf.Bytes()) }))
	t.Cleanup(srv.Close)
	return srv.URL + "/cover.png"
}

// playWithCover makes m play track 1 with the cover at url, fetched.
func playWithCover(t *testing.T, m Model, url string) Model {
	t.Helper()
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
	return update(m, fetchCover(url)())
}

func TestBlocksCover(t *testing.T) {
	m := playWithCover(t, newTestModel(t), serveCover(t))
	rows := m.coverCells()
	if len(rows) != coverRows || ansi.StringWidth(rows[0]) != m.coverCols() {
		t.Fatalf("want %d rows %d cells wide, got %d rows, %d wide", coverRows, m.coverCols(), len(rows), ansi.StringWidth(rows[0]))
	}
	if !strings.Contains(m.View(), "\x1b[38;2;255;0;0m") {
		t.Fatalf("the player must show the cover")
	}
}

func TestITerm2Cover(t *testing.T) {
	m := newTestModel(t)
	m.coverMode = CoverITerm2
	m = playWithCover(t, m, serveCover(t))
	cols := m.coverCols()

	rows := m.coverCells()
	if !strings.Contains(rows[0], "\x1b]1337;File=") || !strings.Contains(rows[0], fmt.Sprintf("width=%d;height=%d;inline=1", cols, coverRows)) {
		t.Fatalf("the first row must draw the image into the cover's cells: %q", rows[0][:min(len(rows[0]), 120)])
	}
	skip := ansi.CursorForward(cols)
	for i, r := range rows {
		if !strings.HasSuffix(r, skip) || strings.ContainsAny(ansi.Strip(r), "░▀ ") {
			t.Fatalf("row %d must skip the image cells instead of writing them: %q", i, r[max(0, len(r)-20):])
		}
		if i > 0 && strings.Contains(r, "1337") {
			t.Fatalf("only the first row may carry the image")
		}
	}

	// The rows beside the image change every second; the image must not.
	first := strings.Split(m.View(), "\n")[1]
	m.statusAt = m.statusAt.Add(-5 * time.Second)
	if again := strings.Split(m.View(), "\n")[1]; again != first {
		t.Fatalf("the image row must stay the same while the track plays, so it is not resent")
	}
}

func TestCoverColsFollowCellAspect(t *testing.T) {
	m := newTestModel(t)
	m.cellAspect = 0
	if got := m.coverCols(); got != 10 {
		t.Fatalf("default: %d columns, want 10", got)
	}
	m.cellAspect = 17.0 / 8.0
	if got := m.coverCols(); got != 11 {
		t.Fatalf("8×17 px cells: %d columns, want 11", got)
	}
}

func TestParseCoverMode(t *testing.T) {
	for in, want := range map[string]CoverMode{"auto": CoverAuto, "iterm2": CoverITerm2, "blocks": CoverBlocks, "off": CoverOff} {
		if got, err := ParseCoverMode(in); err != nil || got != want {
			t.Errorf("ParseCoverMode(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseCoverMode("sixel"); err == nil {
		t.Errorf("unknown modes must be rejected")
	}
}

func TestCoverAutoDetection(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	t.Setenv("TMUX", "")
	if got := CoverAuto.resolve(); got != CoverITerm2 {
		t.Fatalf("iTerm2 outside tmux must draw images, got %v", got)
	}
	t.Setenv("TMUX", "/tmp/tmux-501/default,1,0")
	if got := CoverAuto.resolve(); got != CoverBlocks {
		t.Fatalf("inside tmux the images would not pass through, got %v", got)
	}
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	t.Setenv("LC_TERMINAL", "")
	t.Setenv("TMUX", "")
	if got := CoverAuto.resolve(); got != CoverBlocks {
		t.Fatalf("other terminals get blocks, got %v", got)
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

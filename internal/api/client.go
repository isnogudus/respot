// Package api is a small client for the go-librespot daemon HTTP API.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	// ErrContextTracksUnavailable is returned when the daemon does not serve
	// /context/tracks (older version or metadata.enabled is false).
	ErrContextTracksUnavailable = errors.New("context track listing unavailable")
	// ErrLibraryUnavailable is returned when the daemon does not serve
	// /library/playlists.
	ErrLibraryUnavailable = errors.New("library listing unavailable")
	// ErrLibraryWriteUnavailable is returned when the daemon cannot write to
	// Liked Songs or playlists.
	ErrLibraryWriteUnavailable = errors.New("daemon cannot write to the library")
	// ErrLikedUnavailable is returned when the daemon cannot tell which
	// tracks are liked.
	ErrLikedUnavailable = errors.New("liked state unavailable")
	// ErrNoSession is returned when the daemon has no active Spotify session.
	ErrNoSession = errors.New("no active Spotify session")
)

// Track is a track or podcast episode.
type Track struct {
	URI           string   `json:"uri"`
	Name          string   `json:"name"`
	ArtistNames   []string `json:"artist_names"`
	AlbumName     string   `json:"album_name"`
	AlbumCoverURL *string  `json:"album_cover_url"`
	Position      int64    `json:"position"`
	Duration      int64    `json:"duration"`
	ReleaseDate   string   `json:"release_date"`
	TrackNumber   int      `json:"track_number"`
	DiscNumber    int      `json:"disc_number"`
	Format        string   `json:"format"`
	Codec         string   `json:"codec"`
	Bitrate       *int     `json:"bitrate"`
	SampleRate    *int     `json:"sample_rate"`
	BitDepth      *int     `json:"bit_depth"`
}

// Status is the player status.
type Status struct {
	Username       string  `json:"username"`
	DeviceID       string  `json:"device_id"`
	DeviceType     string  `json:"device_type"`
	DeviceName     string  `json:"device_name"`
	PlayOrigin     *string `json:"play_origin"`
	ContextURI     *string `json:"context_uri"`
	ContextName    *string `json:"context_name"`
	Stopped        bool    `json:"stopped"`
	Paused         bool    `json:"paused"`
	Buffering      bool    `json:"buffering"`
	Volume         int     `json:"volume"`
	VolumeSteps    int     `json:"volume_steps"`
	RepeatContext  bool    `json:"repeat_context"`
	RepeatTrack    bool    `json:"repeat_track"`
	ShuffleContext bool    `json:"shuffle_context"`
	Track          *Track  `json:"track"`
	NextTrack      *Track  `json:"next_track"`
}

// ContextTrackItem is one entry of a context listing.
type ContextTrackItem struct {
	URI   string `json:"uri"`
	Track *Track `json:"track"`
}

// ContextTracks is a context's track listing.
type ContextTracks struct {
	URI    string             `json:"uri"`
	Ready  bool               `json:"ready"`
	Length int                `json:"length"`
	Cached int                `json:"cached"`
	Tracks []ContextTrackItem `json:"tracks"`
}

// Complete reports whether the listing is fully enumerated and resolved.
func (c *ContextTracks) Complete() bool {
	return c.Ready && c.Cached >= c.Length
}

// LibraryPlaylist is a playlist in the user's library.
type LibraryPlaylist struct {
	URI           string   `json:"uri"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	OwnerUsername string   `json:"owner_username"`
	Length        int      `json:"length"`
	ImageURL      *string  `json:"image_url"`
	Collaborative bool     `json:"collaborative"`
	CanEdit       bool     `json:"can_edit"`
	Folder        []string `json:"folder"`
}

type libraryPage struct {
	Total int               `json:"total"`
	Items []LibraryPlaylist `json:"items"`
}

// libraryPageSize is the largest page /library/playlists hands out.
const libraryPageSize = 500

// Client talks to a go-librespot daemon.
type Client struct {
	base string
	http *http.Client
}

// New creates a client for the daemon at base, e.g. "http://localhost:3678".
func New(base string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

// Base returns the daemon base URL.
func (c *Client) Base() string { return c.base }

func (c *Client) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, rd)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return resp.StatusCode, fmt.Errorf("%s %s: %s %s", method, path, resp.Status, strings.TrimSpace(string(msg)))
	}
	if out != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.StatusCode, fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return resp.StatusCode, nil
}

func (c *Client) post(ctx context.Context, path string, body any) error {
	_, err := c.do(ctx, http.MethodPost, path, body, nil)
	return err
}

// Status returns the player status, or nil when there is no active session.
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var st Status
	code, err := c.do(ctx, http.MethodGet, "/status", nil, &st)
	if err != nil {
		return nil, err
	}
	if code == http.StatusNoContent {
		return nil, nil
	}
	return &st, nil
}

// ContextTracks lists the tracks of a context. It never blocks on the network
// on the daemon side; poll until the result is Complete.
func (c *Client) ContextTracks(ctx context.Context, uri string) (*ContextTracks, error) {
	var ct ContextTracks
	code, err := c.do(ctx, http.MethodGet, "/context/tracks?uri="+url.QueryEscape(uri), nil, &ct)
	if code == http.StatusNotFound {
		return nil, ErrContextTracksUnavailable
	}
	if err != nil {
		return nil, err
	}
	return &ct, nil
}

// LibraryPlaylists lists all playlists in the user's library, in library order.
func (c *Client) LibraryPlaylists(ctx context.Context) ([]LibraryPlaylist, error) {
	var all []LibraryPlaylist
	for {
		var page libraryPage
		path := fmt.Sprintf("/library/playlists?offset=%d&limit=%d", len(all), libraryPageSize)
		code, err := c.do(ctx, http.MethodGet, path, nil, &page)
		switch {
		case code == http.StatusNotFound:
			return nil, ErrLibraryUnavailable
		case err != nil:
			return nil, err
		case code == http.StatusNoContent:
			return nil, ErrNoSession
		}
		all = append(all, page.Items...)
		if len(page.Items) == 0 || len(all) >= page.Total {
			return all, nil
		}
	}
}

type likedStates struct {
	Items []struct {
		URI   string `json:"uri"`
		Liked bool   `json:"liked"`
	} `json:"items"`
}

// MaxLikedQuery is how many URIs one Liked query may carry.
const MaxLikedQuery = 50

// Liked tells for each of uris whether it is in Liked Songs.
func (c *Client) Liked(ctx context.Context, uris []string) (map[string]bool, error) {
	var states likedStates
	code, err := c.do(ctx, http.MethodGet, "/library/liked?uris="+url.QueryEscape(strings.Join(uris, ",")), nil, &states)
	switch {
	case code == http.StatusNotFound:
		return nil, ErrLikedUnavailable
	case err != nil:
		return nil, err
	case code == http.StatusNoContent:
		return nil, ErrNoSession
	}
	out := make(map[string]bool, len(states.Items))
	for _, s := range states.Items {
		out[s.URI] = s.Liked
	}
	return out, nil
}

// SetLiked saves uris to, or removes them from, Liked Songs.
func (c *Client) SetLiked(ctx context.Context, uris []string, liked bool) error {
	return c.libraryWrite(ctx, "/library/liked", map[string]any{"uris": uris, "liked": liked})
}

// AddToPlaylist appends uris to the end of a playlist.
func (c *Client) AddToPlaylist(ctx context.Context, playlistURI string, uris []string) error {
	return c.libraryWrite(ctx, "/library/playlists/add_tracks", map[string]any{"playlist_uri": playlistURI, "uris": uris})
}

func (c *Client) libraryWrite(ctx context.Context, path string, body any) error {
	code, err := c.do(ctx, http.MethodPost, path, body, nil)
	switch {
	case code == http.StatusNotFound:
		return ErrLibraryWriteUnavailable
	case err != nil:
		return err
	case code == http.StatusNoContent:
		return ErrNoSession
	}
	return nil
}

func (c *Client) PlayPause(ctx context.Context) error { return c.post(ctx, "/player/playpause", nil) }
func (c *Client) Next(ctx context.Context) error      { return c.post(ctx, "/player/next", nil) }
func (c *Client) Prev(ctx context.Context) error      { return c.post(ctx, "/player/prev", nil) }

// Play starts playing uri; skipTo optionally selects a track within a context.
func (c *Client) Play(ctx context.Context, uri, skipTo string) error {
	body := map[string]any{"uri": uri}
	if skipTo != "" {
		body["skip_to_uri"] = skipTo
	}
	return c.post(ctx, "/player/play", body)
}

func (c *Client) AddToQueue(ctx context.Context, uri string) error {
	return c.post(ctx, "/player/add_to_queue", map[string]any{"uri": uri})
}

func (c *Client) Seek(ctx context.Context, posMs int64, relative bool) error {
	return c.post(ctx, "/player/seek", map[string]any{"position": posMs, "relative": relative})
}

func (c *Client) SetVolume(ctx context.Context, vol int, relative bool) error {
	return c.post(ctx, "/player/volume", map[string]any{"volume": vol, "relative": relative})
}

func (c *Client) SetShuffle(ctx context.Context, on bool) error {
	return c.post(ctx, "/player/shuffle_context", map[string]any{"shuffle_context": on})
}

func (c *Client) SetRepeatContext(ctx context.Context, on bool) error {
	return c.post(ctx, "/player/repeat_context", map[string]any{"repeat_context": on})
}

func (c *Client) SetRepeatTrack(ctx context.Context, on bool) error {
	return c.post(ctx, "/player/repeat_track", map[string]any{"repeat_track": on})
}

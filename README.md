# my-spotify-tui

A terminal UI for the [go-librespot](https://github.com/devgianlu/go-librespot) daemon.
It shows the current track with a live progress bar and controls playback through
the daemon's HTTP API. Updates arrive instantly via the `/events` WebSocket, with
status polling as a fallback.

## Build & run

```sh
go build -o my-spotify-tui .
./my-spotify-tui                          # uses http://localhost:3678
./my-spotify-tui -addr http://pi.local:3678
LIBRESPOT_ADDR=http://pi.local:3678 ./my-spotify-tui
```

## Browsing

The UI works like lynx: the player sits on top, below it a page you browse
with the arrow keys.

```
Start
├─ ▶ Now playing · <what plays now>
├─ ♥ Liked Songs
├─ ≡ Playlists      ← your Spotify folders are folders here too
├─ ◎ Albums         ← saved albums
└─ ☺ Artists        ← followed artists
```

- **↑ / ↓** move, **→** opens the row, **←** goes back to where you were.
- **Enter** plays the row: a playlist, album or artist from its start, a track
  within the page's list.
- The player shows the album cover, 5 rows high. In iTerm2 it is the real
  image (iTerm2's inline image protocol); elsewhere, and inside tmux, it is
  drawn in coloured half blocks (`▀`). `-cover auto|iterm2|blocks|off`
  overrides the choice, `i` hides it, and it hides in windows smaller than
  60 × 20.
- **→ on a track** opens its actions: play, queue, add to or remove from Liked
  Songs, add to a playlist, go to its album or artist.

## Keys

| Key                | Action                                         |
|--------------------|------------------------------------------------|
| `↑` `↓` / `j` `k`  | move (`pgup` `pgdn` `g` `G` too)               |
| `→` / `l`          | open; on a track: its actions                  |
| `←` / `h`          | back                                           |
| `enter`            | play the row                                   |
| `/`                | filter the page; `enter` keeps it, `esc` clears it |
| `m`                | start page                                     |
| `c`                | now playing, on the playing track (and follow it again) |
| `space`            | play / pause                                   |
| `n` / `p`          | next / previous track                          |
| `shift+←` / `shift+→` | seek ±10 s                                  |
| `+` / `-`          | volume ±5 %                                    |
| `s`                | toggle shuffle                                 |
| `r`                | cycle repeat: off → all → track                |
| `f`                | add the selected (or playing) track to Liked Songs, or remove it when liked (♥) |
| `A`                | add the selected (or playing) track to Liked Songs or a playlist |
| `e`                | add the selected track to the queue            |
| `o` / `a`          | play / queue a Spotify URI or open.spotify.com link |
| `ctrl+r`           | reload the library                             |
| `i`                | show / hide the album cover                    |
| `?`                | help                                           |
| `q`                | quit                                           |

## Daemon requirements

Track lists use `GET /context/tracks`, which needs metadata caching in the
daemon's `config.yml`:

```yaml
metadata:
  enabled: true
```

The library pages, Liked Songs hearts, adding tracks and the album and artist
links use endpoints that are not in upstream go-librespot yet
(`/library/playlists`, `/library/albums`, `/library/artists`,
`/library/liked`, `/library/playlists/add_tracks` and `album_uri` /
`artist_uris` on tracks). Build the daemon from the `feature/library-write`
branch of the fork for them; with other daemons those parts show a hint and
everything else keeps working.

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

## Keys

| Key            | Action                                   |
|----------------|------------------------------------------|
| `space`        | play / pause                             |
| `n` / `p`      | next / previous track                    |
| `←` / `→`      | seek ±10 s                               |
| `+` / `-`      | volume ±5 %                              |
| `s`            | toggle shuffle                           |
| `r`            | cycle repeat: off → all → track          |
| `o`            | play a Spotify URI or open.spotify.com link |
| `a`            | add a URI or link to the queue           |
| `b`            | toggle the library (Liked Songs and playlists) |
| `l`            | toggle the track list of the current context; in the library, show the selected playlist's tracks |
| `esc`          | back to the library / close the panel    |
| `j` `k` `g` `G`| navigate lists                           |
| `enter`        | in the library, open the selected playlist; in a track list, play the selected track |
| `P`            | play the selected playlist from the library |
| `/`            | filter the library by name or folder; `enter` keeps the filter, `esc` clears it |
| `e`            | enqueue the selected track               |
| `c`            | jump to what is playing (and follow it again) |
| `ctrl+r`       | reload the library                       |
| `?`            | help                                     |
| `q`            | quit                                     |

## Library

The library lists Liked Songs and the playlists of your library, including
followed and Spotify-owned ones, via `GET /library/playlists`. That endpoint is
not part of upstream go-librespot yet; it needs a build of the
`feature/library-playlists` branch. With other daemons the library panel shows a
hint and everything else keeps working.

## Track list

The track list uses `GET /context/tracks`, which requires a go-librespot version that
provides it and metadata caching enabled in `config.yml`:

```yaml
metadata:
  enabled: true
```

While the list is open the cursor follows the playing track. Moving the cursor
yourself pauses that; `c` or `enter` resumes it.

Without it the rest of the UI works normally and the list shows a hint.

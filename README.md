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
| `l` / `tab`    | toggle the track list of the current context |
| `j` `k` `g` `G`| navigate the track list                  |
| `enter` / `e`  | play / enqueue the selected track        |
| `c`            | jump to the current track and follow it again |
| `?`            | help                                     |
| `q`            | quit                                     |

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

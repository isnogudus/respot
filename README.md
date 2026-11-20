# respot

A terminal UI for the [go-librespot](https://github.com/devgianlu/go-librespot)
daemon: browse your Spotify library like in lynx, see what is playing with its
cover, and control playback — all through the daemon's HTTP API.

```
 ♫ respot  tron · computer                                                ● live
 ██████████  ▶ Close My Eyes ♥
 ██████████  Leonid Vorobyev & Friends · Summer Sessions (2019)
 ██████████  from Liked Songs
 ██████████  ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━──────────────────────── 1:12 / 3:20
 ██████████  vol 60%   s ⤮ shuffle   r ⟳ off   vorbis 320 kbps
 ────────────────────────────────────────────────────────────────────────────────
 Start › Playlists › Cool                                                     123
     1 Tell Me — Pages                                                   ♥ 3:52
 ▶   2 Close My Eyes — Leonid Vorobyev & Friends                         ♥ 3:20
     3 Keep on Running — Byrne & Barnes                                    3:17
 ↑↓ move  → open  ← back  ⏎ play  space pause  ⇧←→ seek  / filter  ? help  q quit
```

## Install

Requires Go 1.24.2 or newer and a running go-librespot daemon with its API
server enabled.

```sh
go install github.com/isnogudus/respot@latest
respot                                   # uses http://localhost:3678
respot -addr http://raspberrypi.local:3678
LIBRESPOT_ADDR=http://raspberrypi.local:3678 respot
```

The daemon's API has no authentication. To control a daemon on another
machine, prefer an SSH tunnel over exposing its port:

```sh
ssh -N -L 3678:127.0.0.1:3678 pi@raspberrypi.local
```

## Browsing

The player sits on top, below it a page you browse with the arrow keys:

```
Start
├─ ♪ Now playing · <what plays now>
├─ ♥ Liked Songs
├─ ≡ Playlists      ← your Spotify folders are folders here too
├─ ◎ Albums         ← saved albums
└─ ☺ Artists        ← followed artists
```

- **↑ / ↓** move, **→** opens the row, **←** goes back to where you were.
- **Enter** plays the row: a playlist, album or artist from its start, a track
  within the page's list.
- **→ on a track** opens its actions: play, queue, add to or remove from Liked
  Songs, add to a playlist, remove it from your playlist, go to its album or
  artist. Adding a track a playlist already holds asks first, and so does
  removing one.
- **N** creates a playlist, at the top of your library like in Spotify.
- **x on a playlist** deletes it, or for someone else's playlist removes it
  from your library, after asking. Like in Spotify, a deleted playlist stays
  for its followers and can be restored.
- **/** filters any page.
- **F** filters a track list by a playlist: without the tracks in it, or only
  those also in it, as a page of its own — `Liked Songs › − Cool` lists your
  liked tracks that Cool lacks. Filters chain, **←** takes one back, and **F**
  on a filtered list adds all it shows to a playlist or queues them. Tracks are
  compared by their Spotify uri, so the same recording from another album
  counts as another track. Enter plays the whole list from the selected track,
  not only what the filter shows.

The album cover is drawn as the real image in iTerm2 (its inline image
protocol) and in Ghostty and kitty (the kitty graphics protocol's Unicode
placeholders), and in coloured half blocks (`▀`) elsewhere and inside tmux.
`-cover auto|iterm2|kitty|blocks|off` overrides the choice, `i` hides it,
and it hides by itself in windows smaller than 60 × 20.

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
| `s`                | shuffle ⤮ / play in order →                    |
| `r`                | cycle repeat: off → all → track                |
| `f`                | add the selected (or playing) track to Liked Songs, or remove it when liked (♥) |
| `A`                | add the selected (or playing) track to Liked Songs or a playlist |
| `e`                | add the selected track to the queue            |
| `x`                | remove the selected track from your playlist, or delete the selected playlist (asks first) |
| `N`                | create a playlist                              |
| `F`                | filter the track list by a playlist; on a filtered one also add or queue all it shows |
| `o` / `a`          | play / queue a Spotify URI or open.spotify.com link |
| `ctrl+r`           | reload the library                             |
| `i`                | show / hide the album cover                    |
| `?`                | help                                           |
| `q`                | quit                                           |

## Daemon requirements

respot talks only to go-librespot's HTTP API and uses what the daemon offers;
parts the daemon lacks show a hint and everything else keeps working.

Track lists need metadata caching in the daemon's `config.yml`:

```yaml
metadata:
  enabled: true
```

Some features need daemon endpoints that newer go-librespot releases added:

| Feature | Needs | Available in |
|---|---|---|
| Player, controls, now playing list, cover | — | any recent release |
| Playlists page | `GET /library/playlists` | 0.10.3 ([#402](https://github.com/devgianlu/go-librespot/pull/402)) |
| Go to album / artist | `album_uri`, `artist_uris` on tracks | 0.10.3 ([#404](https://github.com/devgianlu/go-librespot/pull/404)) |
| Liked Songs hearts, `f`, adding to playlists | `/library/liked`, `/library/playlists/add_tracks` | 0.10.3 ([#403](https://github.com/devgianlu/go-librespot/pull/403)) |
| Removing from playlists, asking before duplicates | `/library/playlists/remove_track`, `/library/playlists/contains` | 0.10.4 ([#411](https://github.com/devgianlu/go-librespot/pull/411)) |
| Albums and Artists pages | `/library/albums`, `/library/artists` | 0.10.6 ([#405](https://github.com/devgianlu/go-librespot/pull/405)) |
| Creating and deleting playlists | `/library/playlists/create`, `/library/playlists/remove` | pending ([#450](https://github.com/devgianlu/go-librespot/pull/450)) |

Prefer 0.10.7 or newer. Before it, respot cannot find the playing track in
its list when Spotify plays it under another uri, as it does for tracks
relinked to a version available in your country
([#448](https://github.com/devgianlu/go-librespot/pull/448)), and on macOS
playback can turn distorted after a few track changes until the daemon
restarts ([#447](https://github.com/devgianlu/go-librespot/pull/447)).

## License

MIT, see [LICENSE](LICENSE).

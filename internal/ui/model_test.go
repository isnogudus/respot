package ui

import "testing"

func TestNormalizeURI(t *testing.T) {
	cases := map[string]string{
		"spotify:track:5C1KYz8GLB8sGZYTpw3LpR":                             "spotify:track:5C1KYz8GLB8sGZYTpw3LpR",
		"  https://open.spotify.com/track/5C1KYz8GLB8sGZYTpw3LpR?si=abc  ": "spotify:track:5C1KYz8GLB8sGZYTpw3LpR",
		"https://open.spotify.com/intl-de/album/0sNOF9WDwhWunNAHPD3Baj":    "spotify:album:0sNOF9WDwhWunNAHPD3Baj",
		"https://open.spotify.com/playlist/3J73XcDyhVRDXdjeRkakPj":         "spotify:playlist:3J73XcDyhVRDXdjeRkakPj",
	}
	for in, want := range cases {
		got, err := normalizeURI(in)
		if err != nil || got != want {
			t.Errorf("normalizeURI(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "foo", "https://example.com/track/x", "https://open.spotify.com/track"} {
		if _, err := normalizeURI(bad); err == nil {
			t.Errorf("normalizeURI(%q): expected error", bad)
		}
	}
}

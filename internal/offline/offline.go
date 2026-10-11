// Package offline holds the settings of offline deployments (no internet
// access): the app then never makes browsers load anything from outside.
package offline

import (
	"net/url"
	"os"
	"strings"
)

const (
	// Env set to "1" enables the offline mode: public pages only load files
	// from the app, the map layout uses MapTilesEnv tiles or none.
	Env = "POCKETBASEPLUS_OFFLINE"
	// MapTilesEnv sets the tile URL of the maps (e.g. a tile server on the
	// local network): "http://10.0.0.5:8080/tile/{z}/{x}/{y}.png".
	MapTilesEnv = "POCKETBASEPLUS_MAP_TILES"

	defaultTiles = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"
)

// Enabled reports whether the offline mode is on.
func Enabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(Env)))
	return v == "1" || v == "true" || v == "yes"
}

// MapTiles returns the tile URL template of the maps, "" for no tiles
// (offline without a tile server).
func MapTiles() string {
	if v := strings.TrimSpace(os.Getenv(MapTilesEnv)); v != "" {
		return v
	}
	if Enabled() {
		return ""
	}
	return defaultTiles
}

// MapTilesOrigin returns the origin of a custom tile server ("" for the
// default one or none), to allow it in the dashboard CSP.
func MapTilesOrigin() string {
	v := strings.TrimSpace(os.Getenv(MapTilesEnv))
	if v == "" {
		return ""
	}
	u, err := url.Parse(strings.NewReplacer("{z}", "0", "{x}", "0", "{y}", "0", "{s}", "a").Replace(v))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

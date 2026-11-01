package deploy

import "runtime/debug"

// Version is the PocketBasePlus release, set by the release builds:
//
//	go build -ldflags "-X github.com/Namarusa/PocketbasePlus/internal/deploy.Version=v0.7.1"
var Version = "dev"

// VersionString is the release, or for a build from the sources "dev" and its
// commit ("dev fac7893", "dev fac7893 modified").
func VersionString() string {
	if Version != "dev" && Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	rev, dirty := "", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	out := "dev " + rev[:min(7, len(rev))]
	if dirty {
		out += " modified"
	}
	return out
}

// PocketBaseVersion is the PocketBase module the binary is built with ("v0.40.4").
func PocketBaseVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, d := range info.Deps {
			if d.Path == "github.com/pocketbase/pocketbase" {
				return d.Version
			}
		}
	}
	return "unknown"
}

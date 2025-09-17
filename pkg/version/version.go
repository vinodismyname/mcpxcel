package version

import "runtime/debug"

var version = "dev"

// Version returns the build string embedded via -ldflags when available.
func Version() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Sum != "" {
		return info.Main.Version
	}
	return version
}

// Set assigns the exported version when ldflags are not provided (e.g. local dev).
func Set(v string) {
	if v != "" {
		version = v
	}
}

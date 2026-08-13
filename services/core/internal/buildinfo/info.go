package buildinfo

// Info contains immutable build metadata.
type Info struct {
	Version string
}

var version string

// Current returns the build metadata injected at link time.
func Current() Info {
	if version == "" {
		return Info{Version: "dev"}
	}

	return Info{Version: version}
}

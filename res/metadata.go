package res

import (
	"strings"

	"codeberg.org/unixery/buildinfo"
)

const (
	AppName          = "discnegative"
	AppID            = "io.github.thenktor.discnegative"
	DisplayName      = "DiscNegative"
	ConfigFile       = "config.toml"
	GithubURL        = "https://github.com/thenktor/discnegative"
	LatestReleaseURL = GithubURL + "/releases/latest"
	KofiURL          = "https://ko-fi.com/dweymouth"
	Copyright        = "Copyright © 2022–2026 Drew Weymouth and contributors"
)

var (
	// AppVersion is the numeric application version derived from the git tag
	// injected at build time (see Makefile), e.g. "1.2.3". Builds without
	// injected information (go run, plain go build) report "dev".
	AppVersion = strings.TrimPrefix(Version(), "v")

	// AppVersionTag is the git tag the binary was built from, e.g. "v1.2.3".
	// It is used for update checks against the latest release and stored in
	// the config. Builds without injected information report "dev".
	AppVersionTag = Version()
)

// Version returns the version from the linker metadata (injected via -ldflags
// in the Makefile, same approach as ../lyrunderground). Builds without
// injected information (go run, plain go build) report "dev".
func Version() string {
	if buildinfo.GitTag == "" || buildinfo.GitTag == "n/a" {
		return "dev"
	}
	return buildinfo.GitTag
}

var (
	WhatsAdded = `
## Added
* New AppName
* Features removed
* UI changes`

	WhatsFixed = `
## Fixed
* none`
)

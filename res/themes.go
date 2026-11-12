package res

import "embed"

// ThemesFS contains the bundled theme files (res/themes/*.toml).
//
//go:embed themes
var ThemesFS embed.FS

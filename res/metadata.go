package res

const (
	AppName          = "supersonic"
	AppID            = "io.github.supersonic-app.supersonic"
	DisplayName      = "Supersonic"
	AppVersion       = "0.22.1"
	AppVersionTag    = "v" + AppVersion
	ConfigFile       = "config.toml"
	GithubURL        = "https://github.com/supersonic-app/supersonic"
	LatestReleaseURL = GithubURL + "/releases/latest"
	KofiURL          = "https://ko-fi.com/dweymouth"
	Copyright        = "Copyright © 2022–2026 Drew Weymouth and contributors"
)

var (
	WhatsAdded = `
## Added
* Added dock icon menu in MacOS
* Github repo moved to supersonic-app organization
* Updated Chinese and Turkish translations
* Hide MacOS dock icon when closing to system tray`

	WhatsFixed = `
## Fixed
* Inconsistent character baseline with some CJK text
* Wayland: crash when "fn" key is pressed
* Wayland: Application tray icon shows multiple windows are open
* Wayland: use app ID to help compositors match app icon to window
* Shuffle control now exposed over MPRIS
* Regression in context menus not showing up in dialogs`
)

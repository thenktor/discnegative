// MPRIS (the freedesktop.org D-Bus media player spec) is only available on
// Linux/BSD, where a D-Bus session bus may exist. On other platforms this
// no-op stub keeps the rest of the app compiling unchanged.
//go:build !linux && !freebsd && !netbsd

package backend

type MPRISHandler struct {
	// Function called if the player is requested to quit through MPRIS.
	OnQuit func() error

	// Function called if the player is requested to bring its UI to the front.
	OnRaise func() error

	// Function to look up the artwork URL for a given track ID
	ArtURLLookup func(trackID string) (string, error)

	lm *LyricsManager
}

func NewMPRISHandler(playerName string, pm *PlaybackManager) *MPRISHandler {
	return &MPRISHandler{}
}

// Start is a no-op on platforms without D-Bus.
func (m *MPRISHandler) Start() {}

// Shutdown is a no-op on platforms without D-Bus.
func (m *MPRISHandler) Shutdown() {}

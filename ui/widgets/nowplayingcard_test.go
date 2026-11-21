package widgets

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"github.com/thenktor/discnegative/backend/mediaprovider"
)

func TestNowPlayingCardCoverTapWithoutImage(t *testing.T) {
	// radio stations have no cover art, tapping the placeholder must still work
	c := NewNowPlayingCard()
	tapped := 0
	c.OnCoverTapped = func() { tapped++ }
	c.Update(&mediaprovider.RadioStation{ID: "radio", StationName: "Radio", StreamURL: "http://example.com/stream"})

	w := test.NewWindow(c)
	defer w.Close()
	w.Resize(fyne.NewSize(300, 120))

	if c.cover.HaveImage() {
		t.Fatal("expected no cover image for a radio station")
	}
	pos := c.cover.Position().Add(fyne.NewPos(c.cover.Size().Width/2, c.cover.Size().Height/2))
	test.TapCanvas(w.Canvas(), pos)

	if tapped != 1 {
		t.Fatalf("expected the cover tap to be reported once, got %d", tapped)
	}
}

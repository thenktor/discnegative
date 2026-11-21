package widgets

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func TestImagePlaceholderPlaceholderIsTappable(t *testing.T) {
	i := NewImagePlaceholder(theme.MediaPlayIcon(), 60)
	tapped := 0
	i.OnTapped = func(*fyne.PointEvent) { tapped++ }
	i.Refresh()

	w := test.NewWindow(i)
	defer w.Close()
	w.Resize(fyne.NewSize(100, 120))

	// the placeholder is shown instead of a cover image
	if i.HaveImage() {
		t.Fatal("expected no image")
	}

	test.TapCanvas(w.Canvas(), fyne.NewPos(50, 60))
	if tapped != 1 {
		t.Fatalf("expected the placeholder to report one tap, got %d", tapped)
	}
}

func TestImagePlaceholderPlaceholderDoesNotSwallowTaps(t *testing.T) {
	// without a tap handler the placeholder must not intercept taps, so that
	// parent widgets (e.g. list rows) still receive them
	i := NewImagePlaceholder(theme.MediaPlayIcon(), 60)
	i.Refresh()

	w := test.NewWindow(i)
	defer w.Close()
	w.Resize(fyne.NewSize(100, 120))

	if !i.placeholderTaps.Hidden {
		t.Fatal("expected the tap catcher to be hidden without a handler")
	}
	if i.HaveImage() {
		t.Fatal("expected no image")
	}
}

package widgets

import (
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/lang"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	myTheme "github.com/thenktor/discnegative/ui/theme"
	"github.com/thenktor/discnegative/ui/util"
)

// The "aux" controls for playback, positioned to the right
// of the BottomPanel. Currently only volume control.
type AuxControls struct {
	widget.BaseWidget

	OnChangeEndlessPlay func(endlessPlay bool)

	VolumeControl *VolumeControl
	endlessPlay   *IconButton

	container *fyne.Container
}

func NewAuxControls(initialVolume int, initialEndlessPlay bool) *AuxControls {
	a := &AuxControls{
		VolumeControl: NewVolumeControl(initialVolume),
		endlessPlay:   NewIconButton(myTheme.EndlessPlayIcon, nil),
	}

	a.endlessPlay.Highlighted = initialEndlessPlay
	// a.endlessPlay.IconSize = IconButtonSizeSmaller
	a.endlessPlay.SetToolTip(lang.L("Endless play"))
	a.endlessPlay.OnTapped = func() {
		a.SetEndlessPlay(!a.endlessPlay.Highlighted)
		if a.OnChangeEndlessPlay != nil {
			a.OnChangeEndlessPlay(a.endlessPlay.Highlighted)
		}
	}

	a.container = container.NewHBox(
		layout.NewSpacer(),
		container.NewVBox(
			layout.NewSpacer(),
			a.VolumeControl,
			container.New(
				layout.NewCustomPaddedHBoxLayout(theme.Padding()*1.5),
				layout.NewSpacer(), a.endlessPlay, util.NewHSpace(5)),
			layout.NewSpacer(),
		),
	)
	return a
}

func (a *AuxControls) CreateRenderer() fyne.WidgetRenderer {
	a.ExtendBaseWidget(a)
	return widget.NewSimpleRenderer(a.container)
}

func (a *AuxControls) SetEndlessPlay(endlessPlay bool) {
	if endlessPlay == a.endlessPlay.Highlighted {
		return
	}
	a.endlessPlay.Highlighted = endlessPlay
	a.endlessPlay.Refresh()
}



type volumeSlider struct {
	widget.Slider

	Width float32
}

func NewVolumeSlider(width float32) *volumeSlider {
	v := &volumeSlider{
		Slider: widget.Slider{
			Min:         0,
			Max:         100,
			Step:        1,
			Orientation: widget.Horizontal,
			Value:       100,
		},
		Width: width,
	}
	v.ExtendBaseWidget(v)
	return v
}

func (v *volumeSlider) Tapped(e *fyne.PointEvent) {
	v.Slider.Tapped(e)
	if c := fyne.CurrentApp().Driver().CanvasForObject(v); c != nil {
		c.Unfocus()
	}
}

func (v *volumeSlider) MinSize() fyne.Size {
	return fyne.NewSize(v.Width, v.Slider.MinSize().Height)
}

func (v *volumeSlider) Scrolled(e *fyne.ScrollEvent) {
	v.SetValue(v.Value + math.Max(-10, math.Min(10, float64(0.5*e.Scrolled.DY))))
}

type VolumeControl struct {
	widget.BaseWidget

	icon   *IconButton
	slider *volumeSlider

	OnSetVolume func(int)

	muted   bool
	lastVol int

	setVolDebouncer func()
	delaySetVolume  bool
	pendingVolume   int

	container *fyne.Container
}

func NewVolumeControl(initialVol int) *VolumeControl {
	v := &VolumeControl{}
	v.ExtendBaseWidget(v)
	v.icon = NewIconButton(theme.VolumeUpIcon(), v.toggleMute)
	v.icon.SetToolTip(lang.L("Mute"))
	v.icon.IconSize = IconButtonSizeSmaller
	v.slider = NewVolumeSlider(100)
	v.lastVol = initialVol
	v.slider.Step = 1
	v.slider.Orientation = widget.Horizontal
	v.slider.Value = float64(v.lastVol)
	v.slider.OnChanged = v.onChanged

	// for players that are slow to respond to volume changes,
	// delay responding to the SetVolume call
	// to avoid hiccuping from callback "echoes"
	v.setVolDebouncer = util.NewDebouncer(100*time.Millisecond, func() {
		v.delaySetVolume = false
		v.SetVolume(v.pendingVolume)
	})

	v.container = container.NewHBox(container.NewCenter(v.icon), v.slider)
	return v
}

// Sets the volume that is displayed in the slider.
// Does not invoke OnSetVolume callback.
func (v *VolumeControl) SetVolume(vol int) {
	if v.delaySetVolume {
		v.pendingVolume = vol
		return
	}

	if (vol == v.lastVol && !v.muted) || (v.muted && vol == 0) {
		return
	}
	v.lastVol = vol
	v.muted = false
	v.setDisplayedVolume(vol)
}

func (v *VolumeControl) onChanged(volume float64) {
	vol := int(volume)
	v.delaySetVolume = true
	v.setVolDebouncer()
	v.lastVol = vol
	v.muted = false
	v.updateIconForVolume(vol)
	v.invokeOnVolumeChange(vol)
}

func (v *VolumeControl) toggleMute() {
	if !v.muted {
		v.muted = true
		v.lastVol = int(v.slider.Value)
		v.setDisplayedVolume(0)
		v.invokeOnVolumeChange(0)
	} else {
		v.muted = false
		v.setDisplayedVolume(v.lastVol)
		v.invokeOnVolumeChange(v.lastVol)
	}
}

func (v *VolumeControl) CreateRenderer() fyne.WidgetRenderer {
	v.ExtendBaseWidget(v)
	return widget.NewSimpleRenderer(v.container)
}

func (v *VolumeControl) setDisplayedVolume(vol int) {
	v.slider.Value = float64(vol)
	v.slider.Refresh()
	v.updateIconForVolume(vol)
}

func (v *VolumeControl) invokeOnVolumeChange(vol int) {
	if v.OnSetVolume != nil {
		v.OnSetVolume(vol)
	}
}

func (v *VolumeControl) updateIconForVolume(vol int) {
	if vol <= 0 {
		v.icon.SetIcon(theme.VolumeMuteIcon())
	} else if vol < 50 {
		v.icon.SetIcon(theme.VolumeDownIcon())
	} else {
		v.icon.SetIcon(theme.VolumeUpIcon())
	}
}

package controller

import (
	"errors"
	"fmt"
	"image"
	"log"
	"runtime"

	fynetooltip "github.com/dweymouth/fyne-tooltip"
	"github.com/thenktor/discnegative/backend"
	"github.com/thenktor/discnegative/backend/mediaprovider"
	"github.com/thenktor/discnegative/backend/player"
	"github.com/thenktor/discnegative/backend/player/mpv"
	"github.com/thenktor/discnegative/sharedutil"
	"github.com/thenktor/discnegative/ui/dialogs"
	myTheme "github.com/thenktor/discnegative/ui/theme"
	"github.com/thenktor/discnegative/ui/util"
	"github.com/thenktor/discnegative/ui/widgets"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver"
	"fyne.io/fyne/v2/lang"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type NavigationHandler func(Route)

type CurPageFunc func() Route

type ToastProvider interface {
	ShowSuccessToast(string)
	ShowErrorToast(string)
}

type Controller struct {
	AppVersion string
	App        *backend.App
	MainWindow fyne.Window

	// dependencies injected from MainWindow
	NavHandler          NavigationHandler
	CurPageFunc         CurPageFunc
	ReloadFunc          func()
	RefreshPageFunc     func()
	SelectAllPageFunc   func()
	UnselectAllPageFunc func()
	ToastProvider       ToastProvider

	escapablePopUp     fyne.CanvasObject
	haveModal          bool
	runOnModalClosed   func()
}

func New(app *backend.App, appVersion string, mainWindow fyne.Window) *Controller {
	c := &Controller{
		AppVersion: appVersion,
		MainWindow: mainWindow,
		App:        app,
	}
	return c
}

func (m *Controller) SelectAll() {
	if m.SelectAllPageFunc != nil {
		m.SelectAllPageFunc()
	}
}

func (m *Controller) UnselectAll() {
	if m.UnselectAllPageFunc != nil {
		m.UnselectAllPageFunc()
	}
}

func (m *Controller) NavigateTo(route Route) {
	m.NavHandler(route)
}

func (m *Controller) ClosePopUpOnEscape(pop fyne.CanvasObject) {
	m.escapablePopUp = pop
}

func (m *Controller) CloseEscapablePopUp() {
	if m.escapablePopUp != nil {
		m.escapablePopUp.Hide()
		m.escapablePopUp = nil
		m.doModalClosed()
	}
}

// If there is currently no modal popup managed by the Controller visible,
// then run f (which should create and show a modal dialog) immediately.
// else run f when the current modal dialog workflow has ended.
func (m *Controller) QueueShowModalFunc(f func()) {
	if m.haveModal {
		m.runOnModalClosed = f
	} else {
		f()
	}
}

func (m *Controller) HaveModal() bool {
	return m.haveModal
}

func (m *Controller) ShowPopUpImage(img image.Image) {
	s := m.MainWindow.Canvas().Size()
	var popS fyne.Size
	if asp := util.ImageAspect(img); s.Width/s.Height > asp {
		// window height is limiting factor
		h := s.Height * 0.8
		popS = fyne.NewSize(h*asp, h)
	} else {
		w := s.Width * 0.8
		popS = fyne.NewSize(w, w*(1/asp))
	}

	pop := widgets.NewImagePopUp(img, m.MainWindow.Canvas(), popS)

	m.ClosePopUpOnEscape(pop)
	pop.Show()
}

func (m *Controller) GetArtistTracks(artistID string) []*mediaprovider.Track {
	if mp := m.App.ServerManager.Server; mp != nil {
		if tr, err := mp.GetArtistTracks(artistID); err != nil {
			log.Println(err.Error())
			return nil
		} else {
			return tr
		}
	}
	return nil
}

func (c *Controller) ShowAboutDialog() {
	dlg := dialogs.NewAboutDialog(c.AppVersion)
	pop := widget.NewModalPopUp(container.NewPadded(dlg), c.MainWindow.Canvas())
	dlg.OnDismiss = func() {
		pop.Hide()
		c.doModalClosed()
	}
	c.ClosePopUpOnEscape(pop)
	c.haveModal = true
	pop.Show()
}

func (c *Controller) ShowSettingsDialog(themeUpdateCallbk func(), themeFiles map[string]string) {
	devs, err := c.App.LocalPlayer.ListAudioDevices()
	if err != nil {
		log.Printf("error listing audio devices: %v", err)
		devs = []mpv.AudioDevice{{Name: "auto", Description: lang.L("Autoselect device")}}
	}

	curPlayer := c.App.PlaybackManager.CurrentPlayer()
	_, isReplayGainPlayer := curPlayer.(player.ReplayGainPlayer)
	_, canSavePlayQueue := c.App.ServerManager.Server.(mediaprovider.CanSavePlayQueue)
	isLocalPlayer := true

	dlg := dialogs.NewSettingsDialog(c.App.Config,
		devs, themeFiles,
		c.App.ServerManager.Server.ClientDecidesScrobble(),
		isLocalPlayer, isReplayGainPlayer, canSavePlayQueue,
		c.App.ImageManager,
		c.MainWindow,
		c.ToastProvider)
	dlg.OnReplayGainSettingsChanged = func() {
		c.App.PlaybackManager.SetReplayGainOptions(c.App.Config.ReplayGain)
	}
	dlg.OnAudioExclusiveSettingChanged = func() {
		c.App.LocalPlayer.SetAudioExclusive(c.App.Config.LocalPlayback.AudioExclusive)
	}
	dlg.OnPauseFadeSettingsChanged = func() {
		c.App.LocalPlayer.SetPauseFade(c.App.Config.LocalPlayback.PauseFade)
	}
	dlg.OnAudioDeviceSettingChanged = func() {
		c.App.LocalPlayer.SetAudioDevice(c.App.Config.LocalPlayback.AudioDeviceName)
	}
	dlg.OnThemeSettingChanged = themeUpdateCallbk
	dlg.OnPageNeedsRefresh = c.RefreshPageFunc
	dlg.OnClearCaches = func() { go c.App.ClearCaches() }
	pop := widget.NewModalPopUp(container.NewPadded(dlg), c.MainWindow.Canvas())
	fynetooltip.AddPopUpToolTipLayer(pop)
	dlg.OnDismiss = func() {
		pop.Hide()
		fynetooltip.DestroyPopUpToolTipLayer(pop)
		c.doModalClosed()
		c.App.SaveConfigFile()
	}
	c.ClosePopUpOnEscape(pop)
	c.haveModal = true
	pop.Show()
}

func (c *Controller) doModalClosed() {
	c.haveModal = false
	if c.runOnModalClosed != nil {
		c.runOnModalClosed()
		c.runOnModalClosed = nil
	}
}

func (c *Controller) SetTrackFavorites(trackIDs []string, favorite bool) {
	go c.App.ServerManager.Server.SetFavorite(mediaprovider.RatingFavoriteParameters{
		TrackIDs: trackIDs,
	}, favorite)

	for _, id := range trackIDs {
		c.App.PlaybackManager.OnTrackFavoriteStatusChanged(id, favorite)
	}
}

func (c *Controller) SetTrackRatings(trackIDs []string, rating int) {
	r, ok := c.App.ServerManager.Server.(mediaprovider.SupportsRating)
	if !ok {
		return
	}
	go r.SetRating(mediaprovider.RatingFavoriteParameters{
		TrackIDs: trackIDs,
	}, rating)

	// Notify PlaybackManager of rating change to update
	// the in-memory track models
	for _, id := range trackIDs {
		c.App.PlaybackManager.OnTrackRatingChanged(id, rating)
	}
}

func (c *Controller) ShowShareDialog(id string) {
	sh, ok := c.App.ServerManager.Server.(mediaprovider.SupportsSharing)
	if !ok {
		// this should not happen since interface checks are done
		// before showing share menu items, but just in case:
		c.ToastProvider.ShowErrorToast(lang.L("server does not support sharing"))
		return
	}
	go func() {
		shareUrl, err := sh.CreateShareURL(id)
		if err != nil {
			log.Printf("error creating share URL: %v", err)
			fyne.Do(func() {
				c.showError(
					"Failed to share content. This commonly occurs when the server does not support sharing, " +
						"or has the feature disabled.\nPlease check the server's settings and try again.",
				)
			})
			return
		}
		fyne.Do(func() {
			hyperlink := widget.NewHyperlink(shareUrl.String(), shareUrl)
			dismissed := false
			dlg := dialog.NewCustom(lang.L("Share content"), lang.L("OK"),
				container.NewHBox(
					hyperlink,
					widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
						fyne.CurrentApp().Clipboard().SetContent(hyperlink.Text)
					}),
					widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), func() {
						go func() {
							shareUrl, err := sh.CreateShareURL(id)
							if err != nil {
								log.Printf("error creating share URL: %v", err)
								return
							}
							fyne.Do(func() {
								if !dismissed {
									hyperlink.Text = shareUrl.String()
									hyperlink.URL = shareUrl
									hyperlink.Refresh()
								}
							})
						}()
					}),
				),
				c.MainWindow,
			)
			dlg.SetOnClosed(func() {
				dismissed = true
			})
			dlg.Show()
		})
	}()
}

func (c *Controller) sendNotification(title, content string) {
	fyne.CurrentApp().SendNotification(&fyne.Notification{
		Title:   title,
		Content: content,
	})
}

func (c *Controller) showError(content string) {
	// TODO: display an in-app toast message instead of a dialog.
	dialog.ShowError(errors.New(content), c.MainWindow)
}

func (c *Controller) ShowAlbumInfoDialog(albumID, albumName string, albumCover image.Image) {
	go func() {
		albumInfo, err := c.App.ServerManager.Server.GetAlbumInfo(albumID)
		if err != nil {
			log.Print("Error getting album info: ", err)
			return
		}
		fyne.Do(func() {
			dlg := dialogs.NewAlbumInfoDialog(albumInfo, albumName, albumCover)
			pop := widget.NewModalPopUp(container.NewPadded(dlg), c.MainWindow.Canvas())
			dlg.OnDismiss = func() {
				pop.Hide()
				c.doModalClosed()
			}
			c.ClosePopUpOnEscape(pop)
			c.haveModal = true
			pop.Resize(pop.Content.MinSize()) // needed so that NonScrollingMinHeight can consider the width
			h := fyne.Min(dlg.NonScrollingMinHeight(), c.MainWindow.Canvas().Size().Height*0.85)
			pop.Resize(fyne.NewSize(dlg.MinSize().Width, h))
			pop.Show()
		})
	}()
}

func (c *Controller) ShowTrackInfoDialog(track *mediaprovider.Track) {
	info := dialogs.NewTrackInfoDialog(track)
	pop := widget.NewModalPopUp(container.NewPadded(info), c.MainWindow.Canvas())
	info.OnDismiss = func() {
		pop.Hide()
		c.doModalClosed()
	}
	info.OnNavigateToAlbum = func(albumID string) {
		info.OnDismiss()
		c.NavigateTo(AlbumRoute(albumID))
	}
	info.OnNavigateToArtist = func(artistID string) {
		info.OnDismiss()
		c.NavigateTo(ArtistRoute(artistID))
	}
	info.OnNavigateToGenre = func(genre string) {
		info.OnDismiss()
		c.NavigateTo(GenreRoute(genre))
	}
	info.OnCopyFilePath = func() {
		c.MainWindow.Clipboard().SetContent(track.FilePath)
	}
	c.ClosePopUpOnEscape(pop)
	winSize := c.MainWindow.Canvas().Size()
	popMin := pop.MinSize()
	width := fyne.Min(750, fyne.Max(popMin.Width, winSize.Width*0.8))
	height := fyne.Min(650, fyne.Max(popMin.Height, winSize.Height*0.8))
	pop.Resize(fyne.NewSize(width, height))
	c.haveModal = true
	pop.Show()
}

func (c *Controller) GetSongRadioTracks(sourceTrack *mediaprovider.Track) ([]*mediaprovider.Track, error) {
	radioTracks, err := c.App.ServerManager.Server.GetSongRadio(sourceTrack.ID, 100)
	if err != nil {
		return nil, fmt.Errorf("error getting song radio: %s", err.Error())
	}

	// The goal of this implementation is to place the source track first in the queue.
	filteredTracks := sharedutil.FilterSlice(radioTracks, func(track *mediaprovider.Track) bool {
		return track.ID != sourceTrack.ID
	})
	tracks := []*mediaprovider.Track{sourceTrack}
	tracks = append(tracks, filteredTracks...)
	return tracks, nil
}

func SetWindowThemeMode(win fyne.Window, mode myTheme.AppearanceMode) {
	arg := 0
	switch fyne.CurrentApp().Settings().Theme().(*myTheme.MyTheme).AppearanceMode() {
	case myTheme.AppearanceDark:
		arg = 1
	case myTheme.AppearanceLight:
		arg = 2
	}
	win.(driver.NativeWindow).RunNative(func(ctx any) {
		switch runtime.GOOS {
		case "darwin":
			// ensure dark mode setting is applied to window controls in title bar on Mac
			setWindowDarkTheme(ctx.(driver.MacWindowContext).NSWindow, arg)
		case "windows":
			setWindowDarkTheme(ctx.(driver.WindowsWindowContext).HWND, arg)
		}
	})
}

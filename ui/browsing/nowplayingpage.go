package browsing

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"log"
	"net/url"
	"slices"
	"strings"

	"github.com/boxes-ltd/imaging"
	"github.com/cenkalti/dominantcolor"
	"github.com/thenktor/discnegative/backend"
	"github.com/thenktor/discnegative/backend/mediaprovider"
	"github.com/thenktor/discnegative/backend/player"
	"github.com/thenktor/discnegative/backend/player/mpv"
	"github.com/thenktor/discnegative/sharedutil"
	"github.com/thenktor/discnegative/ui/controller"
	"github.com/thenktor/discnegative/ui/layouts"
	myTheme "github.com/thenktor/discnegative/ui/theme"
	"github.com/thenktor/discnegative/ui/util"
	"github.com/thenktor/discnegative/ui/widgets"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/lang"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type NowPlayingPage struct {
	widget.BaseWidget

	nowPlayingPageState

	// volatile state
	nowPlaying    mediaprovider.MediaItem
	nowPlayingID  string
	curLyrics     *mediaprovider.Lyrics
	curLyricsID   string // id of track currently shown in lyrics
	curRelatedID  string // id of track currrently used to populate related list
	curCoverArt   string // id of cover art currently shown in background / card
	totalTime     float64
	lastPlayPos   float64
	queue         []mediaprovider.MediaItem
	related       []*mediaprovider.Track
	alreadyLoaded bool

	// widgets for render
	backgroundImgA     *canvas.Image
	backgroundImgB     *canvas.Image
	backgroundGradient *canvas.LinearGradient
	queueList          *widgets.PlayQueueList
	relatedList        *widgets.PlayQueueList
	lyricsViewer       *widgets.LyricsViewer
	card               *widgets.LargeNowPlayingCard
	statusLabel        *widget.Label
	tabs               *container.AppTabs
	lyricsLoading      *widgets.LoadingDots
	relatedLoading     *widgets.LoadingDots
	container          *fyne.Container

	// cancel funcs for background fetch tasks
	imageLoadCancel    context.CancelFunc
	relatedFetchCancel context.CancelFunc
}

type nowPlayingPageState struct {
	conf     *backend.NowPlayingPageConfig
	contr    *controller.Controller
	pool     *util.WidgetPool
	sm       *backend.ServerManager
	pm       *backend.PlaybackManager
	lm       *backend.LyricsManager
	im       *backend.ImageManager
	mp       mediaprovider.MediaProvider
	canRate  bool
	canShare bool
	cfg      *backend.Config
}

func NewNowPlayingPage(
	conf *backend.NowPlayingPageConfig,
	contr *controller.Controller,
	pool *util.WidgetPool,
	sm *backend.ServerManager,
	lm *backend.LyricsManager,
	im *backend.ImageManager,
	pm *backend.PlaybackManager,
	mp mediaprovider.MediaProvider,
	canRate bool,
	canShare bool,
	cfg *backend.Config,
) *NowPlayingPage {
	state := nowPlayingPageState{
		conf: conf, contr: contr, pool: pool, sm: sm, lm: lm, im: im, pm: pm, mp: mp, canRate: canRate, canShare: canShare, cfg: cfg,
	}
	if page, ok := pool.Obtain(util.WidgetTypeNowPlayingPage).(*NowPlayingPage); ok && page != nil {
		page.nowPlayingPageState = state
		page.Reload()
		return page
	}
	a := &NowPlayingPage{nowPlayingPageState: state}
	a.ExtendBaseWidget(a)

	doFmtStatus := util.FyneDoFunc(a.formatStatusLine)
	pm.OnPaused(doFmtStatus)
	pm.OnPlaying(doFmtStatus)
	pm.OnStopped(doFmtStatus)

	a.card = widgets.NewLargeNowPlayingCard()
	a.card.OnAlbumNameTapped = func() {
		contr.NavigateTo(controller.AlbumRoute(a.nowPlaying.Metadata().AlbumID))
	}
	a.card.OnArtistNameTapped = func(artistID string) {
		contr.NavigateTo(controller.ArtistRoute(artistID))
	}
	a.card.OnRadioURLTapped = func(urlText string) {
		if u, err := url.Parse(urlText); err == nil {
			fyne.CurrentApp().OpenURL(u)
		}
	}
	a.card.OnSetFavorite = func(fav bool) {
		a.contr.SetTrackFavorites([]string{a.nowPlayingID}, fav)
	}
	a.card.OnSetRating = func(rating int) {
		a.contr.SetTrackRatings([]string{a.nowPlayingID}, rating)
	}
	a.card.ShowAlbumYear = cfg.AlbumsPage.ShowYears

	a.queueList = widgets.NewPlayQueueList(a.im, false)
	a.relatedList = widgets.NewPlayQueueList(a.im, true)
	a.relatedList.ShowScrollToNowPlaying = false
	a.queueList.Reorderable = true

	a.contr.ConnectPlayQueuelistActions(a.queueList)
	// override OnSetRating and Favorite so we can also update the
	// LargeNowPlayingCard's displayed rating and favorite
	a.queueList.OnSetRating = func(trackIDs []string, rating int) {
		contr.SetTrackRatings(trackIDs, rating)
		if slices.Contains(trackIDs, a.nowPlayingID) {
			a.card.SetDisplayedRating(rating)
		}
	}
	a.queueList.OnSetFavorite = func(trackIDs []string, fav bool) {
		contr.SetTrackFavorites(trackIDs, fav)
		if slices.Contains(trackIDs, a.nowPlayingID) {
			a.card.SetDisplayedFavorite(fav)
		}
	}

	a.relatedList.OnShare = a.queueList.OnShare
	a.relatedList.OnAddToPlaylist = a.queueList.OnAddToPlaylist
	a.relatedList.OnShowArtistPage = a.queueList.OnShowArtistPage
	a.relatedList.OnSetRating = a.queueList.OnSetRating
	a.relatedList.OnSetFavorite = a.queueList.OnSetFavorite
	a.relatedList.OnPlayItemAt = func(idx int) {
		a.pm.LoadTracks(a.related, backend.Replace, false)
		a.pm.PlayTrackAt(idx)
	}
	a.relatedList.OnAddToQueue = func(items []mediaprovider.MediaItem) {
		a.pm.LoadItems(items, backend.Append, false)
	}
	a.relatedList.OnPlaySelection = func(items []mediaprovider.MediaItem, shuffle bool) {
		a.pm.LoadItems(items, backend.Replace, shuffle)
		a.pm.PlayFromBeginning()
	}
	a.relatedList.OnPlaySelectionNext = func(items []mediaprovider.MediaItem) {
		a.pm.LoadItems(items, backend.InsertNext, false)
	}
	a.relatedList.OnPlaySongRadio = func(track *mediaprovider.Track) {
		go func() {
			if err := a.pm.PlaySimilarSongs(track.ID); err != nil {
				fyne.Do(func() {
					a.contr.ToastProvider.ShowErrorToast(lang.L("Unable to play song radio"))
				})
			}
		}()
	}
	a.relatedList.OnShowTrackInfo = func(track *mediaprovider.Track) {
		a.contr.ShowTrackInfoDialog(track)
	}
	a.relatedList.OnReplaceQueueWithRelated = func() {
		cur := a.pm.NowPlaying()
		if cur == nil {
			return
		}
		relatedItems := sharedutil.CopyTrackSliceToMediaItemSlice(a.related)
		// the currently playing track goes first; drop any duplicate of it
		newQueue := make([]mediaprovider.MediaItem, 0, len(relatedItems)+1)
		newQueue = append(newQueue, cur)
		for _, it := range relatedItems {
			if it.Metadata().ID != cur.Metadata().ID {
				newQueue = append(newQueue, it)
			}
		}
		a.pm.UpdatePlayQueue(newQueue)
	}
	a.lyricsViewer = widgets.NewLyricsViewer(a.onSeekToLyricLine)
	a.statusLabel = widget.NewLabel(lang.L("Stopped"))
	a.statusLabel.TextStyle.Monospace = true

	a.Reload()
	return a
}

func (a *NowPlayingPage) onSeekToLyricLine(lineNum int) {
	if a.curLyrics != nil && len(a.curLyrics.Lines) > lineNum-1 {
		time := a.curLyrics.Lines[lineNum-1].Start
		a.pm.SeekSeconds(time)
	}
}

func (a *NowPlayingPage) CreateRenderer() fyne.WidgetRenderer {
	if a.container == nil {
		initialTab := 0
		if a.conf.InitialView == "Lyrics" {
			initialTab = 0
		} else if a.conf.InitialView == "Related" {
			initialTab = 1
		} else if a.conf.InitialView == "Play Queue" {
			initialTab = 0
		}
		_ = initialTab
		paddedLayout := &layouts.PercentPadLayout{
			LeftRightObjectPercent: .8,
			TopBottomObjectPercent: .8,
		}
		a.lyricsLoading = widgets.NewLoadingDots()
		a.relatedLoading = widgets.NewLoadingDots()
		a.tabs = container.NewAppTabs(
			container.NewTabItem(lang.L("Lyrics"), container.NewStack(
				a.lyricsViewer,
				container.NewCenter(a.lyricsLoading))),
			container.NewTabItem(lang.L("Related"), container.NewStack(
				a.relatedList,
				container.NewCenter(a.relatedLoading))),
		)
		a.tabs.SelectIndex(initialTab)
		a.tabs.OnSelected = func(*container.TabItem) {
			idx := a.tabs.SelectedIndex()
			a.saveSelectedTab(idx)
			if idx == 0 /*lyrics*/ {
				a.updateLyrics()
			} else if idx == 1 /*related*/ {
				a.updateRelatedList()
			}
		}
		if initialTab == 0 /*lyrics*/ {
			a.updateLyrics()
		} else if initialTab == 1 /*related*/ {
			a.updateRelatedList()
		}
		c := theme.Color(myTheme.ColorNamePageBackground)
		a.backgroundGradient = canvas.NewLinearGradient(c, c, 0)
		a.backgroundImgA = canvas.NewImageFromImage(nil)
		a.backgroundImgB = canvas.NewImageFromImage(nil)

		mainContent := container.NewGridWithColumns(2,
			container.New(paddedLayout, a.card),
			container.New(paddedLayout,
				util.AddHeaderBackgroundWithColorName(
					a.tabs, myTheme.ColorNameNowPlayingPanel)))
		a.container = container.NewStack(
			a.backgroundImgA,
			a.backgroundImgB,
			a.backgroundGradient,
			mainContent,
			container.NewVBox(
				layout.NewSpacer(),
				container.NewBorder(nil, nil, util.NewHSpace(1), util.NewHSpace(1),
					myTheme.NewThemedRectangle(theme.ColorNameInputBorder)),
				a.statusLabel,
			),
		)
	}
	return widget.NewSimpleRenderer(a.container)
}

func (a *NowPlayingPage) Save() SavedPage {
	if a.imageLoadCancel != nil {
		a.imageLoadCancel()
	}
	a.alreadyLoaded = false
	nps := a.nowPlayingPageState
	a.pool.Release(util.WidgetTypeNowPlayingPage, a)
	return &nps
}

func (a *NowPlayingPage) Route() controller.Route {
	return controller.NowPlayingRoute()
}

var _ Scrollable = (*NowPlayingPage)(nil)

func (a *NowPlayingPage) Scroll(delta float32) {
	idx := a.tabs.SelectedIndex()
	if idx == 1 /*related*/ {
		a.relatedList.Scroll(delta)
	}
}

var _ CanShowNowPlaying = (*NowPlayingPage)(nil)

func (a *NowPlayingPage) OnSongChange(song mediaprovider.MediaItem, lastScrobbledIfAny *mediaprovider.Track) {
	a.nowPlaying = song
	if a.imageLoadCancel != nil {
		a.imageLoadCancel()
	}
	a.nowPlayingID = sharedutil.MediaItemIDOrEmptyStr(song)
	a.queueList.SetNowPlaying(a.nowPlayingID)
	if !a.alreadyLoaded {
		a.queueList.ScrollToNowPlaying()
		a.alreadyLoaded = true
	}
	a.relatedList.SetNowPlaying(a.nowPlayingID)

	a.card.Update(song)
	if song == nil {
		a.card.SetCoverImage(nil)
		a.curCoverArt = ""
	} else if artID := song.Metadata().CoverArtID; artID != a.curCoverArt {
		a.imageLoadCancel = a.im.GetFullSizeCoverArtAsync(song.Metadata().CoverArtID, func(img image.Image, err error) {
			a.curCoverArt = artID
			a.onImageLoaded(img, err)
		})
	}

	if a.tabs != nil && a.tabs.SelectedIndex() == 0 /*lyrics*/ {
		a.updateLyrics()
	} else if a.tabs != nil && a.tabs.SelectedIndex() == 1 /*related*/ {
		a.updateRelatedList()
	}
}

func (a *NowPlayingPage) onImageLoaded(img image.Image, err error) {
	if err != nil {
		log.Printf("error loading cover art: %v\n", err)
	}

	fyne.Do(func() { a.card.SetCoverImage(img) })
	if img == nil {
		if a.conf.UseBackgroundImage {
			fyne.Do(func() {
				if !a.backgroundImgA.Hidden {
					a.backgroundImgA.Hide()
				}
				if !a.backgroundImgB.Hidden {
					a.backgroundImgB.Hide()
				}
			})
		} else {
			if a.backgroundGradient.StartColor != color.Transparent {
				anim := canvas.NewColorRGBAAnimation(
					a.backgroundGradient.StartColor, color.Transparent, myTheme.AnimationDurationMedium, func(c color.Color) {
						a.backgroundGradient.StartColor = c
						a.backgroundGradient.Refresh()
					})
				anim.Start()
			}
		}
		return
	}

	if a.conf.UseBackgroundImage {
		resized := imaging.Resize(img, 300, 0, imaging.NearestNeighbor)
		blurred := imaging.Blur(resized, 10.0)
		fyne.Do(func() {
			if a.backgroundGradient.StartColor != color.Transparent {
				a.backgroundGradient.StartColor = color.Transparent
				a.backgroundGradient.Refresh()
			}
			a.backgroundImgA.Hidden = false
			a.backgroundImgB.Hidden = false
			a.backgroundImgA.Image = a.backgroundImgB.Image
			a.backgroundImgB.Image = blurred
			fyne.NewAnimation(myTheme.AnimationDurationMedium, func(f float32) {
				a.backgroundImgA.Translucency = float64(f)
				a.backgroundImgB.Translucency = float64(1 - f)
				a.backgroundImgA.Refresh()
				a.backgroundImgB.Refresh()
			}).Start()
		})
	} else {
		c := dominantcolor.Find(img)
		if c == a.backgroundGradient.StartColor {
			return
		}
		if !a.backgroundImgA.Hidden {
			a.backgroundImgA.Hide()
		}
		if !a.backgroundImgB.Hidden {
			a.backgroundImgB.Hide()
		}
		// Fyne animation starting is currently thread-safe,
		// despite not being marked as such
		// TODO: if this changes, use fyne.Do
		anim := canvas.NewColorRGBAAnimation(
			a.backgroundGradient.StartColor, c, myTheme.AnimationDurationMedium, func(c color.Color) {
				a.backgroundGradient.StartColor = c
				a.backgroundGradient.Refresh()
			})
		anim.Start()
	}
}

func (a *NowPlayingPage) updateLyrics() {
	if a.nowPlayingID == a.curLyricsID {
		if a.nowPlayingID != "" {
			// just need to sync the current time
			a.lyricsViewer.OnSeeked(a.lastPlayPos)
			return
		}
	}
	if a.nowPlaying == nil || a.nowPlaying.Metadata().Type == mediaprovider.MediaItemTypeRadioStation {
		a.lyricsViewer.SetLyrics(nil)
		a.curLyrics = nil
		a.curLyricsID = ""
		return
	}
	a.curLyricsID = a.nowPlayingID
	a.lyricsLoading.Start()
	// set the widget to an empty (not nil) lyric during fetch
	// to keep it from showing "Lyrics not available"
	a.lyricsViewer.DisableTapToSeek()
	a.lyricsViewer.SetLyrics(&mediaprovider.Lyrics{
		Synced: true,
		Lines:  []mediaprovider.LyricLine{{Text: ""}},
	})
	tr, _ := a.nowPlaying.(*mediaprovider.Track)

	a.lm.FetchLyricsAsync(tr, func(id string, lyrics *mediaprovider.Lyrics) {
		if id != a.nowPlayingID {
			return
		}
		fyne.Do(func() {
			a.lyricsLoading.Stop()
			a.lyricsViewer.EnableTapToSeek()
			a.lyricsViewer.SetLyrics(lyrics)
			a.curLyrics = lyrics
			if lyrics != nil {
				a.lyricsViewer.OnSeeked(a.lastPlayPos)
			}
		})
	})
}

func (a *NowPlayingPage) updateRelatedList() {
	if a.relatedFetchCancel != nil {
		a.relatedFetchCancel()
	}
	if a.curRelatedID == a.nowPlayingID {
		return
	}
	a.curRelatedID = a.nowPlayingID
	if a.nowPlayingID == "" {
		a.relatedList.SetTracks(nil)
	}

	ctx, cancel := context.WithCancel(context.Background())
	a.relatedFetchCancel = cancel
	a.relatedLoading.Start()
	a.relatedList.SetTracks(nil)
	go func(ctx context.Context) {
		related, err := a.mp.GetSongRadio(a.nowPlayingID, 30)
		if err != nil {
			log.Printf("failed to get similar tracks: %s", err.Error())
		}
		select {
		case <-ctx.Done():
			return
		default:
			fyne.Do(func() {
				a.related = related
				a.relatedList.SetTracks(a.related)
				a.relatedLoading.Stop()
			})
		}
	}(ctx)
}

func (a *NowPlayingPage) OnPlayQueueChange() {
	a.Reload()
}

func (a *NowPlayingPage) Reload() {
	a.card.DisableRating = !a.canRate
	a.queueList.DisableRating = !a.canRate
	a.queueList.DisableSharing = !a.canShare
	a.relatedList.DisableRating = !a.canRate
	a.relatedList.DisableSharing = !a.canShare

	a.queue = a.pm.GetActivePlayQueue()
	a.queueList.SetItems(a.queue)
	a.totalTime = 0.0
	for _, tr := range a.queue {
		a.totalTime += tr.Metadata().Duration.Seconds()
	}
	a.formatStatusLine()

	if a.tabs == nil {
		return
	}
	idx := a.tabs.SelectedIndex()
	if idx == 0 /*lyrics*/ {
		a.lastPlayPos = a.pm.PlaybackStatus().TimePos
		a.updateLyrics()
	} else if idx == 1 /*related*/ {
		a.updateRelatedList()
	}
}

func (s *nowPlayingPageState) Restore() Page {
	return NewNowPlayingPage(s.conf, s.contr, s.pool, s.sm, s.lm, s.im, s.pm, s.mp, s.canRate, s.canShare, s.cfg)
}

var _ CanShowPlayTime = (*NowPlayingPage)(nil)

func (a *NowPlayingPage) OnPlayTimeUpdate(curTime, _ float64, seeked bool) {
	a.lastPlayPos = curTime
	a.formatStatusLine()
	if a.tabs == nil || a.tabs.SelectedIndex() != 0 /*lyrics*/ {
		return
	}
	if seeked {
		a.lyricsViewer.OnSeeked(curTime)
	} else {
		a.lyricsViewer.UpdatePlayPos(curTime)
	}
}

func (a *NowPlayingPage) currentTracklistOrNil() *widgets.PlayQueueList {
	if a.tabs != nil {
		if a.tabs.SelectedIndex() == 1 /*related*/ {
			return a.relatedList
		}
	}
	return nil
}

var _ CanSelectAll = (*NowPlayingPage)(nil)

func (a *NowPlayingPage) SelectAll() {
	if l := a.currentTracklistOrNil(); l != nil {
		l.SelectAll()
	}
}

func (a *NowPlayingPage) UnselectAll() {
	if l := a.currentTracklistOrNil(); l != nil {
		l.UnselectAll()
	}
}

func (a *NowPlayingPage) Refresh() {
	if a.backgroundGradient != nil {
		c := theme.Color(myTheme.ColorNamePageBackground)
		if c != a.backgroundGradient.EndColor {
			a.backgroundGradient.EndColor = c
			a.backgroundGradient.Refresh()
		}
	}
	a.BaseWidget.Refresh()

	a.card.ShowAlbumYear = a.cfg.AlbumsPage.ShowYears
	a.card.Update(a.nowPlaying)
}

func (a *NowPlayingPage) saveSelectedTab(tabNum int) {
	var tabName string
	switch tabNum {
	case 0:
		tabName = "Lyrics"
	case 1:
		tabName = "Related"
	}
	a.conf.InitialView = tabName
}

func (a *NowPlayingPage) formatStatusLine() {
	curPlayer := a.pm.CurrentPlayer()
	lastStatus := a.statusLabel.Text

	mediaInfo := a.formatMediaInfoStr(curPlayer)
	a.statusLabel.Text = mediaInfo
	if lastStatus != a.statusLabel.Text {
		a.statusLabel.Refresh()
	}
}

func (a *NowPlayingPage) formatMediaInfoStr(player player.BasePlayer) string {
	mpv, ok := player.(*mpv.Player)
	if !ok {
		return ""
	}
	audioInfo, err := mpv.GetMediaInfo()
	if err != nil {
		log.Printf("error getting playback status: %s", err.Error())
		return ""
	}
	codec := audioInfo.Codec
	if len(codec) <= 4 && !strings.EqualFold(codec, "opus") {
		codec = strings.ToUpper(codec) // FLAC, MP3, AAC, etc
	}

	// Note: bit depth intentionally omitted since MPV reports the decoded bit depth
	// i.e. 24 bit files get reported as 32 bit. Also b/c bit depth isn't meaningful for lossy.
	return fmt.Sprintf("%s %g kHz, %d kbps", codec, float64(audioInfo.Samplerate)/1000, audioInfo.Bitrate/1000)
}

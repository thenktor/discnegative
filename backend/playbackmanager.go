package backend

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charlievieth/strcase"
	"github.com/thenktor/discnegative/backend/mediaprovider"
	"github.com/thenktor/discnegative/backend/player"
	"github.com/thenktor/discnegative/backend/player/mpv"
	"github.com/thenktor/discnegative/sharedutil"
)

// A high-level MediaProvider-aware playback engine, serves as an
// intermediary between the frontend and various Player backends.
type PlaybackManager struct {
	engine   *playbackEngine
	cache    *AudioCache
	cmdQueue *playbackCommandQueue
	appCfg   *AppConfig
	cfg      *PlaybackConfig

	// CoverArtPathFn returns a local filesystem path to the cached cover
	// art image for the given CoverArtID. Set externally
	// (typically wired to ImageManager.GetCoverArtPath).
	CoverArtPathFn func(coverArtID string) (string, error)

	localPlayer player.BasePlayer

	onPlayerChange []func()

	lastPlayTime  float64
	lastPlayingID string

	// Mirrors cfg.EndlessPlay for lock-free reads from player and UI
	// goroutines. cfg is only used for persisting the setting.
	endlessPlay atomic.Bool

	// Whether endless play tracks are currently being fetched/enqueued and when
	// the last attempt happened. Guarded by endlessPlayMu, as endless play is
	// triggered from several goroutines (time pos poller, player callbacks, UI).
	endlessPlayMu          sync.Mutex
	pendingEndlessPlay     bool
	lastEndlessPlayAttempt time.Time

	wasLoadTrackPaused bool

	// current radio metadata
	radioStationName string
	radioIcyTitle    string
	radioIcyArtist   string
}

func NewPlaybackManager(
	ctx context.Context,
	s *ServerManager,
	c *AudioCache,
	p player.BasePlayer,
	playbackCfg *PlaybackConfig,
	scrobbleCfg *ScrobbleConfig,
	transcodeCfg *TranscodingConfig,
	appCfg *AppConfig,
) *PlaybackManager {
	e := NewPlaybackEngine(ctx, s, c, p, playbackCfg, scrobbleCfg, transcodeCfg)
	q := NewCommandQueue()
	pm := &PlaybackManager{
		engine:      e,
		cmdQueue:    q,
		appCfg:      appCfg,
		cfg:         playbackCfg,
		localPlayer: p,
		cache:       c,
	}
	pm.endlessPlay.Store(playbackCfg.EndlessPlay)
	pm.addOnTrackChangeHook()
	go pm.runCmdQueue(ctx)
	return pm
}

func (p *PlaybackManager) addOnTrackChangeHook() {
	// See https://github.com/thenktor/discnegative/issues/483
	// On Windows, MPV sometimes fails to start playback when switching to a track
	// with a different sample rate than the previous. If this is detected,
	// send a command to the MPV player to force restart playback.
	p.OnPlayTimeUpdate(func(curTime, totalTime float64, _ bool) {
		p.lastPlayTime = curTime

		// Fallback trigger for endless play. The primary trigger is the
		// onBeforeSongChange hook below, which requests more tracks as soon as
		// the last track starts. This check only retries during the final
		// seconds of the queue, e.g. after a failed fetch attempt.
		if !p.endlessPlay.Load() || totalTime-curTime >= endlessPlayTriggerSeconds {
			return
		}
		if p.engine.getPlayQueueLength()-1-p.NowPlayingIndex() > endlessPlayUpcomingTracks {
			return
		}
		p.requestEndlessPlayTracks(nil, false)
	})

	// Primary trigger for endless play: request more tracks as soon as the
	// queue is (nearly) exhausted. Doing this when the last track *starts*
	// (instead of shortly before it ends) gives the server a full track
	// duration to answer, which is essential for slow/remote servers.
	p.OnBeforeSongChange(func(next mediaprovider.MediaItem) {
		if next != nil || !p.endlessPlay.Load() {
			return
		}
		p.requestEndlessPlayTracks(nil, false)
	})

	// If playback nevertheless ran out of tracks (e.g. the server was
	// unreachable for a while), append tracks and resume playback automatically.
	p.OnQueueFinished(func(lastPlayed mediaprovider.MediaItem) {
		if !p.endlessPlay.Load() {
			return
		}
		p.requestEndlessPlayTracks(lastPlayed, true)
	})

	// The next track must be set on the player from the playback command queue
	// goroutine, so it cannot race with play queue modifications.
	p.engine.onNextTrackUpdateRequest = append(p.engine.onNextTrackUpdateRequest, func() {
		p.cmdQueue.UpdateNextTrack()
	})

	p.OnSongChange(func(item mediaprovider.MediaItem, _ *mediaprovider.Track) {
		if p.wasLoadTrackPaused {
			p.wasLoadTrackPaused = false
		}

		if runtime.GOOS != "windows" {
			return
		}
		// workaround for https://github.com/thenktor/discnegative/issues/483 (see above comment)
		if p.NowPlayingIndex() != p.engine.getPlayQueueLength() && p.PlaybackStatus().State == player.Playing {
			p.lastPlayTime = 0
			go func() {
				time.Sleep(300 * time.Millisecond)
				if p.lastPlayTime == 0 {
					log.Println("Play stall detected!")
					p.cmdQueue.addCommand(playbackCommand{Type: cmdForceRestartPlayback})
				}
			}()
		}
	})
	p.OnRadioMetadataChange(func(radioName, title, artist string) {
		p.radioStationName = radioName
		p.radioIcyTitle = title
		p.radioIcyArtist = artist
	})
}

func (p *PlaybackManager) CurrentPlayer() player.BasePlayer {
	return p.engine.CurrentPlayer()
}

func (p *PlaybackManager) OnPlayerChange(cb func()) {
	p.onPlayerChange = append(p.onPlayerChange, cb)
}

func (p *PlaybackManager) IsSeeking() bool {
	return p.engine.IsSeeking()
}

// Should only be called before quitting.
// Disables playback state callbacks being sent
func (p *PlaybackManager) DisableCallbacks() {
	p.engine.callbacksDisabled = true
}

// Gets the now playing media item, if any.
func (p *PlaybackManager) NowPlaying() mediaprovider.MediaItem {
	item := p.engine.NowPlaying()
	if station, ok := item.(*mediaprovider.RadioStation); ok {
		station = station.Copy().(*mediaprovider.RadioStation)
		station.StationName = p.radioStationName
		station.Title = p.radioIcyTitle
		station.Artists = []string{p.radioIcyArtist}
		item = station
	}
	return item
}

func (p *PlaybackManager) NowPlayingIndex() int {
	return p.engine.NowPlayingIndex()
}

// Registers a callback that is notified whenever the next-up media item changes.
// The callback receives nil if there is no next item.
func (p *PlaybackManager) OnBeforeSongChange(cb func(next mediaprovider.MediaItem)) {
	p.engine.onBeforeSongChange = append(p.engine.onBeforeSongChange, cb)
}

// Registers a callback that is notified when playback stopped because the end
// of the play queue was reached (not because the user stopped playback).
func (p *PlaybackManager) OnQueueFinished(cb func(lastPlayed mediaprovider.MediaItem)) {
	p.engine.onQueueFinished = append(p.engine.onQueueFinished, cb)
}

// Sets a callback that is notified whenever a new song begins playing.
func (p *PlaybackManager) OnSongChange(cb func(nowPlaying mediaprovider.MediaItem, justScrobbledIfAny *mediaprovider.Track)) {
	p.engine.onSongChange = append(p.engine.onSongChange, cb)
}

// Sets a callback that is notified whenever the Icy radio metadata changes.
func (p *PlaybackManager) OnRadioMetadataChange(cb func(radioName, title, artist string)) {
	p.engine.onRadioMetadataChange = append(p.engine.onRadioMetadataChange, cb)
}

// Registers a callback that is notified whenever the play time should be updated.
func (p *PlaybackManager) OnPlayTimeUpdate(cb func(curTime float64, totalTime float64, seeked bool)) {
	p.engine.onPlayTimeUpdate = append(p.engine.onPlayTimeUpdate, cb)
}

// Registers a callback that is notified whenever the loop mode changes.
func (p *PlaybackManager) OnLoopModeChange(cb func(LoopMode)) {
	p.engine.onLoopModeChange = append(p.engine.onLoopModeChange, cb)
}

// Registers a callback that is notified whenever the shuffle state changes.
func (p *PlaybackManager) OnShuffleChange(cb func(bool)) {
	p.engine.onShuffleChange = append(p.engine.onShuffleChange, cb)
}

// Registers a callback that is notified whenever the volume changes.
func (p *PlaybackManager) OnVolumeChange(cb func(int)) {
	p.engine.onVolumeChange = append(p.engine.onVolumeChange, cb)
}

// Registers a callback that is notified whenever the play queue changes.
func (p *PlaybackManager) OnQueueChange(cb func()) {
	p.engine.onQueueChange = append(p.engine.onQueueChange, cb)
}

// Registers a callback that is notified whenever the player has been seeked.
func (p *PlaybackManager) OnSeek(cb func()) {
	p.engine.onSeek = append(p.engine.onSeek, cb)
}

// Registers a callback that is notified whenever the player has been paused.
func (p *PlaybackManager) OnPaused(cb func()) {
	p.engine.onPaused = append(p.engine.onPaused, cb)
}

// Registers a callback that is notified whenever the player is stopped.
func (p *PlaybackManager) OnStopped(cb func()) {
	p.engine.onStopped = append(p.engine.onStopped, cb)
}

// Registers a callback that is notified whenever the player begins playing.
func (p *PlaybackManager) OnPlaying(cb func()) {
	p.engine.onPlaying = append(p.engine.onPlaying, cb)
}

// Loads the specified album into the play queue.
func (p *PlaybackManager) LoadAlbum(albumID string, insertQueueMode InsertQueueMode, shuffle bool) error {
	album, err := p.engine.sm.Server.GetAlbum(albumID)
	if err != nil {
		return err
	}
	p.LoadTracks(album.Tracks, insertQueueMode, shuffle)
	return nil
}

func (p *PlaybackManager) IsShuffle() bool {
	return p.cfg.Shuffle
}

// Loads the specified playlist into the play queue.
func (p *PlaybackManager) LoadPlaylist(playlistID string, insertQueueMode InsertQueueMode, shuffle bool) error {
	playlist, err := p.engine.sm.Server.GetPlaylist(playlistID)
	if err != nil {
		return err
	}
	p.LoadTracks(playlist.Tracks, insertQueueMode, shuffle)
	return nil
}

// Load tracks into the play queue.
// If replacing the current queue (!appendToQueue), playback will be stopped.
func (p *PlaybackManager) LoadTracks(tracks []*mediaprovider.Track, insertQueueMode InsertQueueMode, shuffle bool) {
	items := sharedutil.CopyTrackSliceToMediaItemSlice(tracks)
	p.cmdQueue.LoadItems(items, insertQueueMode, shuffle)
}

// Replaces the playQueue with tracks and moves the track at idx to position 0
func (p *PlaybackManager) LoadTracksAndPlayAtIdx(tracks []*mediaprovider.Track, shuffle bool, idx int) {
	items := sharedutil.CopyTrackSliceToMediaItemSlice(tracks)
	p.cmdQueue.LoadItemsAndPlayAtIdx(items, shuffle, idx)
}

// Load items into the currently active queue. (shuffledPlayQueue/playQueue)
// If replacing the current queue (!appendToQueue), playback will be stopped.
// Loading items into the shuffledPlayQueue may also modify the playQueue
func (p *PlaybackManager) LoadItems(items []mediaprovider.MediaItem, insertQueueMode InsertQueueMode, shuffle bool) {
	p.cmdQueue.LoadItems(items, insertQueueMode, shuffle)
}

// Replaces the specified queue (PlayQueue/ShuffledPlayQueue) with the given tracks.
// This is used when starting discnegative to load the queue state and directly overrides any previous data.
// For replacing the queue while discnegative is running, use LoadTracks
func (p *PlaybackManager) SetQueueState(tracks []*mediaprovider.Track, queueType QueueType) {
	p.cmdQueue.SetQueueState(tracks, queueType)
}

// Replaces the play queue with the given set of tracks.
// Does not stop playback if the currently playing track is in the new queue,
// but updates the now playing index to point to the first instance of the track in the new queue.
func (p *PlaybackManager) UpdatePlayQueue(items []mediaprovider.MediaItem) {
	p.cmdQueue.UpdatePlayQueue(items)
}

func (p *PlaybackManager) PlayAlbum(albumID string, firstTrack int, shuffle bool) error {
	if err := p.LoadAlbum(albumID, Replace, shuffle); err != nil {
		return err
	}
	if p.engine.replayGainCfg.Mode == ReplayGainAuto {
		p.SetReplayGainMode(player.ReplayGainAlbum)
	}
	p.PlayTrackAt(firstTrack)
	return nil
}

func (p *PlaybackManager) PlayPlaylist(playlistID string, firstTrack int, shuffle bool) error {
	if err := p.LoadPlaylist(playlistID, Replace, shuffle); err != nil {
		return err
	}
	if p.engine.replayGainCfg.Mode == ReplayGainAuto {
		p.SetReplayGainMode(player.ReplayGainTrack)
	}
	p.PlayTrackAt(firstTrack)
	return nil
}

func (p *PlaybackManager) PlayTrack(trackID string) error {
	tr, err := p.engine.sm.Server.GetTrack(trackID)
	if err != nil {
		return err
	}
	p.LoadTracks([]*mediaprovider.Track{tr}, Replace, false)
	if p.engine.replayGainCfg.Mode == ReplayGainAuto {
		p.SetReplayGainMode(player.ReplayGainTrack)
	}
	p.PlayFromBeginning()
	return nil
}

func (p *PlaybackManager) ShuffleArtistAlbums(artistID string) {
	artist, err := p.engine.sm.Server.GetArtist(artistID)
	if err != nil {
		log.Printf("failed to get artist: %v\n", err)
		return
	}
	if len(artist.Albums) == 0 {
		return
	}

	rand.Shuffle(len(artist.Albums), func(i, j int) {
		artist.Albums[i], artist.Albums[j] = artist.Albums[j], artist.Albums[i]
	})
	p.StopAndClearPlayQueue()
	for _, al := range artist.Albums {
		p.LoadAlbum(al.ID, Append, false)
	}

	if p.engine.replayGainCfg.Mode == ReplayGainAuto {
		p.SetReplayGainMode(player.ReplayGainAlbum)
	}
	p.PlayFromBeginning()
}

func (p *PlaybackManager) PlayArtistDiscography(artistID string, shuffleTracks bool) {
	tr, err := p.engine.sm.Server.GetArtistTracks(artistID)
	if err != nil {
		log.Printf("failed to get artist tracks: %v\n", err)
		return
	}
	p.LoadTracks(tr, Replace, shuffleTracks)
	if p.engine.replayGainCfg.Mode == ReplayGainAuto {
		if shuffleTracks {
			p.SetReplayGainMode(player.ReplayGainTrack)
		} else {
			p.SetReplayGainMode(player.ReplayGainAlbum)
		}
	}
	p.PlayFromBeginning()
}

func (p *PlaybackManager) PlayFromBeginning() {
	p.cmdQueue.PlayTrackAt(0)
}

func (p *PlaybackManager) PlayTrackAt(idx int) {
	p.cmdQueue.PlayTrackAt(idx)
}

// LoadTrackPaused sets up engine state as if the track at idx is loaded and
// paused at startTime, updating the UI and OS media integrations, without
// starting MPV. Call Continue (or PlayPause) to begin actual playback.
func (p *PlaybackManager) LoadTrackPaused(idx int, startTime float64) {
	p.cmdQueue.LoadTrackPaused(idx, startTime)
}

func (p *PlaybackManager) PlayRandomSongs(genreName string) error {
	return p.fetchAndPlayTracks(func() ([]*mediaprovider.Track, error) {
		tr, err := p.engine.sm.Server.GetRandomTracks(genreName, RadioTrackCount)
		if err != nil {
			return nil, err
		}
		return sharedutil.FilterSlice(tr, func(t *mediaprovider.Track) bool {
			skipKwd := p.cfg.SkipKeywordWhenShuffling
			include :=
				(skipKwd == "" || !strcase.Contains(t.Title, skipKwd)) &&
					(!p.cfg.SkipOneStarWhenShuffling || t.Rating != 1)
			return include
		}), nil
	})
}

func (p *PlaybackManager) PlaySimilarSongs(id string) error {
	return p.fetchAndPlayTracks(func() ([]*mediaprovider.Track, error) {
		return p.engine.sm.Server.GetSimilarTracks(id, RadioTrackCount)
	})
}

func (p *PlaybackManager) PlayRandomAlbums(genreName string) error {
	mp := p.engine.sm.GetServer()
	if mp == nil {
		return errors.New("logged out")
	}

	if p.engine.replayGainCfg.Mode == ReplayGainAuto {
		p.SetReplayGainMode(player.ReplayGainAlbum)
	}

	var options mediaprovider.AlbumFilterOptions
	if genreName != "" {
		options = mediaprovider.AlbumFilterOptions{
			Genres: []string{genreName},
		}
	}
	iter := mp.IterateAlbums(mediaprovider.AlbumSortRandom, mediaprovider.NewAlbumFilter(options))
	insertMode := Replace
	for i := range 20 {
		al := iter.Next()
		if al == nil {
			break
		}
		if al, err := mp.GetAlbum(al.ID); err == nil {
			p.LoadTracks(al.Tracks, insertMode, false)
			if i == 0 {
				p.PlayFromBeginning()
				insertMode = Append
			}
		}
	}

	return nil
}

func (p *PlaybackManager) LoadRadioStation(station *mediaprovider.RadioStation, queueMode InsertQueueMode) {
	p.cmdQueue.LoadRadioStation(station, queueMode)
}

func (p *PlaybackManager) PlayRadioStation(station *mediaprovider.RadioStation) {
	p.LoadRadioStation(station, Replace)
	p.PlayFromBeginning()
}

func (p *PlaybackManager) fetchAndPlayTracks(fetchFn func() ([]*mediaprovider.Track, error)) error {
	if songs, err := fetchFn(); err != nil {
		return err
	} else {
		p.LoadTracks(songs, Replace, false)
		if p.engine.replayGainCfg.Mode == ReplayGainAuto {
			p.SetReplayGainMode(player.ReplayGainTrack)
		}
		p.PlayFromBeginning()
		return nil
	}
}

func (p *PlaybackManager) GetActivePlayQueue() []mediaprovider.MediaItem {
	return p.engine.GetActivePlayQueueDeepCopy()
}

func (p *PlaybackManager) GetPlayQueue() []mediaprovider.MediaItem {
	return p.engine.GetPlayQueueDeepCopy()
}

func (p *PlaybackManager) GetShuffledPlayQueue() []mediaprovider.MediaItem {
	return p.engine.GetShuffledPlayQueueDeepCopy()
}

// Any time the user changes the favorite status of a track elsewhere in the app,
// this should be called to ensure the in-memory track model is updated.
func (p *PlaybackManager) OnTrackFavoriteStatusChanged(id string, fav bool) {
	p.engine.OnTrackFavoriteStatusChanged(id, fav)
}

// Any time the user changes the rating of a track elsewhere in the app,
// this should be called to ensure the in-memory track model is updated.
func (p *PlaybackManager) OnTrackRatingChanged(id string, rating int) {
	p.engine.OnTrackRatingChanged(id, rating)
}

func (p *PlaybackManager) RemoveTracksFromQueue(idxs []int) {
	p.cmdQueue.RemoveItemsFromQueue(idxs)
}

// Stop playback and clear the play queue.
func (p *PlaybackManager) StopAndClearPlayQueue() {
	p.cmdQueue.StopAndClearPlayQueue()
}

func (p *PlaybackManager) SetReplayGainOptions(config ReplayGainConfig) {
	p.engine.SetReplayGainOptions(config)
}

func (p *PlaybackManager) SetReplayGainMode(mode player.ReplayGainMode) {
	p.engine.SetReplayGainMode(mode)
}

// Changes the loop mode of the player to the next one.
// Useful for toggling UI elements, to change modes without knowing the current player mode.
func (p *PlaybackManager) SetNextLoopMode() {
	switch p.engine.loopMode {
	case LoopNone:
		p.cmdQueue.SetLoopMode(LoopAll)
	case LoopAll:
		p.cmdQueue.SetLoopMode(LoopOne)
	case LoopOne:
		p.cmdQueue.SetLoopMode(LoopNone)
	}
}

func (p *PlaybackManager) SetLoopMode(loopMode LoopMode) {
	p.cmdQueue.SetLoopMode(loopMode)
}

func (p *PlaybackManager) GetLoopMode() LoopMode {
	return p.engine.loopMode
}

func (p *PlaybackManager) IsEndlessPlay() bool {
	return p.endlessPlay.Load()
}

func (p *PlaybackManager) PlaybackStatus() PlaybackStatus {
	return p.engine.PlaybackStatus()
}

func (p *PlaybackManager) SetVolume(vol int) {
	p.cmdQueue.SetVolume(vol)
}

func (p *PlaybackManager) SetEndlessPlay(endlessPlay bool) {
	p.endlessPlay.Store(endlessPlay)
	p.cfg.EndlessPlay = endlessPlay
	if endlessPlay {
		// a user initiated request should not be delayed by the retry interval
		p.endlessPlayMu.Lock()
		p.lastEndlessPlayAttempt = time.Time{}
		p.endlessPlayMu.Unlock()
		// no-op unless the queue is (nearly) exhausted, see endlessPlaySnapshot
		p.requestEndlessPlayTracks(nil, false)
	}
}

func (p *PlaybackManager) SetShuffle(shuffle bool) {
	p.cfg.Shuffle = shuffle
	p.engine.SetShuffle(shuffle)
}

func (p *PlaybackManager) Volume() int {
	return p.engine.CurrentPlayer().GetVolume()
}

func (p *PlaybackManager) SeekNext() {
	p.cmdQueue.SeekNext()
}

func (p *PlaybackManager) SeekBackOrPrevious() {
	p.cmdQueue.SeekBackOrPrevious()
}

// Seek to given absolute position in the current track by seconds.
func (p *PlaybackManager) SeekSeconds(sec float64) {
	p.cmdQueue.SeekSeconds(sec)
}

// Seek by given relative position in the current track by seconds.
func (p *PlaybackManager) SeekBySeconds(sec float64) {
	status := p.engine.PlaybackStatus()
	target := status.TimePos + sec
	if target < 0 {
		target = 0
	} else if target > status.Duration {
		target = status.Duration
	}
	p.cmdQueue.SeekSeconds(target)
}

// Seek to a fractional position in the current track [0..1]
func (p *PlaybackManager) SeekFraction(fraction float64) {
	if fraction < 0 {
		fraction = 0
	} else if fraction > 1 {
		fraction = 1
	}
	target := p.engine.curTrackDuration * fraction
	p.cmdQueue.SeekSeconds(target)
}

func (p *PlaybackManager) Stop() {
	p.cmdQueue.Stop()
}

func (p *PlaybackManager) Shutdown() {
	p.cmdQueue.StopAndWait()
}

func (p *PlaybackManager) Pause() {
	p.cmdQueue.Pause()
}

func (p *PlaybackManager) Continue() {
	p.cmdQueue.Continue()
}

func (p *PlaybackManager) PlayPause() {
	switch p.engine.PlaybackStatus().State {
	case player.Playing:
		p.Pause()
	case player.Paused:
		p.Continue()
	case player.Stopped:
		p.PlayTrackAt(0)
	}
}

func (p *PlaybackManager) SetPauseAfterCurrent(pauseAfterCurrent bool) {
	p.engine.SetPauseAfterCurrent(pauseAfterCurrent)
}

func (p *PlaybackManager) IsPauseAfterCurrent() bool {
	return p.engine.pauseAfterCurrent
}

const (
	// Number of not yet played tracks that may remain in the queue before more
	// tracks are requested. Requesting with one track left in addition to the
	// currently playing one gives the server a full track duration of lead time.
	endlessPlayUpcomingTracks = 1
	// Fallback trigger: seconds before the end of the current track.
	endlessPlayTriggerSeconds = 60.0
	// Minimum time between two (re-)attempts to fetch endless play tracks, so a
	// failing/unreachable server is not hammered with requests.
	endlessPlayRetryInterval = 10 * time.Second
	// Number of already played tracks that are excluded from endless play
	// candidates. The play queue grows indefinitely and is never trimmed, so
	// recently played tracks must be tracked separately from the queue.
	endlessPlayPlayedWindow = 500
	// DefaultEnqueueBatchSize is used if EnqueueBatchSize is not configured.
	DefaultEnqueueBatchSize = 10
	// Allowed range for the configurable Application.EnqueueBatchSize.
	MinEnqueueBatchSize = 3
	MaxEnqueueBatchSize = 100
	// RadioTrackCount is the fixed number of tracks requested for the radio
	// style features ("Play random", "Play song radio"), which are expected to
	// fill the queue with many tracks. Intentionally not configurable.
	RadioTrackCount = 100
)

// Snapshot of the state needed to fetch endless play tracks. It is created from
// the playback command queue goroutine, because it reads the play queue.
type endlessPlaySnapshot struct {
	// reference track for the "similar by artist"/"similar by genre" strategies
	refTrack *mediaprovider.Track
	// IDs of tracks that must never be added: the track that is currently
	// playing and everything still queued after it
	skipIDs map[string]struct{}
	// IDs of the most recently played tracks.
	playedIDs map[string]struct{}

	skipOneStar bool
	skipKeyword string
	batchSize   int

	// generation of the play queue when the snapshot was created; if it
	// changed while the tracks were fetched, the result is discarded
	queueGeneration uint64
}

// Requests more endless play tracks. May be called from any goroutine; the
// actual queue inspection and fetching happens in the playback command queue
// goroutine and in a separate fetch goroutine, respectively.
// lastPlayed is the media item that was playing when playback ran out of
// tracks; if nil, the currently playing item is used. resume requests that
// playback is restarted with the appended tracks if the player is stopped.
func (p *PlaybackManager) requestEndlessPlayTracks(lastPlayed mediaprovider.MediaItem, resume bool) {
	p.endlessPlayMu.Lock()
	if p.pendingEndlessPlay || time.Since(p.lastEndlessPlayAttempt) < endlessPlayRetryInterval {
		p.endlessPlayMu.Unlock()
		return
	}
	p.pendingEndlessPlay = true
	p.lastEndlessPlayAttempt = time.Now()
	p.endlessPlayMu.Unlock()

	p.cmdQueue.EnqueueEndlessPlay(lastPlayed, resume)
}

func (p *PlaybackManager) finishEndlessPlayRequest() {
	p.endlessPlayMu.Lock()
	p.pendingEndlessPlay = false
	p.endlessPlayMu.Unlock()
}

// Must only be called from the playback command queue goroutine.
func (p *PlaybackManager) enqueueEndlessPlayTracks(lastPlayed mediaprovider.MediaItem, resume bool) {
	snap := p.endlessPlaySnapshot(lastPlayed, resume)
	if snap == nil {
		p.finishEndlessPlayRequest()
		return
	}

	// fetching tracks may take a while, so do it asynchronously in order to not
	// block playback commands
	go func() {
		tracks := p.fetchEndlessPlayTracks(snap)
		if len(tracks) == 0 {
			p.finishEndlessPlayRequest()
			return
		}
		items := sharedutil.CopyTrackSliceToMediaItemSlice(tracks)
		p.cmdQueue.AppendEndlessPlay(items, snap, resume)
	}()
}

// Appends fetched endless play tracks to the play queue. The result is
// discarded if the play queue was replaced/modified while the tracks were
// fetched or if endless play was disabled meanwhile.
// Must only be called from the playback command queue goroutine.
func (p *PlaybackManager) appendEndlessPlayTracks(items []mediaprovider.MediaItem, snap *endlessPlaySnapshot, resume bool) {
	defer p.finishEndlessPlayRequest()

	if len(items) == 0 || !p.endlessPlay.Load() {
		return
	}
	if p.engine.getQueueGeneration() != snap.queueGeneration {
		return
	}
	if err := p.engine.LoadItems(items, Append, false); err != nil {
		log.Printf("endless play error: failed to append tracks: %v", err)
		return
	}
	if resume {
		p.resumePlaybackAfterEndlessAppend(len(items))
	}
}

// Must only be called from the playback command queue goroutine.
func (p *PlaybackManager) endlessPlaySnapshot(lastPlayed mediaprovider.MediaItem, resume bool) *endlessPlaySnapshot {
	if !p.endlessPlay.Load() {
		return nil
	}

	queue := p.engine.getActivePlayQueue()
	queueLen := len(queue)

	nowPlayingIdx := p.engine.NowPlayingIndex()
	if nowPlayingIdx < 0 {
		if !resume {
			return nil
		}
		// playback already ran out of tracks, so the whole queue has been played
		nowPlayingIdx = queueLen - 1
	}
	if nowPlayingIdx < 0 || queueLen == 0 {
		return nil
	}

	// Only extend the queue when it is (nearly) exhausted, so endless play
	// doesn't modify the queue while the user still has manually queued tracks.
	if !resume && queueLen-1-nowPlayingIdx > endlessPlayUpcomingTracks {
		return nil
	}

	refItem := lastPlayed
	if refItem == nil {
		refItem = p.engine.NowPlaying()
	}
	if refItem == nil {
		return nil
	}
	refTrack, ok := refItem.(*mediaprovider.Track)
	if !ok {
		// endless play is not supported for radio stations
		return nil
	}

	snap := &endlessPlaySnapshot{
		refTrack:        refTrack,
		skipIDs:         make(map[string]struct{}),
		playedIDs:       make(map[string]struct{}),
		skipOneStar:     p.cfg.SkipOneStarWhenShuffling,
		skipKeyword:     p.cfg.SkipKeywordWhenShuffling,
		batchSize:       p.appCfg.EnqueueBatchSize,
		queueGeneration: p.engine.getQueueGeneration(),
	}
	if snap.batchSize <= 0 {
		snap.batchSize = DefaultEnqueueBatchSize
	}
	snap.batchSize = min(max(snap.batchSize, MinEnqueueBatchSize), MaxEnqueueBatchSize)

	for i := nowPlayingIdx; i < queueLen; i++ {
		snap.skipIDs[queue[i].Metadata().ID] = struct{}{}
	}
	// the queue is append-only, so the already played items are the ones
	// *before* the now playing index (and not the last items of the queue)
	playedStart := max(0, nowPlayingIdx-endlessPlayPlayedWindow)
	for i := playedStart; i < nowPlayingIdx; i++ {
		snap.playedIDs[queue[i].Metadata().ID] = struct{}{}
	}
	return snap
}

func (p *PlaybackManager) fetchEndlessPlayTracks(snap *endlessPlaySnapshot) []*mediaprovider.Track {
	server := p.engine.sm.GetServer()
	if server == nil {
		return nil
	}

	// Prefer tracks that weren't played recently, but allow repeats of recently
	// played tracks rather than running dry on small libraries.
	pick := func(candidates []*mediaprovider.Track) []*mediaprovider.Track {
		if tracks := p.filterEndlessPlayTracks(candidates, snap, true); len(tracks) > 0 {
			return tracks
		}
		return p.filterEndlessPlayTracks(candidates, snap, false)
	}

	if ref := snap.refTrack; ref != nil {
		// similar tracks by the same artist
		if len(ref.ArtistIDs) > 0 {
			similar, err := server.GetSimilarTracks(ref.ArtistIDs[0], snap.batchSize)
			if err != nil {
				log.Printf("endless play error: failed to get similar tracks: %v", err)
			}
			if tracks := pick(similar); len(tracks) > 0 {
				return tracks
			}
		}
		// fallback to random tracks from one of the track's genres
		for _, g := range ref.Genres {
			if g == "" {
				continue
			}
			byGenre, err := server.GetRandomTracks(g, snap.batchSize)
			if err != nil {
				log.Printf("endless play error: failed to get tracks by genre: %v", err)
			}
			if tracks := pick(byGenre); len(tracks) > 0 {
				return tracks
			}
		}
	}

	// fallback to random tracks
	random, err := server.GetRandomTracks("", snap.batchSize)
	if err != nil {
		log.Printf("endless play error: failed to get random tracks: %v", err)
	}
	return pick(random)
}

func (p *PlaybackManager) filterEndlessPlayTracks(candidates []*mediaprovider.Track, snap *endlessPlaySnapshot, skipPlayed bool) []*mediaprovider.Track {
	tracks := make([]*mediaprovider.Track, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, t := range candidates {
		if t == nil {
			continue
		}
		id := t.Metadata().ID
		if _, ok := seen[id]; ok {
			continue // duplicate within the fetched batch
		}
		if _, ok := snap.skipIDs[id]; ok {
			continue // currently playing or already queued
		}
		if skipPlayed {
			if _, ok := snap.playedIDs[id]; ok {
				continue // played recently
			}
		}
		if snap.skipOneStar && t.Rating == 1 {
			continue
		}
		if snap.skipKeyword != "" && strcase.Contains(t.Title, snap.skipKeyword) {
			continue
		}
		seen[id] = struct{}{}
		tracks = append(tracks, t)
	}
	return tracks
}

// Must only be called from the playback command queue goroutine.
func (p *PlaybackManager) resumePlaybackAfterEndlessAppend(appendedCount int) {
	if !p.endlessPlay.Load() || p.engine.stopRequested.Load() {
		return // endless play got disabled or the user stopped playback meanwhile
	}
	if p.engine.PlaybackStatus().State != player.Stopped {
		return // playback is already running (or paused) again
	}
	// append inserts the tracks at the end of the queue
	idx := p.engine.getPlayQueueLength() - appendedCount
	if idx < 0 {
		idx = 0
	}
	if err := p.engine.PlayTrackAt(idx); err != nil {
		log.Printf("endless play error: failed to resume playback: %v", err)
	}
}

func (p *PlaybackManager) runCmdQueue(ctx context.Context) {
	logIfErr := func(action string, err error) {
		if err != nil {
			log.Printf("Playback error (%s): %v", action, err)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case c := <-p.cmdQueue.C():
			switch c.Type {
			case cmdStop:
				logIfErr("Stop", p.engine.Stop())
			case cmdContinue:
				logIfErr("Continue", p.engine.Continue())
			case cmdPause:
				logIfErr("Pause", p.engine.Pause())
			case cmdPlayTrackAt:
				logIfErr("PlayTrackAt", p.engine.PlayTrackAt(c.Arg.(int)))
			case cmdSeekSeconds:
				logIfErr("SeekSeconds", p.engine.SeekSeconds(c.Arg.(float64)))
			case cmdSeekFwdBackN:
				action := fmt.Sprintf("SeekFwdBack[%d]", c.Arg.(int))
				logIfErr(action, p.engine.SeekFwdBackN(c.Arg.(int)))
			case cmdVolume:
				logIfErr("Volume", p.engine.SetVolume(c.Arg.(int)))
			case cmdLoopMode:
				p.engine.SetLoopMode(c.Arg.(LoopMode))
			case cmdStopAndClearPlayQueue:
				p.engine.StopAndClearPlayQueue()
			case cmdUpdatePlayQueue:
				logIfErr("UpdatePlayQueue", p.engine.UpdatePlayQueue(c.Arg.([]mediaprovider.MediaItem)))
			case cmdRemoveTracksFromQueue:
				p.engine.RemoveTracksFromQueue(c.Arg.([]int))
			case cmdLoadItems:
				err := p.engine.LoadItems(
					c.Arg.([]mediaprovider.MediaItem),
					c.Arg2.(InsertQueueMode),
					c.Arg3.(bool),
				)
				logIfErr("LoadItems", err)
			case cmdLoadItemsAndPlayAtIdx:
				err := p.engine.LoadItemsAndPlayAtIdx(
					c.Arg.([]mediaprovider.MediaItem),
					c.Arg2.(bool),
					c.Arg3.(int),
				)
				logIfErr("LoadItemsAndPlayAtIdx", err)
			case cmdSetQueueState:
				err := p.engine.SetQueueState(
					c.Arg.([]*mediaprovider.Track),
					c.Arg2.(QueueType),
				)
				logIfErr("SetQueueState", err)
			case cmdLoadRadioStation:
				p.engine.LoadRadioStation(
					c.Arg.(*mediaprovider.RadioStation),
					c.Arg2.(InsertQueueMode),
				)
			case cmdLoadTrackPaused:
				logIfErr("LoadTrackPaused", p.engine.loadTrackPaused(c.Arg.(int), c.Arg2.(float64)))
			case cmdUpdateNextTrack:
				logIfErr("UpdateNextTrack", p.engine.updateNextTrack())
			case cmdEnqueueEndlessPlay:
				// a nil interface cannot be type asserted, so check it explicitly
				var lastPlayed mediaprovider.MediaItem
				if c.Arg != nil {
					lastPlayed = c.Arg.(mediaprovider.MediaItem)
				}
				p.enqueueEndlessPlayTracks(lastPlayed, c.Arg2.(bool))
			case cmdAppendEndlessPlay:
				p.appendEndlessPlayTracks(
					c.Arg.([]mediaprovider.MediaItem),
					c.Arg2.(*endlessPlaySnapshot),
					c.Arg3.(bool),
				)
			case cmdForceRestartPlayback:
				if mpv, ok := p.engine.CurrentPlayer().(*mpv.Player); ok {
					log.Println("Force-restarting MPV playback")

					// restart player, but perserve the state
					isPaused := false
					stat := p.engine.CurrentPlayer().GetStatus()
					if stat.State == player.Paused {
						isPaused = true
					}
					mpv.ForceRestartPlayback(isPaused)
				}
			}
			if c.OnDone != nil {
				c.OnDone()
			}
		}
	}
}

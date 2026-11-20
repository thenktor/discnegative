package backend

import (
	"fmt"
	"testing"

	"github.com/thenktor/discnegative/backend/mediaprovider"
	"github.com/thenktor/discnegative/backend/player"
)

// minimal player implementation for tests that only inspect the play queue
type endlessPlayTestPlayer struct {
	player.BasePlayerCallbackImpl
	status player.Status
}

func (p *endlessPlayTestPlayer) Continue() error           { p.status.State = player.Playing; return nil }
func (p *endlessPlayTestPlayer) Pause() error              { p.status.State = player.Paused; return nil }
func (p *endlessPlayTestPlayer) Stop(bool) error           { p.status.State = player.Stopped; return nil }
func (p *endlessPlayTestPlayer) SeekSeconds(float64) error { return nil }
func (p *endlessPlayTestPlayer) IsSeeking() bool           { return false }
func (p *endlessPlayTestPlayer) SetVolume(int) error       { return nil }
func (p *endlessPlayTestPlayer) GetVolume() int            { return 100 }
func (p *endlessPlayTestPlayer) GetStatus() player.Status  { return p.status }
func (p *endlessPlayTestPlayer) Destroy()                  {}

func endlessPlayTestTrack(id string) *mediaprovider.Track {
	return &mediaprovider.Track{ID: id, Title: id}
}

func TestFilterEndlessPlayTracks(t *testing.T) {
	snap := &endlessPlaySnapshot{
		skipIDs:     map[string]struct{}{"queued": {}},
		playedIDs:   map[string]struct{}{"played": {}},
		skipOneStar: true,
		skipKeyword: "live",
	}
	pm := &PlaybackManager{}
	candidates := []*mediaprovider.Track{
		nil,
		endlessPlayTestTrack("ok"),
		endlessPlayTestTrack("ok"), // duplicate within the fetched batch
		endlessPlayTestTrack("queued"),
		endlessPlayTestTrack("played"),
		{ID: "onestar", Title: "onestar", Rating: 1},
		endlessPlayTestTrack("live at home"),
	}

	tracks := pm.filterEndlessPlayTracks(candidates, snap, true)
	if len(tracks) != 1 || tracks[0].ID != "ok" {
		t.Fatalf("expected only track \"ok\", got %v", trackIDs(tracks))
	}

	// recently played tracks are allowed as a last resort, but tracks that are
	// still queued must never be added again
	tracks = pm.filterEndlessPlayTracks(candidates, snap, false)
	if ids := trackIDs(tracks); len(ids) != 2 || ids[0] != "ok" || ids[1] != "played" {
		t.Fatalf("expected [ok played], got %v", ids)
	}
}

func TestEndlessPlaySnapshot(t *testing.T) {
	newManager := func(queue []mediaprovider.MediaItem, nowPlayingIdx int) *PlaybackManager {
		pm := &PlaybackManager{
			engine: &playbackEngine{
				playQueue:     queue,
				nowPlayingIdx: nowPlayingIdx,
				player:        &endlessPlayTestPlayer{status: player.Status{State: player.Playing}},
			},
			cfg:    &PlaybackConfig{},
			appCfg: &AppConfig{EnqueueBatchSize: 100},
		}
		pm.endlessPlay.Store(true)
		return pm
	}
	newQueue := func(ids ...string) []mediaprovider.MediaItem {
		queue := make([]mediaprovider.MediaItem, 0, len(ids))
		for _, id := range ids {
			queue = append(queue, endlessPlayTestTrack(id))
		}
		return queue
	}

	t.Run("queue not nearly exhausted", func(t *testing.T) {
		queue := newQueue("a", "b", "c", "d")
		pm := newManager(queue, 1)
		if snap := pm.endlessPlaySnapshot(nil, false); snap != nil {
			t.Fatalf("expected no snapshot, got %+v", snap)
		}
	})

	t.Run("last track", func(t *testing.T) {
		queue := newQueue("a", "b", "c", "d")
		pm := newManager(queue, 3)
		snap := pm.endlessPlaySnapshot(nil, false)
		if snap == nil {
			t.Fatal("expected snapshot")
		}
		if snap.refTrack == nil || snap.refTrack.ID != "d" {
			t.Fatalf("unexpected reference track: %+v", snap.refTrack)
		}
		if _, ok := snap.skipIDs["d"]; !ok {
			t.Fatal("expected the currently playing track to be excluded")
		}
		if len(snap.skipIDs) != 1 {
			t.Fatalf("expected only the currently playing track to be excluded, got %v", snap.skipIDs)
		}
		// the played tracks are the ones *before* the now playing index
		for _, id := range []string{"a", "b", "c"} {
			if _, ok := snap.playedIDs[id]; !ok {
				t.Fatalf("expected %q to be marked as played", id)
			}
		}
		if _, ok := snap.playedIDs["d"]; ok {
			t.Fatal("now playing track must not be marked as played")
		}
		if snap.batchSize != 100 {
			t.Fatalf("unexpected batch size: %d", snap.batchSize)
		}
	})

	t.Run("upcoming tracks are excluded", func(t *testing.T) {
		queue := newQueue("a", "b", "c", "d")
		pm := newManager(queue, 2)
		snap := pm.endlessPlaySnapshot(nil, false)
		if snap == nil {
			t.Fatal("expected snapshot")
		}
		for _, id := range []string{"c", "d"} {
			if _, ok := snap.skipIDs[id]; !ok {
				t.Fatalf("expected track %q to be excluded", id)
			}
		}
		if len(snap.playedIDs) != 2 {
			t.Fatalf("expected two played tracks, got %v", snap.playedIDs)
		}
	})

	t.Run("stopped without resume", func(t *testing.T) {
		queue := newQueue("a", "b")
		pm := newManager(queue, -1)
		if snap := pm.endlessPlaySnapshot(nil, false); snap != nil {
			t.Fatalf("expected no snapshot, got %+v", snap)
		}
	})

	t.Run("stopped with resume", func(t *testing.T) {
		queue := newQueue("a", "b")
		pm := newManager(queue, -1)
		snap := pm.endlessPlaySnapshot(queue[1], true)
		if snap == nil {
			t.Fatal("expected snapshot")
		}
		if snap.refTrack == nil || snap.refTrack.ID != "b" {
			t.Fatalf("unexpected reference track: %+v", snap.refTrack)
		}
		// the last played track must not be added again, the one before it is
		// part of the (recently played) window
		if _, ok := snap.skipIDs["b"]; !ok {
			t.Fatal("expected the last played track to be excluded")
		}
		if _, ok := snap.playedIDs["b"]; ok {
			t.Fatal("expected the last played track to be excluded from the played window")
		}
		if _, ok := snap.playedIDs["a"]; !ok {
			t.Fatal("expected the previously played track to be part of the played window")
		}
	})

	t.Run("radio is ignored", func(t *testing.T) {
		queue := []mediaprovider.MediaItem{&mediaprovider.RadioStation{ID: "r"}}
		pm := newManager(queue, 0)
		if snap := pm.endlessPlaySnapshot(queue[0], false); snap != nil {
			t.Fatalf("expected no snapshot, got %+v", snap)
		}
	})

	t.Run("disabled", func(t *testing.T) {
		queue := newQueue("a")
		pm := newManager(queue, 0)
		pm.endlessPlay.Store(false)
		if snap := pm.endlessPlaySnapshot(nil, false); snap != nil {
			t.Fatalf("expected no snapshot, got %+v", snap)
		}
	})

	t.Run("played window is bounded", func(t *testing.T) {
		ids := make([]string, 0, endlessPlayPlayedWindow+5)
		for i := 0; i < endlessPlayPlayedWindow+5; i++ {
			ids = append(ids, fmt.Sprintf("track-%d", i))
		}
		queue := newQueue(ids...)
		pm := newManager(queue, len(queue)-1)
		snap := pm.endlessPlaySnapshot(nil, false)
		if snap == nil {
			t.Fatal("expected snapshot")
		}
		if len(snap.playedIDs) != endlessPlayPlayedWindow {
			t.Fatalf("expected played window of %d tracks, got %d", endlessPlayPlayedWindow, len(snap.playedIDs))
		}
	})
}

func trackIDs(tracks []*mediaprovider.Track) []string {
	ids := make([]string, 0, len(tracks))
	for _, tr := range tracks {
		ids = append(ids, tr.ID)
	}
	return ids
}

package backend

import (
	"testing"

	"github.com/thenktor/discnegative/backend/mediaprovider"
	"github.com/thenktor/discnegative/backend/player"
)

func newRadioStationTestEngine(queueLen, nowPlayingIdx int, shuffle bool) *playbackEngine {
	queue := make([]mediaprovider.MediaItem, 0, queueLen)
	for i := 0; i < queueLen; i++ {
		queue = append(queue, &mediaprovider.Track{ID: string(rune('a' + i)), Title: "track"})
	}
	e := &playbackEngine{
		playQueue:     queue,
		nowPlayingIdx: nowPlayingIdx,
		shuffle:       shuffle,
		player:        &endlessPlayTestPlayer{status: player.Status{State: player.Playing}},
	}
	if shuffle {
		// the shuffled queue is kept in sync with the play queue while shuffling
		e.shuffledPlayQueue = append([]mediaprovider.MediaItem{}, queue...)
	}
	return e
}

func queueItemIDs(items []mediaprovider.MediaItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.Metadata().ID)
	}
	return ids
}

func equalIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestLoadRadioStationInsertsIntoPlayQueue(t *testing.T) {
	station := &mediaprovider.RadioStation{ID: "radio", StationName: "Radio", StreamURL: "http://example.com/stream"}

	for _, tc := range []struct {
		name       string
		shuffle    bool
		nowPlaying int
		mode       InsertQueueMode
		expected   []string
	}{
		{name: "play next", nowPlaying: 1, mode: InsertNext, expected: []string{"a", "b", "radio", "c", "d"}},
		{name: "play next while shuffling", shuffle: true, nowPlaying: 1, mode: InsertNext, expected: []string{"a", "b", "radio", "c", "d"}},
		{name: "append", nowPlaying: 1, mode: Append, expected: []string{"a", "b", "c", "d", "radio"}},
		{name: "append while shuffling", shuffle: true, nowPlaying: 1, mode: Append, expected: []string{"a", "b", "c", "d", "radio"}},
		{name: "play next without playback running", nowPlaying: -1, mode: InsertNext, expected: []string{"radio", "a", "b", "c", "d"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newRadioStationTestEngine(4, tc.nowPlaying, tc.shuffle)

			e.LoadRadioStation(station, tc.mode)

			if ids := queueItemIDs(e.playQueue); !equalIDs(ids, tc.expected) {
				t.Fatalf("unexpected play queue: %v, expected %v", ids, tc.expected)
			}
			if tc.shuffle {
				if ids := queueItemIDs(e.shuffledPlayQueue); !equalIDs(ids, tc.expected) {
					t.Fatalf("unexpected shuffled play queue: %v, expected %v", ids, tc.expected)
				}
			}
		})
	}
}

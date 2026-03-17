package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/timelinize/timelinize/timeline"
	"go.uber.org/zap"
)

func init() {
	err := timeline.RegisterDataSource(timeline.DataSource{
		Name:            "spotify",
		Title:           "Spotify",
		Icon:            "media.png",
		Description:     "A Spotify streaming history export.",
		NewFileImporter: func() timeline.FileImporter { return new(FileImporter) },
	})
	if err != nil {
		timeline.Log.Fatal("registering data source", zap.Error(err))
	}
}

// FileImporter implements the timeline.FileImporter interface.
type FileImporter struct{}

// streamingHistoryRecord is one Spotify streaming history entry.
type streamingHistoryRecord struct {
	TS                             time.Time `json:"ts"`
	Platform                       string    `json:"platform"`
	MSPlayed                       int64     `json:"ms_played"`
	ConnCountry                    string    `json:"conn_country"`
	IPAddr                         string    `json:"ip_addr"`
	IPAddrDecrypted                string    `json:"ip_addr_decrypted"`
	TrackName                      string    `json:"master_metadata_track_name"`
	TrackArtist                    string    `json:"master_metadata_album_artist_name"`
	TrackAlbum                     string    `json:"master_metadata_album_album_name"`
	SpotifyTrackURI                string    `json:"spotify_track_uri"`
	EpisodeName                    string    `json:"episode_name"`
	EpisodeShowName                string    `json:"episode_show_name"`
	SpotifyEpisodeURI              string    `json:"spotify_episode_uri"`
	AudiobookTitle                 string    `json:"audiobook_title"`
	AudiobookURI                   string    `json:"audiobook_uri"`
	AudiobookChapterURI            string    `json:"audiobook_chapter_uri"`
	AudiobookChapterTitle          string    `json:"audiobook_chapter_title"`
	ReasonStart                    string    `json:"reason_start"`
	ReasonEnd                      string    `json:"reason_end"`
	Shuffle                        bool      `json:"shuffle"`
	Skipped                        bool      `json:"skipped"`
	Offline                        bool      `json:"offline"`
	OfflineTimestamp               *int64    `json:"offline_timestamp"`
	IncognitoMode                  bool      `json:"incognito_mode"`
	IncognitoModeDeprecatedVersion bool      `json:"incognito_mode_deprecated_version"`
}

func (FileImporter) Recognize(_ context.Context, dirEntry timeline.DirEntry, _ timeline.RecognizeParams) (timeline.Recognition, error) {
	if !dirEntry.IsDir() {
		if isSpotifyStreamingHistoryFile(dirEntry.Name()) {
			return timeline.Recognition{Confidence: 1}, nil
		}
		return timeline.Recognition{}, nil
	}

	if hasSpotifyHistoryFile(dirEntry.FS, dirEntry.Filename) {
		return timeline.Recognition{Confidence: .95}, nil
	}

	return timeline.Recognition{}, nil
}

func (FileImporter) FileImport(ctx context.Context, dirEntry timeline.DirEntry, params timeline.ImportParams) error {
	files, err := listStreamingHistoryFiles(dirEntry.FS, dirEntry)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("no Spotify streaming history files found in input")
	}

	for _, filePath := range files {
		if err := importStreamingHistoryFile(ctx, dirEntry.FS, filePath, params); err != nil {
			return fmt.Errorf("importing %s: %w", filePath, err)
		}
	}

	return nil
}

func listStreamingHistoryFiles(fsys fs.FS, dirEntry timeline.DirEntry) ([]string, error) {
	if !dirEntry.IsDir() {
		if isSpotifyStreamingHistoryFile(dirEntry.Name()) {
			return []string{dirEntry.Filename}, nil
		}
		return nil, nil
	}

	var matches []string
	err := fs.WalkDir(fsys, dirEntry.Filename, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if isSpotifyStreamingHistoryFile(d.Name()) {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return matches, nil
}

func hasSpotifyHistoryFile(fsys fs.FS, root string) bool {
	var found bool
	err := fs.WalkDir(fsys, root, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if isSpotifyStreamingHistoryFile(d.Name()) {
			found = true
			return fs.SkipAll
		}
		return nil
	})

	if errors.Is(err, fs.SkipAll) {
		return true
	}

	return err == nil && found
}

func importStreamingHistoryFile(ctx context.Context, fsys fs.FS, filePath string, params timeline.ImportParams) error {
	f, err := fsys.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	dec := json.NewDecoder(f)

	firstToken, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := firstToken.(json.Delim)
	if !ok || delim != '[' {
		return fmt.Errorf("expected a JSON array")
	}

	index := 0
	for dec.More() {
		if err := ctx.Err(); err != nil {
			return err
		}

		var row streamingHistoryRecord
		if err := dec.Decode(&row); err != nil {
			return err
		}

		item, ok := makeSpotifyItem(row, filePath, index)
		index++
		if !ok {
			continue
		}

		params.Pipeline <- &timeline.Graph{Item: item}
	}

	endToken, err := dec.Token()
	if err != nil {
		return err
	}
	endDelim, ok := endToken.(json.Delim)
	if !ok || endDelim != ']' {
		return fmt.Errorf("expected end of JSON array")
	}

	return nil
}

func makeSpotifyItem(row streamingHistoryRecord, filePath string, index int) (*timeline.Item, bool) {
	if row.TS.IsZero() {
		return nil, false
	}

	title, itemType := inferTitleAndType(row)
	if title == "" {
		title = "Spotify playback"
	}

	meta := timeline.Metadata{
		"Source":                "Spotify",
		"Type":                  itemType,
		"Platform":              row.Platform,
		"Country":               row.ConnCountry,
		"Duration Milliseconds": row.MSPlayed,
		"Reason Start":          row.ReasonStart,
		"Reason End":            row.ReasonEnd,
		"Shuffled":              row.Shuffle,
		"Skipped":               row.Skipped,
		"Offline":               row.Offline,
		"Private Session":       row.IncognitoMode,
	}

	setMetaIfNotEmpty(meta, "Track", row.TrackName)
	setMetaIfNotEmpty(meta, "Artist", row.TrackArtist)
	setMetaIfNotEmpty(meta, "Album", row.TrackAlbum)
	setMetaIfNotEmpty(meta, "Track URI", row.SpotifyTrackURI)
	setMetaIfNotEmpty(meta, "Episode", row.EpisodeName)
	setMetaIfNotEmpty(meta, "Episode Show", row.EpisodeShowName)
	setMetaIfNotEmpty(meta, "Episode URI", row.SpotifyEpisodeURI)
	setMetaIfNotEmpty(meta, "Audiobook", row.AudiobookTitle)
	setMetaIfNotEmpty(meta, "Audiobook URI", row.AudiobookURI)
	setMetaIfNotEmpty(meta, "Audiobook Chapter", row.AudiobookChapterTitle)
	setMetaIfNotEmpty(meta, "Audiobook Chapter URI", row.AudiobookChapterURI)
	setMetaIfNotEmpty(meta, "IP Address", preferredIP(row))

	item := &timeline.Item{
		ID:                   fmt.Sprintf("%s#%d", filePath, index),
		Classification:       timeline.ClassMedia,
		Timestamp:            row.TS,
		IntermediateLocation: filePath,
		Content: timeline.ItemData{
			Data: timeline.StringData(title),
		},
		Metadata: meta,
	}

	if row.MSPlayed > 0 {
		item.Timespan = row.TS.Add(time.Duration(row.MSPlayed) * time.Millisecond)
	}

	return item, true
}

func setMetaIfNotEmpty(meta timeline.Metadata, key, value string) {
	if strings.TrimSpace(value) != "" {
		meta[key] = value
	}
}

func preferredIP(row streamingHistoryRecord) string {
	if strings.TrimSpace(row.IPAddrDecrypted) != "" {
		return row.IPAddrDecrypted
	}
	return row.IPAddr
}

func inferTitleAndType(row streamingHistoryRecord) (title string, kind string) {
	if row.TrackName != "" {
		if row.TrackArtist != "" {
			return row.TrackArtist + " - " + row.TrackName, "track"
		}
		return row.TrackName, "track"
	}

	if row.EpisodeName != "" {
		if row.EpisodeShowName != "" {
			return row.EpisodeShowName + " - " + row.EpisodeName, "episode"
		}
		return row.EpisodeName, "episode"
	}

	if row.AudiobookChapterTitle != "" {
		if row.AudiobookTitle != "" {
			return row.AudiobookTitle + " - " + row.AudiobookChapterTitle, "audiobook_chapter"
		}
		return row.AudiobookChapterTitle, "audiobook_chapter"
	}

	if row.AudiobookTitle != "" {
		return row.AudiobookTitle, "audiobook"
	}

	return "", "unknown"
}

func isSpotifyStreamingHistoryFile(name string) bool {
	lower := strings.ToLower(filepath.Base(name))
	if !strings.HasSuffix(lower, ".json") {
		return false
	}

	return strings.HasPrefix(lower, "streaming_history_audio_") ||
		strings.HasPrefix(lower, "streaminghistory_music_") ||
		strings.HasPrefix(lower, "streaminghistory_podcast_") ||
		strings.HasPrefix(lower, "streaminghistory_audiobook_")
}

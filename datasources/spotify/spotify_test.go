package spotify

import (
	"context"
	"io"
	"io/fs"
	"os"
	"testing"

	"github.com/timelinize/timelinize/timeline"
)

func TestRecognize(t *testing.T) {
	imp := new(FileImporter)

	rec, err := imp.Recognize(context.Background(), timeline.DirEntry{
		DirEntry: testDirEntry{name: "Streaming_History_Audio_2013-2016_0.json"},
	}, timeline.RecognizeParams{})
	if err != nil {
		t.Fatalf("recognize failed: %v", err)
	}
	if rec.Confidence != 1 {
		t.Fatalf("unexpected confidence: %v", rec.Confidence)
	}

	rec, err = imp.Recognize(context.Background(), timeline.DirEntry{
		DirEntry: testDirEntry{name: "not-spotify.json"},
	}, timeline.RecognizeParams{})
	if err != nil {
		t.Fatalf("recognize failed: %v", err)
	}
	if rec.Confidence != 0 {
		t.Fatalf("unexpected confidence for non-spotify file: %v", rec.Confidence)
	}
}

func TestFileImport(t *testing.T) {
	ctx := context.Background()
	pipeline := make(chan *timeline.Graph, 10)

	dirEntry := timeline.DirEntry{
		DirEntry: testDirEntry{name: "Streaming_History_Audio_2013-2016_0.json"},
		FS:       os.DirFS("testdata/fixtures"),
		Filename: "Streaming_History_Audio_2013-2016_0.json",
	}

	err := new(FileImporter).FileImport(ctx, dirEntry, timeline.ImportParams{Pipeline: pipeline})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	close(pipeline)

	var graphs []*timeline.Graph
	for g := range pipeline {
		graphs = append(graphs, g)
	}

	if len(graphs) != 2 {
		t.Fatalf("expected 2 imported items, got %d", len(graphs))
	}

	first := graphs[0].Item
	if first.Classification.Name != timeline.ClassMedia.Name {
		t.Fatalf("expected media classification, got %+v", first.Classification)
	}

	if got := first.Metadata["Country"]; got != "DK" {
		t.Fatalf("expected country DK, got %v", got)
	}

	content, err := itemContentString(first)
	if err != nil {
		t.Fatalf("reading content: %v", err)
	}
	if content != "Stephen Merriman - Lament" {
		t.Fatalf("unexpected content: %s", content)
	}
}

func itemContentString(item *timeline.Item) (string, error) {
	rc, err := item.Content.Data(context.Background())
	if err != nil {
		return "", err
	}
	defer rc.Close()

	b, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}

	return string(b), nil
}

type testDirEntry struct {
	name string
}

func (t testDirEntry) Name() string               { return t.name }
func (t testDirEntry) IsDir() bool                { return false }
func (t testDirEntry) Type() fs.FileMode          { return 0 }
func (t testDirEntry) Info() (fs.FileInfo, error) { return nil, nil }

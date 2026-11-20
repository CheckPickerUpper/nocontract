package running

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/CheckPickerUpper/outcome"
)

func TestPublishingRestoresAnUpdatedAndADeletedOutputAfterARenameFails(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	require := func(operation string, failure error) {
		t.Helper()
		outcome.ForEachOption(outcome.EmptyableToOption(failure), func(cause error) { t.Fatalf("%s: %v", operation, cause) })
	}
	update := goFileAt(filepath.Join(directory, "new.go"), []byte("new contents"))
	remove := generatedFile{path: filepath.Join(directory, "stale.go"), content: outcome.None[[]byte](), owner: update.owner}
	added := goFileAt(filepath.Join(directory, "added.go"), []byte("added contents"))
	blocked := goFileAt(filepath.Join(directory, "blocked.go"), []byte("blocked contents"))
	original := fileContents{bytes: []byte("previous contents"), mode: 0o640}
	for _, path := range []string{update.path, remove.path} {
		require("writing "+path, os.WriteFile(path, original.bytes, original.mode))
	}

	result := prepareOutputs([]fileOnDisk{
		{file: update, held: outcome.Some(original)},
		{file: remove, held: outcome.Some(original)},
		{file: added, held: outcome.None[fileContents]()},
		{file: blocked, held: outcome.None[fileContents]()},
	}, nil, func(changes []preparedOutput) Outcome {
		require("occupying "+blocked.path, os.Mkdir(blocked.path, 0o755))
		return publishOutputs(changes, 0, []string{})
	})

	failure, wasBlocked := result.(FileNotWritten)
	if !wasBlocked || failure.Path != blocked.path {
		t.Fatalf("publishing answered %v; want %s refused", result.Lines(), blocked.path)
	}
	for _, path := range []string{update.path, remove.path} {
		observed, readError := os.ReadFile(path)
		require("reading "+path, readError)
		if string(observed) != string(original.bytes) {
			t.Errorf("%s holds %q after the failed publish; want %q", path, observed, original.bytes)
		}
	}
	if _, err := os.Stat(added.path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("unexpected result reading %s: %v", added.path, err)
	}
}

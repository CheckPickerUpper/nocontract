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
	update := filepath.Join(directory, "new.go")
	remove := filepath.Join(directory, "stale.go")
	added := filepath.Join(directory, "added.go")
	blocked := filepath.Join(directory, "blocked.go")
	original := fileContents{bytes: []byte("previous contents"), mode: 0o640}
	for _, path := range []string{update, remove} {
		require("writing "+path, os.WriteFile(path, original.bytes, original.mode))
	}

	result := prepareOutputs([]fileOnDisk{
		{path: update, content: outcome.Some([]byte("new contents")), held: outcome.Some(original)},
		{path: remove, content: outcome.None[[]byte](), held: outcome.Some(original)},
		{path: added, content: outcome.Some([]byte("added contents")), held: outcome.None[fileContents]()},
		{path: blocked, content: outcome.Some([]byte("blocked contents")), held: outcome.None[fileContents]()},
	}, nil, func(changes []preparedOutput) Outcome {
		require("occupying "+blocked, os.Mkdir(blocked, 0o755))
		return publishOutputs(changes, 0, []string{})
	})

	failure, wasBlocked := result.(FileNotWritten)
	if !wasBlocked || failure.Path != blocked {
		t.Fatalf("publishing answered %v; want %s refused", result.Lines(), blocked)
	}
	for _, path := range []string{update, remove} {
		observed, readError := os.ReadFile(path)
		require("reading "+path, readError)
		if string(observed) != string(original.bytes) {
			t.Errorf("%s holds %q after the failed publish; want %q", path, observed, original.bytes)
		}
	}
	if _, err := os.Stat(added); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("unexpected result reading %s: %v", added, err)
	}
}

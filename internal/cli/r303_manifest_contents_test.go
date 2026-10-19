package cli

import (
	"reflect"
	"testing"
)

// The manifest lists only the parts the archive holds: no config/ unless a
// file under it was archived.
func TestArchivedPartsListsOnlyWhatWasArchived(t *testing.T) {
	bm := &BackupManager{hashes: []fileHash{
		{Path: "messages/a/b"}, {Path: "database/x.db"}, {Path: "messages/c"},
	}}
	if got, want := bm.archivedParts(), []string{"database/", "messages/"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("archivedParts = %v, want %v", got, want)
	}
	bm.hashes = append(bm.hashes, fileHash{Path: "config/umailserver.yaml"})
	if got, want := bm.archivedParts(), []string{"config/", "database/", "messages/"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("archivedParts with config = %v, want %v", got, want)
	}
	if got := (&BackupManager{}).archivedParts(); got == nil || len(got) != 0 {
		t.Fatalf("empty archive = %#v, want empty non-nil slice", got)
	}
}

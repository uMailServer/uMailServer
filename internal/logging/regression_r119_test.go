package logging

import (
	"os"
	"testing"
)

// F6018: closing the writer for stdout/stderr must not close the process's
// standard streams.
func TestRegressionF6018StdStreamsNotClosable(t *testing.T) {
	for _, out := range []string{"stdout", "", "STDERR"} {
		w, err := GetLogWriter(out, 1, 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		if f, ok := w.(*os.File); ok {
			t.Errorf("GetLogWriter(%q) returns raw %v; Close() would close it", out, f.Name())
		}
		if _, err := w.Write(nil); err != nil {
			t.Errorf("write: %v", err)
		}
	}
}

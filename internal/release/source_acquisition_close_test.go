package release

import (
	"errors"
	"io"
	"os"
	"testing"
)

// The real file is closed once; only the additional close-error report is
// synthetic because ordinary local files do not reliably fail native Close.
type sourceOutputCloseFailure struct {
	io.Closer
	cause error
}

func (f sourceOutputCloseFailure) Close() error {
	return errors.Join(f.Closer.Close(), f.cause)
}

// A copy-handle close failure must survive successful quarantine deletion and
// prevent source receipt settlement. Both output kinds use this owner boundary.
func TestSourceAcquisition_shouldKeepUnknown_whenCopyCloseFailed(t *testing.T) {
	for _, name := range []string{"manifest.json", "candidate"} {
		t.Run(name, func(t *testing.T) {
			snapshot, err := newSourceSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			file, err := snapshot.create(name)
			if err != nil {
				t.Fatal(err)
			}
			cause := errors.New("output close uncertain")
			if err := snapshot.closeFile(sourceOutputCloseFailure{Closer: file, cause: cause}); !errors.Is(err, cause) {
				t.Fatal("close cause lost", err)
			}
			cleanup := snapshot.cleanup()
			if _, err := os.Stat(snapshot.path); !os.IsNotExist(err) {
				t.Fatalf("fixture should finish real quarantine removal: %v", err)
			}
			r := &resourceTrace{}
			obligation := ResourceObligation{ID: "source-close-failure", Kind: resourceKindSource}
			if err := r.RecordIntent(obligation); err != nil {
				t.Fatal(err)
			}
			err = resourceCleanup(r, obligation, cleanup)
			if !errors.Is(err, cause) || !errors.Is(err, ErrCleanupUncertain) || len(r.closed) != 0 || len(r.unknown) != 1 {
				t.Fatalf("local close uncertainty disappeared: err=%v closed=%d unknown=%d", err, len(r.closed), len(r.unknown))
			}
		})
	}
}

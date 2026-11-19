package runtimeowner

import "testing"

// A receipt can be terminal before the worker finishes releasing its resources.
// A head must continue read-only observation until the lifecycle writer unlocks.
func TestTerminalReceiptIsNotSettledWhileWriterStillOwnsLock(t *testing.T) {
	j := journal(t)
	tx, err := j.Admit(request("1"))
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Close()
	if err := tx.Finish(Rejected); err != nil {
		t.Fatal(err)
	}
	if _, settled, err := j.QuerySettled(request("1").ID); err != nil || settled {
		t.Fatal(settled, err)
	}
	if err := tx.Close(); err != nil {
		t.Fatal(err)
	}
	if r, settled, err := j.QuerySettled(request("1").ID); err != nil || !settled || r.Outcome != Rejected {
		t.Fatal(r, settled, err)
	}
}

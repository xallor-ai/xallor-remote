package release

import "testing"

// A nil callback is not release evidence; a different attempt cannot confirm it.
func TestPreparedStopRequiresBoundCleanup(t *testing.T) {
	i := StopIntent{Mode: "no_peer", AttemptID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", AbsenceBasis: "fresh"}
	if !i.Valid() {
		t.Fatal("valid absence intent rejected")
	}
	o := StopObservation{AttemptID: i.AttemptID, Dispatch: "not_sent", Process: "not_observed", RuntimeResource: "released_under_lock", Jobs: "empty", Observers: "released", Result: "quiescent_no_peer"}
	if !o.Confirms(i) {
		t.Fatal("complete absence evidence rejected")
	}
	o.Observers = "unknown"
	if o.Confirms(i) {
		t.Fatal("incomplete cleanup confirmed")
	}
	o.Observers = "released"
	o.AttemptID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if o.Confirms(i) {
		t.Fatal("wrong attempt confirmed")
	}
}

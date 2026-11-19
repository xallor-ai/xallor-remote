package runtimeowner

import (
	"strings"
	"testing"

	"github.com/xallor-ai/xallor-remote/internal/release"
)

func r1EvidenceFor(t *testing.T, r Record) R1Evidence {
	t.Helper()
	commit := r.Effects[len(r.Effects)-1]
	target := commit.Target
	selection := *commit.Observed
	return R1Evidence{
		NamespaceSHA256:  r.NamespaceSHA256,
		RecordSHA256:     r1RecordDigest(r),
		AdmissionHeld:    true,
		WorkerActive:     false,
		ReconcilerActive: false,
		Current: R1CurrentEvidence{
			OwnerAuthenticated:   true,
			PeerPID:              321,
			ActivationID:         target.ActivationID,
			LaunchID:             target.LaunchID,
			LoadedImageSHA256:    target.SHA256,
			SourceManifestSHA256: r.Binding.ManifestSHA256,
			Selection:            selection,
			ReadyStable:          true,
		},
		Resources: []ResourceEvidence{
			{Kind: "jobs", State: "released"},
			{Kind: "runtime", State: "released"},
			{Kind: "observers", State: "released"},
			{Kind: "source", State: "released"},
			{Kind: "probe", State: "released"},
		},
	}
}

// A complete owner-only observation accepts a lost historical ready acknowledgement
// only when commit is requested and every identity/resource proof is present.
func TestClassifyR1_shouldAcceptCompleteRequestedPresentEvidence(t *testing.T) {
	r := lifecycleFixture(t, "", false)
	r.Outcome = Uncertain
	r.Effects = r.Effects[:len(r.Effects)-1]
	e := r1EvidenceFor(t, r)
	if blockers := ClassifyR1(r, e); len(blockers) != 0 {
		t.Fatalf("complete R1 evidence refused: %+v", blockers)
	}
}

// A current ready baseline cannot prove that the requested candidate was applied.
func TestClassifyR1_shouldRefuseBaselineRestoredPurpose(t *testing.T) {
	r := lifecycleFixture(t, strings.Repeat("a", 64), true)
	e := r1EvidenceFor(t, r)
	if blockers := ClassifyR1(r, e); len(blockers) == 0 {
		t.Fatal("baseline restoration was accepted as requested success")
	}
}

func TestClassifyR1_shouldRefuseReadyOnlyEvidence(t *testing.T) {
	r := lifecycleFixture(t, "", false)
	r.Outcome = Uncertain
	r.Effects = r.Effects[len(r.Effects)-1:]
	if blockers := ClassifyR1(r, r1EvidenceFor(t, r)); len(blockers) == 0 {
		t.Fatal("ready-only evidence was accepted")
	}
}

func TestClassifyR1_shouldRefuseMalformedOrMismatchedEvidence(t *testing.T) {
	r := lifecycleFixture(t, "", false)
	r.Outcome = Uncertain
	r.Effects = r.Effects[:len(r.Effects)-1]
	cases := []struct {
		name   string
		mutate func(*R1Evidence)
	}{
		{"wrong peer", func(e *R1Evidence) { e.Current.PeerPID = 0 }},
		{"wrong namespace", func(e *R1Evidence) { e.NamespaceSHA256 = strings.Repeat("e", 64) }},
		{"stale digest", func(e *R1Evidence) { e.RecordSHA256 = strings.Repeat("e", 64) }},
		{"different selection digest", func(e *R1Evidence) { e.Current.Selection.StateSHA256 = strings.Repeat("e", 64) }},
		{"wrong activation", func(e *R1Evidence) { e.Current.ActivationID = strings.Repeat("e", 32) }},
		{"wrong image", func(e *R1Evidence) { e.Current.LoadedImageSHA256 = strings.Repeat("e", 64) }},
		{"wrong launch", func(e *R1Evidence) { e.Current.LaunchID = strings.Repeat("f", 32) }},
		{"wrong source", func(e *R1Evidence) { e.Current.SourceManifestSHA256 = strings.Repeat("e", 64) }},
		{"unstable ready", func(e *R1Evidence) { e.Current.ReadyStable = false }},
		{"active worker", func(e *R1Evidence) { e.WorkerActive = true }},
		{"active reconciler", func(e *R1Evidence) { e.ReconcilerActive = true }},
		{"unknown debt", func(e *R1Evidence) { e.Resources[4].State = "unknown" }},
		{"missing debt", func(e *R1Evidence) { e.Resources = e.Resources[:4] }},
		{"unauthenticated owner", func(e *R1Evidence) { e.Current.OwnerAuthenticated = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := r1EvidenceFor(t, r)
			tc.mutate(&e)
			if blockers := ClassifyR1(r, e); len(blockers) == 0 {
				t.Fatalf("invalid evidence accepted: %+v", e)
			}
		})
	}
}

var _ = release.SelectionEvidence{}

// Requested-applied resolution metadata must retain the lost ready ACK and
// identify the owner evidence evaluator before any future writer can use it.
func TestResolution_shouldRequireR1UncertaintyMetadata(t *testing.T) {
	r := Resolution{
		OriginalOperationID:         strings.Repeat("1", 32),
		OriginalNamespaceSHA:        strings.Repeat("2", 64),
		OriginalRecordSHA256:        strings.Repeat("3", 64),
		OriginalRevision:            1,
		EvaluatorCapability:         R1EvaluatorCapability(),
		Disposition:                 DispositionResolved,
		RequestResult:               RequestRequestedApplied,
		ResourceState:               ResourceSettled,
		HistoricalReadyAckUncertain: true,
		EvidenceEvaluator:           R1EvaluatorCapability(),
	}
	if !r.valid() {
		t.Fatal("complete R1 resolution metadata rejected")
	}
	r.HistoricalReadyAckUncertain = false
	if r.valid() {
		t.Fatal("requested-applied resolution without historical ACK uncertainty accepted")
	}
}

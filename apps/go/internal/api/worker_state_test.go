package api

import (
	"testing"
	"time"

	"pipelogiq/internal/types"
)

// A worker that has exited, or one that stopped saying anything, must not be
// counted as capacity: the schedule gate and the dashboard both read this.
func TestWorkerCountsAsOnline(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{
		types.WorkerStateReady:    true,
		types.WorkerStateStarting: true,
		types.WorkerStateDegraded: true,
		types.WorkerStateDraining: true,
		types.WorkerStateError:    true,
		types.WorkerStateStopped:  false,
		types.WorkerStateOffline:  false,
	}
	for state, want := range cases {
		if got := workerCountsAsOnline(state); got != want {
			t.Errorf("workerCountsAsOnline(%q) = %v, want %v", state, got, want)
		}
	}
}

func TestResolveEffectiveWorkerState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	offlineAfter := 45 * time.Second

	fresh := now.Add(-10 * time.Second).Format(time.RFC3339)
	stale := now.Add(-10 * time.Minute).Format(time.RFC3339)

	cases := []struct {
		name  string
		state string
		seen  string
		want  string
	}{
		{"a live worker keeps its state", types.WorkerStateReady, fresh, types.WorkerStateReady},
		{"a silent worker goes offline", types.WorkerStateReady, stale, types.WorkerStateOffline},
		{"stopped stays stopped even when fresh", types.WorkerStateStopped, fresh, types.WorkerStateStopped},
		{"an unparsable timestamp falls back to the stored state", types.WorkerStateReady, "not-a-time", types.WorkerStateReady},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveEffectiveWorkerState(
				types.WorkerStatusResponse{State: tc.state, LastSeenAt: tc.seen}, now, offlineAfter)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

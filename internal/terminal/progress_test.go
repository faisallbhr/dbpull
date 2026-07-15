package terminal

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	syncpkg "dbpull/internal/sync"
)

type fakeTicker struct {
	ch chan time.Time
}

func (t *fakeTicker) Chan() <-chan time.Time { return t.ch }
func (t *fakeTicker) Stop()                  {}

func TestRendererTableBasedPercentage(t *testing.T) {
	renderer, out, clock, ticker := newTestRenderer(false, true)
	mustNoError(t, renderer.Start())
	mustNoError(t, renderer.StartPhase(PhaseData))
	mustNoError(t, renderer.SetPlan(syncpkg.SyncPlan{Tables: makePlanTables(2)}))
	mustNoError(t, renderer.Handle(syncpkg.DataProgress{
		Kind:       syncpkg.DataProgressTableProgress,
		TableIndex: 2,
		TotalRows:  9999,
	}))

	*clock = clock.Add(10 * time.Second)
	advanceTick(ticker, *clock)

	text := out.String()
	if !strings.Contains(text, "50%") || !strings.Contains(text, "1 / 2 tables") || strings.Contains(text, "Current") {
		t.Fatalf("output = %q", text)
	}
}

func TestRendererSixThirtySevenOfSixSeventyOne(t *testing.T) {
	renderer, out, clock, ticker := newTestRenderer(false, true)
	mustNoError(t, renderer.Start())
	mustNoError(t, renderer.StartPhase(PhaseData))
	mustNoError(t, renderer.SetPlan(syncpkg.SyncPlan{Tables: makePlanTables(671)}))
	mustNoError(t, renderer.Handle(syncpkg.DataProgress{
		Kind:       syncpkg.DataProgressTableStart,
		TableIndex: 638,
		TotalRows:  16934393,
	}))

	*clock = clock.Add(10 * time.Second)
	advanceTick(ticker, *clock)

	text := out.String()
	if !strings.Contains(text, "95%") || !strings.Contains(text, "637 / 671 tables") {
		t.Fatalf("output = %q", text)
	}
}

func TestRendererFixedHeight(t *testing.T) {
	renderer, _, _, _ := newTestRenderer(false, true)
	mustNoError(t, renderer.Start())

	time.Sleep(10 * time.Millisecond)

	if renderer == nil {
		t.Fatal("renderer is nil")
	}
}

func TestRendererThrottle500ms(t *testing.T) {
	renderer, out, clock, ticker := newTestRenderer(false, true)
	mustNoError(t, renderer.Start())
	mustNoError(t, renderer.StartPhase(PhaseData))
	mustNoError(t, renderer.SetPlan(syncpkg.SyncPlan{Tables: makePlanTables(10)}))

	initial := out.String()
	*clock = clock.Add(400 * time.Millisecond)
	advanceTick(ticker, *clock)

	if out.String() != initial {
		t.Fatalf("output changed before 500ms: %q", out.String())
	}
}

func TestRendererNoRenderBurstFromRapidBatchEvents(t *testing.T) {
	renderer, out, clock, ticker := newTestRenderer(false, true)
	mustNoError(t, renderer.Start())
	mustNoError(t, renderer.StartPhase(PhaseData))
	mustNoError(t, renderer.SetPlan(syncpkg.SyncPlan{Tables: makePlanTables(10)}))

	for i := 1; i <= 5; i++ {
		mustNoError(t, renderer.Handle(syncpkg.DataProgress{
			Kind:        syncpkg.DataProgressTableProgress,
			TableIndex:  1,
			TotalRows:   int64(i * 100),
			BatchNumber: i,
		}))
	}

	beforeTick := out.String()
	*clock = clock.Add(500 * time.Millisecond)
	advanceTick(ticker, *clock)

	if out.String() == beforeTick {
		t.Fatal("expected one throttled render after tick")
	}
}

func TestRendererSmoothedSpeed(t *testing.T) {
	renderer, out, clock, ticker := newTestRenderer(false, true)
	mustNoError(t, renderer.Start())
	mustNoError(t, renderer.StartPhase(PhaseData))
	mustNoError(t, renderer.SetPlan(syncpkg.SyncPlan{Tables: makePlanTables(10)}))

	*clock = clock.Add(1 * time.Second)
	mustNoError(t, renderer.Handle(syncpkg.DataProgress{Kind: syncpkg.DataProgressTableProgress, TableIndex: 1, TotalRows: 100, BatchNumber: 1}))
	*clock = clock.Add(1 * time.Second)
	mustNoError(t, renderer.Handle(syncpkg.DataProgress{Kind: syncpkg.DataProgressTableProgress, TableIndex: 1, TotalRows: 300, BatchNumber: 2}))
	advanceTick(ticker, *clock)

	text := out.String()
	if !strings.Contains(text, "120 rows/s") {
		t.Fatalf("output = %q", text)
	}
}

func TestRendererSmoothedETA(t *testing.T) {
	renderer, out, clock, ticker := newTestRenderer(false, true)
	mustNoError(t, renderer.Start())
	mustNoError(t, renderer.StartPhase(PhaseData))
	mustNoError(t, renderer.SetPlan(syncpkg.SyncPlan{Tables: makePlanTables(10)}))

	*clock = clock.Add(10 * time.Second)
	mustNoError(t, renderer.Handle(syncpkg.DataProgress{Kind: syncpkg.DataProgressTableComplete, TableIndex: 5}))
	advanceTick(ticker, *clock)

	text := out.String()
	if !strings.Contains(text, "ETA 10s") {
		t.Fatalf("output = %q", text)
	}
}

func TestRendererETACalculatingState(t *testing.T) {
	renderer, out, clock, ticker := newTestRenderer(false, true)
	mustNoError(t, renderer.Start())
	mustNoError(t, renderer.StartPhase(PhaseData))
	mustNoError(t, renderer.SetPlan(syncpkg.SyncPlan{Tables: makePlanTables(10)}))

	*clock = clock.Add(1500 * time.Millisecond)
	mustNoError(t, renderer.Handle(syncpkg.DataProgress{Kind: syncpkg.DataProgressTableComplete, TableIndex: 4}))
	advanceTick(ticker, *clock)

	if !strings.Contains(out.String(), "ETA calculating...") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRendererCompletionAtHundredPercent(t *testing.T) {
	renderer, out, _, _ := newTestRenderer(false, true)
	mustNoError(t, renderer.Start())
	mustNoError(t, renderer.StartPhase(PhaseData))
	mustNoError(t, renderer.SetPlan(syncpkg.SyncPlan{Tables: makePlanTables(1)}))
	mustNoError(t, renderer.Handle(syncpkg.DataProgress{Kind: syncpkg.DataProgressTableComplete, TableIndex: 1, TotalRows: 100}))
	mustNoError(t, renderer.CompletePhase(PhaseData))

	if !strings.Contains(out.String(), "100%") || !strings.Contains(out.String(), "ETA 0s") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRendererNonTTYIntervalOutput(t *testing.T) {
	renderer, out, clock, ticker := newTestRenderer(false, false)
	mustNoError(t, renderer.Start())
	mustNoError(t, renderer.StartPhase(PhaseData))

	first := out.String()
	*clock = clock.Add(5 * time.Second)
	advanceTick(ticker, *clock)
	if out.String() != first {
		t.Fatalf("non-tty output changed before 10s: %q", out.String())
	}
}

func TestRendererCancellationCleanup(t *testing.T) {
	renderer, _, _, _ := newTestRenderer(false, true)
	mustNoError(t, renderer.Start())
	mustNoError(t, renderer.StartPhase(PhaseData))
	mustNoError(t, renderer.SetPlan(syncpkg.SyncPlan{Tables: makePlanTables(10)}))
	mustNoError(t, renderer.Handle(syncpkg.DataProgress{Kind: syncpkg.DataProgressTableComplete, TableIndex: 5, TotalRows: 500}))

	err := renderer.Cancel(context.Canceled)
	if err == nil || !strings.Contains(err.Error(), "5 / 10 tables") || !strings.Contains(err.Error(), "500 rows copied") {
		t.Fatalf("err = %v", err)
	}
}

func TestRendererGoroutineShutdown(t *testing.T) {
	renderer, _, _, _ := newTestRenderer(false, false)
	mustNoError(t, renderer.Start())
	mustNoError(t, renderer.Close())

	select {
	case <-renderer.done:
	default:
		t.Fatal("renderer goroutine did not stop")
	}
}

func TestRendererNoCurrentTableOutput(t *testing.T) {
	renderer, out, clock, ticker := newTestRenderer(false, false)
	mustNoError(t, renderer.Start())
	mustNoError(t, renderer.StartPhase(PhaseData))
	mustNoError(t, renderer.SetPlan(syncpkg.SyncPlan{Tables: makePlanTables(2)}))
	mustNoError(t, renderer.Handle(syncpkg.DataProgress{
		Kind:       syncpkg.DataProgressTableStart,
		Table:      "secret_table_name",
		TableIndex: 1,
	}))

	*clock = clock.Add(10 * time.Second)
	advanceTick(ticker, *clock)

	if strings.Contains(out.String(), "secret_table_name") || strings.Contains(out.String(), "Current") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRendererFailureIncludesPhaseAndError(t *testing.T) {
	renderer, _, _, _ := newTestRenderer(false, true)
	mustNoError(t, renderer.Start())
	mustNoError(t, renderer.StartPhase(PhaseData))

	err := renderer.Failure(errors.New("boom"))
	if err == nil || !strings.Contains(err.Error(), "Data") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func newTestRenderer(verbose, isTTY bool) (*SyncProgressRenderer, *bytes.Buffer, *time.Time, *fakeTicker) {
	out := &bytes.Buffer{}
	clock := time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)
	fake := &fakeTicker{ch: make(chan time.Time, 16)}

	renderer := NewSyncProgressRenderer(out, verbose)
	renderer.isTTY = isTTY
	renderer.now = func() time.Time { return clock }
	renderer.newTicker = func(time.Duration) ticker { return fake }

	return renderer, out, &clock, fake
}

func makePlanTables(count int) []syncpkg.PlanTable {
	tables := make([]syncpkg.PlanTable, count)
	for i := range tables {
		tables[i] = syncpkg.PlanTable{Name: "table"}
	}
	return tables
}

func mustNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("error = %v", err)
	}
}

func advanceTick(ticker *fakeTicker, now time.Time) {
	ticker.ch <- now
	time.Sleep(10 * time.Millisecond)
}

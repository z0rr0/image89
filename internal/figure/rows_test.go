package figure

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var errBoom = errors.New("boom")

func TestForRowsVisitsEveryRowOnce(t *testing.T) {
	t.Parallel()

	tests := []struct {
		height       int
		rowsPerBlock int
		workers      int
		wantBlock    int // expected length of every block but the last one
		wantWorkers  int // upper bound of the worker index
	}{
		{height: 1, rowsPerBlock: 16, workers: 1, wantBlock: 16, wantWorkers: 1},
		{height: 1, rowsPerBlock: 16, workers: 4, wantBlock: 16, wantWorkers: 1},
		{height: 15, rowsPerBlock: 16, workers: 1, wantBlock: 16, wantWorkers: 1},
		{height: 15, rowsPerBlock: 16, workers: 4, wantBlock: 16, wantWorkers: 1},
		{height: 16, rowsPerBlock: 16, workers: 1, wantBlock: 16, wantWorkers: 1},
		{height: 16, rowsPerBlock: 16, workers: 4, wantBlock: 16, wantWorkers: 1},
		{height: 17, rowsPerBlock: 16, workers: 1, wantBlock: 16, wantWorkers: 1},
		{height: 17, rowsPerBlock: 16, workers: 4, wantBlock: 16, wantWorkers: 2},
		{height: 1000, rowsPerBlock: 16, workers: 1, wantBlock: 16, wantWorkers: 1},
		{height: 1000, rowsPerBlock: 16, workers: 4, wantBlock: 16, wantWorkers: 4},
		{height: 1000, rowsPerBlock: 1, workers: 4, wantBlock: 1, wantWorkers: 4},
		{height: 1000, rowsPerBlock: 0, workers: 4, wantBlock: defaultRowsPerBlock, wantWorkers: 4},
		{height: 1000, rowsPerBlock: -1, workers: 0, wantBlock: defaultRowsPerBlock, wantWorkers: runtime.GOMAXPROCS(0)},
	}

	for _, tt := range tests {
		name := fmt.Sprintf("h%d_rpb%d_w%d", tt.height, tt.rowsPerBlock, tt.workers)
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			visits := make([]atomic.Int32, tt.height)
			busy := make([]atomic.Bool, tt.wantWorkers)
			err := ForRows(t.Context(), tt.height, tt.rowsPerBlock, tt.workers, func(worker, y0, y1 int) error {
				if worker < 0 || worker >= tt.wantWorkers {
					return fmt.Errorf("worker %d out of [0, %d)", worker, tt.wantWorkers)
				}
				if busy[worker].Swap(true) {
					return fmt.Errorf("worker %d runs two blocks at once", worker)
				}
				defer busy[worker].Store(false)
				if y0%tt.wantBlock != 0 || y1 <= y0 || y1 > tt.height ||
					(y1-y0 != tt.wantBlock && y1 != tt.height) {
					return fmt.Errorf("bad block [%d, %d)", y0, y1)
				}
				for y := y0; y < y1; y++ {
					visits[y].Add(1)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("ForRows error: %v", err)
			}
			for y := range visits {
				if n := visits[y].Load(); n != 1 {
					t.Errorf("row %d visited %d times, want 1", y, n)
				}
			}
		})
	}
}

func TestForRowsEmpty(t *testing.T) {
	t.Parallel()

	for _, height := range []int{0, -1} {
		var calls atomic.Int32
		err := ForRows(t.Context(), height, 16, 4, func(_, _, _ int) error {
			calls.Add(1)
			return nil
		})
		if err != nil || calls.Load() != 0 {
			t.Errorf("ForRows(height %d) = %v with %d calls, want nil and no calls", height, err, calls.Load())
		}
	}
}

func TestForRowsError(t *testing.T) {
	t.Parallel()

	t.Run("one worker stops at once", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32
		err := ForRows(t.Context(), 1000, 1, 1, func(_, _, _ int) error {
			calls.Add(1)
			return errBoom
		})
		if !errors.Is(err, errBoom) {
			t.Fatalf("ForRows error = %v, want %v", err, errBoom)
		}
		if n := calls.Load(); n != 1 {
			t.Errorf("fn called %d times, want 1", n)
		}
	})

	t.Run("many workers", func(t *testing.T) {
		t.Parallel()

		err := ForRows(t.Context(), 1000, 1, 4, func(_, y0, _ int) error {
			if y0 == 0 {
				return errBoom
			}
			return nil
		})
		if !errors.Is(err, errBoom) {
			t.Fatalf("ForRows error = %v, want %v", err, errBoom)
		}
	})

	t.Run("fn error wins over cancel", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		err := ForRows(ctx, 1000, 1, 1, func(_, _, _ int) error {
			cancel()
			return errBoom
		})
		if !errors.Is(err, errBoom) {
			t.Fatalf("ForRows error = %v, want %v", err, errBoom)
		}
	})
}

func TestForRowsDoneBeforeStart(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	expired, cancelExpired := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	t.Cleanup(cancelExpired)

	tests := []struct {
		name string
		ctx  context.Context //nolint:containedctx // test case input
		want error
	}{
		{name: "canceled", ctx: canceled, want: context.Canceled},
		{name: "deadline exceeded", ctx: expired, want: context.DeadlineExceeded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var calls atomic.Int32
			err := ForRows(tt.ctx, 1000, 16, 4, func(_, _, _ int) error {
				calls.Add(1)
				return nil
			})
			if !errors.Is(err, tt.want) {
				t.Errorf("ForRows error = %v, want %v", err, tt.want)
			}
			if n := calls.Load(); n != 0 {
				t.Errorf("fn called %d times, want 0", n)
			}
		})
	}
}

// TestForRowsCancelDuringWork holds every worker inside its first block while
// ctx is canceled: none of them may start another block afterwards.
func TestForRowsCancelDuringWork(t *testing.T) {
	t.Parallel()

	const workers = 4
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var calls atomic.Int32
	started := make(chan struct{}, workers)
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- ForRows(ctx, 1000, 1, workers, func(_, _, _ int) error {
			if calls.Add(1) <= workers {
				started <- struct{}{}
				<-release
			}
			return nil
		})
	}()

	for range workers {
		<-started
	}
	cancel()
	close(release)

	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("ForRows error = %v, want %v", err, context.Canceled)
	}
	if n := calls.Load(); n != workers {
		t.Errorf("fn called %d times, want %d: a block started after cancel", n, workers)
	}
}

func TestForRowsPanic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		value     any
		wantIsErr bool
	}{
		{name: "error value", value: errBoom, wantIsErr: true},
		{name: "string value", value: "bad block"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := catchPanic(func() {
				_ = ForRows(t.Context(), 1000, 1, 4, func(_, y0, _ int) error {
					if y0 == 0 {
						panic(tt.value)
					}
					return nil
				})
			})
			err, ok := r.(error)
			if !ok {
				t.Fatalf("recovered %T %v, want an error", r, r)
			}
			msg := err.Error()
			if !strings.Contains(msg, "worker panic") || !strings.Contains(msg, fmt.Sprint(tt.value)) {
				t.Errorf("panic message %q has no worker panic or value %v", msg, tt.value)
			}
			if !strings.Contains(msg, "rows_test.go") {
				t.Errorf("panic message has no worker stack:\n%s", msg)
			}
			if got := errors.Is(err, errBoom); got != tt.wantIsErr {
				t.Errorf("errors.Is(panic, errBoom) = %v, want %v", got, tt.wantIsErr)
			}
		})
	}
}

// catchPanic runs f and returns the value it panics with, or nil.
func catchPanic(f func()) any {
	var r any
	func() {
		defer func() { r = recover() }()
		f()
	}()
	return r
}

func BenchmarkForRows(b *testing.B) {
	b.ReportAllocs()
	ctx := b.Context()
	for b.Loop() {
		if err := ForRows(ctx, 1024, 16, 0, func(_, _, _ int) error { return nil }); err != nil {
			b.Fatal(err)
		}
	}
}

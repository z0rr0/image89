package figure

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// defaultRowsPerBlock is the block height used when ForRows gets rowsPerBlock <= 0.
const defaultRowsPerBlock = 16

// errWorkerExit means fn called runtime.Goexit, so some blocks may be left undone.
var errWorkerExit = errors.New("figure.ForRows: worker exited by runtime.Goexit")

// ForRows splits the rows [0, height) into blocks of rowsPerBlock rows and calls
// fn(worker, y0, y1) for every block [y0, y1) exactly once from a pool of
// goroutines. Blocks are taken in order from a shared counter.
//
// rowsPerBlock <= 0 means 16 and workers <= 0 means runtime.GOMAXPROCS(0) read
// during the call. The pool has n = min(workers, number of blocks) goroutines,
// and the worker index passed to fn is dense in [0, n). One worker calls fn
// sequentially. To keep per-worker state in a slice, normalize workers to a
// positive value once, allocate the slice of that length and pass the same
// value to ForRows: the runtime may change the default GOMAXPROCS between two
// reads, so passing 0 again could start more workers than the slice holds.
//
// ctx is checked before every block. ForRows returns ctx.Err() without calling
// fn if ctx is already done, and nil if height <= 0. The first error returned
// by fn stops the other workers before their next block and is returned; if fn
// never fails, ForRows returns ctx.Err() (nil when all blocks are done with a
// live ctx). ForRows cannot interrupt a running fn: a long fn has to check ctx
// itself, e.g. by a budget of work.
//
// A panic in fn stops the other workers and is raised again in the goroutine
// that called ForRows, so a recover there (an HTTP middleware) handles it; it
// wins over any error. runtime.Goexit in fn stops the other workers as well
// and counts as an error of fn, because some blocks are left undone.
func ForRows(ctx context.Context, height, rowsPerBlock, workers int, fn func(worker, y0, y1 int) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if height <= 0 {
		return nil
	}
	if rowsPerBlock <= 0 {
		rowsPerBlock = defaultRowsPerBlock
	}
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	blocks := 1 + (height-1)/rowsPerBlock // no overflow for any rowsPerBlock
	workers = min(workers, blocks)

	inner, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		next     atomic.Int64
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
		panicked *panicError
	)
	// stop keeps the first error and the first panic; a panic wins over any
	// error because it is a bug that must not be hidden.
	stop := func(err error, p *panicError) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		if panicked == nil {
			panicked = p
		}
		mu.Unlock()
		cancel()
	}

	work := func(worker int) {
		for inner.Err() == nil {
			b := int(next.Add(1) - 1)
			if b >= blocks {
				return
			}
			y0 := b * rowsPerBlock
			if err := fn(worker, y0, y0+min(rowsPerBlock, height-y0)); err != nil {
				stop(err, nil)
				return
			}
		}
	}

	for worker := range workers {
		wg.Go(func() {
			finished := false
			defer func() {
				if finished {
					return
				}
				// fn panicked or called runtime.Goexit, where recover returns nil.
				if p := recover(); p != nil {
					stop(nil, &panicError{value: p, stack: debug.Stack()})
				} else {
					stop(errWorkerExit, nil)
				}
			}()
			work(worker)
			finished = true
		})
	}
	wg.Wait()

	if panicked != nil {
		panic(panicked)
	}
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err()
}

// panicError carries a panic of fn from a ForRows worker to its caller,
// together with the stack of the worker where it happened.
type panicError struct {
	value any
	stack []byte
}

// Error makes the re-raised panic readable in logs and keeps the original value
// reachable through Unwrap when it is an error.
func (p *panicError) Error() string {
	return fmt.Sprintf("figure.ForRows worker panic: %v\n%s", p.value, p.stack)
}

// Unwrap returns the original panic value if it is an error.
func (p *panicError) Unwrap() error {
	if err, ok := p.value.(error); ok {
		return err
	}
	return nil
}

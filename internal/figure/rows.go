package figure

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// defaultRowsPerBlock is the block height used when ForRows gets rowsPerBlock <= 0.
const defaultRowsPerBlock = 16

// ForRows splits the rows [0, height) into blocks of rowsPerBlock rows and calls
// fn(worker, y0, y1) for every block [y0, y1) exactly once from a pool of
// goroutines. Blocks are taken in order from a shared counter.
//
// rowsPerBlock <= 0 means 16 and workers <= 0 means runtime.GOMAXPROCS(0). The
// pool has n = min(workers, number of blocks) goroutines, and the worker index
// passed to fn is dense in [0, n), so fn may keep per-worker state in a slice
// of length workers. One worker calls fn sequentially.
//
// ctx is checked before every block. ForRows returns ctx.Err() without calling
// fn if ctx is already done, and nil if height <= 0. The first error returned
// by fn stops the other workers before their next block and is returned; if fn
// never fails, ForRows returns ctx.Err() (nil when all blocks are done with a
// live ctx). ForRows cannot interrupt a running fn: a long fn has to check ctx
// itself, e.g. by a budget of work.
//
// A panic in fn stops the other workers and is raised again in the goroutine
// that called ForRows, so a recover there (an HTTP middleware) handles it.
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
	blocks := (height + rowsPerBlock - 1) / rowsPerBlock
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

	for worker := range workers {
		wg.Go(func() {
			defer func() {
				if p := recover(); p != nil {
					stop(nil, &panicError{value: p, stack: debug.Stack()})
				}
			}()
			for inner.Err() == nil {
				b := int(next.Add(1) - 1)
				if b >= blocks {
					return
				}
				y0 := b * rowsPerBlock
				if err := fn(worker, y0, min(y0+rowsPerBlock, height)); err != nil {
					stop(err, nil)
					return
				}
			}
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

package pool

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gotest.tools/v3/assert"
)

type X struct {
	slice []byte
}

func TestPool(t *testing.T) {
	// Init a pool
	p, err := New(3, func(x *X) (*X, error) {
		x = new(X)
		x.slice = make([]byte, 0, 32)
		return x, nil
	})
	assert.NilError(t, err)               // Confirm it init'd clean
	assert.Equal(t, 3, len(p.containers)) // Confirm we have the expected initial size
	p.SetCap(3)                           // Set the capacity limit to the initial size

	// Register a deinitializer
	p.WithPost(func(x *X) (*X, error) {
		if len(x.slice) == 0 {
			return x, nil
		}
		x.slice = x.slice[:0]
		return x, nil
	})

	// Borrow 3x then look for a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// 1x
	bs, err := p.Borrow(ctx)
	assert.NilError(t, err)
	assert.Equal(t, 0, len(bs.slice))
	assert.Equal(t, 32, cap(bs.slice))

	// 2x
	bs2, err := p.Borrow(ctx)
	assert.NilError(t, err)
	assert.Check(t, bs2 != nil)
	assert.Equal(t, 0, len(bs.slice))
	assert.Equal(t, 32, cap(bs.slice))

	// 3x
	bs3, err := p.Borrow(ctx)
	assert.NilError(t, err)
	assert.Check(t, bs3 != nil)
	assert.Equal(t, 0, len(bs.slice))
	assert.Equal(t, 32, cap(bs.slice))

	// Write some data to the slice and make sure it gets cleaned up
	bs3.slice = append(bs3.slice, []byte("Hello, world!")...)
	assert.Equal(t, "Hello, world!", string(bs3.slice))

	// Deadline (at capacity)
	bs, err = p.Borrow(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Bring one back!
	err = p.Return(bs3)
	assert.Check(t, err)

	// Now rent another (this time should work)
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	bs3, err = p.Borrow(ctx)
	assert.NilError(t, err)

	// Confirm once we get it back the data is empty (deinit func did its job)
	assert.Equal(t, 0, len(bs3.slice))
}

func TestPoolRace(t *testing.T) {
	const expCap uint32 = 4

	wg := new(sync.WaitGroup)
	p := MustNew(3, func(t *bool) (*bool, error) {
		return t, nil
	})
	p.SetCap(expCap)

	// 64-rounds of 128 concurrent borrow attempts
	for range 64 {
		for range 128 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				item, err := p.Borrow(t.Context())
				assert.NilError(t, err)
				err = p.Return(item)
				assert.NilError(t, err)
			}()
		}
		wg.Wait()
	}
	assert.Equal(t, expCap, *p.cap)
}

func BenchmarkPool(b *testing.B) {
	// Benchmark with 2^1 through 2^12 pool capacity
	for i := 1; i < 13; i++ {
		n := 1 << i
		p := MustNew(uint32(n), func(t *bytes.Buffer) (*bytes.Buffer, error) {
			return t, nil
		})
		b.Run("capacity_"+strconv.Itoa(n), func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					i, _ := p.Borrow(context.Background())
					_ = p.Return(i)
				}
			})
		})
	}
}

type SyncMutex struct {
	i  int
	mu *sync.Mutex
}

type AtomicBool struct {
	i  int
	mu *atomic.Bool
}

func BenchmarkSyncMutex(b *testing.B) {
	sm := new(SyncMutex)
	sm.mu = new(sync.Mutex)
	wg := new(sync.WaitGroup)

	fn := func(wg *sync.WaitGroup) {
		sm.mu.Lock()
		sm.i += 1
		sm.mu.Unlock()
		wg.Done()
	}

	for i := 1; i < 8; i++ {
		n := 1 << i
		title := fmt.Sprintf("sync-mutex/%d_concurrent_ops", n)
		runN(b, title, n, wg, fn)
	}
}

func BenchmarkAtomicBool(b *testing.B) {
	const concurrentOps = 256

	ab := new(AtomicBool)
	ab.mu = new(atomic.Bool)
	wg := new(sync.WaitGroup)

	fn := func(wg *sync.WaitGroup) {
		ab.mu.CompareAndSwap(false, true)
		ab.i += 1
		ab.mu.CompareAndSwap(true, false)
		wg.Done()
	}

	for i := 1; i < 8; i++ {
		n := 1 << i
		title := fmt.Sprintf("atomic-bool/%d_concurrent_ops", n)
		runN(b, title, n, wg, fn)
	}
}

func runN(b *testing.B, title string, n int, wg *sync.WaitGroup, fn func(wg *sync.WaitGroup)) {
	b.Run(title, func(b *testing.B) {
		for b.Loop() {
			for range n {
				wg.Add(1)
				go fn(wg)
			}
			wg.Wait()
		}
	})
}

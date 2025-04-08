package pool

import (
	"context"
	"errors"
	"sync"
	"time"
)

type (
	Constructor[T any] func(*T) (*T, error)
	Init[T any]        func(*T) (*T, error)
	Deinit[T any]      func(*T) (*T, error)
)

func NewPool[T any](initialSize int, initializer Constructor[T]) (*Pool[T], error) {
	if initialSize < 0 {
		initialSize = 0
	}

	pool := new(Pool[T])

	pool.containers = make([]*Container[T], initialSize)
	for i := range initialSize {
		next, err := initializer(new(T))
		if err != nil {
			return nil, err
		}

		pool.containers[i] = &Container[T]{
			mu:   new(sync.Mutex),
			item: next,
		}
	}

	return pool, nil
}

func MustNew[T any](initialSize uint32, initializer Constructor[T]) *Pool[T] {
	p, err := New(initialSize, initializer)
	if err != nil {
		panic(err)
	}
	return p
}

/*------------------------------------------------------------------------------
 * Pool
 *----------------------------------------------------------------------------*/

type Pool[T any] struct {
	containers []*Container[T]
	construct  Constructor[T] // Called once when new items are created
	setup      Init[T]        // Called during each call to Borrow when an item is loaned out
	teardown   Deinit[T]      // Called each time a borrowed item is released
	size, cap  int
}

func (p *Pool[T]) SetCap(i int) *Pool[T] {
	p.cap = i
	return p
}

func (p *Pool[T]) RegisterInit(handler Init[T]) {
	p.setup = handler
}

func (p *Pool[T]) RegisterDeinit(handler Deinit[T]) {
	p.teardown = handler
}

func (p *Pool[T]) RegisterConstructor(handler Constructor[T]) {
	p.construct = handler
}

func (p *Pool[T]) Borrow(ctx context.Context) (*T, error) {
	for _, container := range p.containers {
		if container.mu.TryLock() {
			if p.setup == nil {
				return container.item, nil
			}
			return p.setup(container.item)
		}
	}

	select {
	case <-ctx.Done():
		return nil, context.DeadlineExceeded
	default:
		// If we have capacity headroom, just spin up a new one
		if len(p.containers) < p.cap {
			return p.new()
		}

		// Try again until we hit the context deadline or a container frees up
		<-time.After(1 * time.Millisecond)
		return p.Borrow(ctx)
	}
}

func (p *Pool[T]) MustBorrow(ctx context.Context) *T {
	v, err := p.Borrow(ctx)
	if err != nil {
		panic(err)
	}
	return v
}

var ErrNotFound = errors.New("uhh, we didn't sell you that?")

func (p *Pool[T]) Return(item *T) (err error) {
	for _, v := range p.containers {
		if v.item == item {
			if p.teardown != nil {
				v.item, err = p.teardown(v.item)
			}
			v.mu.Unlock()
			return err
		}
	}
	return ErrNotFound
}

func (p *Pool[T]) new() (*T, error) {
	// Init the container item
	item, err := p.construct(new(T))
	if err != nil {
		return nil, err
	}

	// Init the container and lock it
	ct := &Container[T]{
		item: item,
		mu:   new(sync.Mutex),
	}
	ct.mu.Lock()

	p.containers = append(p.containers, ct)

	return ct.item, nil
}

/*------------------------------------------------------------------------------
 * Container
 *----------------------------------------------------------------------------*/

type Container[T any] struct {
	mu   *sync.Mutex
	item *T
}

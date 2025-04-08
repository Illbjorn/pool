package pool

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

type (
	Constructor[T any] func(*T) (*T, error)
	Pre[T any]         func(*T) (*T, error)
	Post[T any]        func(*T) (*T, error)
)

func New[T any](initialSize uint32, constructor Constructor[T]) (*Pool[T], error) {
	pool := new(Pool[T])
	pool.mu = new(sync.Mutex)
	pool.cap = &initialSize
	pool.containers = make([]*Container[T], initialSize)

	pool.mu.Lock()
	for i := range initialSize {
		next, err := constructor(new(T))
		if err != nil {
			return nil, err
		}
		pool.containers[i] = newContainer(next)
	}
	pool.mu.Unlock()

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
	containers  []*Container[T]
	constructor Constructor[T] // Called once when new items are created
	pre         Pre[T]         // Called during each call to Borrow when an item is loaned out
	post        Post[T]        // Called each time a borrowed item is released
	cap         *uint32
	mu          *sync.Mutex
}

func (p *Pool[T]) SetCap(i uint32) {
	atomic.StoreUint32(p.cap, i)
}

func (p *Pool[T]) WithPre(handler Pre[T]) {
	p.pre = handler
}

func (p *Pool[T]) applyPre(v *T) (*T, error) {
	if p.pre != nil {
		return p.pre(v)
	}
	return v, nil
}

func (p *Pool[T]) WithPost(handler Post[T]) {
	p.post = handler
}

func (p *Pool[T]) applyPost(v *T) (*T, error) {
	if p.post == nil {
		return v, nil
	}
	return p.post(v)
}

func (p *Pool[T]) WithConstructor(handler Constructor[T]) {
	if handler == nil {
		return
	}
	p.constructor = handler
}

func (p *Pool[T]) construct() (*T, error) {
	// Construct a new Container[T] with a new `T`
	ct := newContainer(new(T))
	ct.Lock()

	p.mu.Lock()
	p.containers = append(p.containers, ct)
	p.mu.Unlock()

	// If we don't have a constructor, we're done here
	if p.constructor == nil {
		return ct.item, nil
	}

	// If we do have a constructor, construct and return the result
	return p.constructor(ct.item)
}

func (p *Pool[T]) Borrow(ctx context.Context) (*T, error) {
	for _, container := range p.containers {
		if container.TryLock() {
			return p.applyPre(container.item)
		}
	}

	select {
	case <-ctx.Done():
		return nil, context.DeadlineExceeded
	default:
		// If we have capacity, just spin up a new one
		if len(p.containers) < int(*p.cap) {
			return p.construct()
		}
		// Try again until we hit the context deadline or a container frees up
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
	for _, container := range p.containers {
		if container.item == item {
			container.Unlock()
			container.item, err = p.applyPost(container.item)
			return err
		}
	}
	return ErrNotFound
}

/*------------------------------------------------------------------------------
 * Container
 *----------------------------------------------------------------------------*/

func newContainer[T any](v *T) *Container[T] {
	return &Container[T]{
		item:   v,
		locked: new(atomic.Bool),
	}
}

type Container[T any] struct {
	locked *atomic.Bool
	item   *T
}

func (ct *Container[T]) TryLock() bool {
	return ct.locked.CompareAndSwap(false, true)
}

func (ct *Container[T]) Lock() {
	ct.locked.Store(true)
}

func (ct *Container[T]) Unlock() {
	ct.locked.Store(false)
}

package pool

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type X struct {
	slice []byte
}

func TestPool(t *testing.T) {
	// Init a pool
	p, err := NewPool(3, func(x *X) (*X, error) {
		x = new(X)
		x.slice = make([]byte, 0, 32)
		return x, nil
	})
	require.NoError(t, err)                // Confirm it init'd clean
	require.Equal(t, 3, len(p.containers)) // Confirm we have the expected initial size
	p.SetCap(3)                            // Set the capacity limit to the initial size

	// Register a deinitializer
	p.RegisterDeinit(func(x *X) (*X, error) {
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
	require.NoError(t, err)
	assert.Equal(t, 0, len(bs.slice))
	assert.Equal(t, 32, cap(bs.slice))

	// 2x
	bs2, err := p.Borrow(ctx)
	require.NoError(t, err)
	assert.NotNil(t, bs2)
	assert.Equal(t, 0, len(bs.slice))
	assert.Equal(t, 32, cap(bs.slice))

	// 3x
	bs3, err := p.Borrow(ctx)
	require.NoError(t, err)
	assert.NotNil(t, bs3)
	assert.Equal(t, 0, len(bs.slice))
	assert.Equal(t, 32, cap(bs.slice))

	// Write some data to the slice and make sure it gets cleaned up
	bs3.slice = append(bs3.slice, []byte("Hello, world!")...)
	require.Equal(t, "Hello, world!", string(bs3.slice))

	// Deadline (at capacity)
	bs, err = p.Borrow(ctx)
	require.Error(t, err)

	// Bring one back!
	err = p.Return(bs3)
	assert.NoError(t, err)

	// Now rent another (this time should work)
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	bs3, err = p.Borrow(ctx)
	require.NoError(t, err)

	// Confirm once we get it back the data is empty (deinit func did its job)
	require.Equal(t, 0, len(bs3.slice))
}

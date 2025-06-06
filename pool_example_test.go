package pool_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/illbjorn/pool"
	"gotest.tools/v3/assert"
)

func TestPoolExample(t *testing.T) {
	// Creates a `pool.Pool[bytes.Buffer]` with a maximum capacity of 1
	p := pool.MustNew(1, func() (*bytes.Buffer, error) {
		return bytes.NewBuffer(make([]byte, 0, 256)), nil
	})

	// Set a post-return handler to zero the buffer when we're done with it
	p.WithPost(func(b *bytes.Buffer) (*bytes.Buffer, error) {
		b.Reset()
		return b, nil
	})

	// Initialize a context with a 250ms timeout for our borrow operation
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	// Borrow a buffer
	b, err := p.Borrow(ctx)
	assert.Check(t, err)
	assert.Check(t, b != nil)

	b.WriteString("Hello, world!") // Write some data to it
	assert.DeepEqual(t, []byte("Hello, world!"), b.Bytes())

	// Return it when we're done. This allows this buffer to be reused by future
	// callers.
	//
	// Also, since we set `p.WithPost()`, the buffers been reset.
	err = p.Return(b)
	assert.Check(t, err)

	// Borrow it again, confirm the data is gone
	ctx, cancel = context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	b, err = p.Borrow(ctx)
	assert.Check(t, err)
	assert.Equal(t, 0, len(b.Bytes()))

	// Borrow again
	//
	// Since we set our pool's capacity to 1, we get a context.DeadlineExceeded
	ctx, cancel = context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	b, err = p.Borrow(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)

	// Handle the deadline, increasing our capacity
	if errors.Is(err, context.DeadlineExceeded) {
		p.SetCap(2)

		// Retry the borrow
		ctx, cancel = context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()
		b2, err := p.Borrow(ctx) // Success!
		assert.Check(t, err)
		assert.Check(t, b2 != nil)
	}
}

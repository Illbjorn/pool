# Overview

`Pool` is a barebones implementation to allocate objects you plan to reuse
throughout the lifetime of your application upfront and avoid some memory
thrash. An example might be to create something like a `Pool[bytes.Buffer]` in
codebases like a transpiler where you might frequently want access to throwaway
buffers.

# Example

> [!NOTE]
> These examples are directly copied from `pool_example_test.go`. Feel free to
> check that out for a more succinct overview.

Create a pool of `bytes.Buffer`s:

```go
// Creates a `pool.Pool[bytes.Buffer]` with a maximum capacity of 1
p := pool.MustNew(1, func() (*bytes.Buffer, error) {
  return bytes.NewBuffer(make([]byte, 0, 256)), nil
})

// Set a post handler to zero the buffer when we return it
p.WithPost(func(b *bytes.Buffer) (*bytes.Buffer, error) {
  b.Reset()
  return b, nil
})
```

Using ("borrowing") from the `Pool`:

```go
// Borrow a buffer
b, err := p.Borrow(ctx)
assert.NoError(t, err)
assert.NotNil(t, b)

b.WriteString("Hello, world!") // Write some data to it
assert.Equal(t, []byte("Hello, world!"), b.Bytes())

// Return it when we're done. This allows this buffer to be reused by future
// callers.
//
// Also, since we set `p.WithPost()` earlier, the buffers been reset.
err = p.Return(b)
assert.NoError(t, err)

// Borrow it again, confirm the data is gone
ctx, cancel = context.WithTimeout(context.Background(), 250*time.Millisecond)
defer cancel()
b, err = p.Borrow(ctx)
assert.NoError(t, err)
assert.Empty(t, b.Bytes())
```

Borrowing when at capacity:

```go
// Borrow again
//
// Since we set our pool's capacity to 1 and haven't returned the instance we
// borrowed above, we get a context.DeadlineExceeded
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
  assert.NoError(t, err)
  assert.NotNil(t, b2)
}
```

# Non-goals

- This package assumes you're not a masochist, there are no safeguards directly **preventing** a borrowed value `x` from being used after `Pool.Return(x)` has been called. It's up to the developer to return and discontinue use when appropriate.

# Notes

- The `Must*` functions perform the same functionality as their counterparts (i.e. `New`, `Borrow`) but will panic if an error is encountered.
- The **only** errors produced directly by this package are
  - `context.DeadlineExceeded` from a call to `MustBorrow` / `Borrow` when the capacity has been reached and all items are currently borrowed
  - `ErrNotFound` if you attempt to return something we didn't sell you!
  - Otherwise, the only errors that could be returned are those you return in your `constructor`, `pre` and `post` handlers.

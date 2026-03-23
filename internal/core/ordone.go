package core

// OrDone wraps a channel read with a done channel, returning values from
// the input channel until either the done channel closes or the input
// channel closes. This prevents goroutine leaks by ensuring readers
// can always exit when the parent context is cancelled.
//
// Usage:
//
//	for val := range core.OrDone(ctx.Done(), inputCh) {
//	    process(val)
//	}
func OrDone[T any](done <-chan struct{}, c <-chan T) <-chan T {
	out := make(chan T)
	go func() {
		defer close(out)
		for {
			select {
			case <-done:
				return
			case v, ok := <-c:
				if !ok {
					return
				}
				select {
				case out <- v:
				case <-done:
					return
				}
			}
		}
	}()
	return out
}

// OrDoneFunc runs fn in a goroutine and returns when either fn completes
// or done is closed. Returns fn's error, or nil if done closed first.
func OrDoneFunc(done <-chan struct{}, fn func() error) error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- fn()
	}()
	select {
	case <-done:
		return nil
	case err := <-errCh:
		return err
	}
}

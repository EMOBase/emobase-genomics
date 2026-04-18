package text

import (
	"bufio"
	"context"
	"io"
)

// ReadLines streams each line of f on the returned channel, stripping line endings.
func ReadLines(ctx context.Context, f io.Reader) (<-chan string, <-chan error) {
	lineCh := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		defer close(lineCh)
		defer close(errCh)

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			case lineCh <- scanner.Text():
			}
		}

		if err := scanner.Err(); err != nil {
			errCh <- err
		}
	}()

	return lineCh, errCh
}

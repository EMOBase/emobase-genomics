package file

import (
	"bufio"
	"context"
	"io"
	"strings"
)

func ReadLines(ctx context.Context, f io.Reader) (<-chan string, <-chan error) {
	lineCh := make(chan string)
	errCh := make(chan error, 1)

	go func() {
		defer close(lineCh)
		defer close(errCh)

		reader := bufio.NewReader(f)

		for {
			line, err := reader.ReadString('\n')

			if err != nil {
				if err != io.EOF {
					errCh <- err
				} else {
					errCh <- nil
				}
				return
			}

			if len(line) > 0 {
				select {
				case <-ctx.Done():
					errCh <- ctx.Err()
					return
				case lineCh <- strings.TrimRight(line, "\n"):
				}
			}
		}
	}()

	return lineCh, errCh
}

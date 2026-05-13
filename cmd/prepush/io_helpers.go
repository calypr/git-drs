package prepush

import (
	"fmt"
	"io"
	"os"
)

func bufferStdin(stdin io.Reader, createTempFile func(dir, pattern string) (*os.File, error)) (*os.File, error) {
	tmp, err := createTempFile("", "prepush-stdin-*")
	if err != nil {
		return nil, fmt.Errorf("error creating temp file for stdin: %w", err)
	}

	if _, err := io.Copy(tmp, stdin); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, fmt.Errorf("error buffering stdin: %w", err)
	}

	if _, err := tmp.Seek(0, 0); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return nil, fmt.Errorf("error seeking temp stdin: %w", err)
	}
	return tmp, nil
}

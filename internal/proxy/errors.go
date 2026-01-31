package proxy

import (
	"errors"
	"io"
	"syscall"
)

// Add helper function
func isClientDisconnect(err error) bool {
	return errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, io.EOF)
}

package comfy

import (
	"io"
	"net"
	"net/url"
	"syscall"

	"github.com/rm4n0s/errors"
)

// isConnError reports a refused, reset or aborted connection, a failed DNS
// lookup, or a connection that closed in the middle of an answer.
func isConnError(err error) bool {
	var opErr *net.OpError
	var dnsErr *net.DNSError
	var urlErr *url.Error
	switch {
	case errors.As(err, &opErr), errors.As(err, &dnsErr):
		return true
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return true
	case errors.Is(err, syscall.ECONNREFUSED), errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return true
	case errors.As(err, &urlErr):
		return true
	}
	return false
}

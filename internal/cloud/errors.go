package cloud

import (
	"context"
	"errors"
	"net"
)

// requestError reports a failed HTTP exchange without the cause's text, which net/http may
// fill with signed urls (url.Error, bad Location headers); the cause stays available to errors.Is/As.
type requestError struct {
	op    string // method and host+path, never the query
	cause error
}

func (e *requestError) Error() string { return e.op + ": " + describe(e.cause) }

func (e *requestError) Unwrap() error { return e.cause }

func describe(err error) string {
	var dns *net.DNSError
	var op *net.OpError
	var ne net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.As(err, &dns):
		return "dns lookup failed"
	case errors.As(err, &op):
		return "network error (" + op.Op + ")"
	}
	return "request failed"
}

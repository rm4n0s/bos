package cli

import (
	"fmt"
	"io"
)

// say, sayf and sayRaw write to a terminal. A failed write has nobody to be
// told to, so the error is dropped here once.
func say(w io.Writer, args ...any) { _, _ = fmt.Fprintln(w, args...) }

func sayf(w io.Writer, format string, args ...any) { _, _ = fmt.Fprintf(w, format, args...) }

func sayRaw(w io.Writer, args ...any) { _, _ = fmt.Fprint(w, args...) }

// wrapped is an error with a sentence of its own and the error behind it.
type wrapped struct {
	msg   string
	cause error
}

func (e wrapped) Error() string { return e.msg + ": " + e.cause.Error() }
func (e wrapped) Unwrap() error { return e.cause }

func wrapf(cause error, format string, args ...any) error {
	return wrapped{msg: fmt.Sprintf(format, args...), cause: cause}
}

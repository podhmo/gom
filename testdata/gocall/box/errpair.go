package box

// `(error, error)` helpers for the Go → Gom direction of the
// Ok-payload boundary (spec sections 8.3/8.4): the generated API
// exposes the derived `Result (Option GoError) GoError` view as
// `result.Result[option.Option[error], error]` and reads the same
// value a Go caller hands back.

import "errors"

// TwoErrs is an `(error, error)` pair — slot 0 is a nil-able
// payload error: `(nil, nil)` is legitimate (no partial error, no
// fatal error) and arrives at the Gom side as `Ok None`.
func TwoErrs(n int64) (error, error) {
	switch {
	case n < 0:
		return nil, errors.New("fatal")
	case n == 0:
		return nil, nil
	default:
		return errors.New("soft"), nil
	}
}

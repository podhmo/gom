package rt

// StepError is the frame a `|?>` pipeline attaches to a failing
// step's error (docs/spec.md section 9.2): `Step` names the step as
// written in the source — `strconv.Atoi`, `parse_toml`, or a
// "file:line" position for a nameless step — and `Err` is the
// original error. `errors.Is`/`errors.As` reach `Err` through
// Unwrap; `Error()` keeps the `step: message` wording, so the
// rendered text is identical to the `fmt.Errorf("step: %w", err)`
// frame it replaces while the step name becomes structured data a
// Go-side consumer can read back with `errors.As`.
type StepError struct {
	Step string
	Err  error
}

func (e *StepError) Error() string { return e.Step + ": " + e.Err.Error() }
func (e *StepError) Unwrap() error { return e.Err }

// StepErr wraps err in a StepError frame. Generated code calls this
// where it used to emit `fmt.Errorf("<step>: %w", err)`; callers
// must only construct a frame around a non-nil err (the generated
// call site is the `err != nil` arm).
func StepErr(step string, err error) error {
	return &StepError{Step: step, Err: err}
}

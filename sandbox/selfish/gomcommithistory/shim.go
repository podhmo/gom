package commithistory

// Handwritten Go backing the @extern declarations in
// commithistory.gom — fmt.Errorf("%s: %w") can't be spelled from gom
// because the generated call site boxes the error arg into `any`,
// which go vet's printf check rejects.
import "fmt"

func wrap(e error, msg string) error { return fmt.Errorf("%s: %w", msg, e) }

package cli

import "os"

// Windows pipe writes already return delivery loss without a process signal.
// The inherited file is borrowed; ownership stays with process teardown.
type transactionAcceptanceWriter struct{ file *os.File }

func (w transactionAcceptanceWriter) Write(p []byte) (int, error) { return w.file.Write(p) }

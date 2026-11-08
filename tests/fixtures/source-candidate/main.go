// An adversarial version-probe candidate: any execution leaves an owned marker.
package main

import (
	"fmt"
	"os"
)

func main() {
	if marker := os.Getenv("XALLOR_TEST_CANDIDATE_MARKER"); marker != "" {
		_ = os.WriteFile(marker, []byte("candidate executed"), 0600)
	}
	fmt.Println(`{"version":"0.2.0"}`)
}

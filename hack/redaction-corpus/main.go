// Command redaction-corpus emits the serialized public DTOs that the
// release-blocking redaction oracle is meant to scan. It exists so `make
// redaction` exercises the real shapes -- a public capture with its
// completeness manifest, a heavy-object manifest, sessions, identities --
// instead of one hand-written line that would pass forever.
package main

import (
	"fmt"
	"os"

	"github.com/bayleafwalker/bindery-core/internal/externalruntime"
)

func main() {
	corpus, forbidden, err := externalruntime.RedactionCorpus()
	if err != nil {
		fmt.Fprintln(os.Stderr, "build redaction corpus:", err)
		os.Exit(2)
	}
	if _, err := os.Stdout.Write(corpus); err != nil {
		fmt.Fprintln(os.Stderr, "write redaction corpus:", err)
		os.Exit(2)
	}
	// The forbidden values are reported for the operator's benefit; the scan
	// itself works on the bytes above.
	for _, value := range forbidden {
		fmt.Fprintln(os.Stderr, "forbidden fixture value:", value)
	}
}

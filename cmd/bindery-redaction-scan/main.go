package main

import (
	"fmt"
	"io"
	"os"

	"github.com/bayleafwalker/bindery-core/internal/externalruntime"
)

func main() {
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read input:", err)
		os.Exit(2)
	}
	if err := externalruntime.ScanPublicOutput(input); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

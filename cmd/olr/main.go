// Command olr is the hub CLI for open-linux-router.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "olr:", err)
		os.Exit(1)
	}
}

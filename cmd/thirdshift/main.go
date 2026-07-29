package main

import (
	"fmt"
	"os"

	"github.com/anianroid/thirdshift/internal/shared/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version.Version)
		return
	}

	fmt.Fprintf(os.Stdout, "thirdshift %s\n", version.Version)
	fmt.Fprintln(os.Stdout, "Milestone 0 host CLI scaffold. Host commands arrive in Milestone 1.")
}

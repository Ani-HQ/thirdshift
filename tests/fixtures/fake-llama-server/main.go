package main

import (
	"os"

	"github.com/Ani-HQ/thirdshift/internal/node/runtime/fakellama"
)

func main() {
	os.Exit(fakellama.Main(os.Args[1:]))
}

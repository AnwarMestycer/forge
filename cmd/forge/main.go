package main

import (
	"fmt"
	"os"
)

var version = "0.0.0-dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println("forge", version)
		return
	}
	fmt.Println("forge — build → verify → break → learn")
	fmt.Println("Early development. See ROADMAP.md.")
}

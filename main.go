package main

import (
	"flag"
	"log"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		log.Panic("Must provide a command'\n")
	}

	command := os.Args[1]
	switch command {
	case "app-update":
		buildFlags := flag.NewFlagSet("user", flag.ExitOnError)
		isFullUpdatePtr := buildFlags.Bool(
			"full",
			false,
			"Does a full update instead of just replacing a few files.",
		)
		buildFlags.Parse(os.Args[2:])

		execAppUpdate(*isFullUpdatePtr)
	case "user-update":
		execUserUpdate()
	}
}

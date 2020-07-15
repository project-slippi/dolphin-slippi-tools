package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"
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
		skipUpdaterUpdatePtr := buildFlags.Bool(
			"skip-updater",
			false,
			"If not a full update, this will likely be false first which will update the updater and "+
				"then re-trigger the new updater in order to update the app.",
		)
		shouldLaunchPtr := buildFlags.Bool(
			"launch",
			false,
			"If true, will launch Dolphin after update.",
		)
		isoPathPtr := buildFlags.String(
			"iso",
			"",
			"ISO path to launch when shouldLaunch is true.",
		)
		buildFlags.Parse(os.Args[2:])

		err := execAppUpdate(*isFullUpdatePtr, *skipUpdaterUpdatePtr, *shouldLaunchPtr, *isoPathPtr)

		if err != nil {
			fmt.Println("")
			fmt.Println("Something went wrong. Read above messages to see if there's additional help info. If Dolphin isn't working, screenshot this and head to the Slippi Discord")
			for {
				time.Sleep(1 * time.Second)
			}
		}
	case "user-update":
		execUserUpdate()
	default:
		fmt.Println("Command not valid")
	}

}

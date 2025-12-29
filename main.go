package main

import (
	"errors"
	"log"
	"os"

	"neubibackup/internal/app"
	"neubibackup/internal/singleinstance"

	"github.com/getlantern/systray"
)

// version is set at build time via ldflags
var version = "dev"

// application holds the main App instance
var application *app.App

// instanceLock holds the single instance lock
var instanceLock *singleinstance.Lock

func main() {
	// Acquire single instance lock before starting
	var err error
	instanceLock, err = singleinstance.Acquire()
	if err != nil {
		if errors.Is(err, singleinstance.ErrAlreadyRunning) {
			log.Println("Another instance of NeubiBackup is already running. Exiting.")
			os.Exit(0)
		}
		log.Fatalf("Failed to acquire instance lock: %v", err)
	}

	systray.Run(onReady, onExit)
}

func onReady() {
	application = app.New(version,
		app.WithOnIconUpdate(systray.SetIcon),
		app.WithOnQuit(systray.Quit),
		app.WithSetTooltip(systray.SetTooltip),
	)

	if err := application.Initialize(); err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}

	if err := application.Run(); err != nil {
		log.Fatalf("Failed to run: %v", err)
	}
}

func onExit() {
	if application != nil {
		application.Shutdown()
	}
	if instanceLock != nil {
		instanceLock.Release()
	}
}

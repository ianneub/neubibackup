package main

import (
	"errors"
	"log/slog"
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
			slog.Info("Another instance of NeubiBackup is already running. Exiting.")
			os.Exit(0)
		}
		slog.Error("Failed to acquire instance lock", "error", err)
		os.Exit(1)
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
		slog.Error("Failed to initialize", "error", err)
		os.Exit(1)
	}

	if err := application.Run(); err != nil {
		slog.Error("Failed to run", "error", err)
		os.Exit(1)
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

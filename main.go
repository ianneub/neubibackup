package main

import (
	"log"

	"neubibackup/internal/app"

	"github.com/getlantern/systray"
)

// version is set at build time via ldflags
var version = "dev"

// application holds the main App instance
var application *app.App

func main() {
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
}

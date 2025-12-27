package main

import (
	"neubibackup/assets"

	"github.com/getlantern/systray"
)

// version is set at build time via ldflags
var version = "dev"

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(assets.IconIdle)
	systray.SetTitle("")
	systray.SetTooltip("NeubiBackup")

	// Basic menu for Phase 1 - just Quit
	mQuit := systray.AddMenuItem("Quit", "Quit NeubiBackup")

	go func() {
		<-mQuit.ClickedCh
		systray.Quit()
	}()
}

func onExit() {
	// Cleanup when the app exits
}

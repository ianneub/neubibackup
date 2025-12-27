//go:build darwin

package power

import (
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Start begins monitoring for system wake events on macOS.
// Uses polling of kern.waketime sysctl since cgo-free approach.
func (w *Watcher) Start() {
	go w.pollWakeTime()
}

func (w *Watcher) pollWakeTime() {
	var lastWakeTime int64

	// Get initial wake time
	if wt, err := getWakeTime(); err == nil {
		lastWakeTime = wt
	}

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stop:
			return
		case <-ticker.C:
			wt, err := getWakeTime()
			if err != nil {
				continue
			}

			if wt > lastWakeTime && lastWakeTime > 0 {
				log.Println("Wake from sleep detected")
				lastWakeTime = wt
				w.callback()
			} else if wt > lastWakeTime {
				lastWakeTime = wt
			}
		}
	}
}

func getWakeTime() (int64, error) {
	// sysctl kern.waketime returns the last wake time
	cmd := exec.Command("sysctl", "-n", "kern.waketime")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	// Parse output like "{ sec = 1234567890, usec = 123456 } ..."
	// or "sec = 1234567890, usec = 123456"
	s := string(output)

	// Find "sec = " and extract the number
	idx := strings.Index(s, "sec = ")
	if idx == -1 {
		return 0, nil
	}

	s = s[idx+6:]
	endIdx := strings.IndexAny(s, ", }")
	if endIdx == -1 {
		endIdx = len(s)
	}

	sec, err := strconv.ParseInt(strings.TrimSpace(s[:endIdx]), 10, 64)
	if err != nil {
		return 0, err
	}

	return sec, nil
}

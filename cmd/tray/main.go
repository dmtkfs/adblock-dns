package main

import (
	_ "embed"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/dmtkfs/adblock-dns/internal/proxy"
	"github.com/getlantern/systray"
)

//go:embed icon.ico
var iconBytes []byte

func main() {
	// SETUP LOGGING — LOG FILE ONLY!
	logPath := filepath.Join(exeDir(), "adblock.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// As a fallback, use the default logger (which won't be visible in GUI mode)
	} else {
		log.SetOutput(f)
	}
	// continue as before...
	systray.Run(onReady, onExit)
}

func onReady() {
	systray.SetIcon(iconBytes)
	systray.SetTooltip("Adblock-DNS")

	mStatus := systray.AddMenuItem("Status: Stopped", "")
	mStatus.Disable()
	mStart := systray.AddMenuItem("Start", "Start the DNS proxy")
	mStop := systray.AddMenuItem("Stop", "Stop the DNS proxy")
	mDryRun := systray.AddMenuItemCheckbox("Dry-run", "Log only, no blocking", false)
	mOpenLog := systray.AddMenuItem("Open log file", "View adblock.log")
	mQuit := systray.AddMenuItem("Quit", "Exit the app")

	updateStatus(mStatus, false, false) // Initial state

	go func() {
		for {
			select {
			case <-mStart.ClickedCh:
				if proxy.Running() {
					log.Println("Proxy already running")
				} else {
					startOrRetry(mDryRun.Checked())
					updateStatus(mStatus, proxy.Running(), mDryRun.Checked())
				}
			case <-mStop.ClickedCh:
				proxy.Stop()
				updateStatus(mStatus, proxy.Running(), mDryRun.Checked())
			case <-mDryRun.ClickedCh:
				if mDryRun.Checked() {
					mDryRun.Uncheck()
				} else {
					mDryRun.Check()
				}
				if proxy.Running() {
					proxy.Stop()
					startOrRetry(mDryRun.Checked())
				}
				updateStatus(mStatus, proxy.Running(), mDryRun.Checked())
			case <-mOpenLog.ClickedCh:
				openLogFile()
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()

	// Update tooltip and status every 30s with last refresh time/state
	go func() {
		for range time.Tick(30 * time.Second) {
			if proxy.Running() {
				systray.SetTooltip("Adblock-DNS • updated " +
					proxy.LastUpdated().Format("15:04"))
			} else {
				systray.SetTooltip("Adblock-DNS • stopped")
			}
			updateStatus(mStatus, proxy.Running(), mDryRun.Checked())
		}
	}()
}

func updateStatus(mStatus *systray.MenuItem, running, dryRun bool) {
	if running {
		if dryRun {
			mStatus.SetTitle("Status: Running (Dry-run)")
		} else {
			mStatus.SetTitle("Status: Running")
		}
	} else {
		mStatus.SetTitle("Status: Stopped")
	}
}

func startOrRetry(dryRun bool) {
	for i := 0; i < 3; i++ {
		err := proxy.Start(proxy.Options{
			Listen:   "127.0.0.1:53",
			Interval: 24 * time.Hour,
			DryRun:   dryRun,
			Verbose:  true,
		})
		if err == nil {
			return
		}
		log.Printf("Proxy start failed: %v (retry %d/3)", err, i+1)
		time.Sleep(300 * time.Millisecond)
	}
	log.Println("Proxy failed to start after retries. Is another process using port 53?")
}

func openLogFile() {
	logPath := filepath.Join(exeDir(), "adblock.log")
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("notepad.exe", logPath)
	case "darwin":
		cmd = exec.Command("open", logPath)
	default:
		cmd = exec.Command("xdg-open", logPath)
	}
	_ = cmd.Start()
}

func exeDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

func onExit() {
	proxy.Stop()
}

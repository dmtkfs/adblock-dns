package main

import (
	_ "embed"
	"fmt"
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

func exeDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

func logPath() string {
	return filepath.Join(exeDir(), "adblock.log")
}

func openLog() {
	lp := logPath()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("notepad.exe", lp)
	case "darwin":
		cmd = exec.Command("open", lp)
	default:
		cmd = exec.Command("xdg-open", lp)
	}
	_ = cmd.Start()
}

func onExit() {
	proxy.Stop()
}

func updateStatus(mi *systray.MenuItem) {
	for {
		running := proxy.Running()
		state := "Stopped"
		if running {
			dr := "Running"
			// We cannot read dry-run from proxy; reflect in title via tick handler where we toggle.
			state = dr
		}
		lu := proxy.LastUpdated()
		if !lu.IsZero() {
			state = fmt.Sprintf("Status: %s — Lists: %s", state, lu.Format("2006-01-02 15:04"))
		} else {
			state = fmt.Sprintf("Status: %s", state)
		}
		mi.SetTitle(state)
		time.Sleep(3 * time.Second)
	}
}

func main() {
	logFile, _ := os.OpenFile(logPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	defer logFile.Close()
	log.SetOutput(logFile)

	systray.Run(onReady, onExit)
}

func onReady() {
	if len(iconBytes) > 0 {
		systray.SetIcon(iconBytes)
	}
	systray.SetTitle("Adblock-DNS")
	systray.SetTooltip("DNS ad blocker")

	status := systray.AddMenuItem("Status: Starting…", "")
	status.Disable()

	start := systray.AddMenuItem("Start", "Start DNS proxy")
	stop := systray.AddMenuItem("Stop", "Stop DNS proxy")
	stop.Disable()

	dry := systray.AddMenuItemCheckbox("Dry-run", "Log only; do not block", false)
	openLogItem := systray.AddMenuItem("Open log file", "Open adblock.log")
	quit := systray.AddMenuItem("Quit", "Quit Adblock-DNS")

	// start automatically
	opts := proxy.Options{
		Listen:    "127.0.0.1:53",
		Interval:  24 * time.Hour,
		DryRun:    dry.Checked(),
		Verbose:   false,
		Upstreams: []string{"9.9.9.9:53", "149.112.112.112:53"},
		MatchMode: "suffix",
		BlockMode: "null",
	}
	if err := proxy.Start(opts); err != nil {
		log.Printf("start error: %v", err)
	} else {
		start.Disable()
		stop.Enable()
	}

	go updateStatus(status)

	go func() {
		for {
			select {
			case <-start.ClickedCh:
				if !proxy.Running() {
					if err := proxy.Start(opts); err != nil {
						log.Printf("start error: %v", err)
					} else {
						start.Disable()
						stop.Enable()
					}
				}
			case <-stop.ClickedCh:
				if proxy.Running() {
					proxy.Stop()
					stop.Disable()
					start.Enable()
				}
			case <-dry.ClickedCh:
				// toggle
				if dry.Checked() {
					dry.Uncheck()
					proxy.SetDryRun(false)
				} else {
					dry.Check()
					proxy.SetDryRun(true)
				}
			case <-openLogItem.ClickedCh:
				openLog()
			case <-quit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

package main

import (
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dmtkfs/adblock-dns/internal/proxy"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:53", "addr:port")
	interval := flag.Duration("interval", 24*time.Hour, "refresh interval")
	dryRun := flag.Bool("dry-run", false, "log only")
	verbose := flag.Bool("v", false, "verbose")
	flag.Parse()

	// Log file in the current working directory
	logPath := "adblock.log"
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file %s: %v", logPath, err)
	}
	defer f.Close()
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.Printf("LOG PATH: %s\n", logPath)

	// start proxy
	if err := proxy.Start(proxy.Options{
		Listen:   *listen,
		Interval: *interval,
		DryRun:   *dryRun,
		Verbose:  *verbose,
	}); err != nil {
		log.Fatal(err)
	}
	log.Printf("proxy running on %s (dry=%v)", *listen, *dryRun)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	proxy.Stop()
	log.Println("bye!")
}

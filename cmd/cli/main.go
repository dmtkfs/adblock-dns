
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dmtkfs/adblock-dns/internal/proxy"
)

func exeDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

type strSlice []string
func (s *strSlice) String() string { return strings.Join(*s, ",") }
func (s *strSlice) Set(v string) error {
	for _, part := range strings.Split(v, ",") {
		part = strings.TrimSpace(part)
		if part == "" { continue }
		if !strings.Contains(part, ":") { part += ":53" }
		*s = append(*s, part)
	}
	return nil
}

func main() {
	listen := flag.String("listen", "127.0.0.1:53", "addr:port to listen on")
	interval := flag.Duration("interval", 24*time.Hour, "blocklist refresh interval, e.g. 24h")
	dryRun := flag.Bool("dry-run", false, "log block hits but do not block")
	verbose := flag.Bool("v", false, "verbose logging")
	match := flag.String("match", "suffix", "domain match mode: exact|suffix")
	mode := flag.String("block-mode", "null", "block response: null (0.0.0.0/::) or nxdomain")
	var upstreams strSlice
	flag.Var(&upstreams, "upstream", "upstream DNS server(s) ip[:port]; can be repeated or comma-separated")
	flag.Parse()

	// Log file next to the executable
	logPath := filepath.Join(exeDir(), "adblock.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file %s: %v", logPath, err)
	}
	defer f.Close()
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.Printf("LOG PATH: %s", logPath)

	if err := proxy.Start(proxy.Options{
		Listen:    *listen,
		Interval:  *interval,
		DryRun:    *dryRun,
		Verbose:   *verbose,
		Upstreams: upstreams,
		MatchMode: *match,
		BlockMode: *mode,
	}); err != nil {
		log.Fatal(err)
	}
	log.Printf("proxy running on %s (dry=%v)", *listen, *dryRun)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	proxy.Stop()
	log.Println("bye!")
	fmt.Println()
}

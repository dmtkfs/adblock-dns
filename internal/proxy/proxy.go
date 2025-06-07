package proxy

import (
	"bufio"
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type Options struct {
	Listen   string
	Interval time.Duration
	DryRun   bool
	Verbose  bool
}

var (
	blocked     = make(map[string]struct{})
	whitelist   = make(map[string]struct{})
	srv         *dns.Server
	cancelFunc  context.CancelFunc
	lastUpdated time.Time
	running     bool

	blocklistURLs = []string{
		"https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
		"https://adaway.org/hosts.txt",
	}

	// For dynamic options
	currentOpts Options
	optsMu      sync.RWMutex
)

// Register handler ONCE at process startup
func init() {
	dns.HandleFunc(".", dynamicDNSHandler)
}

func dynamicDNSHandler(w dns.ResponseWriter, r *dns.Msg) {
	optsMu.RLock()
	opts := currentOpts
	optsMu.RUnlock()
	handleDNS(w, r, opts)
}

func Running() bool          { return running }
func LastUpdated() time.Time { return lastUpdated }

func exeDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

func Start(o Options) error {
	if running {
		return nil
	}
	running = true

	optsMu.Lock()
	currentOpts = o
	optsMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancelFunc = cancel

	srv = &dns.Server{Addr: o.Listen, Net: "udp"}

	go scheduler(ctx, o.Interval)

	go func() {
		if err := srv.ListenAndServe(); err != nil && ctx.Err() == nil {
			log.Printf("dns listen error: %v", err)
		}
	}()

	log.Printf("proxy started (dry=%v)", o.DryRun)
	return nil
}

func Stop() {
	if !running {
		return
	}
	log.Println("proxy stopping...")
	cancelFunc()
	if srv != nil {
		go func(s *dns.Server) {
			_ = s.Shutdown()
		}(srv)
	}
	time.Sleep(300 * time.Millisecond)
	srv = nil
	cancelFunc = nil
	running = false
	log.Println("proxy stopped.")
}

func scheduler(ctx context.Context, interval time.Duration) {
	refresh()
	tick := time.NewTicker(interval)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			refresh()
		}
	}
}

func loadWhitelist() {
	path := filepath.Join(exeDir(), "whitelist.txt")
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("whitelist read error: %v", err)
		}
		return
	}
	defer f.Close()

	tmp := make(map[string]struct{})
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.ToLower(strings.TrimSpace(sc.Text()))
		if line != "" && !strings.HasPrefix(line, "#") {
			tmp[line] = struct{}{}
		}
	}
	whitelist = tmp
	log.Printf("whitelist loaded — %d entries", len(whitelist))
}

func refresh() {
	tmp := make(map[string]struct{})
	for _, url := range blocklistURLs {
		resp, err := http.Get(url)
		if err != nil {
			log.Printf("fetch error %s: %v", url, err)
			continue
		}
		defer resp.Body.Close()

		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				tmp[strings.ToLower(parts[1])] = struct{}{}
			}
		}
	}

	blocked = tmp
	loadWhitelist()
	lastUpdated = time.Now()
	log.Printf("block-list refreshed — %d domains", len(blocked))
}

func handleDNS(w dns.ResponseWriter, r *dns.Msg, opts Options) {
	m := new(dns.Msg)
	m.SetReply(r)
	m.Authoritative = true

	for _, q := range r.Question {
		name := strings.TrimSuffix(strings.ToLower(q.Name), ".")

		// 1) Whitelist check
		if _, ok := whitelist[name]; ok {
			if opts.Verbose {
				log.Printf("[WL] %s", name)
			}
			forward(name, m)
			continue
		}

		// 2) Block check
		if _, hit := blocked[name]; hit && !opts.DryRun {
			rrA, _ := dns.NewRR(q.Name + " 0 IN A 0.0.0.0")
			rr6, _ := dns.NewRR(q.Name + " 0 IN AAAA ::")
			m.Answer = append(m.Answer, rrA, rr6)
			if opts.Verbose {
				log.Printf("[BL] %s", name)
			}
			continue
		}

		// 3) Forward everything else (and also on dry-run)
		forward(name, m)
	}

	_ = w.WriteMsg(m)
}

func forward(name string, m *dns.Msg) {
	ip4, ip6 := upstreamA(name)
	if ip4 != "" {
		rr, _ := dns.NewRR(name + ". 0 IN A " + ip4)
		m.Answer = append(m.Answer, rr)
	}
	if ip6 != "" {
		rr, _ := dns.NewRR(name + ". 0 IN AAAA " + ip6)
		m.Answer = append(m.Answer, rr)
	}
}

func upstreamA(name string) (string, string) {
	ips, _ := net.LookupIP(name)
	var v4, v6 string
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil && v4 == "" {
			v4 = ip4.String()
		} else if v6 == "" {
			v6 = ip.String()
		}
	}
	return v4, v6
}

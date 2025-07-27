
package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/idna"

	"github.com/miekg/dns"
)

type Options struct {
	Listen    string
	Interval  time.Duration
	DryRun    bool
	Verbose   bool
	Upstreams []string // "ip:port"
	MatchMode string   // "exact" or "suffix"
	BlockMode string   // "null" or "nxdomain"
}

var (
	// dynamic state
	currentOpts Options
	optsMu      sync.RWMutex

	blockedSet  atomic.Value // holds map[string]struct{}
	whitelistSet atomic.Value // holds map[string]struct{}

	srvUDP *dns.Server
	srvTCP *dns.Server

	cancelFunc  context.CancelFunc
	lastUpdated atomic.Value // time.Time
	runningFlag atomic.Bool

	httpClient = &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			IdleConnTimeout:       30 * time.Second,
			MaxIdleConns:          10,
		},
	}

	blocklistURLs = []string{
		"https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
		"https://adaway.org/hosts.txt",
	}

	etagCache = struct{
		sync.Mutex
		m map[string]string
	}{m: make(map[string]string)}
)

func init() {
	// defaults
	blockedSet.Store(make(map[string]struct{}))
	whitelistSet.Store(make(map[string]struct{}))
	lastUpdated.Store(time.Time{})

	// Register handler ONCE
	dns.HandleFunc(".", dynamicDNSHandler)
}

func dynamicDNSHandler(w dns.ResponseWriter, r *dns.Msg) {
	optsMu.RLock()
	opts := currentOpts
	optsMu.RUnlock()
	handleDNS(w, r, opts)
}

func Running() bool          { return runningFlag.Load() }
func LastUpdated() time.Time { return lastUpdated.Load().(time.Time) }

func exeDir() string {
	exe, _ := os.Executable()
	return filepath.Dir(exe)
}

func Start(o Options) error {
	if Running() {
		return nil
	}
	if o.Listen == "" {
		o.Listen = "127.0.0.1:53"
	}
	if len(o.Upstreams) == 0 {
		o.Upstreams = []string{"9.9.9.9:53", "149.112.112.112:53"} // Quad9 primary+secondary
	}
	if o.MatchMode == "" {
		o.MatchMode = "suffix"
	}
	if o.BlockMode == "" {
		o.BlockMode = "null"
	}

	optsMu.Lock()
	currentOpts = o
	optsMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	cancelFunc = cancel

	// Start scheduler (blocklist refresh)
	go scheduler(ctx, o.Interval)

	// Start UDP and TCP servers
	srvUDP = &dns.Server{Addr: o.Listen, Net: "udp"}
	srvTCP = &dns.Server{Addr: o.Listen, Net: "tcp"}

	go func() {
		if err := srvUDP.ListenAndServe(); err != nil && ctx.Err() == nil {
			log.Printf("dns udp listen error: %v", err)
		}
	}()
	go func() {
		if err := srvTCP.ListenAndServe(); err != nil && ctx.Err() == nil {
			log.Printf("dns tcp listen error: %v", err)
		}
	}()

	runningFlag.Store(true)
	log.Printf("proxy started on %s (dry=%v, upstreams=%v, match=%s, mode=%s)",
		o.Listen, o.DryRun, o.Upstreams, o.MatchMode, o.BlockMode)
	return nil
}

func Stop() {
	if !Running() {
		return
	}
	log.Println("proxy stopping...")
	if cancelFunc != nil {
		cancelFunc()
	}
	shutdown := func(s *dns.Server) {
		if s != nil {
			_ = s.Shutdown()
		}
	}
	go shutdown(srvUDP)
	go shutdown(srvTCP)

	time.Sleep(300 * time.Millisecond)
	srvUDP = nil
	srvTCP = nil
	cancelFunc = nil
	runningFlag.Store(false)
	log.Println("proxy stopped.")
}

// Live option toggles
func SetDryRun(d bool) {
	optsMu.Lock()
	currentOpts.DryRun = d
	optsMu.Unlock()
	log.Printf("dry-run set to %v", d)
}

func SetVerbose(v bool) {
	optsMu.Lock()
	currentOpts.Verbose = v
	optsMu.Unlock()
	log.Printf("verbose set to %v", v)
}

func scheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	refresh()
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			refresh()
		}
	}
}

func normalizeDomain(s string) (string, error) {
	s = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(s)), ".")
	if s == "" {
		return "", errors.New("empty domain")
	}
	ascii, err := idna.Lookup.ToASCII(s)
	if err != nil {
		return "", err
	}
	return ascii, nil
}

func loadWhitelist() {
	path := filepath.Join(exeDir(), "whitelist.txt")
	f, err := os.Open(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("whitelist read error: %v", err)
		}
		// Still swap an empty set to avoid stale values
		whitelistSet.Store(make(map[string]struct{}))
		return
	}
	defer f.Close()

	tmp := make(map[string]struct{})
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if d, err := normalizeDomain(line); err == nil {
			tmp[d] = struct{}{}
		}
	}
	if err := sc.Err(); err != nil {
		log.Printf("whitelist scan error: %v", err)
	}
	whitelistSet.Store(tmp)
	log.Printf("whitelist loaded — %d entries", len(tmp))
}

func fetchURL(url string) (*http.Response, error) {
	req, _ := http.NewRequest("GET", url, nil)
	etagCache.Lock()
	if et, ok := etagCache.m[url]; ok && et != "" {
		req.Header.Set("If-None-Match", et)
	}
	etagCache.Unlock()
	return httpClient.Do(req)
}

func refresh() {
	tmp := make(map[string]struct{})
	total := 0
	for _, url := range blocklistURLs {
		resp, err := fetchURL(url)
		if err != nil {
			log.Printf("fetch error %s: %v", url, err)
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusNotModified {
				return
			}
			if et := resp.Header.Get("ETag"); et != "" {
				etagCache.Lock()
				etagCache.m[url] = et
				etagCache.Unlock()
			}
			sc := bufio.NewScanner(resp.Body)
			for sc.Scan() {
				line := strings.TrimSpace(sc.Text())
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				// hosts format: "ip domain" or just "domain"
				fields := strings.Fields(line)
				var cand string
				switch len(fields) {
				case 1:
					cand = fields[0]
				default:
					cand = fields[1]
				}
				if d, err := normalizeDomain(cand); err == nil {
					tmp[d] = struct{}{}
					total++
				}
			}
			if err := sc.Err(); err != nil {
				log.Printf("parse error %s: %v", url, err)
			}
		}()
	}

	blockedSet.Store(tmp)
	loadWhitelist()
	lastUpdated.Store(time.Now())
	log.Printf("block-list refreshed — %d domains", len(tmp))
}

func getBlocked() map[string]struct{} {
	return blockedSet.Load().(map[string]struct{})
}
func getWhitelist() map[string]struct{} {
	return whitelistSet.Load().(map[string]struct{})
}

// match helper: exact or suffix with label boundary
func inSet(set map[string]struct{}, q string, mode string) bool {
	if _, ok := set[q]; ok {
		return true
	}
	if mode != "suffix" {
		return false
	}
	// suffix: check ".example.com" style boundaries
	for i := strings.Index(q, "."); i > 0; i = strings.Index(q, ".") {
		// BUG: strings.Index returns first '.'; we want next segments iteratively.
		// Instead, walk from first label to the right by trimming leftmost label repeatedly.
		break
	}
	// implement trim loop
	for {
		dot := strings.Index(q, ".")
		if dot == -1 {
			break
		}
		q = q[dot+1:]
		if _, ok := set[q]; ok {
			return true
		}
	}
	return false
}

func handleDNS(w dns.ResponseWriter, r *dns.Msg, opts Options) {
	if len(r.Question) == 0 {
		_ = w.WriteMsg(new(dns.Msg).SetRcode(r, dns.RcodeFormatError))
		return
	}

	q := r.Question[0]
	name, err := normalizeDomain(q.Name)
	if err != nil {
		_ = w.WriteMsg(new(dns.Msg).SetRcode(r, dns.RcodeNameError))
		return
	}

	// Whitelist first
	if inSet(getWhitelist(), name, opts.MatchMode) {
		if opts.Verbose {
			log.Printf("[WL] %s", name)
		}
		resp, err := forwardQuery(r, opts.Upstreams)
		if err != nil {
			_ = w.WriteMsg(new(dns.Msg).SetRcode(r, dns.RcodeServerFailure))
			return
		}
		_ = w.WriteMsg(resp)
		return
	}

	// Block check
	if inSet(getBlocked(), name, opts.MatchMode) && !opts.DryRun {
		if opts.Verbose {
			log.Printf("[BL] %s", name)
		}
		m := new(dns.Msg)
		m.SetReply(r)
		m.RecursionAvailable = true
		switch opts.BlockMode {
		case "nxdomain":
			m.Rcode = dns.RcodeNameError
		default: // "null"
			// Answer zeros for A/AAAA; for other types, return NODATA (NOERROR, empty answer)
			switch q.Qtype {
			case dns.TypeA:
				rr, _ := dns.NewRR(fmt.Sprintf("%s 0 IN A 0.0.0.0", dns.Fqdn(name)))
				m.Answer = append(m.Answer, rr)
			case dns.TypeAAAA:
				rr, _ := dns.NewRR(fmt.Sprintf("%s 0 IN AAAA ::", dns.Fqdn(name)))
				m.Answer = append(m.Answer, rr)
			default:
				// NODATA: no answers, no error
			}
		}
		_ = w.WriteMsg(m)
		return
	}

	// Forward (either not blocked, or DryRun)
	resp, err := forwardQuery(r, opts.Upstreams)
	if err != nil {
		_ = w.WriteMsg(new(dns.Msg).SetRcode(r, dns.RcodeServerFailure))
		return
	}
	_ = w.WriteMsg(resp)
}

func forwardQuery(r *dns.Msg, upstreams []string) (*dns.Msg, error) {
	// try UDP, fallback to TCP on truncation, rotate through upstreams
	var lastErr error
	cUDP := &dns.Client{Net: "udp", Timeout: 4 * time.Second, UDPSize: 4096}
	cTCP := &dns.Client{Net: "tcp", Timeout: 4 * time.Second}

	for _, up := range upstreams {
		resp, _, err := cUDP.Exchange(r, up)
		if err == nil {
			if resp.Truncated {
				// retry via TCP
				resp2, _, err2 := cTCP.Exchange(r, up)
				if err2 == nil {
					return resp2, nil
				}
				lastErr = err2
				continue
			}
			return resp, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no upstreams configured")
	}
	return nil, lastErr
}

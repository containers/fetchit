package engine

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

// statusAddrEnv, when set (e.g. ":8080"), makes fetchit serve a small HTTP
// status server exposing /healthz and /status. Left unset, no port is opened.
const statusAddrEnv = "FETCHIT_STATUS_ADDR"

// methodStatus is the per-method reconciliation state surfaced at /status.
type methodStatus struct {
	Kind     string     `json:"kind"`
	Name     string     `json:"name"`
	URL      string     `json:"url,omitempty"`
	Schedule string     `json:"schedule"`
	Runs     int        `json:"runs"`
	LastRun  *time.Time `json:"lastRun,omitempty"`
}

type statusRegistry struct {
	mu      sync.Mutex
	started time.Time
	methods map[string]*methodStatus
}

var status = &statusRegistry{methods: make(map[string]*methodStatus)}
var statusServerOnce sync.Once

func statusKey(m Method) string {
	return m.GetKind() + "/" + m.GetName() + "/" + m.GetTarget().url
}

// replace tracks only the current configuration, preserving statistics for
// methods whose identity remains unchanged across a reload.
func (r *statusRegistry) replace(methods map[Method]SchedInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started.IsZero() {
		r.started = time.Now()
	}
	next := make(map[string]*methodStatus, len(methods))
	for m, sched := range methods {
		key := statusKey(m)
		entry := r.methods[key]
		if entry == nil {
			entry = &methodStatus{Kind: m.GetKind(), Name: m.GetName(), URL: m.GetTarget().url}
		}
		entry.Schedule = sched.schedule
		next[key] = entry
	}
	r.methods = next
}

// recordRun stamps the most recent execution of a method.
func (r *statusRegistry) recordRun(m Method) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.methods[statusKey(m)]
	if !ok {
		return
	}
	now := time.Now()
	s.Runs++
	s.LastRun = &now
}

func (r *statusRegistry) writeJSON(w http.ResponseWriter) {
	r.mu.Lock()
	// Copy by value under the lock so concurrent recordRun/register calls
	// cannot mutate what we encode below.
	ms := make([]methodStatus, 0, len(r.methods))
	for _, s := range r.methods {
		ms = append(ms, *s)
	}
	started := r.started
	r.mu.Unlock()

	sort.Slice(ms, func(i, j int) bool {
		if ms[i].Kind != ms[j].Kind {
			return ms[i].Kind < ms[j].Kind
		}
		if ms[i].Name != ms[j].Name {
			return ms[i].Name < ms[j].Name
		}
		return ms[i].URL < ms[j].URL
	})

	// Before the first method registers, started is zero; report a sane state
	// instead of an uptime measured from year 1.
	state := "running"
	var uptime int64
	if started.IsZero() {
		state = "initializing"
	} else {
		uptime = int64(time.Since(started).Seconds())
	}

	var startedAt *time.Time
	if !started.IsZero() {
		startedAt = &started
	}
	resp := struct {
		Status        string         `json:"status"`
		StartedAt     *time.Time     `json:"startedAt,omitempty"`
		UptimeSeconds int64          `json:"uptimeSeconds"`
		Methods       []methodStatus `json:"methods"`
	}{
		Status:        state,
		StartedAt:     startedAt,
		UptimeSeconds: uptime,
		Methods:       ms,
	}

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(resp); err != nil {
		logger.Warnf("status: failed to encode response: %v", err)
	}
}

// startStatusServer launches the status/health HTTP server in a goroutine if
// FETCHIT_STATUS_ADDR is set. It is a no-op otherwise.
func startStatusServer() {
	addr := os.Getenv(statusAddrEnv)
	if addr == "" {
		return
	}
	statusServerOnce.Do(func() {
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			logger.Errorf("status server listen error: %v", err)
			return
		}
		server := newStatusServer(addr, status)
		logger.Infof("Status server listening on %s (/healthz, /status)", listener.Addr())
		go func() {
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Errorf("status server error: %v", err)
			}
		}()
	})
}

func newStatusServer(addr string, registry *statusRegistry) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := w.Write([]byte("ok\n")); err != nil {
			logger.Debugf("status: failed to write health response: %v", err)
		}
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, _ *http.Request) { registry.writeJSON(w) })
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second}
}

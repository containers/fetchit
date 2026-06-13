package engine

import (
	"encoding/json"
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

func statusKey(m Method) string {
	return m.GetKind() + "/" + m.GetName() + "/" + m.GetTarget().url
}

// register records a scheduled method so it shows up at /status before its
// first run.
func (r *statusRegistry) register(m Method, schedule string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.started.IsZero() {
		r.started = time.Now()
	}
	r.methods[statusKey(m)] = &methodStatus{
		Kind:     m.GetKind(),
		Name:     m.GetName(),
		URL:      m.GetTarget().url,
		Schedule: schedule,
	}
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
		return ms[i].Name < ms[j].Name
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

	resp := struct {
		Status        string         `json:"status"`
		StartedAt     time.Time      `json:"startedAt"`
		UptimeSeconds int64          `json:"uptimeSeconds"`
		Methods       []methodStatus `json:"methods"`
	}{
		Status:        state,
		StartedAt:     started,
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
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		status.writeJSON(w)
	})
	go func() {
		logger.Infof("Status server listening on %s (/healthz, /status)", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			logger.Errorf("status server error: %v", err)
		}
	}()
}

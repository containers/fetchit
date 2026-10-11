package engine

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const metricsAddrEnv = "FETCHIT_METRICS_ADDR"
const hostnameEnv = "FETCHIT_HOSTNAME"

// No repository, method name, revision, path, or error text becomes a label.
// The set of kinds and states is bounded independently of configuration.
func metricKind(kind string) string {
	switch kind {
	case rawMethod, kubeMethod, ansibleMethod, filetransferMethod, systemdMethod, quadletMethod, imageMethod, pruneMethod, configFileMethod:
		return kind
	default:
		return "other"
	}
}

type gitMetric struct {
	kind     string
	state    string
	observed time.Time
	run      uint64
}
type metricsRegistry struct {
	mu       sync.Mutex
	hostname string
	methods  map[Method]*gitMetric
}

var metrics = &metricsRegistry{}
var metricsServerOnce sync.Once
var hostnamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

func configuredHostname() (string, error) {
	host := os.Getenv(hostnameEnv)
	if host == "" {
		var err error
		host, err = os.Hostname()
		if err != nil {
			return "", err
		}
	}
	if len(host) > 253 || !hostnamePattern.MatchString(host) {
		return "", errors.New("FETCHIT_HOSTNAME must be a hostname of 1–253 ASCII letters, digits, dots, underscores or hyphens")
	}
	return host, nil
}

func gitBacked(m Method) bool {
	t := m.GetTarget()
	return t != nil && t.url != "" && !t.disconnected
}

func (r *metricsRegistry) replace(methods map[Method]SchedInfo, hostname string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hostname = hostname
	r.methods = make(map[Method]*gitMetric, len(methods))
	for m := range methods {
		state := "not_applicable"
		if gitBacked(m) {
			state = "unknown"
		}
		r.methods[m] = &gitMetric{kind: metricKind(m.GetKind()), state: state}
	}
}

type observationKey struct{}
type gitObservation struct {
	registry *metricsRegistry
	method   Method
	entry    *gitMetric
	run      uint64
}

// Each run gets a generation-specific token. A retired run cannot publish into
// a replacement configuration, even if its name and URL are unchanged. An
// overlapping older run also cannot overwrite a newer run's state.
func (r *metricsRegistry) begin(ctx context.Context, m Method) context.Context {
	r.mu.Lock()
	entry := r.methods[m]
	var run uint64
	if entry != nil && entry.state != "not_applicable" {
		entry.state = "unknown"
		entry.run++
		run = entry.run
	}
	r.mu.Unlock()
	return context.WithValue(ctx, observationKey{}, gitObservation{r, m, entry, run})
}

// Process returns no error, and its callers cannot infer success from return.
// Only the Git reconciliation paths explicitly publish verified observations
// and successful application/baseline completion through this context token.
func observeGit(ctx context.Context, state string, fetched bool) {
	observation, ok := ctx.Value(observationKey{}).(gitObservation)
	if !ok || observation.entry == nil || ctx.Err() != nil {
		return
	}
	r := observation.registry
	r.mu.Lock()
	defer r.mu.Unlock()
	entry := r.methods[observation.method]
	if entry != observation.entry || entry.run != observation.run || entry.state == "not_applicable" {
		return
	}
	entry.state = state
	if fetched {
		entry.observed = time.Now()
	}
}

func (r *metricsRegistry) writeMetrics(w http.ResponseWriter) {
	type totals struct {
		configured int
		states     map[string]int
		oldest     time.Time
		git        int
	}
	r.mu.Lock()
	hostname := r.hostname
	counts := map[string]*totals{}
	for _, entry := range r.methods {
		group := counts[entry.kind]
		if group == nil {
			group = &totals{states: map[string]int{}}
			counts[entry.kind] = group
		}
		group.configured++
		if entry.state == "not_applicable" {
			continue
		}
		group.states[entry.state]++
		if group.git == 0 || entry.observed.Before(group.oldest) {
			group.oldest = entry.observed
		}
		group.git++
	}
	r.mu.Unlock()
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	var output strings.Builder
	fmt.Fprintf(&output, "# HELP fetchit_host_info Configured host identity.\n# TYPE fetchit_host_info gauge\nfetchit_host_info{hostname=%q} 1\n", hostname)
	output.WriteString("# HELP fetchit_configured_methods Number of configured method declarations.\n# TYPE fetchit_configured_methods gauge\n")
	for _, kind := range kinds {
		fmt.Fprintf(&output, "fetchit_configured_methods{hostname=%q,kind=%q} %d\n", hostname, kind, counts[kind].configured)
	}
	output.WriteString("# HELP fetchit_git_methods Number of connected Git method declarations by synchronization state.\n# TYPE fetchit_git_methods gauge\n")
	for _, kind := range kinds {
		if counts[kind].git == 0 {
			continue
		}
		for _, state := range []string{"synced", "out_of_sync", "unknown"} {
			fmt.Fprintf(&output, "fetchit_git_methods{hostname=%q,kind=%q,state=%q} %d\n", hostname, kind, state, counts[kind].states[state])
		}
	}
	output.WriteString("# HELP fetchit_git_oldest_observation_timestamp_seconds Oldest successful Git observation per kind, or zero if any method has never observed Git.\n# TYPE fetchit_git_oldest_observation_timestamp_seconds gauge\n")
	for _, kind := range kinds {
		if counts[kind].git == 0 {
			continue
		}
		timestamp := float64(0)
		if !counts[kind].oldest.IsZero() {
			timestamp = float64(counts[kind].oldest.UnixNano()) / 1e9
		}
		fmt.Fprintf(&output, "fetchit_git_oldest_observation_timestamp_seconds{hostname=%q,kind=%q} %.3f\n", hostname, kind, timestamp)
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("X-Fetchit-Hostname", hostname)
	_, _ = fmt.Fprint(w, output.String())
}

func newMetricsServer(addr string, r *metricsRegistry) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) { r.writeMetrics(w) })
	return &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
}

func startMetricsServer() error {
	addr := os.Getenv(metricsAddrEnv)
	if addr == "" {
		return nil
	}
	var result error
	metricsServerOnce.Do(func() {
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			result = fmt.Errorf("metrics listener: %w", err)
			return
		}
		server := newMetricsServer(addr, metrics)
		logger.Infof("Metrics server listening on %s (/metrics)", listener.Addr())
		go func() {
			if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
				logger.Errorf("metrics server: %v", err)
			}
		}()
	})
	return result
}

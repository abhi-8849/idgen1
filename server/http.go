// Package server exposes the generator over HTTP.
package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/abhi-8849/idgen1/snowflake"
)

const maxCount = 1000

// Server wires HTTP routes to one Generator.
type Server struct {
	gen *snowflake.Generator
	// lost is the lease's Done channel. We only read it: it is closed by the lease
	// owner when the worker id is lost, which turns /healthz into a 503.
	lost <-chan struct{}
	vars http.Handler // serves /debug/vars
	log  *slog.Logger
}

// New returns a Server. lost may be nil (lease can never be lost).
func New(gen *snowflake.Generator, lost <-chan struct{}, vars http.Handler, log *slog.Logger) *Server {
	return &Server{gen: gen, lost: lost, vars: vars, log: log}
}

// Handler returns the routes for this server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/ids", s.handleIDs)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.Handle("GET /debug/vars", s.vars)
	return mux
}

// handleIDs serves GET /v1/ids?count=n.
func (s *Server) handleIDs(w http.ResponseWriter, r *http.Request) {
	count, err := parseCount(r.URL.Query().Get("count"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	ids := make([]string, 0, count)
	for range count {
		id, err := s.gen.NextID(r.Context())
		if err != nil {
			status := statusFor(err)
			if status == http.StatusServiceUnavailable {
				s.log.Warn("request refused", "err", err)
			}
			http.Error(w, err.Error(), status)
			return
		}
		// Strings, because JavaScript loses precision above 2^53.
		ids = append(ids, strconv.FormatUint(id, 10))
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string][]string{"ids": ids})
}

// handleHealth reports 503 once the worker id lease is lost.
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	select {
	case <-s.lost: // closed: the lease is gone, so this node must not serve IDs
		http.Error(w, "lease lost", http.StatusServiceUnavailable)
	default: // open, or nil (never lost)
		w.Write([]byte("ok"))
	}
}

// parseCount returns 1 for an empty value and rejects anything outside 1..maxCount.
func parseCount(v string) (int, error) {
	if v == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > maxCount {
		return 0, errors.New("count must be an integer from 1 to " + strconv.Itoa(maxCount))
	}
	return n, nil
}

// statusFor maps generator errors to HTTP codes: refusals are 503, the rest 500.
func statusFor(err error) int {
	if errors.Is(err, snowflake.ErrClockBackwards) || errors.Is(err, snowflake.ErrLeaseLost) {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

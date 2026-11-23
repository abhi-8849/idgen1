package server_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/abhi-8849/idgen1/server"
	"github.com/abhi-8849/idgen1/snowflake"
)

func newServer(t *testing.T, lost chan struct{}) *httptest.Server {
	t.Helper()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	metrics := server.NewMetrics(log)
	gen, err := snowflake.New(snowflake.Config{Lost: lost, Hooks: metrics}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(server.New(gen, lost, metrics, log).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func status(t *testing.T, url string) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestIDsReturnsStringsInOrder(t *testing.T) {
	ts := newServer(t, nil)
	resp, err := http.Get(ts.URL + "/v1/ids?count=3")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct{ IDs []string }
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || len(body.IDs) != 3 {
		t.Fatalf("want 3 ids, got %v (err %v)", body.IDs, err)
	}
	var last uint64
	for _, s := range body.IDs {
		id, err := strconv.ParseUint(s, 10, 64)
		if err != nil || id <= last {
			t.Fatalf("ids must be increasing numeric strings: %v", body.IDs)
		}
		last = id
	}
}

func TestBadCountIs400(t *testing.T) {
	ts := newServer(t, nil)
	for _, c := range []string{"0", "1001", "abc"} {
		if got := status(t, ts.URL+"/v1/ids?count="+c); got != http.StatusBadRequest {
			t.Errorf("count=%s: want 400, got %d", c, got)
		}
	}
}

func TestLostLeaseGives503(t *testing.T) {
	lost := make(chan struct{})
	ts := newServer(t, lost)
	if got := status(t, ts.URL+"/healthz"); got != http.StatusOK {
		t.Fatalf("healthz before loss: want 200, got %d", got)
	}
	close(lost)
	for _, path := range []string{"/healthz", "/v1/ids"} {
		if got := status(t, ts.URL+path); got != http.StatusServiceUnavailable {
			t.Errorf("%s after loss: want 503, got %d", path, got)
		}
	}
}

func TestDebugVarsShowsFourCounters(t *testing.T) {
	ts := newServer(t, nil)
	status(t, ts.URL+"/v1/ids?count=2")
	resp, err := http.Get(ts.URL + "/debug/vars")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var vars map[string]int
	if err := json.NewDecoder(resp.Body).Decode(&vars); err != nil || len(vars) != 4 || vars["ids_issued_total"] != 2 {
		t.Fatalf("unexpected counters %v (err %v)", vars, err)
	}
}

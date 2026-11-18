package main

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFlushKeepsRowsWhileClickHouseIsUnreachableThenDeliversThem(t *testing.T) {
	addr := unusedAddress(t)
	q := &rowQueue{}
	q.target.Store(&clickhouseTarget{URL: "http://" + addr, Database: "cliproxy"})
	pending := newBatch()
	pending.add(tableRow{table: "prompts", row: promptRow{RequestID: "req-1"}})

	schemaReadyFor := q.flush(pending, clickhouseTarget{})
	if pending.rows != 1 || !pending.retrying {
		t.Fatalf("after refused connection: rows=%d retrying=%v, want 1 true", pending.rows, pending.retrying)
	}

	recorded := serveAt(t, addr, http.StatusOK)
	q.flush(pending, schemaReadyFor)
	if pending.rows != 0 || pending.retrying {
		t.Fatalf("after ClickHouse came up: rows=%d retrying=%v, want 0 false", pending.rows, pending.retrying)
	}
	if got := recorded.rows("prompts"); len(got) != 1 {
		t.Fatalf("delivered prompts = %v, want 1 row", got)
	}
}

func TestFlushDropsRowsClickHouseRejects(t *testing.T) {
	addr := unusedAddress(t)
	serveAt(t, addr, http.StatusBadRequest)
	q := &rowQueue{}
	target := clickhouseTarget{URL: "http://" + addr, Database: "cliproxy"}
	q.target.Store(&target)
	pending := newBatch()
	pending.add(tableRow{table: "usage", row: usageRow{RequestID: "req-1"}})

	q.flush(pending, target)
	if pending.rows != 0 || pending.retrying {
		t.Fatalf("rows=%d retrying=%v, want 0 false", pending.rows, pending.retrying)
	}
}

func unusedAddress(t *testing.T) string {
	t.Helper()
	listener, errListen := net.Listen("tcp", "127.0.0.1:0")
	if errListen != nil {
		t.Fatal(errListen)
	}
	addr := listener.Addr().String()
	if errClose := listener.Close(); errClose != nil {
		t.Fatal(errClose)
	}
	return addr
}

func serveAt(t *testing.T, addr string, status int) *recordedQueries {
	t.Helper()
	listener, errListen := net.Listen("tcp", addr)
	if errListen != nil {
		t.Fatal(errListen)
	}
	recorded := &recordedQueries{byTable: map[string][]string{}}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		body, _ := io.ReadAll(r.Body)
		recorded.add(r.URL.Query().Get("query"), string(body))
	}))
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)
	return recorded
}

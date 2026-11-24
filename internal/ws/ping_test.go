/*
FILE: internal/ws/ping_test.go

DESCRIPTION:
Client ping (Config.PingInterval): a quiet connection whose server answers
pings stays up — every pong refreshes the read deadline; a connection whose
server stopped answering (socket open, nothing arriving) is dropped by the
short ReadTimeout and redialled.
*/

package ws

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// pingTestServer counts connections; answerPings=false makes it ignore the
// client's pings (a stalled connection from the client's point of view).
func pingTestServer(t *testing.T, answerPings bool) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var conns atomic.Int32
	var upgrader websocket.Upgrader
	var srv *httptest.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var conn, err = upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conns.Add(1)
		if !answerPings {
			conn.SetPingHandler(func(string) error { return nil })
		}
		for {
			if _, _, err = conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &conns
}

func pingTestConfig() Config {
	var cfg Config = testConfig()
	cfg.PingInterval = 50 * time.Millisecond
	cfg.ReadTimeout = 150 * time.Millisecond
	return cfg
}

func TestClientPingKeepsQuietConnectionAlive(t *testing.T) {
	var srv, conns = pingTestServer(t, true)
	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	var url string = "ws" + strings.TrimPrefix(srv.URL, "http")
	var conn *Conn = NewConn(pingTestConfig(), func(context.Context) (string, error) { return url, nil }, nil, nil)
	conn.Start(ctx)
	defer conn.Close()

	time.Sleep(700 * time.Millisecond) // ~4.5 read timeouts without any data
	if n := conns.Load(); n != 1 {
		t.Fatalf("connections = %d, want 1: pongs must keep a quiet connection alive", n)
	}
}

func TestClientPingDetectsStalledConnection(t *testing.T) {
	var srv, conns = pingTestServer(t, false)
	var ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	var url string = "ws" + strings.TrimPrefix(srv.URL, "http")
	var conn *Conn = NewConn(pingTestConfig(), func(context.Context) (string, error) { return url, nil }, nil, nil)
	conn.Start(ctx)
	defer conn.Close()

	var deadline time.Time = time.Now().Add(3 * time.Second)
	for conns.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("connections = %d, want a redial of the stalled connection", conns.Load())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

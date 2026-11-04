package api

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestShutdownDrainParsesEnv(t *testing.T) {
	cases := map[string]time.Duration{"": 0, "  ": 0, "abc": 0, "-1s": 0, "0": 0, "5m": 5 * time.Minute, " 90s ": 90 * time.Second}
	for raw, want := range cases {
		t.Setenv(ShutdownDrainEnv, raw)
		if got := ShutdownDrain(); got != want {
			t.Fatalf("ShutdownDrain(%q) = %v, want %v", raw, got, want)
		}
	}
}

type drainTestServer struct {
	server   *Server
	addr     string
	started  chan struct{}
	release  chan struct{}
	reqDone  chan error
	stopDone chan error
}

// startDrainTestServer starts a real Server with a blocking handler, issues one
// in-flight request and then calls Stop.
func startDrainTestServer(t *testing.T) *drainTestServer {
	t.Helper()
	gin.SetMode(gin.TestMode)
	ln, errListen := net.Listen("tcp", "127.0.0.1:0")
	if errListen != nil {
		t.Fatalf("listen: %v", errListen)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	d := &drainTestServer{
		server:   NewServer(&config.Config{Host: "127.0.0.1", Port: port}, nil, nil, ""),
		addr:     fmt.Sprintf("127.0.0.1:%d", port),
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		reqDone:  make(chan error, 1),
		stopDone: make(chan error, 1),
	}
	t.Cleanup(func() {
		select {
		case <-d.release:
		default:
			close(d.release)
		}
	})
	engine := gin.New()
	engine.GET("/slow", func(c *gin.Context) {
		close(d.started)
		<-d.release
		c.String(http.StatusOK, "ok")
	})
	d.server.server.Handler = engine
	go func() { _ = d.server.Start() }()

	var errDial error
	for i := 0; i < 100; i++ {
		var conn net.Conn
		if conn, errDial = net.Dial("tcp", d.addr); errDial == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if errDial != nil {
		t.Fatalf("server did not start: %v", errDial)
	}

	go func() {
		resp, errGet := http.Get("http://" + d.addr + "/slow")
		if errGet != nil {
			d.reqDone <- errGet
			return
		}
		defer func() { _ = resp.Body.Close() }()
		body, errRead := io.ReadAll(resp.Body)
		if errRead == nil && string(body) != "ok" {
			errRead = fmt.Errorf("body = %q", body)
		}
		d.reqDone <- errRead
	}()
	<-d.started
	go func() { d.stopDone <- d.server.Stop(context.Background()) }()
	return d
}

func waitErr(t *testing.T, ch <-chan error, what string) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		return nil
	}
}

func TestServerStopDrainCompletesInFlightAndRefusesNew(t *testing.T) {
	t.Setenv(ShutdownDrainEnv, "1m")
	d := startDrainTestServer(t)

	refused := false
	for i := 0; i < 200 && !refused; i++ {
		conn, errDial := net.Dial("tcp", d.addr)
		if errDial != nil {
			refused = true
			break
		}
		_ = conn.Close()
		time.Sleep(5 * time.Millisecond)
	}
	if !refused {
		t.Fatal("new connections are still accepted during drain")
	}
	select {
	case errStop := <-d.stopDone:
		t.Fatalf("Stop returned before the in-flight request finished: %v", errStop)
	default:
	}

	close(d.release)
	if errReq := waitErr(t, d.reqDone, "in-flight request"); errReq != nil {
		t.Fatalf("in-flight request failed during drain: %v", errReq)
	}
	if errStop := waitErr(t, d.stopDone, "Stop"); errStop != nil {
		t.Fatalf("Stop error = %v", errStop)
	}
}

func TestServerStopDrainDeadlineFallsBackToClose(t *testing.T) {
	t.Setenv(ShutdownDrainEnv, "50ms")
	d := startDrainTestServer(t)
	if errStop := waitErr(t, d.stopDone, "Stop"); errStop != nil {
		t.Fatalf("Stop error = %v", errStop)
	}
	if errReq := waitErr(t, d.reqDone, "in-flight request"); errReq == nil {
		t.Fatal("in-flight request completed although the drain deadline passed")
	}
}

func TestServerStopWithoutDrainClosesImmediately(t *testing.T) {
	for _, raw := range []string{"", "not-a-duration", "0s"} {
		t.Run(fmt.Sprintf("env=%q", raw), func(t *testing.T) {
			t.Setenv(ShutdownDrainEnv, raw)
			d := startDrainTestServer(t)
			if errStop := waitErr(t, d.stopDone, "Stop"); errStop != nil {
				t.Fatalf("Stop error = %v", errStop)
			}
			if errReq := waitErr(t, d.reqDone, "in-flight request"); errReq == nil {
				t.Fatal("in-flight request completed; want immediate close")
			}
		})
	}
}

package api

import (
	"context"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
)

// ShutdownDrainEnv names the opt-in graceful shutdown drain window.
const ShutdownDrainEnv = "CLIPROXY_SHUTDOWN_DRAIN"

// ShutdownDrain returns the drain window from CLIPROXY_SHUTDOWN_DRAIN (a Go
// duration such as "5m"). Unset, empty, invalid or non-positive values return 0,
// which keeps the immediate-close shutdown.
func ShutdownDrain() time.Duration {
	raw := strings.TrimSpace(os.Getenv(ShutdownDrainEnv))
	if raw == "" {
		return 0
	}
	drain, errParse := time.ParseDuration(raw)
	if errParse != nil {
		log.Warnf("ignoring invalid %s=%q: %v", ShutdownDrainEnv, raw, errParse)
		return 0
	}
	if drain <= 0 {
		return 0
	}
	return drain
}

// drainHTTPServer lets in-flight requests (including SSE streams) finish for up
// to drain, then force-closes whatever is left. Listeners are already closed.
func (s *Server) drainHTTPServer(ctx context.Context, drain time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	drainCtx, cancel := context.WithTimeout(ctx, drain)
	defer cancel()
	log.Infof("draining in-flight requests for up to %s", drain)
	errShutdown := s.server.Shutdown(drainCtx)
	if errShutdown == nil {
		return nil
	}
	log.Warnf("shutdown drain ended before requests finished, closing remaining connections: %v", errShutdown)
	return s.server.Close()
}

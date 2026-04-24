package obs

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// Stats holds observability data about a running node.
type Stats struct {
	mu         sync.RWMutex
	PeerCount  int
	RouteCount int
	Uptime     time.Time
	Connected  bool
}

// NewStats creates a new Stats tracker.
func NewStats() *Stats {
	return &Stats{Uptime: time.Now(), Connected: true}
}

// Update sets the current stats.
func (s *Stats) Update(peers, routes int) {
	s.mu.Lock()
	s.PeerCount = peers
	s.RouteCount = routes
	s.mu.Unlock()
}

// HealthServer serves /health and /metrics over HTTP.
type HealthServer struct {
	srv    *http.Server
	stats  *Stats
	logger *slog.Logger
}

// NewHealthServer creates a health check HTTP server.
func NewHealthServer(addr string, stats *Stats) *HealthServer {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	hs := &HealthServer{
		stats:  stats,
		logger: logger,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", hs.handleHealth)
	mux.HandleFunc("/metrics", hs.handleMetrics)

	hs.srv = &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	return hs
}

// Start begins serving health checks.
func (hs *HealthServer) Start() error {
	ln, err := net.Listen("tcp", hs.srv.Addr)
	if err != nil {
		return err
	}
	hs.logger.Info("health server started", "addr", hs.srv.Addr)
	go hs.srv.Serve(ln)
	return nil
}

// Stop gracefully shuts down the health server.
func (hs *HealthServer) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	hs.srv.Shutdown(ctx)
}

func (hs *HealthServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	hs.stats.mu.RLock()
	connected := hs.stats.Connected
	uptime := time.Since(hs.stats.Uptime)
	hs.stats.mu.RUnlock()

	if !connected {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "unhealthy",
			"uptime": uptime.String(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "healthy",
		"uptime": uptime.String(),
	})
}

func (hs *HealthServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	hs.stats.mu.RLock()
	peers := hs.stats.PeerCount
	routes := hs.stats.RouteCount
	uptime := time.Since(hs.stats.Uptime)
	hs.stats.mu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"uptime_seconds": int(uptime.Seconds()),
		"peer_count":     peers,
		"route_count":    routes,
	})
}

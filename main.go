// Command personal-time-tracker serves a single-user weekly timesheet.
package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"personaltimetracker/internal/store"
	"personaltimetracker/internal/web"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "probe the running server and exit 0 if healthy")
	flag.Parse()

	port := 24871
	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p < 1 || p > 65535 {
			slog.Error("PORT must be a number from 1 to 65535", "PORT", v)
			os.Exit(1)
		}
		port = p
	}
	if *healthcheck {
		os.Exit(probe(port))
	}
	dataDir := os.Getenv("DATA_DIR")
	if dataDir == "" {
		dataDir = "/data"
	}

	st, err := store.Open(dataDir, time.Now)
	if err != nil {
		slog.Error("can't open data", "err", err)
		os.Exit(1)
	}
	snapshot := func() {
		if err := st.EnsureDailySnapshot(); err != nil {
			slog.Error("daily snapshot failed", "err", err)
		}
	}
	snapshot()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				snapshot()
			}
		}
	}()

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(port),
		Handler:           web.NewHandler(st, port),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	slog.Info("Personal Time Tracker is running", "url", "http://localhost:"+strconv.Itoa(port), "data", dataDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// probe is the container healthcheck: the runtime image has no curl.
func probe(port int) int {
	addr := "127.0.0.1:" + strconv.Itoa(port)
	req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/healthz", nil)
	if err != nil {
		return 1
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

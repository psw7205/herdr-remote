package main

import (
	"context"
	"errors"
	"flag"
	"herdr-remote/internal/command"
	"herdr-remote/internal/herdr"
	"herdr-remote/internal/httpapi"
	"herdr-remote/internal/session"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		slog.Error("home unavailable", "error", err)
		os.Exit(1)
	}
	socket := flag.String("herdr-socket", filepath.Join(home, ".config", "herdr", "herdr.sock"), "existing Herdr API socket")
	claudeDir := flag.String("claude-dir", filepath.Join(home, ".claude"), "Claude native transcript root")
	receiptDir := flag.String("receipts-dir", filepath.Join(home, ".local", "state", "herdr-remote", "receipts"), "durable command receipts")
	listen := flag.String("listen", "127.0.0.1:8787", "loopback HTTP address")
	origins := flag.String("origins", "http://127.0.0.1:8787", "comma-separated exact browser Origins")
	staticDir := flag.String("static", "web/dist", "built client directory")
	tailnetHost := flag.String("tailnet-host", "", "Tailscale Serve DNS host without a trailing dot")
	tailnetLogin := flag.String("tailnet-login", "", "only Tailscale user login allowed through Serve")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || host != "localhost" && host != "127.0.0.1" && host != "::1" {
		slog.Error("Bridge must bind a loopback address")
		os.Exit(2)
	}
	if _, err = os.Stat(filepath.Join(*staticDir, "index.html")); err != nil {
		slog.Error("client build missing", "path", *staticDir, "error", err)
		os.Exit(2)
	}
	receipts, err := command.Open(*receiptDir)
	if err != nil {
		slog.Error("command receipts unavailable", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	registry := session.NewRegistry(ctx, herdr.NewGateway(*socket), *claudeDir)
	if err = registry.Refresh(ctx); err != nil {
		slog.Error("Herdr unavailable", "error", err)
		os.Exit(1)
	}
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := registry.Refresh(ctx); err != nil {
					slog.Warn("Herdr refresh failed", "error", err)
				}
			}
		}
	}()
	api := httpapi.New(registry, receipts, strings.Split(*origins, ","), *staticDir)
	api.SetTailnetIdentity(*tailnetHost, *tailnetLogin)
	if err = api.ValidateOrigins(); err != nil {
		slog.Error("invalid Origin config", "error", err)
		os.Exit(2)
	}
	server := &http.Server{Addr: *listen, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, MaxHeaderBytes: 16 << 10}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		slog.Error("Bridge listen failed", "error", err)
		os.Exit(1)
	}
	defer listener.Close()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	slog.Info("Bridge listening", "address", *listen)
	if err = server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("Bridge failed", "error", err)
		os.Exit(1)
	}
}

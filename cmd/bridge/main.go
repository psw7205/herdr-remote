package main

import (
	"context"
	"errors"
	"flag"
	"github.com/psw7205/herdr-remote/internal/command"
	"github.com/psw7205/herdr-remote/internal/herdr"
	"github.com/psw7205/herdr-remote/internal/httpapi"
	"github.com/psw7205/herdr-remote/internal/session"
	"github.com/psw7205/herdr-remote/internal/tailnet"
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
	tailnetHost := flag.String("tailnet-host", tailnet.Auto, "Tailscale Serve DNS host: auto (from tailscale status), off (localhost-only), or an explicit host")
	tailnetLogin := flag.String("tailnet-login", tailnet.Auto, "only Tailscale user login allowed through Serve: auto (node owner) or an explicit login")
	tailscaleBin := flag.String("tailscale-bin", "", "Tailscale CLI path (default: PATH, then well-known install locations)")
	var projectRoots []string
	flag.Func("project-root", "absolute folder whose Git repo (itself or one level below) may host a new session; repeatable. Without one, only open workspaces get new tabs", func(value string) error {
		projectRoots = append(projectRoots, value)
		return nil
	})
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
	gateway := herdr.NewGateway(*socket)
	starter, err := session.NewStarter(gateway, projectRoots)
	if err != nil {
		slog.Error("invalid -project-root", "error", err)
		os.Exit(2)
	}
	registry := session.NewRegistry(ctx, gateway, *claudeDir)
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
	identity := resolveTailnet(ctx, *tailnetHost, *tailnetLogin, *tailscaleBin)
	allowed, dropped := identity.DropUnverifiedOrigins(strings.Split(*origins, ","))
	for _, origin := range dropped {
		slog.Warn("tailnet setup incomplete", "reason", "dropping -origins entry without a resolved tailnet owner login", "origin", origin)
	}
	if identity.Host != "" && identity.Login != "" {
		// ValidateOrigins only accepts the tailnet Origin with an owner login;
		// without one every tailnet request is rejected anyway.
		allowed = tailnet.WithOrigin(allowed, identity.Host)
	}
	api := httpapi.New(registry, receipts, allowed, *staticDir)
	api.SetTailnetIdentity(identity.Host, identity.Login)
	api.SetStarter(starter)
	if err = api.ValidateOrigins(); err != nil {
		slog.Error("invalid Origin config", "error", err)
		os.Exit(2)
	}
	// No WriteTimeout: WebSocket streams are long-lived and a session start
	// waits up to Herdr's 30s agent readiness; each handler bounds itself.
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
	slog.Info("Bridge listening", "address", *listen, "origins", allowed, "project_roots", len(projectRoots))
	if err = server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("Bridge failed", "error", err)
		os.Exit(1)
	}
}

// resolveTailnet reads `tailscale status --json` once at startup when a flag
// is auto. Failures degrade to localhost-only instead of exiting.
func resolveTailnet(ctx context.Context, hostFlag, loginFlag, bin string) tailnet.Identity {
	var run tailnet.Runner
	var cliErr error
	if hostFlag != tailnet.Off && (hostFlag == tailnet.Auto || loginFlag == tailnet.Auto) {
		var path string
		if path, cliErr = tailnet.FindCLI(bin); cliErr == nil {
			run = tailnet.ExecRunner(path)
		}
	}
	// ExecRunner adds at most its WaitDelay (1s) after this timeout.
	resolveCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	identity := tailnet.Resolve(resolveCtx, hostFlag, loginFlag, run, cliErr)
	for _, warning := range identity.Warnings {
		slog.Warn("tailnet setup incomplete", "reason", warning)
	}
	slog.Info("tailnet identity", "host", identity.Host, "host_source", identity.HostSource, "login", identity.Login, "login_source", identity.LoginSource)
	return identity
}

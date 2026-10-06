package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/ssh"
	"github.com/charmbracelet/colorprofile"
)

type config struct {
	Content, Listen, HostKey, Lang, Theme    string
	SiteURL, Title                           string
	Idle, Duration                           time.Duration
	MaxSessions, MaxConnections, PerIP, Rate int
}

func defaults() config {
	return config{Content: defaultContentDir(), SiteURL: envOr("BLOG_SITE_URL", defaultSite.URL), Title: envOr("BLOG_TITLE", defaultSite.Title), Listen: "127.0.0.1:2222", HostKey: ".ssh/host_ed25519", Lang: os.Getenv("BLOG_TERMINAL_LANG"), Theme: "auto", Idle: 5 * time.Minute, Duration: time.Hour, MaxSessions: 32, MaxConnections: 64, PerIP: 4, Rate: 12}
}

// localContent is a git-ignored link to the Blog's posts for development.
const localContent = "content"

func defaultContentDir() string {
	if dir := os.Getenv("BLOG_CONTENT_DIR"); dir != "" {
		return dir
	}
	if info, err := os.Stat(localContent); err == nil && info.IsDir() {
		return localContent
	}
	return ""
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseConfig(args []string) (string, config, error) {
	cfg := defaults()
	if len(args) == 0 {
		return "", cfg, fmt.Errorf("usage: blog-terminal {serve|local} [flags]")
	}
	mode := args[0]
	if mode != "serve" && mode != "local" {
		return "", cfg, fmt.Errorf("unknown mode %q; use serve or local", mode)
	}
	f := flag.NewFlagSet(mode, flag.ContinueOnError)
	f.StringVar(&cfg.Content, "content", cfg.Content, "posts directory containing zh/ and en/ (also BLOG_CONTENT_DIR, then ./content)")
	f.StringVar(&cfg.SiteURL, "site-url", cfg.SiteURL, "public base URL of the blog, for article links (also BLOG_SITE_URL)")
	f.StringVar(&cfg.Title, "title", cfg.Title, "blog name shown in the header (also BLOG_TITLE)")
	f.StringVar(&cfg.Listen, "listen", cfg.Listen, "SSH listen address")
	f.StringVar(&cfg.HostKey, "host-key", cfg.HostKey, "persistent SSH host private key")
	f.StringVar(&cfg.Lang, "lang", cfg.Lang, "initial language override: zh or en (also BLOG_TERMINAL_LANG)")
	f.StringVar(&cfg.Theme, "theme", cfg.Theme, themeList())
	f.DurationVar(&cfg.Idle, "idle-timeout", cfg.Idle, "SSH connection idle timeout")
	f.DurationVar(&cfg.Duration, "max-duration", cfg.Duration, "maximum connection/session duration")
	f.IntVar(&cfg.MaxSessions, "max-sessions", cfg.MaxSessions, "global SSH channel/session cap")
	f.IntVar(&cfg.MaxConnections, "max-connections", cfg.MaxConnections, "global TCP connection cap (including handshakes)")
	f.IntVar(&cfg.PerIP, "per-ip", cfg.PerIP, "concurrent TCP connections per IP")
	f.IntVar(&cfg.Rate, "rate", cfg.Rate, "connection attempts per IP per minute")
	if err := f.Parse(args[1:]); err != nil {
		return "", cfg, err
	}
	if f.NArg() != 0 {
		return "", cfg, fmt.Errorf("unexpected arguments: %v", f.Args())
	}
	if cfg.Lang != "" && cfg.Lang != "zh" && cfg.Lang != "zh-CN" && cfg.Lang != "en" {
		return "", cfg, fmt.Errorf("--lang must be zh or en")
	}
	if u, err := url.Parse(cfg.SiteURL); err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", cfg, fmt.Errorf("--site-url must be an absolute http(s) URL")
	}
	if strings.TrimSpace(cfg.Title) == "" {
		return "", cfg, fmt.Errorf("--title must not be empty")
	}
	if !validTheme(cfg.Theme) {
		return "", cfg, fmt.Errorf("--theme must be one of %s", themeList())
	}
	if cfg.Idle <= 0 || cfg.Duration <= 0 || cfg.MaxSessions < 1 || cfg.MaxConnections < 1 || cfg.PerIP < 1 || cfg.Rate < 1 {
		return "", cfg, fmt.Errorf("timeouts and limits must be positive")
	}
	return mode, cfg, nil
}

func run(args []string) error {
	mode, cfg, err := parseConfig(args)
	if err != nil {
		return err
	}
	if cfg.Content == "" {
		return fmt.Errorf("no content directory: pass --content, set BLOG_CONTENT_DIR, or link ./content to the posts (e.g. ../Blog/src/content/posts)")
	}
	c, err := loadCatalog(cfg.Content)
	if err != nil {
		return fmt.Errorf("load posts: %w", err)
	}
	c.setSite(site{URL: strings.TrimSuffix(cfg.SiteURL, "/"), Title: cfg.Title})
	if len(c.Posts) == 0 {
		return fmt.Errorf("no published Markdown posts found in %s", cfg.Content)
	}
	if mode == "local" {
		m := newModel(c, initialLanguage(os.Environ(), cfg.Lang), cfg.Theme, colorprofile.Env(os.Environ()), 80, 24)
		_, err := tea.NewProgram(m).Run()
		return err
	}
	srv, err := newServer(cfg, c)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()
	log.Printf("blog-terminal: %d posts from %s; listening %s", len(c.Posts), cfg.Content, cfg.Listen)
	select {
	case err := <-done:
		if errors.Is(err, ssh.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return srv.Close()
		}
		return nil
	}
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		log.Print(err)
		os.Exit(1)
	}
}

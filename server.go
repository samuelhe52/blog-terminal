package main

import (
	"net"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/ssh"
	"charm.land/wish/v2"
	wishtea "charm.land/wish/v2/bubbletea"
	"github.com/charmbracelet/colorprofile"
	gossh "golang.org/x/crypto/ssh"
)

func newServer(cfg config, c *catalog) (*ssh.Server, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.HostKey), 0700); err != nil {
		return nil, err
	}
	budget := newLimits(cfg)
	srv, err := wish.NewServer(
		wish.WithAddress(cfg.Listen),
		wish.WithHostKeyPath(cfg.HostKey),
		wish.WithIdleTimeout(cfg.Idle),
		wish.WithMaxTimeout(cfg.Duration),
		wish.WithMiddleware(wishtea.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
			pty, _, _ := s.Pty()
			env := append(s.Environ(), "TERM="+pty.Term)
			return newModel(c, initialLanguage(env, cfg.Lang), cfg.Theme, colorprofile.Env(env), pty.Window.Width, pty.Window.Height), nil
		}), func(next ssh.Handler) ssh.Handler {
			return func(s ssh.Session) {
				if _, _, ok := s.Pty(); !ok || len(s.Command()) != 0 {
					wish.Fatalln(s, "Interactive PTY shell required.")
					return
				}
				timer := time.AfterFunc(cfg.Duration, func() { _ = s.Close() })
				defer timer.Stop()
				idle := time.AfterFunc(cfg.Idle, func() { _ = s.Close() })
				defer idle.Stop()
				next(&activitySession{Session: s, idle: idle, timeout: cfg.Idle})
			}
		}),
	)
	if err != nil {
		return nil, err
	}
	srv.HandshakeTimeout = 10 * time.Second
	srv.ConnCallback = func(_ ssh.Context, conn net.Conn) net.Conn {
		ip, _, err := net.SplitHostPort(conn.RemoteAddr().String())
		if err != nil {
			_ = conn.Close()
			return nil
		}
		release, ok := budget.connection(ip, time.Now())
		if !ok {
			_ = conn.Close()
			return nil
		}
		return &limitedConn{Conn: conn, release: release}
	}
	srv.SessionRequestCallback = func(s ssh.Session, kind string) bool { _, _, pty := s.Pty(); return kind == "shell" && pty }
	srv.PtyCallback = func(_ ssh.Context, p ssh.Pty) bool {
		return p.Window.Width >= 1 && p.Window.Width <= 512 && p.Window.Height >= 1 && p.Window.Height <= 256
	}
	srv.ChannelHandlers = map[string]ssh.ChannelHandler{
		"session": func(server *ssh.Server, conn *gossh.ServerConn, ch gossh.NewChannel, ctx ssh.Context) {
			release, ok := budget.session()
			if !ok {
				_ = ch.Reject(gossh.ResourceShortage, "Session limit reached")
				return
			}
			defer release()
			serveSizedSession(server, conn, restrictedChannel{ch}, ctx)
		},
	}
	srv.RequestHandlers = map[string]ssh.RequestHandler{}
	srv.SubsystemHandlers = map[string]ssh.SubsystemHandler{}
	// Port forwarding has no handlers; both forwarding callbacks default deny.
	return srv, nil
}

// Count client input, not repaint traffic: a blinking filter cursor must not
// keep an unattended reader alive indefinitely.
type activitySession struct {
	ssh.Session
	idle    *time.Timer
	timeout time.Duration
}

func (s *activitySession) Read(p []byte) (int, error) {
	n, err := s.Session.Read(p)
	if n > 0 {
		s.idle.Reset(s.timeout)
	}
	return n, err
}

// Charm SSH accepts agent-forward requests by default, even without an agent
// handler. Filter channel requests before its session parser sees them.
type restrictedChannel struct{ gossh.NewChannel }

func (c restrictedChannel) Accept() (gossh.Channel, <-chan *gossh.Request, error) {
	ch, requests, err := c.NewChannel.Accept()
	if err != nil {
		return nil, nil, err
	}
	allowed := make(chan *gossh.Request)
	go func() {
		defer close(allowed)
		count := 0
		for req := range requests {
			count++
			if len(req.Payload) > 4096 || count > 1024 {
				_ = req.Reply(false, nil)
				_ = ch.Close()
				continue
			}
			switch req.Type {
			case "env", "pty-req", "window-change", "shell":
				allowed <- req
			default:
				_ = req.Reply(false, nil)
			}
		}
	}()
	return ch, allowed, nil
}

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"charm.land/ssh"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	gossh "golang.org/x/crypto/ssh"
)

type captureBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *captureBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.data.Write(p)
}
func (b *captureBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.data.String() }

func waitFor(t *testing.T, b *captureBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		plain := ansi.Strip(b.String())
		plain = strings.ReplaceAll(strings.ReplaceAll(plain, "\r", ""), "\n", "")
		if strings.Contains(plain, want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("SSH output missing %q:\n%s", want, ansi.Strip(b.String()))
}

func waitForRaw(t *testing.T, b *captureBuffer, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(b.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("SSH output missing raw sequence %q:\n%q", want, b.String())
}

func dialSSH(t *testing.T, addr string) *gossh.Client {
	t.Helper()
	client, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{User: "any-visitor", HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func interactive(t *testing.T, client *gossh.Client, lang string) (*gossh.Session, io.WriteCloser, *captureBuffer) {
	t.Helper()
	s, err := client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Setenv("LANG", lang); err != nil {
		t.Fatal(err)
	}
	if err := s.RequestPty("xterm-256color", 32, 80, gossh.TerminalModes{}); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.SendRequest("auth-agent-req@openssh.com", true, nil); err != nil || ok {
		t.Fatalf("agent forwarding accepted: %v %v", ok, err)
	}
	input, err := s.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	b := &captureBuffer{}
	s.Stdout, s.Stderr = b, b
	if err := s.Shell(); err != nil {
		t.Fatal(err)
	}
	return s, input, b
}

func waitSession(t *testing.T, s *gossh.Session) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- s.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unclean SSH quit: %v", err)
		}
	case <-time.After(8 * time.Second):
		t.Fatal("SSH quit hung")
	}
}

func TestServeSSHCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("binary/SSH integration disabled by -short")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bin := filepath.Join(t.TempDir(), "terminal")
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	_ = l.Close()
	content, err := filepath.Abs("testdata/fixture")
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), ".ssh/host_ed25519")
	cmd := exec.CommandContext(ctx, bin, "serve", "--listen", addr, "--content", content, "--host-key", key, "--theme", "dark")
	logs := &captureBuffer{}
	cmd.Stdout, cmd.Stderr = logs, logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	var client *gossh.Client
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		client, err = gossh.Dial("tcp", addr, &gossh.ClientConfig{User: "blog", HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: time.Second})
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("serve did not start: %v\n%s", err, logs.String())
	}
	t.Cleanup(func() { _ = client.Close() })
	s, input, screen := interactive(t, client, "en_US.UTF-8")
	waitFor(t, screen, "blog.konakona.dev")
	waitFor(t, screen, "course-notes/")
	// Filter, leave filter editing, open the selected real article.
	if _, err := io.WriteString(input, "/Linear Attention\r\r"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, screen, webURL("attention-notes", en))
	if _, err := io.WriteString(input, "\x0c"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, screen, "线性注意力笔记")
	if err := s.WindowChange(24, 60); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(input, "G"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, screen, "100%")
	if _, err := io.WriteString(input, "\x03"); err != nil {
		t.Fatal(err)
	}
	waitSession(t, s)

	// No PTY, PTY exec, shell without PTY, and subsystems all fail at the
	// request layer rather than starting a TUI and merely printing an error.
	for _, withPTY := range []bool{false, true} {
		s, err := client.NewSession()
		if err != nil {
			t.Fatal(err)
		}
		if withPTY {
			if err := s.RequestPty("xterm", 24, 80, gossh.TerminalModes{}); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Run("somecmd"); err == nil {
			t.Fatal("exec accepted")
		}
		_ = s.Close()
	}
	s, err = client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Shell(); err == nil {
		t.Fatal("shell without PTY accepted")
	}
	_ = s.Close()
	s, err = client.NewSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RequestSubsystem("sftp"); err == nil {
		t.Fatal("subsystem accepted")
	}
	_ = s.Close()
	if conn, err := client.Dial("tcp", "127.0.0.1:80"); err == nil {
		_ = conn.Close()
		t.Fatal("local forwarding accepted")
	}
	if listener, err := client.Listen("tcp", "127.0.0.1:0"); err == nil {
		_ = listener.Close()
		t.Fatal("reverse forwarding accepted")
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0077 != 0 {
		t.Fatalf("host key permissions too broad: %o", info.Mode().Perm())
	}
	// A clean CLI signal path is part of the local-only verification.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("SIGTERM shutdown: %v\n%s", err, logs.String())
	}
	stopped = true
}

func testConfig() config {
	cfg := defaults()
	cfg.Content = "testdata/fixture"
	return cfg
}

func startTestServer(t *testing.T, cfg config) (string, *ssh.Server) {
	t.Helper()
	cfg.HostKey = filepath.Join(t.TempDir(), ".ssh/key")
	srv, err := newServer(cfg, fixtureCatalog(t))
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close(); <-done })
	return l.Addr().String(), srv
}

func TestSSHClientLanguageAndSessionCap(t *testing.T) {
	cfg := testConfig()
	cfg.MaxSessions = 1
	addr, _ := startTestServer(t, cfg)
	client := dialSSH(t, addr)
	s, input, screen := interactive(t, client, "zh_CN.UTF-8")
	waitFor(t, screen, "中文")
	if second, err := client.NewSession(); err == nil {
		_ = second.Close()
		t.Fatal("global session cap ignored")
	}
	if _, err := io.WriteString(input, "q"); err != nil {
		t.Fatal(err)
	}
	waitSession(t, s)
}

func TestSSHFilterEscapeSequence(t *testing.T) {
	addr, _ := startTestServer(t, testConfig())
	client := dialSSH(t, addr)
	for _, width := range []int{40, 90} {
		for _, sequence := range []string{"\x1b\x1b/two-node\r\r", "\x1b/two-node\r\r"} {
			t.Run(fmt.Sprintf("width%d/%q", width, sequence), func(t *testing.T) {
				s, input, screen := interactive(t, client, "en_US.UTF-8")
				if err := s.WindowChange(32, width); err != nil {
					t.Fatal(err)
				}
				waitFor(t, screen, "blog.konakona.dev")
				if _, err := io.WriteString(input, "/lecture"); err != nil {
					t.Fatal(err)
				}
				waitFor(t, screen, "Lecture")
				// Send adjacent Escape bytes in a single write, as tmux can.
				// Legacy input decoding produces Alt+Escape or Alt+/ here.
				if _, err := io.WriteString(input, sequence); err != nil {
					t.Fatal(err)
				}
				waitFor(t, screen, webURL("server-setup", en))
				if _, err := io.WriteString(input, "\x03"); err != nil {
					t.Fatal(err)
				}
				waitSession(t, s)
			})
		}
	}
}

func TestSSHTimeoutsAndConnectionLimit(t *testing.T) {
	for _, kind := range []string{"idle", "duration"} {
		t.Run(kind, func(t *testing.T) {
			cfg := testConfig()
			cfg.Idle = 5 * time.Second
			cfg.Duration = 5 * time.Second
			if kind == "idle" {
				cfg.Idle = 300 * time.Millisecond
			} else {
				cfg.Duration = 300 * time.Millisecond
			}
			addr, _ := startTestServer(t, cfg)
			client := dialSSH(t, addr)
			done := make(chan error, 1)
			go func() { done <- client.Wait() }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatalf("%s timeout ignored", kind)
			}
		})
	}
	cfg := testConfig()
	cfg.PerIP = 1
	addr, _ := startTestServer(t, cfg)
	_ = dialSSH(t, addr)
	second, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{User: "x", HostKeyCallback: gossh.InsecureIgnoreHostKey(), Timeout: time.Second})
	if err == nil {
		_ = second.Close()
		t.Fatal("per-IP connection cap ignored")
	}
}

func TestSSHIdleTimeoutWhileFilterBlinks(t *testing.T) {
	cfg := testConfig()
	cfg.Idle = 500 * time.Millisecond
	addr, _ := startTestServer(t, cfg)
	client := dialSSH(t, addr)
	s, input, screen := interactive(t, client, "en_US.UTF-8")
	waitFor(t, screen, "blog.konakona.dev")
	if _, err := io.WriteString(input, "/"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, screen, "Search posts")
	done := make(chan error, 1)
	go func() { done <- s.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("filter repaint traffic kept an idle session alive")
	}
}

func TestSSHBackgroundQueryAndReply(t *testing.T) {
	cfg := testConfig()
	addr, _ := startTestServer(t, cfg)
	client := dialSSH(t, addr)
	s, input, screen := interactive(t, client, "en_US.UTF-8")
	// Observe the real OSC 11 request over the SSH channel, then emulate a
	// terminal returning a white background. No terminal emulator is assumed.
	waitForRaw(t, screen, ansi.RequestBackgroundColor)
	colorSequence := regexp.MustCompile(`38;5;\d+`)
	palette := func(theme string) string {
		m := newModel(fixtureCatalog(t), en, theme, colorprofile.ANSI256, 80, 32)
		return colorSequence.FindString(m.accent("test"))
	}
	dark, light := palette("dark"), palette("light")
	if dark == light || light == "" {
		t.Fatal("expected distinct theme colors")
	}
	waitForRaw(t, screen, dark)
	if _, err := io.WriteString(input, "\x1b]11;rgb:ffff/ffff/ffff\x1b\\"); err != nil {
		t.Fatal(err)
	}
	waitForRaw(t, screen, light)

	// A second session on the same connection does not answer OSC 11 and
	// starts dark independently. It still renders and accepts input.
	other, otherInput, otherScreen := interactive(t, client, "zh_CN.UTF-8")
	waitForRaw(t, otherScreen, ansi.RequestBackgroundColor)
	waitForRaw(t, otherScreen, dark)
	waitFor(t, otherScreen, "中文")
	if strings.Contains(otherScreen.String(), light) {
		t.Fatal("background response leaked across sessions")
	}
	if _, err := io.WriteString(otherInput, "\x03"); err != nil {
		t.Fatal(err)
	}
	waitSession(t, other)
	if _, err := io.WriteString(input, "\x03"); err != nil {
		t.Fatal(err)
	}
	waitSession(t, s)
}

func TestLimits(t *testing.T) {
	cfg := testConfig()
	cfg.MaxConnections = 2
	cfg.MaxSessions = 1
	cfg.PerIP = 1
	cfg.Rate = 2
	l := newLimits(cfg)
	now := time.Now()
	release, ok := l.connection("a", now)
	if !ok {
		t.Fatal("first connection denied")
	}
	if _, ok := l.connection("a", now); ok {
		t.Fatal("per-IP concurrency limit ignored")
	}
	release()
	release()
	if _, ok := l.connection("a", now); ok {
		t.Fatal("rate limit ignored")
	}
	a, ok := l.connection("a", now.Add(time.Minute))
	if !ok {
		t.Fatal("rate window never resets")
	}
	b, ok := l.connection("b", now.Add(time.Minute))
	if !ok {
		t.Fatal("independent IP denied")
	}
	if _, ok := l.connection("c", now.Add(time.Minute)); ok {
		t.Fatal("global connection cap ignored")
	}
	a()
	b()
	s, ok := l.session()
	if !ok {
		t.Fatal("session denied")
	}
	if _, ok := l.session(); ok {
		t.Fatal("global session cap ignored")
	}
	s()
	s()
	if l.sessions != 0 || l.connections != 0 {
		t.Fatal("limit release leaked")
	}
	// Bookkeeping sweeps stale inactive addresses and has a hard upper bound.
	for i := 0; i < 4096; i++ {
		release, ok := l.connection(fmt.Sprint(i), now.Add(2*time.Minute))
		if !ok {
			t.Fatal("unexpected denial")
		}
		release()
	}
	if _, ok := l.connection("overflow", now.Add(2*time.Minute)); ok {
		t.Fatal("unbounded IP map")
	}
}

func TestFlags(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"serve", "--lang", "fr"}, {"local", "--theme", "auto-dark"}, {"serve", "--per-ip", "0"}, {"serve", "--idle-timeout", "0s"}} {
		if _, _, err := parseConfig(args); err == nil {
			t.Fatalf("invalid flags accepted: %v", args)
		}
	}
	_, cfg, err := parseConfig([]string{"serve", "--listen", "127.0.0.1:0", "--lang", "zh", "--theme", "light"})
	if err != nil || cfg.Lang != "zh" || cfg.Theme != "light" {
		t.Fatalf("flags: %v", err)
	}
}

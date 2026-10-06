package main

import (
	"charm.land/ssh"
	gossh "golang.org/x/crypto/ssh"
)

// Charm SSH 0.4.3 writes Pty.Window from its request goroutine without locking
// Pty(). Capture the initial PTY in that same goroutine, before the handler
// starts, and let Wish consume subsequent sizes only through the winch channel.
type sizedSession struct {
	ssh.Session
	pty     ssh.Pty
	changes <-chan ssh.Window
}

func (s *sizedSession) Pty() (ssh.Pty, <-chan ssh.Window, bool) {
	return s.pty, s.changes, true
}

func serveSizedSession(server *ssh.Server, conn *gossh.ServerConn, channel gossh.NewChannel, ctx ssh.Context) {
	var snapshot *sizedSession
	// DefaultSessionHandler reads only these configuration fields. Construct
	// a channel-local configuration rather than copying Server's mutexes.
	sessionServer := &ssh.Server{
		PtyCallback:       server.PtyCallback,
		PtyHandler:        server.PtyHandler,
		SubsystemHandlers: server.SubsystemHandlers,
		SessionRequestCallback: func(s ssh.Session, kind string) bool {
			if !server.SessionRequestCallback(s, kind) {
				return false
			}
			pty, changes, ok := s.Pty()
			if !ok {
				return false
			}
			snapshot = &sizedSession{Session: s, pty: pty, changes: changes}
			return true
		},
		Handler: func(_ ssh.Session) { server.Handler(snapshot) },
	}
	ssh.DefaultSessionHandler(sessionServer, conn, channel, ctx)
}

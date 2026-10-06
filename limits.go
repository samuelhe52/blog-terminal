package main

import (
	"net"
	"sync"
	"time"
)

type ipWindow struct {
	since            time.Time
	attempts, active int
}

type limits struct {
	mu                                            sync.Mutex
	ips                                           map[string]*ipWindow
	connections, sessions                         int
	maxConnections, maxSessions, perIP, perMinute int
}

func newLimits(cfg config) *limits {
	return &limits{ips: make(map[string]*ipWindow), maxConnections: cfg.MaxConnections, maxSessions: cfg.MaxSessions, perIP: cfg.PerIP, perMinute: cfg.Rate}
}

func (l *limits) connection(ip string, now time.Time) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for addr, w := range l.ips {
		if w.active == 0 && now.Sub(w.since) >= time.Minute {
			delete(l.ips, addr)
		}
	}
	w := l.ips[ip]
	if w == nil {
		// Bound bookkeeping even during a flood from distinct addresses.
		if len(l.ips) >= 4096 {
			return nil, false
		}
		w = &ipWindow{since: now}
		l.ips[ip] = w
	}
	if now.Sub(w.since) >= time.Minute {
		w.since, w.attempts = now, 0
	}
	w.attempts++
	if w.attempts > l.perMinute || w.active >= l.perIP || l.connections >= l.maxConnections {
		return nil, false
	}
	w.active++
	l.connections++
	var once sync.Once
	return func() { once.Do(func() { l.mu.Lock(); defer l.mu.Unlock(); w.active--; l.connections-- }) }, true
}

func (l *limits) session() (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sessions >= l.maxSessions {
		return nil, false
	}
	l.sessions++
	var once sync.Once
	return func() { once.Do(func() { l.mu.Lock(); defer l.mu.Unlock(); l.sessions-- }) }, true
}

type limitedConn struct {
	net.Conn
	release func()
}

func (c *limitedConn) Close() error { err := c.Conn.Close(); c.release(); return err }

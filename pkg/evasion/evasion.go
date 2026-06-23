package evasion

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	utls "github.com/refraction-networking/utls"
)

type BrowserProfile string

const (
	Chrome  BrowserProfile = "chrome"
	Firefox BrowserProfile = "firefox"
	Safari  BrowserProfile = "safari"
	Edge    BrowserProfile = "edge"
)

type Manager struct {
	Profile BrowserProfile
	Timeout time.Duration
}

func NewManager(profile BrowserProfile) *Manager {
	return &Manager{
		Profile: profile,
		Timeout: 30 * time.Second,
	}
}

func (m *Manager) getUTLSClientHelloID() utls.ClientHelloID {
	switch m.Profile {
	case Firefox:
		return utls.HelloFirefox_105
	case Safari:
		return utls.HelloSafari_16_0
	case Edge:
		return utls.HelloChrome_Auto
	default:
		return utls.HelloChrome_Auto
	}
}

func (m *Manager) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("split host port: %w", err)
	}

	dialer := &net.Dialer{Timeout: m.Timeout}
	tcpConn, err := dialer.DialContext(ctx, network, addr)
	if err != nil {
		return nil, fmt.Errorf("tcp dial: %w", err)
	}

	uTLSConn := utls.UClient(tcpConn, &utls.Config{
		ServerName:         host,
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
		MaxVersion:         tls.VersionTLS13,
		NextProtos:         []string{"h2", "http/1.1"},
	}, m.getUTLSClientHelloID())

	if err := uTLSConn.Handshake(); err != nil {
		tcpConn.Close()
		return nil, fmt.Errorf("utls handshake: %w", err)
	}
	return uTLSConn, nil
}

func (m *Manager) DialDirect(ctx context.Context, network, addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: m.Timeout}
	return dialer.DialContext(ctx, network, addr)
}
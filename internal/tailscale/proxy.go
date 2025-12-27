package tailscale

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"sync"

	"tailscale.com/tsnet"
)

// Proxy is a simple SOCKS5 proxy that routes connections through Tailscale.
type Proxy struct {
	server   *tsnet.Server
	listener net.Listener
	wg       sync.WaitGroup
	closed   bool
	mu       sync.Mutex
}

// NewProxy creates a new SOCKS5 proxy that routes through the given tsnet server.
func NewProxy(server *tsnet.Server) *Proxy {
	return &Proxy{
		server: server,
	}
}

// Start begins listening for SOCKS5 connections on a random local port.
// Returns the address the proxy is listening on (e.g., "127.0.0.1:12345").
func (p *Proxy) Start() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen: %w", err)
	}

	p.listener = listener

	p.wg.Add(1)
	go p.acceptLoop()

	return listener.Addr().String(), nil
}

// Close stops the proxy and closes all connections.
func (p *Proxy) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	p.mu.Unlock()

	if p.listener != nil {
		p.listener.Close()
	}

	p.wg.Wait()
	return nil
}

func (p *Proxy) acceptLoop() {
	defer p.wg.Done()

	for {
		conn, err := p.listener.Accept()
		if err != nil {
			p.mu.Lock()
			closed := p.closed
			p.mu.Unlock()
			if closed {
				return
			}
			log.Printf("[tailscale-proxy] accept error: %v", err)
			continue
		}

		p.wg.Add(1)
		go p.handleConnection(conn)
	}
}

// handleConnection handles a single SOCKS5 connection.
// Implements a minimal SOCKS5 server (RFC 1928) supporting CONNECT only.
func (p *Proxy) handleConnection(conn net.Conn) {
	defer p.wg.Done()
	defer conn.Close()

	// Read SOCKS5 greeting
	// +----+----------+----------+
	// |VER | NMETHODS | METHODS  |
	// +----+----------+----------+
	// | 1  |    1     | 1 to 255 |
	// +----+----------+----------+
	buf := make([]byte, 258)
	n, err := conn.Read(buf[:2])
	if err != nil || n < 2 {
		return
	}

	version := buf[0]
	if version != 0x05 {
		return // Not SOCKS5
	}

	nmethods := int(buf[1])
	if nmethods > 0 {
		_, err = io.ReadFull(conn, buf[:nmethods])
		if err != nil {
			return
		}
	}

	// Reply with no authentication required
	// +----+--------+
	// |VER | METHOD |
	// +----+--------+
	// | 1  |   1    |
	// +----+--------+
	_, err = conn.Write([]byte{0x05, 0x00})
	if err != nil {
		return
	}

	// Read SOCKS5 request
	// +----+-----+-------+------+----------+----------+
	// |VER | CMD |  RSV  | ATYP | DST.ADDR | DST.PORT |
	// +----+-----+-------+------+----------+----------+
	// | 1  |  1  | X'00' |  1   | Variable |    2     |
	// +----+-----+-------+------+----------+----------+
	n, err = conn.Read(buf[:4])
	if err != nil || n < 4 {
		return
	}

	if buf[0] != 0x05 {
		return
	}

	cmd := buf[1]
	if cmd != 0x01 { // Only support CONNECT
		p.sendReply(conn, 0x07) // Command not supported
		return
	}

	atyp := buf[3]
	var host string
	var port uint16

	switch atyp {
	case 0x01: // IPv4
		n, err = io.ReadFull(conn, buf[:4])
		if err != nil {
			return
		}
		host = net.IP(buf[:4]).String()

	case 0x03: // Domain name
		n, err = conn.Read(buf[:1])
		if err != nil {
			return
		}
		domainLen := int(buf[0])
		n, err = io.ReadFull(conn, buf[:domainLen])
		if err != nil {
			return
		}
		host = string(buf[:domainLen])

	case 0x04: // IPv6
		n, err = io.ReadFull(conn, buf[:16])
		if err != nil {
			return
		}
		host = net.IP(buf[:16]).String()

	default:
		p.sendReply(conn, 0x08) // Address type not supported
		return
	}

	// Read port (2 bytes, big endian)
	n, err = io.ReadFull(conn, buf[:2])
	if err != nil {
		return
	}
	port = uint16(buf[0])<<8 | uint16(buf[1])

	// Connect through Tailscale
	addr := fmt.Sprintf("%s:%d", host, port)
	ctx := context.Background()

	targetConn, err := p.server.Dial(ctx, "tcp", addr)
	if err != nil {
		log.Printf("[tailscale-proxy] dial %s failed: %v", addr, err)
		p.sendReply(conn, 0x04) // Host unreachable
		return
	}
	defer targetConn.Close()

	// Send success reply
	// For simplicity, we send back 0.0.0.0:0 as the bound address
	reply := []byte{
		0x05, 0x00, 0x00, 0x01, // VER, REP (success), RSV, ATYP (IPv4)
		0x00, 0x00, 0x00, 0x00, // BND.ADDR (0.0.0.0)
		0x00, 0x00, // BND.PORT (0)
	}
	_, err = conn.Write(reply)
	if err != nil {
		return
	}

	// Proxy data bidirectionally
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(targetConn, conn)
		// Signal EOF to target
		if tc, ok := targetConn.(interface{ CloseWrite() error }); ok {
			tc.CloseWrite()
		}
	}()

	go func() {
		defer wg.Done()
		io.Copy(conn, targetConn)
		// Signal EOF to client
		if tc, ok := conn.(interface{ CloseWrite() error }); ok {
			tc.CloseWrite()
		}
	}()

	wg.Wait()
}

func (p *Proxy) sendReply(conn net.Conn, rep byte) {
	reply := []byte{
		0x05, rep, 0x00, 0x01, // VER, REP, RSV, ATYP (IPv4)
		0x00, 0x00, 0x00, 0x00, // BND.ADDR
		0x00, 0x00, // BND.PORT
	}
	conn.Write(reply)
}

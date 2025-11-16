package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Config represents the gateway configuration
type Config struct {
	ProxyAddr   string `json:"proxy_addr"`
	LocalAddr   string `json:"local_addr"`
	HTTPPort    int    `json:"http_port"`
	HTTPSPort   int    `json:"https_port"`
	BufferSize  int    `json:"buffer_size"`
	GatewayPort int    `json:"gateway_port"`
	PacketQueue int    `json:"packet_queue"`
}

// ProxyConnection represents a proxy connection
type ProxyConnection struct {
	OriginalDest string
	ClientConn   net.Conn
	ProxyConn    net.Conn
	CreatedAt    time.Time
	LastUsed     time.Time
	Host         string
	Port         int
}

// Gateway represents the network gateway
type Gateway struct {
	config         *Config
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	logger         *log.Logger
	running        bool
	proxyConns     map[string]*ProxyConnection
	connPoolMutex  sync.RWMutex
	connPoolSize   int
	connPoolExpiry time.Duration
	serverListener net.Listener
}

// NewGateway creates a new Gateway instance
func NewGateway(configPath string) (*Gateway, error) {
	config, err := loadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	gw := &Gateway{
		config:         config,
		ctx:            ctx,
		cancel:         cancel,
		logger:         log.New(os.Stdout, "[Gateway] ", log.LstdFlags|log.Lshortfile),
		running:        false,
		proxyConns:     make(map[string]*ProxyConnection),
		connPoolSize:   100,
		connPoolExpiry: 5 * time.Minute,
	}

	return gw, nil
}

// loadConfig loads configuration from file
func loadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Set defaults
	if config.BufferSize == 0 {
		config.BufferSize = 8192
	}
	if config.GatewayPort == 0 {
		config.GatewayPort = 8080
	}
	if config.PacketQueue == 0 {
		config.PacketQueue = 1000
	}

	return &config, nil
}

// Start starts the gateway
func (g *Gateway) Start() error {
	g.logger.Println("Starting Network Gateway...")

	// Start HTTP server for proxy interception
	err := g.startProxyServer()
	if err != nil {
		return fmt.Errorf("failed to start proxy server: %w", err)
	}

	// Start connection pool cleanup
	go g.cleanupConnectionPool()

	// Start status monitoring
	go g.statusMonitor()

	// Setup signal handling
	go g.handleSignals()

	g.running = true
	g.logger.Printf("Gateway started successfully")
	g.logger.Printf("Proxy address: %s", g.config.ProxyAddr)
	g.logger.Printf("Gateway listening on: 0.0.0.0:%d", g.config.GatewayPort)

	return nil
}

// Stop stops the gateway
func (g *Gateway) Stop() {
	if !g.running {
		return
	}

	g.logger.Println("Stopping gateway...")
	g.running = false
	g.cancel()

	if g.serverListener != nil {
		g.serverListener.Close()
	}

	// Close all proxy connections
	g.connPoolMutex.Lock()
	for _, conn := range g.proxyConns {
		if conn.ClientConn != nil {
			conn.ClientConn.Close()
		}
		if conn.ProxyConn != nil {
			conn.ProxyConn.Close()
		}
	}
	g.connPoolMutex.Unlock()

	g.wg.Wait()
}

// startProxyServer starts the proxy server
func (g *Gateway) startProxyServer() error {
	var err error
	g.serverListener, err = net.Listen("tcp", fmt.Sprintf(":%d", g.config.GatewayPort))
	if err != nil {
		return err
	}

	g.wg.Add(1)
	go func() {
		defer g.wg.Done()
		g.acceptConnections()
	}()

	return nil
}

// acceptConnections accepts incoming connections
func (g *Gateway) acceptConnections() {
	g.logger.Printf("Accepting connections on port %d", g.config.GatewayPort)

	for {
		conn, err := g.serverListener.Accept()
		if err != nil {
			if g.ctx.Err() != nil {
				return
			}
			g.logger.Printf("Error accepting connection: %v", err)
			continue
		}

		g.wg.Add(1)
		go g.handleConnection(conn)
	}
}

// handleConnection handles a client connection
func (g *Gateway) handleConnection(clientConn net.Conn) {
	defer g.wg.Done()
	defer clientConn.Close()

	// Read the initial request
	buf := make([]byte, g.config.BufferSize)
	n, err := clientConn.Read(buf)
	if err != nil {
		g.logger.Printf("Error reading request: %v", err)
		return
	}

	request := buf[:n]
	g.logger.Printf("Received request: %s", strings.TrimSpace(string(request[:min(100, n)])))

	// Parse the request to determine if it's HTTP or HTTPS
	if strings.Contains(string(request), "CONNECT") {
		g.handleCONNECTRequest(clientConn, request)
	} else {
		g.handleHTTPRequest(clientConn, request)
	}
}

// handleHTTPRequest handles HTTP requests
func (g *Gateway) handleHTTPRequest(clientConn net.Conn, request []byte) {
	// Parse HTTP request to get target URL
	targetURL, err := g.parseHTTPRequest(string(request))
	if err != nil {
		g.logger.Printf("Failed to parse HTTP request: %v", err)
		clientConn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		return
	}

	g.logger.Printf("HTTP request to: %s", targetURL)

	// Forward request to proxy
	err = g.forwardToProxy(clientConn, targetURL, request, "")
	if err != nil {
		g.logger.Printf("Failed to forward HTTP request: %v", err)
		clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
}

// handleCONNECTRequest handles HTTPS CONNECT requests
func (g *Gateway) handleCONNECTRequest(clientConn net.Conn, request []byte) {
	// Parse CONNECT request
	targetHost, targetPort, err := g.parseCONNECTRequest(string(request))
	if err != nil {
		g.logger.Printf("Failed to parse CONNECT request: %v", err)
		clientConn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		return
	}

	targetAddr := fmt.Sprintf("%s:%d", targetHost, targetPort)
	g.logger.Printf("HTTPS CONNECT to: %s", targetAddr)

	// Send 200 Connection Established
	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Forward to proxy
	err = g.forwardToProxy(clientConn, targetAddr, request, targetHost)
	if err != nil {
		g.logger.Printf("Failed to forward CONNECT request: %v", err)
	}
}

// forwardToProxy forwards data to proxy server
func (g *Gateway) forwardToProxy(clientConn net.Conn, targetAddr string, initialRequest []byte, targetHost string) error {
	// Connect to proxy
	proxyConn, err := net.Dial("tcp", g.config.ProxyAddr)
	if err != nil {
		return fmt.Errorf("failed to connect to proxy: %w", err)
	}
	defer proxyConn.Close()

	// If it's not a CONNECT request, modify the request to go through proxy
	if !strings.Contains(string(initialRequest), "CONNECT") {
		modifiedRequest := g.modifyRequestForProxy(string(initialRequest), targetAddr)
		_, err = proxyConn.Write([]byte(modifiedRequest))
		if err != nil {
			return fmt.Errorf("failed to write request to proxy: %w", err)
		}
	} else {
		// For CONNECT requests, just forward the original
		_, err = proxyConn.Write(initialRequest)
		if err != nil {
			return fmt.Errorf("failed to write CONNECT to proxy: %w", err)
		}
	}

	// Setup bidirectional forwarding
	return g.setupBidirectionalForward(clientConn, proxyConn)
}

// modifyRequestForProxy modifies HTTP request to route through proxy
func (g *Gateway) modifyRequestForProxy(request, targetURL string) string {
	lines := strings.Split(request, "\r\n")

	for i, line := range lines {
		if strings.HasPrefix(strings.ToUpper(line), "PROXY-CONNECTION:") {
			lines[i] = "Connection: close"
		} else if strings.HasPrefix(line, "Connection:") {
			lines[i] = "Connection: close"
		} else if strings.HasPrefix(line, "Proxy-Connection:") {
			lines[i] = "Connection: close"
		}
	}

	return strings.Join(lines, "\r\n")
}

// setupBidirectionalForward sets up bidirectional data forwarding
func (g *Gateway) setupBidirectionalForward(clientConn, proxyConn net.Conn) error {
	var wg sync.WaitGroup
	wg.Add(2)

	// Forward client -> proxy
	go func() {
		defer wg.Done()
		_, err := io.Copy(proxyConn, clientConn)
		if err != nil {
			g.logger.Printf("Client to proxy forwarding error: %v", err)
		}
		proxyConn.Close()
	}()

	// Forward proxy -> client
	go func() {
		defer wg.Done()
		_, err := io.Copy(clientConn, proxyConn)
		if err != nil {
			g.logger.Printf("Proxy to client forwarding error: %v", err)
		}
		clientConn.Close()
	}()

	wg.Wait()
	return nil
}

// parseHTTPRequest parses HTTP request to extract target URL
func (g *Gateway) parseHTTPRequest(request string) (string, error) {
	lines := strings.Split(request, "\r\n")
	if len(lines) == 0 {
		return "", fmt.Errorf("empty request")
	}

	// Parse request line
	parts := strings.Fields(lines[0])
	if len(parts) < 3 {
		return "", fmt.Errorf("invalid request line")
	}

	// method := parts[0] // not used currently
	url := parts[1]

	// If absolute URL, use as is
	if strings.HasPrefix(url, "http://") {
		return url, nil
	}

	// For relative URLs, extract Host header
	host := ""
	for _, line := range lines {
		if strings.HasPrefix(strings.ToLower(line), "host:") {
			host = strings.TrimSpace(line[5:])
			break
		}
	}

	if host == "" {
		return "", fmt.Errorf("no host header found")
	}

	return fmt.Sprintf("http://%s%s", host, url), nil
}

// parseCONNECTRequest parses CONNECT request
func (g *Gateway) parseCONNECTRequest(request string) (string, int, error) {
	lines := strings.Split(request, "\r\n")
	if len(lines) == 0 {
		return "", 0, fmt.Errorf("empty request")
	}

	// Parse request line
	parts := strings.Fields(lines[0])
	if len(parts) < 3 || parts[0] != "CONNECT" {
		return "", 0, fmt.Errorf("invalid CONNECT request")
	}

	hostPort := parts[1]
	host, portStr, err := net.SplitHostPort(hostPort)
	if err != nil {
		// If no port specified, assume 443
		host = hostPort
		portStr = "443"
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port: %s", portStr)
	}

	return host, port, nil
}

// cleanupConnectionPool periodically cleans up old connections
func (g *Gateway) cleanupConnectionPool() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			g.cleanupOldConnections()
		}
	}
}

// cleanupOldConnections removes expired connections
func (g *Gateway) cleanupOldConnections() {
	g.connPoolMutex.Lock()
	defer g.connPoolMutex.Unlock()

	now := time.Now()
	expiredCount := 0
	for key, conn := range g.proxyConns {
		if now.Sub(conn.LastUsed) > g.connPoolExpiry {
			if conn.ClientConn != nil {
				conn.ClientConn.Close()
			}
			if conn.ProxyConn != nil {
				conn.ProxyConn.Close()
			}
			delete(g.proxyConns, key)
			expiredCount++
		}
	}
	if expiredCount > 0 {
		g.logger.Printf("Cleaned up %d expired connections", expiredCount)
	}
}

// statusMonitor monitors gateway status
func (g *Gateway) statusMonitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			if g.running {
				g.logger.Printf("Status: Gateway running, Proxy: %s, Active connections: %d",
					g.config.ProxyAddr, len(g.proxyConns))
			}
		}
	}
}

// handleSignals handles system signals for graceful shutdown
func (g *Gateway) handleSignals() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	sig := <-c
	g.logger.Printf("Received signal: %v", sig)
	g.Stop()
}

// GetStatus returns current gateway status
func (g *Gateway) GetStatus() map[string]interface{} {
	g.connPoolMutex.RLock()
	defer g.connPoolMutex.RUnlock()

	return map[string]interface{}{
		"running":          g.running,
		"proxy_addr":       g.config.ProxyAddr,
		"gateway_port":     g.config.GatewayPort,
		"buffer_size":      g.config.BufferSize,
		"active_conns":     len(g.proxyConns),
		"conn_pool_size":   g.connPoolSize,
		"conn_pool_expiry": g.connPoolExpiry.String(),
	}
}

func main() {
	configPath := "config.json"

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Fatalf("Config file not found: %s", configPath)
	}

	// Create gateway
	gateway, err := NewGateway(configPath)
	if err != nil {
		log.Fatalf("Failed to create gateway: %v", err)
	}

	// Start gateway
	if err := gateway.Start(); err != nil {
		log.Fatalf("Failed to start gateway: %v", err)
	}

	gateway.logger.Printf("Gateway status: %+v", gateway.GetStatus())
	gateway.logger.Println("Gateway is running. Press Ctrl+C to stop.")
	gateway.logger.Println("Configure your applications to use this gateway as HTTP/HTTPS proxy")

	// Wait for shutdown signal
	<-gateway.ctx.Done()
	gateway.Stop()
	gateway.logger.Println("Gateway stopped gracefully")
}

// Helper function to get minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/url"
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
	PacketQueue int    `json:"packet_queue"`
}

// FilterRule represents filtering rules for packets
type FilterRule struct {
	Protocol string `json:"protocol"` // "tcp", "udp"
	Port     uint16 `json:"port"`
	Action   string `json:"action"` // "forward", "drop", "bypass"
	RemoteIP string `json:"remote_ip"`
}

// ProxyConnection represents a proxy connection
type ProxyConnection struct {
	OriginalDest string
	ProxyConn    net.Conn
	CreatedAt    time.Time
	LastUsed     time.Time
}

// Gateway represents the WinDivert gateway
type Gateway struct {
	config         *Config
	rules          []FilterRule
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	logger         *log.Logger
	running        bool
	proxyConns     map[string]*ProxyConnection
	connPoolMutex  sync.RWMutex
	connPoolSize   int
	connPoolExpiry time.Duration
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
		logger:         log.New(os.Stdout, "[WinDivert Gateway] ", log.LstdFlags|log.Lshortfile),
		running:        false,
		proxyConns:     make(map[string]*ProxyConnection),
		connPoolSize:   100,
		connPoolExpiry: 5 * time.Minute,
	}

	gw.rules = []FilterRule{
		{Protocol: "tcp", Port: 80, Action: "forward"},  // HTTP
		{Protocol: "tcp", Port: 443, Action: "forward"}, // HTTPS
		{Protocol: "udp", Port: 53, Action: "bypass"},   // DNS
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

	return &config, nil
}

// Start starts the gateway
func (g *Gateway) Start() error {
	g.logger.Println("Starting WinDivert Gateway...")

	// Initialize gateway (simplified - no WinDivert dependency)
	err := g.initGateway()
	if err != nil {
		return fmt.Errorf("failed to initialize gateway: %w", err)
	}

	// Start connection pool cleanup
	go g.cleanupConnectionPool()

	// Start packet processing goroutines (simplified)
	g.wg.Add(2)
	go g.packetProcessor()
	go g.proxyHandler()

	// Setup signal handling
	go g.handleSignals()

	g.running = true
	g.logger.Printf("Gateway started successfully")
	g.logger.Printf("Proxy address: %s", g.config.ProxyAddr)
	g.logger.Printf("Buffer size: %d, Packet queue: %d", g.config.BufferSize, g.config.PacketQueue)
	g.logger.Printf("Connection pool size: %d, Expiry: %v", g.connPoolSize, g.connPoolExpiry)

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
	g.wg.Wait()

	// Close all proxy connections
	g.connPoolMutex.Lock()
	for _, conn := range g.proxyConns {
		if conn.ProxyConn != nil {
			conn.ProxyConn.Close()
		}
	}
	g.connPoolMutex.Unlock()
}

// initGateway initializes the gateway (simplified)
func (g *Gateway) initGateway() error {
	g.logger.Println("Initializing Gateway...")
	return nil
}

// packetProcessor processes packets (simplified simulation)
func (g *Gateway) packetProcessor() {
	defer g.wg.Done()

	g.logger.Println("Packet processor started (simulation mode)")

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			g.logger.Println("Packet processor stopping...")
			return
		case <-ticker.C:
			g.logger.Printf("Processing simulated packets... Active connections: %d", len(g.proxyConns))

			// Simulate some proxy connections for demonstration
			if len(g.proxyConns) < 5 {
				flowKey := fmt.Sprintf("sim-flow-%d", time.Now().UnixNano()%1000000)
				proxyAddr := g.config.ProxyAddr

				proxyConn, err := net.Dial("tcp", proxyAddr)
				if err == nil {
					conn := &ProxyConnection{
						OriginalDest: "example.com:80",
						ProxyConn:    proxyConn,
						CreatedAt:    time.Now(),
						LastUsed:     time.Now(),
					}
					g.proxyConns[flowKey] = conn
					g.logger.Printf("Created simulated connection for flow: %s", flowKey)
				}
			}
		}
	}
}

// proxyHandler handles proxy connections and management
func (g *Gateway) proxyHandler() {
	defer g.wg.Done()

	g.logger.Println("Proxy handler started")

	// Periodic status checks
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-g.ctx.Done():
			g.logger.Println("Proxy handler stopping...")
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
		"buffer_size":      g.config.BufferSize,
		"packet_queue":     g.config.PacketQueue,
		"rules_count":      len(g.rules),
		"active_conns":     len(g.proxyConns),
		"conn_pool_size":   g.connPoolSize,
		"conn_pool_expiry": g.connPoolExpiry.String(),
	}
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
	for flowKey, conn := range g.proxyConns {
		if now.Sub(conn.LastUsed) > g.connPoolExpiry {
			if conn.ProxyConn != nil {
				conn.ProxyConn.Close()
			}
			delete(g.proxyConns, flowKey)
			expiredCount++
		}
	}
	if expiredCount > 0 {
		g.logger.Printf("Cleaned up %d expired connections", expiredCount)
	}
}

// Test packet processing functions
func (g *Gateway) testPacketProcessing() {
	// Test HTTP request parsing
	httpRequest := "GET http://example.com/path HTTP/1.1\r\nHost: example.com\r\n\r\n"
	host, port, err := g.parseHTTPRequest([]byte(httpRequest))
	if err == nil {
		g.logger.Printf("HTTP test: Host=%s, Port=%d", host, port)
	}

	// Test HTTPS CONNECT parsing
	connectRequest := "CONNECT example.com:443 HTTP/1.1\r\n\r\n"
	host, port, err = g.parseCONNECTRequest([]byte(connectRequest))
	if err == nil {
		g.logger.Printf("CONNECT test: Host=%s, Port=%d", host, port)
	}
}

// HTTP request parsing
func (g *Gateway) parseHTTPRequest(payload []byte) (string, int, error) {
	reader := bufio.NewReader(strings.NewReader(string(payload)))

	// Read first line (GET / HTTP/1.1)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", 0, err
	}

	// Parse request line
	parts := strings.Fields(line)
	if len(parts) < 3 {
		return "", 0, fmt.Errorf("invalid HTTP request")
	}

	// Extract URL from request
	urlStr := parts[1]
	if urlStr == "*" {
		return "", 0, fmt.Errorf("invalid URL in request")
	}

	// Parse URL
	if !strings.HasPrefix(urlStr, "http://") {
		urlStr = "http://" + urlStr
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return "", 0, err
	}

	// Extract host and port
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "80"
	}

	portInt, err := strconv.Atoi(port)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port: %s", port)
	}
	return host, portInt, nil
}

// CONNECT request parsing
func (g *Gateway) parseCONNECTRequest(payload []byte) (string, int, error) {
	reader := bufio.NewReader(strings.NewReader(string(payload)))

	// Read first line (CONNECT example.com:443 HTTP/1.1)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", 0, err
	}

	// Parse CONNECT request line
	parts := strings.Fields(line)
	if len(parts) < 3 || parts[0] != "CONNECT" {
		return "", 0, fmt.Errorf("invalid CONNECT request")
	}

	// Extract host:port from CONNECT request
	hostPort := parts[1]
	host, port, err := net.SplitHostPort(hostPort)
	if err != nil {
		return "", 0, err
	}

	if port == "" {
		port = "443"
	}

	portInt, err := strconv.Atoi(port)
	if err != nil {
		return "", 0, fmt.Errorf("invalid port: %s", port)
	}
	return host, portInt, nil
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
	gateway.logger.Println("Gateway is running (simulation mode). Press Ctrl+C to stop.")

	// Test packet processing functions
	gateway.testPacketProcessing()

	// Wait for shutdown signal
	<-gateway.ctx.Done()
	gateway.Stop()
	gateway.logger.Println("Gateway stopped gracefully")
}

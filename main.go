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

	"github.com/imgk/divert-go"
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
	windivert      *divert.Handle
	ctx            context.Context
	cancel         context.CancelFunc
	packetCh       chan PacketInfo
	wg             sync.WaitGroup
	logger         *log.Logger
	running        bool
	proxyConns     map[string]*ProxyConnection
	connPoolMutex  sync.RWMutex
	connPoolSize   int
	connPoolExpiry time.Duration
}

// PacketInfo represents packet information
type PacketInfo struct {
	RawPacket []byte
	Addr      *divert.Address
	FlowKey   string // Unique identifier for the connection flow
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
		packetCh:       make(chan PacketInfo, config.PacketQueue),
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

	// Initialize WinDivert
	err := g.initWinDivert()
	if err != nil {
		return fmt.Errorf("failed to initialize windivert: %w", err)
	}

	// Start connection pool cleanup
	go g.cleanupConnectionPool()

	// Start packet processing goroutines
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
	close(g.packetCh)
	g.wg.Wait()

	// Close all proxy connections
	g.connPoolMutex.Lock()
	for _, conn := range g.proxyConns {
		if conn.ProxyConn != nil {
			conn.ProxyConn.Close()
		}
	}
	g.connPoolMutex.Unlock()

	if g.windivert != nil {
		g.windivert.Close()
	}
}

// initWinDivert initializes the WinDivert driver
func (g *Gateway) initWinDivert() error {
	g.logger.Println("Initializing WinDivert...")

	// Build filter string for outbound HTTP and HTTPS traffic
	filter := "outbound and (tcp.DstPort == 80 or tcp.DstPort == 443)"

	// Open WinDivert handle
	var err error
	g.windivert, err = divert.Open(filter, divert.LayerNetwork, 100, divert.FlagDefault)
	if err != nil {
		return fmt.Errorf("failed to open windivert: %w", err)
	}

	g.logger.Printf("WinDivert filter set: %s", filter)
	return nil
}

// packetProcessor processes packets received from WinDivert
func (g *Gateway) packetProcessor() {
	defer g.wg.Done()

	packetBuffer := make([]byte, g.config.BufferSize)
	g.logger.Println("Packet processor started")

	for {
		select {
		case <-g.ctx.Done():
			g.logger.Println("Packet processor stopping...")
			return
		default:
			// Read packet using divert-go API
			n, addr, err := g.windivert.Read(packetBuffer)
			if err != nil {
				if g.ctx.Err() != nil {
					return
				}
				g.logger.Printf("Error reading packet: %v", err)
				continue
			}

			packet := packetBuffer[:n]

			// Create packet info with flow key
			flowKey := g.generateFlowKey(addr)
			packetInfo := PacketInfo{
				RawPacket: packet,
				Addr:      addr,
				FlowKey:   flowKey,
			}

			// Process packet
			err = g.processPacket(packetInfo)
			if err != nil {
				g.logger.Printf("Error processing packet: %v", err)
			}

			// Send to processing channel if there's room
			select {
			case g.packetCh <- packetInfo:
			default:
				g.logger.Printf("Packet channel full, dropping packet for flow: %s", flowKey)
			}
		}
	}
}

// processPacket processes a single packet
func (g *Gateway) processPacket(packetInfo PacketInfo) error {
	// Parse TCP packet
	if !g.isTCP(packetInfo.RawPacket) {
		// Not TCP, reinject as-is
		return g.windivert.Send(packetInfo.RawPacket, packetInfo.Addr)
	}

	// Extract TCP payload
	payload, err := g.extractTCPPayload(packetInfo.RawPacket)
	if err != nil {
		g.logger.Printf("Failed to extract TCP payload: %v", err)
		return g.windivert.Send(packetInfo.RawPacket, packetInfo.Addr)
	}

	// Check if packet should be forwarded to proxy
	destPort := g.extractDestinationPort(packetInfo.RawPacket)
	if destPort == 80 || destPort == 443 {
		return g.handleProxyTraffic(packetInfo, payload, destPort)
	}

	// Just reinject packet if no modification needed
	return g.windivert.Send(packetInfo.RawPacket, packetInfo.Addr)
}

// handleProxyTraffic handles traffic redirection to proxy
func (g *Gateway) handleProxyTraffic(packetInfo PacketInfo, payload []byte, destPort uint16) error {
	flowKey := packetInfo.FlowKey

	// Log the redirection
	if destPort == 80 {
		g.logger.Printf("HTTP packet -> proxy: %s, flow: %s", g.config.ProxyAddr, flowKey)
	} else if destPort == 443 {
		g.logger.Printf("HTTPS packet -> proxy: %s, flow: %s", g.config.ProxyAddr, flowKey)
	}

	// Get or create proxy connection
	proxyConn, err := g.getProxyConnection(packetInfo, flowKey, destPort)
	if err != nil {
		g.logger.Printf("Failed to get proxy connection: %v", err)
		return g.windivert.Send(packetInfo.RawPacket, packetInfo.Addr)
	}

	// Handle HTTP traffic
	if destPort == 80 {
		return g.handleHTTPTraffic(packetInfo, payload, proxyConn)
	}

	// Handle HTTPS traffic
	if destPort == 443 {
		return g.handleHTTPSTraffic(packetInfo, payload, proxyConn)
	}

	return g.windivert.Send(packetInfo.RawPacket, packetInfo.Addr)
}

// handleHTTPTraffic handles HTTP traffic modification
func (g *Gateway) handleHTTPTraffic(packetInfo PacketInfo, payload []byte, proxyConn *ProxyConnection) error {
	// Parse HTTP request to extract destination
	destHost, destPort, err := g.parseHTTPRequest(payload)
	if err != nil {
		g.logger.Printf("Failed to parse HTTP request: %v", err)
		return g.windivert.Send(packetInfo.RawPacket, proxyConn.ProxyConn)
	}

	// Update original destination in proxy connection
	originalDest := fmt.Sprintf("%s:%d", destHost, destPort)
	proxyConn.OriginalDest = originalDest
	proxyConn.LastUsed = time.Now()

	g.logger.Printf("HTTP request to %s via proxy %s", originalDest, g.config.ProxyAddr)

	// Forward payload to proxy
	_, err = proxyConn.ProxyConn.Write(payload)
	if err != nil {
		g.logger.Printf("Failed to forward HTTP request to proxy: %v", err)
		return g.windivert.Send(packetInfo.RawPacket, packetInfo.Addr)
	}

	// For HTTP, we need to modify the packet to redirect to proxy
	modifiedPacket := g.redirectPacketToProxy(packetInfo.RawPacket, g.config.ProxyAddr, 8080)
	return g.windivert.Send(modifiedPacket, packetInfo.Addr)
}

// handleHTTPSTraffic handles HTTPS traffic modification
func (g *Gateway) handleHTTPSTraffic(packetInfo PacketInfo, payload []byte, proxyConn *ProxyConnection) error {
	// Check if it's a CONNECT method
	if g.isCONNECTMethod(payload) {
		// Parse CONNECT request
		destHost, destPort, err := g.parseCONNECTRequest(payload)
		if err != nil {
			g.logger.Printf("Failed to parse CONNECT request: %v", err)
			return g.windivert.Send(packetInfo.RawPacket, proxyConn.ProxyConn)
		}

		// Update original destination in proxy connection
		originalDest := fmt.Sprintf("%s:%d", destHost, destPort)
		proxyConn.OriginalDest = originalDest
		proxyConn.LastUsed = time.Now()

		g.logger.Printf("HTTPS CONNECT to %s via proxy %s", originalDest, g.config.ProxyAddr)

		// Forward CONNECT request to proxy
		_, err = proxyConn.ProxyConn.Write(payload)
		if err != nil {
			g.logger.Printf("Failed to forward CONNECT request to proxy: %v", err)
			return g.windivert.Send(packetInfo.RawPacket, packetInfo.Addr)
		}

		// For HTTPS CONNECT, we need to modify the packet to redirect to proxy
		modifiedPacket := g.redirectPacketToProxy(packetInfo.RawPacket, g.config.ProxyAddr, 8080)
		return g.windivert.Send(modifiedPacket, packetInfo.Addr)
	}

	// For HTTPS data traffic, just forward to proxy
	_, err = proxyConn.ProxyConn.Write(payload)
	if err != nil {
		g.logger.Printf("Failed to forward HTTPS data to proxy: %v", err)
		return g.windivert.Send(packetInfo.RawPacket, packetInfo.Addr)
	}

	return g.windivert.Send(packetInfo.RawPacket, packetInfo.Addr)
}

// getProxyConnection gets or creates a proxy connection
func (g *Gateway) getProxyConnection(packetInfo PacketInfo, flowKey string, destPort uint16) (*ProxyConnection, error) {
	g.connPoolMutex.RLock()
	conn, exists := g.proxyConns[flowKey]
	g.connPoolMutex.RUnlock()

	if exists && conn.ProxyConn != nil {
		// Check if connection is still alive
		err := conn.ProxyConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		if err == nil {
			// Connection is still alive, update last used time
			conn.LastUsed = time.Now()
			return conn, nil
		}
	}

	// Connection doesn't exist or is dead, create new one
	g.connPoolMutex.Lock()
	defer g.connPoolMutex.Unlock()

	// Check if we need to cleanup old connections
	if len(g.proxyConns) >= g.connPoolSize {
		g.cleanupOldConnections()
	}

	// Create new proxy connection
	proxyAddr := g.config.ProxyAddr
	if destPort == 443 {
		// For HTTPS, connect to proxy directly
		proxyConn, err := net.Dial("tcp", proxyAddr)
		if err != nil {
			return nil, fmt.Errorf("failed to connect to proxy: %w", err)
		}

		conn = &ProxyConnection{
			OriginalDest: "",
			ProxyConn:    proxyConn,
			CreatedAt:    time.Now(),
			LastUsed:     time.Now(),
		}

		g.proxyConns[flowKey] = conn
		g.logger.Printf("Created new proxy connection for flow: %s", flowKey)
	}

	return conn, nil
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
	for flowKey, conn := range g.proxyConns {
		if now.Sub(conn.LastUsed) > g.connPoolExpiry {
			if conn.ProxyConn != nil {
				conn.ProxyConn.Close()
			}
			delete(g.proxyConns, flowKey)
			g.logger.Printf("Cleaned up expired connection for flow: %s", flowKey)
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
		case packetInfo := <-g.packetCh:
			// Process queued packets
			err := g.processPacket(packetInfo)
			if err != nil {
				g.logger.Printf("Error processing queued packet: %v", err)
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

// Utility functions

func (g *Gateway) generateFlowKey(addr *divert.Address) string {
	return fmt.Sprintf("%s:%d-%s:%d",
		addr.LocalIP, addr.LocalPort,
		addr.RemoteIP, addr.RemotePort)
}

func (g *Gateway) isTCP(packet []byte) bool {
	if len(packet) < 20 {
		return false
	}
	return (packet[12] >> 4) == 4 // IPv4
}

func (g *Gateway) extractTCPPayload(packet []byte) ([]byte, error) {
	if len(packet) < 40 {
		return nil, fmt.Errorf("packet too small")
	}

	// Extract IP header length
	ipHeaderLen := int((packet[0] & 0x0F) * 4)
	if ipHeaderLen > len(packet) {
		return nil, fmt.Errorf("invalid IP header length")
	}

	// Extract TCP header length
	tcpHeaderLen := int((packet[ipHeaderLen+12]&0xF0)>>4) * 4
	if tcpHeaderLen > len(packet)-ipHeaderLen {
		return nil, fmt.Errorf("invalid TCP header length")
	}

	payloadStart := ipHeaderLen + tcpHeaderLen
	if payloadStart >= len(packet) {
		return nil, fmt.Errorf("no payload")
	}

	return packet[payloadStart:], nil
}

func (g *Gateway) extractDestinationPort(packet []byte) uint16 {
	if len(packet) < 22 {
		return 0
	}
	return uint16(packet[20])<<8 | uint16(packet[21])
}

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

	return host, strconv.Atoi(port)
}

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

	return host, strconv.Atoi(port)
}

func (g *Gateway) isCONNECTMethod(payload []byte) bool {
	reader := bufio.NewReader(strings.NewReader(string(payload)))

	// Read first line
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	// Check if it's CONNECT method
	return strings.HasPrefix(strings.ToUpper(line), "CONNECT ")
}

func (g *Gateway) redirectPacketToProxy(packet []byte, proxyAddr string, proxyPort int) []byte {
	// This is a simplified implementation
	// In a real implementation, you would:
	// 1. Parse the IP and TCP headers
	// 2. Update the destination IP and port to proxy
	// 3. Recalculate IP and TCP checksums
	// 4. Serialize the modified packet

	// For now, return the original packet
	// In a production implementation, this would be properly modified
	return packet
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

	// Wait for shutdown signal
	<-gateway.ctx.Done()
	gateway.Stop()
	gateway.logger.Println("Gateway stopped gracefully")
}

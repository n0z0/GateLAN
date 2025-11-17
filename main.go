package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Config represents the forwarder configuration
type Config struct {
	ProxyAddr  string `json:"proxy_addr"`
	BufferSize int    `json:"buffer_size"`
}

// Forwarder represents the simple HTTP forwarder
type Forwarder struct {
	config     *Config
	ctx        context.Context
	cancel     context.CancelFunc
	logger     *log.Logger
	running    bool
	httpClient *http.Client
}

// NewForwarder creates a new Forwarder instance
func NewForwarder(configPath string) (*Forwarder, error) {
	config, err := loadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Create HTTP client that will forward all requests through the upstream proxy
	proxyURL, _ := url.Parse("http://" + config.ProxyAddr)
	httpClient := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 30 * time.Second,
	}

	fwd := &Forwarder{
		config:     config,
		ctx:        ctx,
		cancel:     cancel,
		logger:     log.New(os.Stdout, "[Forwarder] ", log.LstdFlags|log.Lshortfile),
		running:    false,
		httpClient: httpClient,
	}

	return fwd, nil
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

	return &config, nil
}

// Start starts the forwarder
func (f *Forwarder) Start() error {
	f.logger.Println("Starting HTTP Forwarder...")

	// Start a simple HTTP server that forwards all requests
	err := f.startHTTPServer()
	if err != nil {
		return fmt.Errorf("failed to start HTTP server: %w", err)
	}

	// Setup signal handling
	go f.handleSignals()

	f.running = true
	f.logger.Printf("Forwarder started successfully")
	f.logger.Printf("Upstream proxy: %s", f.config.ProxyAddr)
	f.logger.Printf("Forwarder listening on: 0.0.0.0:8080")
	f.logger.Println("Configure your applications to use this forwarder as HTTP/HTTPS proxy")

	return nil
}

// Stop stops the forwarder
func (f *Forwarder) Stop() {
	if !f.running {
		return
	}

	f.logger.Println("Stopping forwarder...")
	f.running = false
	f.cancel()
}

// startHTTPServer starts the HTTP server for forwarding
func (f *Forwarder) startHTTPServer() error {
	// HTTP handler for all requests
	http.HandleFunc("/", f.handleHTTPRequest)

	// Special handler for CONNECT method (HTTPS tunneling)
	http.HandleFunc("/connect", f.handleCONNECTRequest)

	server := &http.Server{
		Addr:         ":8080",
		Handler:      nil, // Using default handler
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		f.logger.Printf("HTTP server listening on %s", server.Addr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			f.logger.Printf("HTTP server error: %v", err)
		}
	}()

	return nil
}

// handleHTTPRequest handles HTTP requests by forwarding them to the upstream proxy
func (f *Forwarder) handleHTTPRequest(w http.ResponseWriter, r *http.Request) {
	f.logger.Printf("HTTP request: %s %s", r.Method, r.URL.String())

	// Create a new request to forward to upstream proxy
	proxyReq, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
	if err != nil {
		f.logger.Printf("Failed to create proxy request: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// Copy headers from original request
	for name, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(name, value)
		}
	}

	// Remove hop-by-hop headers
	removeHopByHopHeaders(proxyReq.Header)

	// Set additional headers for proxy request
	proxyReq.Header.Set("Connection", "keep-alive")
	proxyReq.Header.Set("User-Agent", "SimpleHTTPForwarder/1.0")

	// Forward the request to upstream proxy
	resp, err := f.httpClient.Do(proxyReq)
	if err != nil {
		f.logger.Printf("Failed to forward request: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers back to client
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}

	// Set status code and copy response body
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		f.logger.Printf("Failed to copy response body: %v", err)
	}
}

// handleCONNECTRequest handles HTTPS CONNECT requests for tunneling
func (f *Forwarder) handleCONNECTRequest(w http.ResponseWriter, r *http.Request) {
	// Parse CONNECT request to get target host and port
	targetHost, targetPort, err := parseCONNECTRequest(r.URL.String())
	if err != nil {
		f.logger.Printf("Failed to parse CONNECT request: %v", err)
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	targetAddr := fmt.Sprintf("%s:%s", targetHost, targetPort)
	f.logger.Printf("HTTPS CONNECT to: %s", targetAddr)

	// For CONNECT requests, we need to establish a tunnel through the upstream proxy
	// This is a simplified implementation - in production you might want to use
	// a more sophisticated approach

	// Connect to upstream proxy
	proxyConn, err := net.Dial("tcp", f.config.ProxyAddr)
	if err != nil {
		f.logger.Printf("Failed to connect to upstream proxy: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}
	defer proxyConn.Close()

	// Send CONNECT request to upstream proxy
	connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\nConnection: keep-alive\r\n\r\n", targetAddr, targetAddr)
	_, err = proxyConn.Write([]byte(connectReq))
	if err != nil {
		f.logger.Printf("Failed to send CONNECT to proxy: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	// Read response from upstream proxy
	resp := make([]byte, 1024)
	n, err := proxyConn.Read(resp)
	if err != nil {
		f.logger.Printf("Failed to read CONNECT response: %v", err)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	// Check if CONNECT was successful
	connectResp := string(resp[:n])
	if !strings.Contains(connectResp, "200") {
		f.logger.Printf("CONNECT failed: %s", connectResp)
		http.Error(w, "Bad Gateway", http.StatusBadGateway)
		return
	}

	// Upgrade the connection to support bidirectional forwarding
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		f.logger.Printf("Hijacking not supported")
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	clientConn, _, err := hijacker.Hijack()
	if err != nil {
		f.logger.Printf("Failed to hijack connection: %v", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Send 200 Connection Established to client
	clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	// Setup bidirectional forwarding between client and upstream proxy
	f.setupBidirectionalForward(clientConn, proxyConn)
}

// parseCONNECTRequest parses CONNECT request
func parseCONNECTRequest(hostPort string) (string, string, error) {
	host, portStr, err := net.SplitHostPort(hostPort)
	if err != nil {
		// If no port specified, assume 443 for HTTPS
		host = hostPort
		portStr = "443"
	}

	// Validate port
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return "", "", fmt.Errorf("invalid port: %s", portStr)
	}

	return host, portStr, nil
}

// removeHopByHopHeaders removes hop-by-hop headers
func removeHopByHopHeaders(headers http.Header) {
	hopByHopHeaders := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"TE",
		"Trailers",
		"Transfer-Encoding",
		"Upgrade",
	}

	for _, header := range hopByHopHeaders {
		headers.Del(header)
	}
}

// setupBidirectionalForward sets up bidirectional data forwarding
func (f *Forwarder) setupBidirectionalForward(clientConn net.Conn, proxyConn net.Conn) {
	defer clientConn.Close()
	defer proxyConn.Close()

	// Create channels for handling errors
	errChan := make(chan error, 2)

	// Forward client -> proxy
	go func() {
		_, err := io.Copy(proxyConn, clientConn)
		errChan <- err
	}()

	// Forward proxy -> client
	go func() {
		_, err := io.Copy(clientConn, proxyConn)
		errChan <- err
	}()

	// Wait for first error or successful completion
	err := <-errChan
	if err != nil {
		f.logger.Printf("Forwarding error: %v", err)
	}
}

// handleSignals handles system signals for graceful shutdown
func (f *Forwarder) handleSignals() {
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	sig := <-c
	f.logger.Printf("Received signal: %v", sig)
	f.Stop()
}

// GetStatus returns current forwarder status
func (f *Forwarder) GetStatus() map[string]interface{} {
	return map[string]interface{}{
		"running":     f.running,
		"proxy_addr":  f.config.ProxyAddr,
		"buffer_size": f.config.BufferSize,
		"upstream":    f.config.ProxyAddr,
	}
}

func main() {
	configPath := "config.json"

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Fatalf("Config file not found: %s", configPath)
	}

	// Create forwarder
	forwarder, err := NewForwarder(configPath)
	if err != nil {
		log.Fatalf("Failed to create forwarder: %v", err)
	}

	// Start forwarder
	if err := forwarder.Start(); err != nil {
		log.Fatalf("Failed to start forwarder: %v", err)
	}

	forwarder.logger.Printf("Forwarder status: %+v", forwarder.GetStatus())
	forwarder.logger.Println("Forwarder is running. Press Ctrl+C to stop.")

	// Wait for shutdown signal
	<-forwarder.ctx.Done()
	forwarder.Stop()
	forwarder.logger.Println("Forwarder stopped gracefully")
}

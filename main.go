package main

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

// Config represents the forwarder configuration
type Config struct {
	ProxyAddr  string `json:"proxy_addr"`
	BufferSize int    `json:"buffer_size"`
}

// Forwarder represents the simple HTTP client forwarder
type Forwarder struct {
	config     *Config
	httpClient *http.Client
	logger     *log.Logger
}

// NewForwarder creates a new Forwarder instance
func NewForwarder(configPath string) (*Forwarder, error) {
	config, err := loadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Create HTTP client that will forward all requests through the upstream proxy
	proxyURL, _ := url.Parse("http://" + config.ProxyAddr)

	// Create a custom transport that ignores proxy environment variables
	// and only uses our configured proxy
	transport := &http.Transport{
		Proxy: http.ProxyURL(proxyURL),
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // Allow self-signed certificates for MITM
		},
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}

	fwd := &Forwarder{
		config:     config,
		httpClient: httpClient,
		logger:     log.New(os.Stdout, "[Forwarder] ", log.LstdFlags|log.Lshortfile),
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

// ForwardRequest forwards an HTTP request through the upstream proxy
func (f *Forwarder) ForwardRequest(req *http.Request) (*http.Response, error) {
	f.logger.Printf("Forwarding request: %s %s", req.Method, req.URL.String())

	// Create a copy of the request to avoid modifying the original
	proxyReq, err := http.NewRequest(req.Method, req.URL.String(), req.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to create proxy request: %w", err)
	}

	// Copy headers from original request
	for name, values := range req.Header {
		for _, value := range values {
			proxyReq.Header.Add(name, value)
		}
	}

	// Remove hop-by-hop headers that shouldn't be forwarded
	f.removeHopByHopHeaders(proxyReq.Header)

	// Set additional headers for proxy request
	proxyReq.Header.Set("User-Agent", "SimpleHTTPForwarder/1.0")

	// Forward the request to upstream proxy
	resp, err := f.httpClient.Do(proxyReq)
	if err != nil {
		return nil, fmt.Errorf("failed to forward request: %w", err)
	}

	return resp, nil
}

// ForwardHTTPRequest is a convenience method for simple HTTP requests
func (f *Forwarder) ForwardHTTPRequest(method, urlStr string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequest(method, urlStr, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	return f.ForwardRequest(req)
}

// GetHTTPClient returns the configured HTTP client for direct use
func (f *Forwarder) GetHTTPClient() *http.Client {
	return f.httpClient
}

// GetConfig returns the forwarder configuration
func (f *Forwarder) GetConfig() *Config {
	return f.config
}

// removeHopByHopHeaders removes hop-by-hop headers
func (f *Forwarder) removeHopByHopHeaders(headers http.Header) {
	hopByHopHeaders := []string{
		"Connection",
		"Keep-Alive",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"TE",
		"Trailers",
		"Transfer-Encoding",
		"Upgrade",
		"Proxy-Connection",
	}

	for _, header := range hopByHopHeaders {
		headers.Del(header)
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

	log.Printf("HTTP Forwarder Client - Ready")
	log.Printf("Upstream proxy: %s", forwarder.GetConfig().ProxyAddr)
	log.Printf("Buffer size: %d bytes", forwarder.GetConfig().BufferSize)
	log.Println("")
	log.Println("This is a client-side HTTP forwarder tool.")
	log.Println("It provides an HTTP client that forwards requests through the upstream proxy.")
	log.Println("No server is listening - this is a library/tool for your applications.")
	log.Println("")
	log.Printf("Usage examples:")
	log.Printf("- Use GetHTTPClient() to get the configured HTTP client")
	log.Printf("- Use ForwardHTTPRequest() for simple request forwarding")
	log.Printf("- Use ForwardRequest() for full control over requests")
	log.Println("")

	// Test the connection
	testURL := "http://httpbin.org/ip"
	if resp, err := forwarder.ForwardHTTPRequest("GET", testURL, nil); err != nil {
		log.Printf("Test request failed: %v", err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			log.Printf("Test successful! Response: %s", string(body))
		} else {
			log.Printf("Test request returned status: %d", resp.StatusCode)
		}
	}

	log.Println("")
	log.Println("Forwarder is ready for use. Configure your applications to use this as a proxy client.")
	log.Println("Press Ctrl+C to exit.")

	// Keep the application running to show it's ready
	select {}
}

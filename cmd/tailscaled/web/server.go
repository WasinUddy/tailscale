// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package web

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"

	"tailscale.com/ipn/ipnlocal"
	"tailscale.com/types/logger"
)

// Server represents the custom web server
type Server struct {
	logf logger.Logf
	addr string
	lb   *ipnlocal.LocalBackend
}

// New creates a new web server
// lb can be nil initially and set later with SetLocalBackend
func New(logf logger.Logf, addr string) *Server {
	return &Server{
		logf: logf,
		addr: addr,
	}
}

// SetLocalBackend sets the LocalBackend for Tailscale network checks
func (s *Server) SetLocalBackend(lb *ipnlocal.LocalBackend) {
	s.lb = lb
}

// Start starts the web server in a goroutine
func (s *Server) Start() {
	mux := http.NewServeMux()

	// Root endpoint - return hostname
	mux.HandleFunc("/", s.requireTailscale(s.handleRoot))

	// Metrics endpoint - Prometheus format
	mux.HandleFunc("/metrics", s.requireTailscale(s.handleMetrics))

	// Shutdown endpoint - force shutdown machine
	mux.HandleFunc("/shutdown", s.requireTailscale(s.handleShutdown))

	server := &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	s.logf("Starting custom web server on %s", s.addr)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logf("Web server error: %v", err)
		}
	}()
}

// handleRoot returns the hostname
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
		s.logf("Failed to get hostname: %v", err)
	}

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "hostname: %s\n", hostname)
}

// handleMetrics returns system metrics in Prometheus format
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	metrics, err := GetSystemMetrics()
	if err != nil {
		s.logf("Failed to get metrics: %v", err)
		http.Error(w, fmt.Sprintf("Failed to get metrics: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP system_cpu_usage_percent CPU usage percentage\n")
	fmt.Fprintf(w, "# TYPE system_cpu_usage_percent gauge\n")
	fmt.Fprintf(w, "system_cpu_usage_percent %.2f\n\n", metrics.CPUPercent)

	fmt.Fprintf(w, "# HELP system_memory_used_bytes Memory used in bytes\n")
	fmt.Fprintf(w, "# TYPE system_memory_used_bytes gauge\n")
	fmt.Fprintf(w, "system_memory_used_bytes %d\n\n", metrics.MemoryUsed)

	fmt.Fprintf(w, "# HELP system_memory_total_bytes Total memory in bytes\n")
	fmt.Fprintf(w, "# TYPE system_memory_total_bytes gauge\n")
	fmt.Fprintf(w, "system_memory_total_bytes %d\n\n", metrics.MemoryTotal)

	fmt.Fprintf(w, "# HELP system_memory_usage_percent Memory usage percentage\n")
	fmt.Fprintf(w, "# TYPE system_memory_usage_percent gauge\n")
	fmt.Fprintf(w, "system_memory_usage_percent %.2f\n\n", metrics.MemoryPercent)

	fmt.Fprintf(w, "# HELP system_disk_used_bytes Disk used in bytes\n")
	fmt.Fprintf(w, "# TYPE system_disk_used_bytes gauge\n")
	fmt.Fprintf(w, "system_disk_used_bytes %d\n\n", metrics.DiskUsed)

	fmt.Fprintf(w, "# HELP system_disk_total_bytes Total disk space in bytes\n")
	fmt.Fprintf(w, "# TYPE system_disk_total_bytes gauge\n")
	fmt.Fprintf(w, "system_disk_total_bytes %d\n\n", metrics.DiskTotal)

	fmt.Fprintf(w, "# HELP system_disk_usage_percent Disk usage percentage\n")
	fmt.Fprintf(w, "# TYPE system_disk_usage_percent gauge\n")
	fmt.Fprintf(w, "system_disk_usage_percent %.2f\n\n", metrics.DiskPercent)

	fmt.Fprintf(w, "# HELP system_network_bytes_sent Network bytes sent\n")
	fmt.Fprintf(w, "# TYPE system_network_bytes_sent counter\n")
	fmt.Fprintf(w, "system_network_bytes_sent %d\n\n", metrics.NetworkBytesSent)

	fmt.Fprintf(w, "# HELP system_network_bytes_recv Network bytes received\n")
	fmt.Fprintf(w, "# TYPE system_network_bytes_recv counter\n")
	fmt.Fprintf(w, "system_network_bytes_recv %d\n\n", metrics.NetworkBytesRecv)

	fmt.Fprintf(w, "# HELP system_uptime_seconds System uptime in seconds\n")
	fmt.Fprintf(w, "# TYPE system_uptime_seconds counter\n")
	fmt.Fprintf(w, "system_uptime_seconds %d\n", metrics.UptimeSeconds)
}

// handleShutdown shuts down the machine
// Query parameter: force=true for forced shutdown, force=false (default) for graceful
func (s *Server) handleShutdown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed (use POST)", http.StatusMethodNotAllowed)
		return
	}

	// Parse force parameter (default: false for graceful)
	force := r.URL.Query().Get("force") == "true"

	shutdownType := "graceful"
	if force {
		shutdownType = "forced"
	}
	s.logf("Shutdown requested via web API (%s)", shutdownType)

	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprintf(w, "Shutdown initiated (%s)...\n", shutdownType)

	// Flush response before shutting down
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Shutdown in a goroutine to allow response to be sent
	go func() {
		if err := ShutdownSystem(force); err != nil {
			s.logf("Shutdown failed: %v", err)
		}
	}()
}

// requireTailscale is middleware that restricts access to Tailscale network only
func (s *Server) requireTailscale(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get client IP
		clientIP := r.RemoteAddr
		if host, _, err := net.SplitHostPort(clientIP); err == nil {
			clientIP = host
		}

		// Check if request is from localhost (always allow for local testing)
		if isLocalhost(clientIP) {
			next(w, r)
			return
		}

		// Parse client IP
		addr, err := netip.ParseAddr(clientIP)
		if err != nil {
			s.logf("Invalid client IP %s: %v", clientIP, err)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		// Check if IP is in Tailscale CGNAT range (100.64.0.0/10)
		if isTailscaleIP(addr) {
			next(w, r)
			return
		}

		// If we have LocalBackend, check if it's a known peer
		if s.lb != nil {
			status := s.lb.StatusWithoutPeers()
			if status.Self != nil {
				// Check if it's our own Tailscale IP
				for _, ip := range status.Self.TailscaleIPs {
					if ip == addr {
						next(w, r)
						return
					}
				}
			}
		}

		s.logf("Blocked request from non-Tailscale IP: %s", clientIP)
		http.Error(w, "Forbidden: Only accessible from Tailscale network", http.StatusForbidden)
	}
}

// isLocalhost checks if an IP is localhost
func isLocalhost(ip string) bool {
	return ip == "127.0.0.1" || ip == "::1" || ip == "localhost"
}

// isTailscaleIP checks if an IP is in the Tailscale CGNAT range (100.64.0.0/10)
func isTailscaleIP(addr netip.Addr) bool {
	// Tailscale uses 100.64.0.0/10 for IPv4
	if addr.Is4() {
		// 100.64.0.0/10 means 100.64.0.0 to 100.127.255.255
		bytes := addr.As4()
		return bytes[0] == 100 && (bytes[1]&0xC0) == 64
	}

	// For IPv6, check if it's in fd7a:115c:a1e0::/48 (Tailscale IPv6 range)
	if addr.Is6() {
		str := addr.String()
		return strings.HasPrefix(str, "fd7a:115c:a1e0:")
	}

	return false
}

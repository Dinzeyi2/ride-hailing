// Command railway-gateway exposes the independently implemented core services
// behind one public Railway domain. It is intentionally deployment-only: the
// application services still run as normal binaries on private loopback ports.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"
)

var services = []struct {
	name   string
	prefix string
	target string
}{
	{name: "auth", prefix: "/auth", target: "http://127.0.0.1:9081"},
	{name: "mobile", prefix: "/mobile", target: "http://127.0.0.1:9087"},
	{name: "rides", prefix: "/rides", target: "http://127.0.0.1:9082"},
	{name: "geo", prefix: "/geo", target: "http://127.0.0.1:9083"},
	{name: "payments", prefix: "/payments", target: "http://127.0.0.1:9084"},
	{name: "notifications", prefix: "/notifications", target: "http://127.0.0.1:9085"},
	{name: "realtime", prefix: "/realtime", target: "http://127.0.0.1:9086"},
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", health)
	mux.HandleFunc("/", index)

	for _, service := range services {
		target, err := url.Parse(service.target)
		if err != nil {
			log.Fatalf("parse %s target: %v", service.name, err)
		}
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
			log.Printf("upstream proxy error: %v", err)
			http.Error(w, "service temporarily unavailable", http.StatusBadGateway)
		}
		prefix := service.prefix
		mux.Handle(prefix+"/", http.StripPrefix(prefix, proxy))
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	log.Printf("Railway gateway listening on :%s", port)
	log.Fatal(server.ListenAndServe())
}

func index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"name":     "ride-hailing-api",
		"status":   "running",
		"services": []string{"auth", "mobile", "rides", "geo", "payments", "notifications", "realtime"},
	})
}

func health(w http.ResponseWriter, r *http.Request) {
	client := http.Client{Timeout: 2 * time.Second}
	status := make(map[string]string, len(services))
	healthy := true
	for _, service := range services {
		response, err := client.Get(service.target + "/healthz")
		if err != nil {
			status[service.name] = err.Error()
			healthy = false
			continue
		}
		_ = response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			status[service.name] = fmt.Sprintf("HTTP %d", response.StatusCode)
			healthy = false
		} else {
			status[service.name] = "healthy"
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if !healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":   strings.TrimSpace(map[bool]string{true: "healthy", false: "degraded"}[healthy]),
		"services": status,
	})
}

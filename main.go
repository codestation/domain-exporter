// Copyright 2025 codestation. All rights reserved.
// Use of this source code is governed by a MIT-license
// that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"gopkg.in/yaml.v3"
)

type Domain struct {
	Domain    string    `yaml:"domain"`
	Expires   string    `yaml:"expires"`
	ExpiresAt time.Time // Preloaded time conversion
}

type DomainConfig struct {
	Domains []Domain `yaml:"domains"`
}

var domainExpirationGauge = prometheus.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "domain_days_to_expire",
		Help: "Days until the domain expires",
	},
	[]string{"domain"},
)

func loadConfig(filePath string) (*DomainConfig, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config DomainConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config file: %w", err)
	}

	// Preload time conversion
	for i, domain := range config.Domains {
		expirationDate, err := time.Parse("2006-01-02", domain.Expires)
		if err != nil {
			return nil, fmt.Errorf("invalid expiration date for domain %s: %w", domain.Domain, err)
		}
		config.Domains[i].ExpiresAt = expirationDate
	}

	return &config, nil
}

func updateMetrics(config *DomainConfig) {
	for _, domain := range config.Domains {
		daysToExpire := math.Ceil(time.Until(domain.ExpiresAt).Hours() / 24)
		if daysToExpire < 0 {
			daysToExpire = 0
		}

		domainExpirationGauge.WithLabelValues(domain.Domain).Set(daysToExpire)
	}
}

func main() {
	// Command-line flags
	bindAddr := flag.String("address", getEnv("ADDRESS", ":8080"), "Address to bind the HTTP server")
	configPath := flag.String("config", getEnv("CONFIG_PATH", "config.yaml"), "Path to the configuration file")
	flag.Parse()

	registry := prometheus.NewRegistry()
	registry.MustRegister(domainExpirationGauge)
	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry})

	config, err := loadConfig(*configPath)
	if err != nil {
		slog.Error("Error loading config", "error", err)
		os.Exit(1)
	}

	slog.Info("domain-exporter started",
		slog.String("version", Tag),
		slog.String("commit", Revision),
		slog.Time("date", LastCommit),
		slog.Bool("clean_build", !Modified),
	)

	updateMetrics(config)

	http.Handle("/metrics", handler)

	server := &http.Server{Addr: *bindAddr}

	// Graceful shutdown handling
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("Prometheus exporter running", "address", *bindAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}()

	<-stop
	slog.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	} else {
		slog.Info("Server gracefully stopped")
	}
}

// getEnv retrieves the value of the environment variable or returns the fallback value.
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

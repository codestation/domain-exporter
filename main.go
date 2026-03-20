// Copyright 2026 codestation. All rights reserved.
// Use of this source code is governed by a MIT-license
// that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/posflag"
	"github.com/knadh/koanf/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	flag "github.com/spf13/pflag"
	"golang.org/x/term"
)

const (
	DefaultConfigPath = "config.yaml"
	DefaultAddress    = ":8080"
)

type Domain struct {
	Domain  string    `yaml:"domain"`
	Expires time.Time `yaml:"expires"`
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

func updateMetrics(config DomainConfig) {
	domainExpirationGauge.Reset()

	for _, domain := range config.Domains {
		daysToExpire := math.Ceil(time.Until(domain.Expires).Hours() / 24)
		if daysToExpire < 0 {
			daysToExpire = 0
		}

		domainExpirationGauge.WithLabelValues(domain.Domain).Set(daysToExpire)
	}
}

func main() {
	f := flag.NewFlagSet("config", flag.ContinueOnError)
	f.Usage = func() {
		fmt.Println(f.FlagUsages())
		os.Exit(0)
	}

	f.StringP("config-path", "c", "", "path to one or more configuration files")
	f.StringP("address", "a", "", "Address to bind the HTTP server")
	f.StringP("log-format", "f", "", "Log format (logfmt, json)")
	f.BoolP("version", "v", false, "Print version information")

	if err := f.Parse(os.Args[1:]); err != nil {
		slog.Error("Failed to parse command line args", slog.String("error", err.Error()))
		os.Exit(1)
	}

	showVersion, err := f.GetBool("version")
	if err != nil {
		panic(err)
	}

	if showVersion {
		slog.Info("domain-exporter",
			slog.String("version", Tag),
			slog.String("commit", Revision),
			slog.Time("date", LastCommit),
			slog.Bool("clean_build", !Modified),
		)
		os.Exit(0)
	}

	// load config path from flag and env by hand to avoid catch-22 with koanf
	configPath, err := f.GetString("config-path")
	if err != nil {
		slog.Error("Failed to get config path", slog.String("error", err.Error()))
		os.Exit(1)
	}

	if configPath == "" {
		envPath := os.Getenv("CONFIG_PATH")
		if envPath != "" {
			configPath = envPath
		} else {
			configPath = DefaultConfigPath
		}
	}

	k := koanf.New(".")

	err = k.Load(confmap.Provider(map[string]any{
		"config-path": DefaultConfigPath,
		"address":     DefaultAddress,
	}, "."), nil)
	if err != nil {
		// panic here, the config was provided by the app itself
		panic(err)
	}

	// load config from file
	config, err := NewConfig[DomainConfig](k, configPath)
	if err != nil {
		slog.Error("Failed to load configuration file", slog.String("path", configPath), slog.String("error", err.Error()))
		os.Exit(1)
	}

	if err := config.Parse(""); err != nil {
		slog.Error("Error loading config", slog.String("error", err.Error()))
		os.Exit(1)
	}

	// load config from env
	err = k.Load(env.Provider("", ".", func(s string) string {
		return strings.ReplaceAll(strings.ToLower(s), "_", "-")
	}), nil)
	if err != nil {
		slog.Error("Failed to load environment variables", "error", err)
		os.Exit(1)
	}

	// load config from flags
	err = k.Load(posflag.ProviderWithValue(f, ".", k, func(key string, value string) (string, any) {
		return strings.ReplaceAll(key, "-", "."), value
	}), nil)
	if err != nil {
		slog.Error("Failed to load flags", "error", err)
		os.Exit(1)
	}

	isTerminal := term.IsTerminal(int(os.Stdout.Fd()))

	switch k.String("log.format") {
	case "json":
		slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	case "logfmt":
	case "":
		if !isTerminal {
			slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
		}
	default:
		slog.Error("Invalid log format specified")
		os.Exit(1)
	}

	registry := prometheus.NewRegistry()
	registry.MustRegister(domainExpirationGauge)
	handler := promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry})

	slog.Info("domain-exporter started",
		slog.String("version", Tag),
		slog.String("commit", Revision),
		slog.Time("date", LastCommit),
		slog.Bool("clean_build", !Modified),
	)

	changed := make(chan struct{})
	_ = config.Watch(func() {
		slog.Info("Configuration file changed, reloading...")
		changed <- struct{}{}
	})
	defer func(config *Config[DomainConfig]) {
		err := config.Unwatch()
		if err != nil {
			slog.Error("Failed to unwatch", slog.String("error", err.Error()))
		}
	}(config)

	updateMetrics(config.Get())

	var wg sync.WaitGroup
	done := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-changed:
				updateMetrics(config.Get())
			case <-ticker.C:
				updateMetrics(config.Get())
			case <-done:
				return
			}
		}
	}()

	http.Handle("/metrics", handler)

	bindAddr := k.String("address")
	server := &http.Server{Addr: bindAddr}

	// Graceful shutdown handling
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("Prometheus exporter running", "address", bindAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server error", "error", err)
			os.Exit(1)
		}
	}()

	<-stop
	close(done)
	slog.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	} else {
		slog.Info("Server gracefully stopped")
	}

	wg.Wait()
}

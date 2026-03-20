// Copyright 2026 codestation. All rights reserved.
// Use of this source code is governed by a MIT-license
// that can be found in the LICENSE file.

package main

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config[T any] struct {
	config     T
	configFile *file.File
	configMu   sync.RWMutex
	k          *koanf.Koanf
	timeHook   any
}

func NewConfig[T any](k *koanf.Koanf, path string) (*Config[T], error) {
	configFile := file.Provider(path)
	err := k.Load(configFile, yaml.Parser())
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	timeHook := mapstructure.StringToTimeHookFunc("2006-01-02")

	return &Config[T]{
		configFile: configFile,
		k:          k,
		timeHook:   timeHook,
	}, nil
}

func (c *Config[T]) Watch(cb func()) error {
	err := c.configFile.Watch(func(event any, err error) {
		if err != nil {
			slog.Error("Failed to watch configuration file", slog.String("error", err.Error()))
			return
		}

		err = c.k.Load(c.configFile, yaml.Parser())
		if err != nil {
			slog.Error("Failed to reload configuration file", slog.String("error", err.Error()))
			return
		}

		if err := c.Parse(""); err != nil {
			slog.Error("Error loading config", slog.String("error", err.Error()))
			return
		}

		cb()
	})
	if err != nil {
		return fmt.Errorf("error watching configuration file: %w", err)
	}

	return nil
}

func (c *Config[T]) Parse(path string) error {
	var config T
	err := c.k.UnmarshalWithConf(path, &config, koanf.UnmarshalConf{
		Tag: "yaml",
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook: c.timeHook,
			Result:     &config,
			TagName:    "yaml",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to unmarshal: %w", err)
	}

	c.configMu.Lock()
	defer c.configMu.Unlock()
	c.config = config

	return nil
}

func (c *Config[T]) Unwatch() error {
	return c.configFile.Unwatch()
}

func (c *Config[T]) Get() T {
	c.configMu.RLock()
	defer c.configMu.RUnlock()
	return c.config
}

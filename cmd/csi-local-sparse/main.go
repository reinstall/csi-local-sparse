/*
Copyright 2023 Aleksandr Ovsiankin

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"fmt"
	"github.com/jessevdk/go-flags"
	"github.com/reinstall/csi-local-sparse/internal/plugin"
	"github.com/reinstall/csi-local-sparse/internal/volumes"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"log"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

var (
	// PluginName csi plugin name
	PluginName = "local-sparse.csi.reinstall.ru"
	// PluginVersion csi plugin version
	PluginVersion = "1.0.0"
)

var cfg Config

func main() {
	parser := flags.NewParser(&cfg, flags.Default)
	_, err := parser.Parse()
	if err != nil {
		log.Fatal(fatalJsonLog("Failed to parse config.", err))
	}

	logger, err := initLogger(cfg.LogLevel, cfg.LogJSON)
	if err != nil {
		log.Fatal(fatalJsonLog("Failed to init logger.", err))
	}

	ctx, cancelFunc := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer cancelFunc()
	go func() {
		<-ctx.Done()
		logger.Info("Received exit signal! Initialize graceful shutdown")
	}()

	defer func() {
		if msg := recover(); msg != nil {
			err := fmt.Errorf("%s", msg)
			logger.Error("recovered from panic, but application will be terminated", zap.Error(err))
		}
	}()

	volumeManager := volumes.NewLinuxSparseFileVolumeController(cfg.ImagesDir, cfg.UseDirectIO, logger)
	mounter := volumes.NewLinuxMounter(logger)
	csiPlugin := plugin.NewPlugin(PluginName, PluginVersion, cfg.NodeId, cfg.NodeNameTopologyKey, cfg.GrpcSocket, volumeManager, mounter, logger)

	err = csiPlugin.Run(ctx)
	if err != nil {
		logger.Fatal("Error run plugin", zap.Error(err))
	}
}

func fatalJsonLog(msg string, err error) string {
	escape := func(s string) string {
		return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`)
	}
	errString := ""
	if err != nil {
		errString = err.Error()
	}
	return fmt.Sprintf(
		`{"level":"fatal","ts":"%s","msg":"%s","error":"%s"}`,
		time.Now().Format(time.RFC3339),
		escape(msg),
		escape(errString),
	)
}

// initLogger creates and configs new logger
func initLogger(logLevel string, isLogJson bool) (*zap.Logger, error) {
	lvl := zap.InfoLevel
	err := lvl.UnmarshalText([]byte(logLevel))
	if err != nil {
		return nil, fmt.Errorf("can't unmarshal log-level: %w", err)
	}
	opts := zap.NewProductionConfig()
	opts.Level = zap.NewAtomicLevelAt(lvl)
	opts.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	if opts.InitialFields == nil {
		opts.InitialFields = map[string]interface{}{}
	}
	opts.InitialFields["version"] = PluginVersion
	if !isLogJson {
		opts.Encoding = "console"
		opts.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	}

	return opts.Build()
}

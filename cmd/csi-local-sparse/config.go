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

// Config application config
type Config struct {
	// LogLevel log level
	LogLevel string `long:"log-level" description:"Log level: panic, fatal, warn or warning, info, debug" env:"LOG_LEVEL" default:"info"`
	// LogJSON output logs in json format if true
	LogJSON bool `long:"log-json" description:"Enable force log format JSON" env:"LOG_JSON"`
	// GrpcSocket grpc listening socket
	GrpcSocket string `long:"grpc-listen-socket" description:"Listening socket of grpc-server (only unix socket supported)" env:"GRPC_LISTEN_SOCKET" required:"true"`
	// ImagesDir Path where sparse files will be store (must be existed)
	ImagesDir string `long:"images-dir" description:"Path where sparse files will be store (must be existed)" env:"IMAGES_DIR" required:"true"`
	// NodeId Identifier of node where this instance is running
	NodeId string `long:"node" description:"Identifier of node where this instance is running" env:"NODE_ID" required:"true"`
	// NodeNameTopologyKey kubernetes node label, that will be used for accessible topology
	NodeNameTopologyKey string `long:"node-name-topology-key" description:"Kubernetes node label, that will be used for accessible topology" env:"NODE_NAME_TOPOLOGY_KEY" required:"true"`
	// UseDirectIO
	UseDirectIO bool `long:"direct-io" description:"Use direct-io on loop devices" env:"DIRECT_IO"`
}

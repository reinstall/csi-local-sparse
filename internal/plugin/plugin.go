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

package plugin

import (
	"context"
	"fmt"
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/reinstall/csi-local-sparse/internal/volumes"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"net"
	"net/url"
	"os"
	"path"
	"path/filepath"
)

// Plugin implements csi plugin spec
type Plugin struct {
	csi.UnimplementedIdentityServer
	csi.UnimplementedControllerServer
	csi.UnimplementedNodeServer

	// name plugin name
	name string
	// version plugin version
	version string
	// nodeId ID of host where this plugin's instance is running
	nodeId string

	// nodeNameTopologyKey kubernetes node topology key
	nodeNameTopologyKey string

	// socket listening grpc socket
	socket string

	// volumeController volume controller
	volumeController volumes.VolumeController
	// mounter volume mounter
	mounter volumes.Mounter

	// logger .
	logger *zap.Logger
}

// NewPlugin returns new plugin
func NewPlugin(
	name string,
	version string,
	nodeId string,
	nodeNameTopologyKey string,
	socket string,
	volumeManager volumes.VolumeController,
	mounter volumes.Mounter,
	logger *zap.Logger,
) *Plugin {
	return &Plugin{
		name:                name,
		version:             version,
		nodeId:              nodeId,
		nodeNameTopologyKey: nodeNameTopologyKey,
		socket:              socket,
		volumeController:    volumeManager,
		mounter:             mounter,
		logger:              logger.With(zap.String("logger", "plugin")),
	}
}

// Run runs grpc server and socket listening
func (p *Plugin) Run(ctx context.Context) error {
	errHandler := func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		resp, err := handler(ctx, req)
		if err != nil {
			p.logger.Error("method failed", zap.Error(err))
		}
		return resp, err
	}

	u, err := url.Parse(p.socket)
	if err != nil {
		return fmt.Errorf("unable to parse grpc listen address: %w", err)
	}

	grpcAddr := path.Join(u.Host, filepath.FromSlash(u.Path))
	if u.Host == "" {
		grpcAddr = filepath.FromSlash(u.Path)
	}

	if u.Scheme != "unix" {
		return fmt.Errorf("only unix domains are supported, but %s given", u.Scheme)
	}

	// remove socket when it was already created by past run
	err = os.Remove(grpcAddr)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove unix socket (%s): %w", grpcAddr, err)
	}

	grpcListener, err := net.Listen(u.Scheme, grpcAddr)
	if err != nil {
		return fmt.Errorf("failed to listen socket: %w", err)
	}

	srv := grpc.NewServer(grpc.UnaryInterceptor(errHandler))
	csi.RegisterIdentityServer(srv, p)
	csi.RegisterControllerServer(srv, p)
	csi.RegisterNodeServer(srv, p)

	go func() {
		<-ctx.Done()
		srv.GracefulStop()
	}()

	return srv.Serve(grpcListener)
}

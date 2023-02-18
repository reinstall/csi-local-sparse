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
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/reinstall/csi-local-sparse/internal/volumes"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NodeStageVolume mounts the volume to a staging path
func (p *Plugin) NodeStageVolume(ctx context.Context, request *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	volumeId := request.VolumeId
	p.logger.Debug("NodeStageVolume called", zap.String("volume_id", volumeId))

	if volumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeStageVolume invalid argument: volumeId")
	}

	if request.StagingTargetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "NodeStageVolume (%s) invalid argument: StagingTargetPath", volumeId)
	}

	if request.VolumeCapability == nil {
		return nil, status.Errorf(codes.InvalidArgument, "NodeStageVolume (%s) invalid argument: VolumeCapability", volumeId)
	}

	switch request.VolumeCapability.AccessType.(type) {
	//case *csi.VolumeCapability_Block: // todo: implement block type
	case *csi.VolumeCapability_Mount:
	default:
		return nil, status.Errorf(codes.Unimplemented, "NodeStageVolume (%s) unsupported access type", volumeId)
	}

	mnt := request.VolumeCapability.GetMount()
	mntOptions := mnt.MountFlags

	fsType := "ext4"
	if mnt.FsType != "" {
		fsType = mnt.FsType
	}

	stagingTargetPath := request.StagingTargetPath

	if err := p.volumeController.FormatIfNot(ctx, volumeId, fsType); err != nil {
		return nil, status.Errorf(codes.Internal, "NodeStageVolume (%s) error format volume device", volumeId)
	}

	dev, err := p.volumeController.AttachDevice(ctx, volumeId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "NodeStageVolume (%s) error attach device: %v", volumeId, err)
	}

	if err := p.mounter.Mount(ctx, dev, stagingTargetPath, mntOptions); err != nil {
		return nil, status.Errorf(codes.Internal, "NodeStageVolume (%s) error mount target: %v", volumeId, err.Error())
	}

	p.logger.Info("NodeStageVolume volume was formatted, attached and mounted to staging path", zap.String("volume_id", volumeId))
	return &csi.NodeStageVolumeResponse{}, nil
}

// NodeUnstageVolume unmounts staging path
func (p *Plugin) NodeUnstageVolume(ctx context.Context, request *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	volumeId := request.VolumeId
	p.logger.Debug("NodeUnstageVolume called", zap.String("volume_id", volumeId))

	if volumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeUnstageVolume invalid argument: volumeId")
	}

	if request.StagingTargetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "NodeUnstageVolume (%s) invalid argument: StagingTargetPath", volumeId)
	}

	if err := p.mounter.Unmount(ctx, request.StagingTargetPath); err != nil {
		return nil, status.Errorf(codes.Internal, "NodeUnstageVolume (%s) error unmount staging target: %v", volumeId, err)
	}

	if err := p.volumeController.DetachDevice(ctx, volumeId); err != nil {
		return nil, status.Errorf(codes.Internal, "NodeUnstageVolume (%s) error detach device: %v", volumeId, err)
	}

	p.logger.Info("NodeUnstageVolume volume was unmounted and detached", zap.String("volume_id", volumeId))
	return &csi.NodeUnstageVolumeResponse{}, nil
}

// NodePublishVolume mounts staging path to target path
func (p *Plugin) NodePublishVolume(ctx context.Context, request *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	volumeId := request.VolumeId
	p.logger.Debug("NodePublishVolume called", zap.String("volume_id", volumeId))

	if volumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodePublishVolume invalid argument: VolumeId")
	}

	if request.StagingTargetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "NodePublishVolume (%s) invalid argument: StagingTargetPath", volumeId)
	}

	if request.TargetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "NodePublishVolume (%s) invalid argument: TargetPath", volumeId)
	}

	if request.VolumeCapability == nil {
		return nil, status.Errorf(codes.InvalidArgument, "NodePublishVolume (%s) invalid argument: VolumeCapability", volumeId)
	}

	switch request.VolumeCapability.AccessType.(type) {
	// case *csi.VolumeCapability_Block: // todo: implement block mode
	case *csi.VolumeCapability_Mount:
	default:
		return nil, status.Errorf(codes.InvalidArgument, "NodePublishVolume (%s) unsupported access type", volumeId)
	}

	source := request.StagingTargetPath
	target := request.TargetPath
	mountOptions := []string{"bind"}
	if request.Readonly {
		mountOptions = append(mountOptions, "ro")
	}

	mnt := request.VolumeCapability.GetMount()
	for _, flag := range mnt.MountFlags {
		mountOptions = append(mountOptions, flag)
	}

	if err := p.mounter.Mount(ctx, source, target, mountOptions); err != nil {
		return nil, status.Errorf(codes.Internal, "NodePublishVolume (%s) error mount volume: %v", volumeId, err)
	}

	p.logger.Info("NodePublishVolume volume was mounted to target path", zap.String("volume_id", volumeId))
	return &csi.NodePublishVolumeResponse{}, nil
}

// NodeUnpublishVolume unmounts target path
func (p *Plugin) NodeUnpublishVolume(ctx context.Context, request *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	volumeId := request.VolumeId
	p.logger.Debug("NodeUnpublishVolume called", zap.String("volume_id", request.VolumeId))

	if volumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeUnpublishVolume invalid argument: VolumeId")
	}

	if request.TargetPath == "" {
		return nil, status.Errorf(codes.InvalidArgument, "NodeUnpublishVolume (%s) invalid argument: TargetPath", volumeId)
	}

	target := request.TargetPath
	if err := p.mounter.Unmount(ctx, target); err != nil {
		return nil, status.Errorf(codes.Internal, "NodeUnpublishVolume (%s) error unmount volume: %v", volumeId, err)
	}

	p.logger.Info("NodeUnpublishVolume target path was unmounted", zap.String("volume_id", request.VolumeId))
	return &csi.NodeUnpublishVolumeResponse{}, nil
}

// NodeExpandVolume .
func (p *Plugin) NodeExpandVolume(ctx context.Context, request *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	volumeId := request.VolumeId
	p.logger.Debug("NodeExpandVolume called", zap.String("volume_id", volumeId))

	if request.VolumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeExpandVolume invalid argument: VolumeId")
	}

	if request.VolumeCapability == nil {
		return nil, status.Errorf(codes.InvalidArgument, "NodeExpandVolume (%s) invalid argument: VolumeCapability", volumeId)
	}

	switch request.VolumeCapability.AccessType.(type) {
	//case *csi.VolumeCapability_Block: // todo: implement block type
	case *csi.VolumeCapability_Mount:
	default:
		return nil, status.Errorf(codes.Unimplemented, "NodeExpandVolume (%s) unsupported access type", volumeId)
	}

	size, err := p.calculateVolumeSize(request.CapacityRange)
	if err != nil {
		return nil, status.Errorf(codes.OutOfRange, "NodeExpandVolume (%s) invalid argument: capacityRange: %v", volumeId, err)
	}

	if err := p.volumeController.ExpandVolumeSize(ctx, volumeId, size); err != nil {
		if err == volumes.ErrorVolumeNotFound {
			return nil, status.Errorf(codes.NotFound, "NodeExpandVolume error expand volume size: volume (%s) not found", volumeId)
		}

		return nil, status.Errorf(codes.Internal, "NodeExpandVolume (%s) error expand volume size: %v", volumeId, err)
	}

	err = p.volumeController.ResizeDeviceFileSystem(ctx, volumeId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "NodeExpandVolume (%s) error resize filesystem: %v", volumeId, err)
	}

	p.logger.Info("NodeExpandVolume volume was expanded", zap.String("volume_id", volumeId))
	return &csi.NodeExpandVolumeResponse{CapacityBytes: size}, nil
}

// NodeGetVolumeStats returns the volume capacity statistics
func (p *Plugin) NodeGetVolumeStats(ctx context.Context, request *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	volumeId := request.VolumeId
	p.logger.Debug("NodeGetVolumeStats called", zap.String("volume_id", volumeId))

	if volumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeGetVolumeStats invalid argument: VolumeId")
	}

	path := request.VolumePath
	if path == "" {
		return nil, status.Error(codes.InvalidArgument, "NodeGetVolumeStats invalid argument: VolumePath")
	}

	isMounted, err := p.mounter.IsMounted(ctx, path)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "NodeGetVolumeStats (%s) error check if volume is mounted: %v", volumeId, err)
	}

	if !isMounted {
		return nil, status.Errorf(codes.NotFound, "NodeGetVolumeStats path (%s) is not mounted", path)
	}

	stats, err := p.volumeController.GetVolumeStats(ctx, path)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "NodeGetVolumeStats (%s) error get volume stats: %v", volumeId, err)
	}

	p.logger.Info("NodeGetVolumeStats send volume statistics", zap.String("volume_id", volumeId))
	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: stats.AvailableBytes,
				Total:     stats.TotalBytes,
				Used:      stats.UsedBytes,
				Unit:      csi.VolumeUsage_BYTES,
			},
			{
				Available: stats.AvailableInodes,
				Total:     stats.TotalInodes,
				Used:      stats.UsedInodes,
				Unit:      csi.VolumeUsage_INODES,
			},
		},
	}, nil
}

// NodeGetCapabilities returns the supported capabilities of the node server
func (p *Plugin) NodeGetCapabilities(_ context.Context, _ *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	p.logger.Debug("NodeGetCapabilities called")

	return &csi.NodeGetCapabilitiesResponse{
		Capabilities: []*csi.NodeServiceCapability{
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_EXPAND_VOLUME,
					},
				},
			},
			{
				Type: &csi.NodeServiceCapability_Rpc{
					Rpc: &csi.NodeServiceCapability_RPC{
						Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
					},
				},
			},
		},
	}, nil
}

// NodeGetInfo returns the supported capabilities of the node server.
// This is used so the CO knows where to place the workload. The result of this function will be used by the CO in ControllerPublishVolume.
func (p *Plugin) NodeGetInfo(_ context.Context, _ *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	p.logger.Debug("NodeGetInfo called")

	return &csi.NodeGetInfoResponse{
		NodeId:            p.nodeId,
		MaxVolumesPerNode: maxVolumesPerNode,
		AccessibleTopology: &csi.Topology{
			Segments: map[string]string{
				p.nodeNameTopologyKey: p.nodeId,
			},
		},
	}, nil
}

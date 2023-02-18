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
	"github.com/golang/protobuf/ptypes/wrappers"
	"github.com/reinstall/csi-local-sparse/internal/volumes"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// CreateVolume creates a new volume from the given request
func (p *Plugin) CreateVolume(ctx context.Context, request *csi.CreateVolumeRequest) (*csi.CreateVolumeResponse, error) {
	volumeId := request.Name
	p.logger.Debug("CreateVolume called", zap.String("volume_id", request.Name))

	if volumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "CreateVolume invalid argument: name")
	}

	if request.VolumeCapabilities == nil || len(request.VolumeCapabilities) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "CreateVolume (%s) invalid argument: volumeCapabilities", volumeId)
	}

	for _, c := range request.VolumeCapabilities {
		// only ReadWriteOnce mode supported
		if c.AccessMode.Mode != csi.VolumeCapability_AccessMode_SINGLE_NODE_WRITER {
			return nil, status.Errorf(codes.InvalidArgument, "CreateVolume (%s) unsupported access mode: %s", volumeId, c.GetAccessMode().GetMode().String())
		}

		accessType := c.AccessType
		switch accessType.(type) {
		//case *csi.VolumeCapability_Block: // todo: implement block type
		case *csi.VolumeCapability_Mount:
		default:
			return nil, status.Errorf(codes.InvalidArgument, "CreateVolume (%s) unsupported access type", volumeId)
		}
	}

	// In strict mode Requisite = Preferred = Selected node topology
	// https://github.com/kubernetes-csi/external-provisioner/blob/master/README.md#topology-support
	topologyList := request.AccessibilityRequirements.Preferred
	if len(topologyList) <= 0 {
		p.logger.Error("No preferred topology set. Make sure that external-provisioner run with --strict-topology flag.")
		return nil, status.Errorf(codes.InvalidArgument, "CreateVolume (%s) invalid argument: no preferred topology set", volumeId)
	}

	segments := topologyList[0].Segments
	if _, ok := segments[p.nodeNameTopologyKey]; !ok {
		return nil, status.Errorf(codes.InvalidArgument, fmt.Sprintf("CreateVolume (%s) topology key (%s) not found", volumeId, p.nodeNameTopologyKey))
	}

	nodeName := segments[p.nodeNameTopologyKey]

	size, err := p.calculateVolumeSize(request.CapacityRange)
	if err != nil {
		return nil, status.Errorf(codes.OutOfRange, "CreateVolume (%s) invalid argument: capacityRange: %v", volumeId, err)
	}

	if err := p.volumeController.Create(ctx, volumeId, size); err != nil {
		if err == volumes.ErrorVolumeAlreadyExists {
			p.logger.Info("Volume already exists", zap.String("volume_id", volumeId))

			return &csi.CreateVolumeResponse{
				Volume: &csi.Volume{
					VolumeId:      volumeId,
					CapacityBytes: size,
				},
			}, nil
		}

		return nil, status.Errorf(codes.Internal, "CreateVolume (%s) error create volume: %v", volumeId, err)
	}

	p.logger.Info("Volume was created", zap.String("volume_id", volumeId))
	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			CapacityBytes: size,
			VolumeId:      volumeId,
			AccessibleTopology: []*csi.Topology{
				{
					Segments: map[string]string{
						p.nodeNameTopologyKey: nodeName,
					},
				},
			},
		},
	}, nil
}

// DeleteVolume deletes the given volume
func (p *Plugin) DeleteVolume(ctx context.Context, request *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	volumeId := request.VolumeId
	p.logger.Debug("DeleteVolume called", zap.String("volume_id", volumeId))

	if volumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "DeleteVolume invalid argument: volumeId")
	}

	if err := p.volumeController.Delete(ctx, volumeId); err != nil {
		if err == volumes.ErrorVolumeNotFound {
			p.logger.Info("Assuming volume is already deleted because it does not exist", zap.String("volume_id", volumeId))
			return &csi.DeleteVolumeResponse{}, nil
		}

		return nil, status.Errorf(codes.Internal, "DeleteVolume (%s) error delete volume: %v", volumeId, err)
	}

	p.logger.Info("Volume was deleted", zap.String("volume_id", volumeId))
	return &csi.DeleteVolumeResponse{}, nil
}

// GetCapacity returns the capacity of the storage pool
func (p *Plugin) GetCapacity(ctx context.Context, _ *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	p.logger.Debug("GetCapacity called")

	availableCapacity, err := p.volumeController.GetCapacity(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "GetCapacity error get capacity: %v", err)
	}

	p.logger.Info("Send available capacity", zap.Int64("available_capacity", availableCapacity))
	return &csi.GetCapacityResponse{
		AvailableCapacity: availableCapacity,
		MaximumVolumeSize: &wrappers.Int64Value{
			Value: maximumVolumeSize,
		},
		MinimumVolumeSize: &wrappers.Int64Value{
			Value: minimumVolumeSize,
		},
	}, nil
}

// ControllerExpandVolume expands given volume
func (p *Plugin) ControllerExpandVolume(_ context.Context, request *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	volumeId := request.VolumeId
	p.logger.Debug("ControllerExpandVolume called", zap.String("volume_id", volumeId))

	if volumeId == "" {
		return nil, status.Error(codes.InvalidArgument, "ControllerExpandVolume invalid argument: name")
	}

	size, err := p.calculateVolumeSize(request.CapacityRange)
	if err != nil {
		return nil, status.Errorf(codes.OutOfRange, "ControllerExpandVolume (%s) invalid argument: capacityRange: %v", volumeId, err)
	}

	// just return OK, so NodeController does all work
	return &csi.ControllerExpandVolumeResponse{
		CapacityBytes:         size,
		NodeExpansionRequired: true,
	}, nil
}

// ControllerGetCapabilities .
func (p *Plugin) ControllerGetCapabilities(_ context.Context, _ *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	p.logger.Debug("ControllerGetCapabilities called")

	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: []*csi.ControllerServiceCapability{
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_GET_CAPACITY,
					},
				},
			},
			{
				Type: &csi.ControllerServiceCapability_Rpc{
					Rpc: &csi.ControllerServiceCapability_RPC{
						Type: csi.ControllerServiceCapability_RPC_EXPAND_VOLUME,
					},
				},
			},
		},
	}, nil
}

// calculateVolumeSize returns storage size in bytes from the given capacity range.
func (p *Plugin) calculateVolumeSize(capRange *csi.CapacityRange) (int64, error) {
	if capRange == nil {
		return defaultVolumeSize, nil
	}

	required := capRange.RequiredBytes
	requiredSet := 0 < required
	limit := capRange.LimitBytes
	limitSet := 0 < limit

	if !requiredSet && !limitSet {
		return defaultVolumeSize, nil
	}

	if requiredSet && limitSet && limit < required {
		return 0, fmt.Errorf("limit (%d) can't be less than required (%d) size", limit, required)
	}

	if requiredSet && required < minimumVolumeSize {
		return 0, fmt.Errorf("required (%d) can't be less than minimum supported volume size (%d)", required, minimumVolumeSize)
	}

	if limitSet && limit < minimumVolumeSize {
		return 0, fmt.Errorf("limit (%d) can't be less than minimum supported volume size (%d)", limit, minimumVolumeSize)
	}

	if requiredSet && required > maximumVolumeSize {
		return 0, fmt.Errorf("required (%d) can't be greater than maximum supported volume size (%d)", required, maximumVolumeSize)
	}

	if limitSet && limit > maximumVolumeSize {
		return 0, fmt.Errorf("limit (%d) can't be greater than maximum supported volume size (%d)", limit, maximumVolumeSize)
	}

	if requiredSet && limitSet && required == limit {
		return limit, nil
	}

	if limitSet {
		return limit, nil
	}

	if requiredSet {
		return required, nil
	}

	return defaultVolumeSize, nil
}

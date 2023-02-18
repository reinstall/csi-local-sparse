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

package volumes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go.uber.org/zap"
	"os"
	"os/exec"
	"strings"
)

// Mounter is responsible for low level local mount operations
// Implementations MUST ensure idempotence of all functions
type Mounter interface {
	// Mount mounts source to target with given options
	Mount(ctx context.Context, source string, target string, options []string) error
	// Unmount unmounts target
	Unmount(ctx context.Context, target string) error
	// IsMounted returns true if target is already mounted
	IsMounted(ctx context.Context, target string) (bool, error)
}

// LinuxMounter implements Mounter functions on Linux systems
type LinuxMounter struct {
	// logger .
	logger *zap.Logger
}

// NewLinuxMounter returns new mounter
func NewLinuxMounter(logger *zap.Logger) *LinuxMounter {
	return &LinuxMounter{
		logger: logger.With(zap.String("logger", "real_mounter")),
	}
}

// Mount mounts source to target with given options. Returns nil if mount successfully or volume already mounted
func (r *LinuxMounter) Mount(ctx context.Context, source string, target string, options []string) error {
	r.logger.Debug("Mount called",
		zap.String("source", source),
		zap.String("target", target),
		zap.Strings("options", options),
	)

	if source == "" {
		return errors.New("mount source can't be empty")
	}

	if target == "" {
		return errors.New("mount target can't be empty")
	}

	isMounted, err := r.IsMounted(ctx, target)
	if err != nil {
		return fmt.Errorf("error check if target mounted: %w", err)
	}

	if isMounted {
		r.logger.Debug("Target already mounted",
			zap.String("source", source),
			zap.String("target", target),
		)
		return nil
	}

	if err := os.MkdirAll(target, 0750); err != nil {
		return fmt.Errorf("error create directory: %w", err)
	}

	mountCmd := fmt.Sprintf("mount")
	if _, err := exec.LookPath(mountCmd); err != nil {
		if err == exec.ErrNotFound {
			return fmt.Errorf("%q executable not found in $PATH", mountCmd)
		}
		return fmt.Errorf("error on check executable: %w", err)
	}

	args := make([]string, 0)
	if len(options) > 0 {
		args = append(args, "-o", strings.Join(options, ","))
	}

	args = append(
		args,
		source,
		target,
	)

	r.logger.Debug("Exec command", zap.String("cmd", mountCmd), zap.Strings("args", args))
	cmd := exec.CommandContext(ctx, mountCmd, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.logger.Error("Error exec command",
			zap.String("cmd", mountCmd),
			zap.Strings("args", args),
			zap.ByteString("output", out),
			zap.Error(err),
		)
		return fmt.Errorf("error exec command (%s): %w", mountCmd, err)
	}

	r.logger.Debug("Mounted source to target successfully",
		zap.String("source", source),
		zap.String("target", target),
		zap.Strings("options", options),
	)
	return nil
}

// Unmount unmounts target. Returns nil if unmount successfully or already unmounted
func (r *LinuxMounter) Unmount(ctx context.Context, target string) error {
	r.logger.Debug("Unmount called", zap.String("target", target))

	if target == "" {
		return errors.New("unmount target can't be empty")
	}

	isMounted, err := r.IsMounted(ctx, target)
	if err != nil {
		return fmt.Errorf("error check if target mounted: %w", err)
	}

	if !isMounted {
		r.logger.Debug("Target already unmounted",
			zap.String("target", target),
		)
		return nil
	}

	umountCmd := fmt.Sprintf("umount")
	if _, err := exec.LookPath(umountCmd); err != nil {
		if err == exec.ErrNotFound {
			return fmt.Errorf("%q executable not found in $PATH", umountCmd)
		}
		return fmt.Errorf("error on check executable: %w", err)
	}

	args := []string{
		target,
	}

	r.logger.Debug("Exec command", zap.String("cmd", umountCmd), zap.Strings("args", args))
	cmd := exec.CommandContext(ctx, umountCmd, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.logger.Error("Error exec command",
			zap.String("cmd", umountCmd),
			zap.Strings("args", args),
			zap.ByteString("output", out),
			zap.Error(err),
		)

		return fmt.Errorf("error exec command (%s): %w", umountCmd, err)
	}

	r.logger.Debug("Target was unmounted successfully",
		zap.String("target", target),
	)
	return nil
}

// IsMounted checks and returns true if target is mounted
func (r *LinuxMounter) IsMounted(ctx context.Context, target string) (bool, error) {
	r.logger.Debug("IsMounted called", zap.String("target", target))

	if target == "" {
		return false, errors.New("isMounted target can't be empty")
	}

	findMntCmd := "findmnt"
	if _, err := exec.LookPath(findMntCmd); err != nil {
		if err == exec.ErrNotFound {
			return false, fmt.Errorf("%q executable not found in $PATH", findMntCmd)
		}
		return false, fmt.Errorf("error on check executable: %w", err)
	}

	args := []string{
		"-o",
		"TARGET,PROPAGATION,FSTYPE,OPTIONS",
		"-J",
		"-M",
		target,
	}

	r.logger.Debug("Exec command", zap.String("cmd", findMntCmd), zap.Strings("args", args))
	out, err := exec.CommandContext(ctx, findMntCmd, args...).CombinedOutput()
	if err != nil {
		if strings.TrimSpace(string(out)) == "" {
			r.logger.Debug("Findmnt exists with non-zero exit code, assume it couldn't find anything",
				zap.String("target", target),
			)
			return false, nil
		}

		r.logger.Error("Error exec command",
			zap.String("cmd", findMntCmd),
			zap.Strings("args", args),
			zap.ByteString("output", out),
			zap.Error(err),
		)
		return false, fmt.Errorf("error exec command (%s): %w", findMntCmd, err)
	}

	if strings.TrimSpace(string(out)) == "" {
		r.logger.Debug("Findmnt no response means there is no mount", zap.String("target", target))
		return false, nil
	}

	type findMntResponse struct {
		FileSystems []struct {
			Target      string `json:"target"`
			Propagation string `json:"propagation"`
			FsType      string `json:"fstype"`
			Options     string `json:"options"`
		} `json:"filesystems"`
	}

	var resp *findMntResponse
	err = json.Unmarshal(out, &resp)
	if err != nil {
		return false, fmt.Errorf("error on unmarshal: %w", err)
	}

	isMounted := false
	for _, fs := range resp.FileSystems {
		if fs.Propagation != "shared" {
			return true, fmt.Errorf("bad mount propagation (%s) for target %s", fs.Propagation, target)
		}

		if fs.Target == target {
			isMounted = true
		}
	}

	r.logger.Debug("Result of mount search",
		zap.String("target", target),
		zap.Bool("is_mounted", isMounted),
	)
	return isMounted, nil
}

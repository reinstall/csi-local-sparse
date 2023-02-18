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
	"errors"
	"fmt"
	"go.uber.org/zap"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

var (
	ErrorVolumeNotFound      = errors.New("volume not found")
	ErrorVolumeAlreadyExists = errors.New("volume already exists")
)

// VolumeController is responsible for low level local volumes operations
// Implementations MUST ensure idempotence of all functions
type VolumeController interface {
	// Create creates new volume with the given size
	Create(ctx context.Context, volumeId string, sizeBytes int64) error
	// Delete deletes volume by id
	Delete(ctx context.Context, volumeId string) error
	// GetVolumeStats returns volume capacity statistics
	GetVolumeStats(_ context.Context, path string) (*VolumeStatistics, error)
	// GetCapacity returns available storage pool space
	GetCapacity(ctx context.Context) (bytes int64, err error)
	// GetVolumeSize returns size of volume by id
	GetVolumeSize(ctx context.Context, volumeId string) (bytes int64, err error)
	// ExpandVolumeSize satisfy requested size of volume. Do nothing if newSize <= currentSize
	ExpandVolumeSize(ctx context.Context, volumeId string, newSizeBytes int64) error
	// ResizeDeviceFileSystem resize filesystem of attached to given volume
	ResizeDeviceFileSystem(ctx context.Context, volumeId string) error
	// AttachDevice attaches volume to device and returns device name
	AttachDevice(ctx context.Context, volumeId string) (string, error)
	// DetachDevice detaches volume from loop device
	DetachDevice(ctx context.Context, volumeId string) error
	// GetDeviceByVolumeId returns device path attached to given volume
	GetDeviceByVolumeId(ctx context.Context, volumeId string) (string, error)
	// FormatIfNot formats volume by id when it isn't already has given filesystem
	// If volume has different filesystem type from given, it will have to format with given
	FormatIfNot(ctx context.Context, volumeId string, fsType string) error
}

// VolumeStatistics volume capacity statistics
type VolumeStatistics struct {
	// AvailableBytes .
	AvailableBytes int64
	// UsedBytes .
	UsedBytes int64
	// TotalBytes .
	TotalBytes int64
	// AvailableInodes .
	AvailableInodes int64
	// UserInodes .
	UsedInodes int64
	// TotalInodes .
	TotalInodes int64
}

// SparseFileVolumeController volume controller working with linux sparse files
type SparseFileVolumeController struct {
	// imagesDir sparse images directory path
	imagesDir string
	// directIO use direct-io on loop devices
	directIO bool
	// logger .
	logger *zap.Logger
}

// NewLinuxSparseFileVolumeController returns new controller
func NewLinuxSparseFileVolumeController(dataDir string, directIO bool, logger *zap.Logger) *SparseFileVolumeController {
	return &SparseFileVolumeController{
		imagesDir: dataDir,
		directIO:  directIO,
		logger:    logger.With(zap.String("logger", "SparseFileVolumeController")),
	}
}

// Create creates volume sparse file if it's not already exists. Returns null if file is exists or created successfully
func (s *SparseFileVolumeController) Create(ctx context.Context, volumeId string, sizeBytes int64) error {
	s.logger.Debug("Create called",
		zap.String("volume_id", volumeId),
		zap.Int64("size_bytes", sizeBytes),
	)

	if volumeId == "" {
		return fmt.Errorf("volumeId can't be empty")
	}

	if sizeBytes == 0 {
		return fmt.Errorf("size can't be equal 0")
	}

	filename := s.getImageFullPath(volumeId)
	if s.isFileExists(filename) {
		s.logger.Debug("File is already exists, so skip creating",
			zap.String("volume_id", volumeId),
			zap.String("filename", filename),
		)
		return nil
	}

	if err := s.truncate(ctx, filename, sizeBytes); err != nil {
		return fmt.Errorf("error truncate file: %w", err)
	}

	s.logger.Debug("Volume file was created successfully",
		zap.String("volume_id", volumeId),
		zap.String("filename", filename),
	)
	return nil
}

// Delete deletes volume sparse file. Returns nil if file is not exists or deleted successfully
func (s *SparseFileVolumeController) Delete(ctx context.Context, volumeId string) error {
	s.logger.Debug("Delete called", zap.String("volume_id", volumeId))

	if volumeId == "" {
		return fmt.Errorf("volumeId can't be empty")
	}

	filename := s.getImageFullPath(volumeId)
	if !s.isFileExists(filename) {
		s.logger.Debug("File is not exists, assume it was already deleted and skip removing",
			zap.String("volume_id", volumeId),
			zap.String("filename", filename),
		)
		return nil
	}

	removeCmd := "rm"
	if _, err := exec.LookPath(removeCmd); err != nil {
		if err == exec.ErrNotFound {
			return fmt.Errorf("%q executable not found in $PATH", removeCmd)
		}
		return fmt.Errorf("error on check executable: %w", err)
	}

	args := []string{
		"-f",
		filename,
	}

	s.logger.Debug("Exec command", zap.String("cmd", removeCmd), zap.Strings("args", args))
	cmd := exec.CommandContext(ctx, removeCmd, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("Error exec command",
			zap.String("cmd", removeCmd),
			zap.Strings("args", args),
			zap.ByteString("output", out),
			zap.Error(err),
		)
		return fmt.Errorf("error exec command (%s): %w", removeCmd, err)
	}

	s.logger.Debug("Volume file was deleted successfully",
		zap.String("volume_id", volumeId),
		zap.String("filename", filename),
	)
	return nil
}

// GetVolumeStats returns volume capacity statistics
func (s *SparseFileVolumeController) GetVolumeStats(_ context.Context, path string) (*VolumeStatistics, error) {
	s.logger.Debug("GetVolumeStats called")

	if path == "" {
		return nil, fmt.Errorf("path can't be empty")
	}

	fs := syscall.Statfs_t{}
	if err := syscall.Statfs(path, &fs); err != nil {
		return nil, fmt.Errorf("error get volume capacity stats: %w", err)
	}

	stats := &VolumeStatistics{
		AvailableBytes: int64(fs.Bavail) * int64(fs.Bsize),
		TotalBytes:     int64(fs.Blocks) * int64(fs.Bsize),
		UsedBytes:      (int64(fs.Blocks) - int64(fs.Bfree)) * int64(fs.Bsize),

		AvailableInodes: int64(fs.Ffree),
		TotalInodes:     int64(fs.Files),
		UsedInodes:      int64(fs.Files) - int64(fs.Ffree),
	}

	s.logger.Debug("Finish calculate volume stats",
		zap.String("path", path),
		zap.Int64("avail_bytes", stats.AvailableBytes),
		zap.Int64("used_bytes", stats.UsedBytes),
		zap.Int64("total_bytes", stats.TotalBytes),
		zap.Int64("avail_inodes", stats.AvailableInodes),
		zap.Int64("used_inodes", stats.UsedInodes),
		zap.Int64("total_inodes", stats.TotalInodes),
	)
	return stats, nil
}

// GetCapacity returns available storage pool space in bytes
func (s *SparseFileVolumeController) GetCapacity(_ context.Context) (int64, error) {
	s.logger.Debug("GetCapacity called")

	fs := syscall.Statfs_t{}
	if err := syscall.Statfs(s.imagesDir, &fs); err != nil {
		return 0, fmt.Errorf("error get storage capacity stats: %w", err)
	}

	avail := int64(fs.Bfree) * int64(fs.Bsize)
	s.logger.Debug("Finish calculate storage available capacity",
		zap.String("storage_path", s.imagesDir),
		zap.Int64("available_bytes", avail),
	)
	return avail, nil
}

// GetVolumeSize returns given volume size
func (s *SparseFileVolumeController) GetVolumeSize(ctx context.Context, volumeId string) (int64, error) {
	s.logger.Debug("GetVolumeSize called", zap.String("volume_id", volumeId))

	if volumeId == "" {
		return 0, fmt.Errorf("volumeId can't be empty")
	}

	filename := s.getImageFullPath(volumeId)
	if !s.isFileExists(filename) {
		return 0, ErrorVolumeNotFound
	}

	statCmd := "stat"
	if _, err := exec.LookPath(statCmd); err != nil {
		if err == exec.ErrNotFound {
			return 0, fmt.Errorf("%q executable not found in $PATH", statCmd)
		}
		return 0, fmt.Errorf("error on check executable: %w", err)
	}

	args := []string{
		"-c",
		"%s",
		filename,
	}

	s.logger.Debug("Exec command", zap.String("cmd", statCmd), zap.Strings("args", args))
	cmd := exec.CommandContext(ctx, statCmd, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("Error exec command",
			zap.String("cmd", statCmd),
			zap.Strings("args", args),
			zap.ByteString("output", out),
			zap.Error(err),
		)
		return 0, fmt.Errorf("error exec command (%s): %w", statCmd, err)
	}

	size, err := strconv.ParseInt(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("error parse output: %w", err)
	}

	s.logger.Debug("Finish calculate volume size",
		zap.String("volume_id", volumeId),
		zap.Int64("size_bytes", size),
	)
	return size, nil
}

// ExpandVolumeSize expands given volume. Returns nil if newSize <= currentSize or expand successfully
func (s *SparseFileVolumeController) ExpandVolumeSize(ctx context.Context, volumeId string, newSizeBytes int64) error {
	s.logger.Debug("ExpandVolumeSize called", zap.String("volume_id", volumeId), zap.Int64("new_size", newSizeBytes))

	if volumeId == "" {
		return fmt.Errorf("volumeId can't be empty")
	}

	if newSizeBytes <= 0 {
		return fmt.Errorf("size can't be less or equal 0")
	}

	filename := s.getImageFullPath(volumeId)
	if !s.isFileExists(filename) {
		return ErrorVolumeNotFound
	}

	currentSize, err := s.GetVolumeSize(ctx, volumeId)
	if err != nil {
		return fmt.Errorf("error get current volume size: %w", err)
	}

	available, err := s.GetCapacity(ctx)
	if err != nil {
		return fmt.Errorf("error get storage capacity: %w", err)
	}

	addSize := newSizeBytes - currentSize
	if addSize >= available {
		return fmt.Errorf("addiditional space (%d) is not available. %d bytes is available on storage", addSize, available)
	}

	// currently shrinking is not supported
	if addSize > 0 {
		if err := s.truncate(ctx, filename, newSizeBytes); err != nil {
			return fmt.Errorf("error truncate file: %w", err)
		}
	}

	s.logger.Debug("Volume size was expanded successfully",
		zap.String("volume_id", volumeId),
		zap.Int64("add_size_bytes", addSize),
	)
	return nil
}

// ResizeDeviceFileSystem resizes filesystem of device, attached to given volume
func (s *SparseFileVolumeController) ResizeDeviceFileSystem(ctx context.Context, volumeId string) error {
	s.logger.Debug("ResizeDeviceFileSystem called", zap.String("volume_id", volumeId))

	if volumeId == "" {
		return fmt.Errorf("volumeId can't be empty")
	}

	filename := s.getImageFullPath(volumeId)
	if !s.isFileExists(filename) {
		return ErrorVolumeNotFound
	}

	dev, err := s.GetDeviceByVolumeId(ctx, volumeId)
	if err != nil {
		return fmt.Errorf("error get loop device: %w", err)
	}

	if dev == "" {
		return ErrorVolumeNotFound
	}

	if err := s.expandLoopDevice(ctx, dev); err != nil {
		return fmt.Errorf("error expand loop device: %w", err)
	}

	if err := s.resizeFs(ctx, filename); err != nil {
		return fmt.Errorf("error resize filesystem: %w", err)
	}

	s.logger.Debug("Device filesystem was resized successfully", zap.String("volume_id", volumeId))
	return nil
}

// AttachDevice attaches volume sparse file to loop device and returns device name
func (s *SparseFileVolumeController) AttachDevice(ctx context.Context, volumeId string) (string, error) {
	s.logger.Debug("AttachDevice called", zap.String("volume_id", volumeId))

	if volumeId == "" {
		return "", fmt.Errorf("volumeId can't be empty")
	}

	filename := s.getImageFullPath(volumeId)
	if !s.isFileExists(filename) {
		return "", ErrorVolumeNotFound
	}

	dev, err := s.GetDeviceByVolumeId(ctx, volumeId)
	if err != nil {
		return "", fmt.Errorf("error get device by volumeId: %w", err)
	}

	// do nothing if already attached
	if dev != "" {
		s.logger.Debug("Device already attached, so skip it",
			zap.String("volume_id", volumeId),
			zap.String("device", dev),
		)
		return dev, nil
	}

	loSetupCmd := fmt.Sprintf("losetup")
	if _, err := exec.LookPath(loSetupCmd); err != nil {
		if err == exec.ErrNotFound {
			return "", fmt.Errorf("%q executable not found in $PATH", loSetupCmd)
		}
		return "", fmt.Errorf("error on check executable: %w", err)
	}

	args := []string{
		"--find",
		"--show",
	}

	if s.directIO {
		args = append(args, "--direct-io=on")
	}

	args = append(args, filename)

	s.logger.Debug("Exec command", zap.String("cmd", loSetupCmd), zap.Strings("args", args))
	cmd := exec.CommandContext(ctx, loSetupCmd, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("Error exec command",
			zap.String("cmd", loSetupCmd),
			zap.Strings("args", args),
			zap.ByteString("output", out),
			zap.Error(err),
		)

		return "", fmt.Errorf("error exec command (%s): %w", loSetupCmd, err)
	}

	dev = strings.TrimSpace(string(out))

	s.logger.Debug("Device was attached successfully",
		zap.String("volume_id", volumeId),
		zap.String("device", dev),
	)
	return dev, nil
}

// DetachDevice detaches volume sparse file from loop device
func (s *SparseFileVolumeController) DetachDevice(ctx context.Context, volumeId string) error {
	s.logger.Debug("DetachDevice called", zap.String("volume_id", volumeId))

	if volumeId == "" {
		return fmt.Errorf("volumeId can't be empty")
	}

	filename := s.getImageFullPath(volumeId)
	if !s.isFileExists(filename) {
		return ErrorVolumeNotFound
	}

	loSetupCmd := fmt.Sprintf("losetup")
	if _, err := exec.LookPath(loSetupCmd); err != nil {
		if err == exec.ErrNotFound {
			return fmt.Errorf("%q executable not found in $PATH", loSetupCmd)
		}
		return fmt.Errorf("error on check executable: %w", err)
	}

	args := []string{
		"--detach-all",
		filename,
	}

	s.logger.Debug("Exec command", zap.String("cmd", loSetupCmd), zap.Strings("args", args))
	cmd := exec.CommandContext(ctx, loSetupCmd, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("Error exec command",
			zap.String("cmd", loSetupCmd),
			zap.Strings("args", args),
			zap.ByteString("output", out),
			zap.Error(err),
		)

		return fmt.Errorf("error exec command (%s): %w", loSetupCmd, err)
	}

	s.logger.Debug("Device was detached successfully", zap.String("volume_id", volumeId))
	return nil
}

// GetDeviceByVolumeId returns device path if attached otherwise empty string
func (s *SparseFileVolumeController) GetDeviceByVolumeId(ctx context.Context, volumeId string) (string, error) {
	s.logger.Debug("GetDeviceByVolumeId called", zap.String("volume_id", volumeId))

	if volumeId == "" {
		return "", fmt.Errorf("volumeId can't be empty")
	}

	filename := s.getImageFullPath(volumeId)
	if !s.isFileExists(filename) {
		return "", ErrorVolumeNotFound
	}

	loSetupCmd := fmt.Sprintf("losetup")
	if _, err := exec.LookPath(loSetupCmd); err != nil {
		if err == exec.ErrNotFound {
			return "", fmt.Errorf("%q executable not found in $PATH", loSetupCmd)
		}
		return "", fmt.Errorf("error on check executable: %w", err)
	}

	args := []string{
		"--associated",
		filename,
	}

	s.logger.Debug("Exec command", zap.String("cmd", loSetupCmd), zap.Strings("args", args))
	cmd := exec.CommandContext(ctx, loSetupCmd, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("Error exec command",
			zap.String("cmd", loSetupCmd),
			zap.Strings("args", args),
			zap.ByteString("output", out),
			zap.Error(err),
		)
		return "", fmt.Errorf("error exec command (%s): %w", loSetupCmd, err)
	}

	outStr := strings.Split(strings.TrimSpace(string(out)), ":")
	if len(outStr) > 0 {
		dev := outStr[0]

		s.logger.Debug("Find device by volumeId successfully",
			zap.String("volume_id", volumeId),
			zap.String("device", dev),
		)
		return dev, nil
	}

	s.logger.Debug("Can't find device by volumeId, result is empty",
		zap.String("volume_id", volumeId),
	)
	return "", nil
}

// FormatIfNot formats sparse file with given file system type if it's not yet
// If volume has different filesystem type from given, it will be formatted with new given fsType
func (s *SparseFileVolumeController) FormatIfNot(ctx context.Context, volumeId string, fsType string) error {
	s.logger.Debug("FormatIfNot called",
		zap.String("volume_id", volumeId),
		zap.String("fs_type", fsType),
	)

	if volumeId == "" {
		return fmt.Errorf("volumeId can't be empty")
	}

	// todo: support other filesystems
	if fsType != "ext4" {
		return fmt.Errorf("given filesystem type (%s) not supported", fsType)
	}

	filename := s.getImageFullPath(volumeId)
	if !s.isFileExists(filename) {
		return ErrorVolumeNotFound
	}

	currentFs, err := s.getCurrentFilesystem(ctx, filename)
	if err != nil {
		return fmt.Errorf("error get current filesystem: %w", err)
	}

	if currentFs == fsType {
		s.logger.Debug("Sparse file already formatted with given filesystem. Skip formatting",
			zap.String("filename", filename),
			zap.String("fs_type", fsType),
			zap.String("current_fs_type", currentFs),
		)
		return nil
	}

	mkfsCmd := fmt.Sprintf("mkfs.%s", fsType)
	if _, err := exec.LookPath(mkfsCmd); err != nil {
		if err == exec.ErrNotFound {
			return fmt.Errorf("%q executable not found in $PATH", mkfsCmd)
		}
		return fmt.Errorf("error on check executable: %w", err)
	}

	args := []string{
		filename,
	}

	s.logger.Debug("Exec command", zap.String("cmd", mkfsCmd), zap.Strings("args", args))
	cmd := exec.CommandContext(ctx, mkfsCmd, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("Error exec command",
			zap.String("cmd", mkfsCmd),
			zap.Strings("args", args),
			zap.ByteString("output", out),
			zap.Error(err),
		)
		return fmt.Errorf("error exec command (%s): %w", mkfsCmd, err)
	}

	s.logger.Debug("Sparse file was formatted successfully",
		zap.String("volume_id", volumeId),
		zap.String("filename", filename),
		zap.String("fs_type", fsType),
	)
	return nil
}

// getCurrentFilesystem returns current filesystem or empty string
func (s *SparseFileVolumeController) getCurrentFilesystem(ctx context.Context, filename string) (string, error) {
	s.logger.Debug("getCurrentFilesystem called", zap.String("filename", filename))

	if filename == "" {
		return "", fmt.Errorf("filename can't be empty")
	}

	if !s.isFileExists(filename) {
		return "", ErrorVolumeNotFound
	}

	blkIdCmd := "blkid"
	if _, err := exec.LookPath(blkIdCmd); err != nil {
		if err == exec.ErrNotFound {
			return "", fmt.Errorf("%q executable not found in $PATH", blkIdCmd)
		}
		return "", fmt.Errorf("error on check executable: %w", err)
	}

	args := []string{
		"-o",
		"value",
		"-s",
		"TYPE",
		filename,
	}

	s.logger.Debug("Exec command", zap.String("cmd", blkIdCmd), zap.Strings("args", args))
	cmd := exec.CommandContext(ctx, blkIdCmd, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		// If the specified token was found, or if any tags were shown from (specified) devices, 0 is returned.
		// If the specified token was not found, or no (specified) devices could be identified, an exit code of 2 is returned.
		// For usage or other errors, an exit code of 4 is returned.
		if err.(*exec.ExitError).ExitCode() == 2 {
			s.logger.Debug("Blkid returns code 2, assumed file has not filesystem", zap.String("filename", filename))
			return "", nil
		}

		s.logger.Error("Error exec command",
			zap.String("cmd", blkIdCmd),
			zap.Strings("args", args),
			zap.ByteString("output", out),
			zap.Error(err),
		)
		return "", fmt.Errorf("error exec command (%s): %w", blkIdCmd, err)
	}

	fsType := strings.TrimSpace(string(out))

	s.logger.Debug("Blkid returns code 0, assumed file has filesystem",
		zap.String("filename", filename),
		zap.String("fs_type", fsType),
	)
	return fsType, nil
}

// expandLoopDevice forces the loop driver to reread the size of the file associated with the specified loop device
func (s *SparseFileVolumeController) expandLoopDevice(ctx context.Context, device string) error {
	s.logger.Debug("expandLoopDevice called", zap.String("device", device))

	loSetupCmd := fmt.Sprintf("losetup")
	if _, err := exec.LookPath(loSetupCmd); err != nil {
		if err == exec.ErrNotFound {
			return fmt.Errorf("%q executable not found in $PATH", loSetupCmd)
		}
		return fmt.Errorf("error on check executable: %w", err)
	}

	args := []string{
		"--set-capacity",
		device,
	}

	s.logger.Debug("Exec command", zap.String("cmd", loSetupCmd), zap.Strings("args", args))
	cmd := exec.CommandContext(ctx, loSetupCmd, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("Error exec command",
			zap.String("cmd", loSetupCmd),
			zap.Strings("args", args),
			zap.ByteString("output", out),
			zap.Error(err),
		)
		return fmt.Errorf("error exec command (%s): %w", loSetupCmd, err)
	}

	s.logger.Debug("Expanded loop device successfully", zap.String("device", device))
	return nil
}

// truncate truncates file with given size
func (s *SparseFileVolumeController) truncate(ctx context.Context, filename string, sizeBytes int64) error {
	s.logger.Debug("truncate called", zap.String("filename", filename), zap.Int64("size", sizeBytes))

	truncateCmd := "truncate"
	if _, err := exec.LookPath(truncateCmd); err != nil {
		if err == exec.ErrNotFound {
			return fmt.Errorf("%q executable not found in $PATH", truncateCmd)
		}
		return fmt.Errorf("error on check executable: %w", err)
	}

	args := []string{
		"-s",
		strconv.FormatInt(sizeBytes, 10),
		filename,
	}

	s.logger.Debug("Exec command", zap.String("cmd", truncateCmd), zap.Strings("args", args))
	cmd := exec.CommandContext(ctx, truncateCmd, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("Error exec command",
			zap.String("cmd", truncateCmd),
			zap.Strings("args", args),
			zap.ByteString("output", out),
			zap.Error(err),
		)
		return fmt.Errorf("error exec command (%s): %w", truncateCmd, err)
	}

	s.logger.Debug("Truncated file successfully",
		zap.String("filename", filename),
		zap.Int64("size_bytes", sizeBytes),
	)
	return nil
}

// resizeFs resizes filesystem
func (s *SparseFileVolumeController) resizeFs(ctx context.Context, filename string) error {
	s.logger.Debug("resizeFs called", zap.String("filename", filename))

	if !s.isFileExists(filename) {
		return ErrorVolumeNotFound
	}

	// todo: support other filesystems
	resize2fsCmd := "resize2fs"
	if _, err := exec.LookPath(resize2fsCmd); err != nil {
		if err == exec.ErrNotFound {
			return fmt.Errorf("%q executable not found in $PATH", resize2fsCmd)
		}
		return fmt.Errorf("error on check executable: %w", err)
	}

	args := []string{
		filename,
	}

	s.logger.Debug("Exec command", zap.String("cmd", resize2fsCmd), zap.Strings("args", args))
	cmd := exec.CommandContext(ctx, resize2fsCmd, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		s.logger.Error("Error exec command",
			zap.String("cmd", resize2fsCmd),
			zap.Strings("args", args),
			zap.ByteString("output", out),
			zap.Error(err),
		)
		return fmt.Errorf("error exec command (%s): %w", resize2fsCmd, err)
	}

	s.logger.Debug("Resized sparse file filesystem successfully", zap.String("filename", filename))
	return nil
}

// getImageFullPath returns volume's image storage absolute path
func (s *SparseFileVolumeController) getImageFullPath(volumeId string) string {
	return fmt.Sprintf("%s/%s.img", strings.TrimSuffix(s.imagesDir, "/"), volumeId)
}

// isFileExists returns true if file exists
func (s *SparseFileVolumeController) isFileExists(filename string) bool {
	info, err := os.Stat(filename)
	if os.IsNotExist(err) {
		return false
	}

	return !info.IsDir()
}

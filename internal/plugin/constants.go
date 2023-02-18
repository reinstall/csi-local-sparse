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

const (
	_  = iota
	Kb = 1 << (10 * iota)
	Mb
	Gb
)

const (
	// minimumVolumeSize is default size when no capacity range requested
	defaultVolumeSize int64 = 1 * Gb
	// minimumVolumeSize is minimal supported volume size
	minimumVolumeSize int64 = 1 * Gb
	// minimumVolumeSize is maximum supported volume size
	maximumVolumeSize int64 = 200 * Gb
)

const (
	// maxVolumesPerNode is maximum count of volumes that can be created per one node
	maxVolumesPerNode = 200
)

var (
	_ = Kb
	_ = Mb
)

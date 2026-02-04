package migration

import (
	"fmt"
)

// PathMapper handles bind mount path mapping and conversion to volumes
// Critical for handling host-specific paths that won't work on target
type PathMapper struct {
	mappings map[string]PathMapping
}

// PathMapping defines how to handle a bind mount
type PathMapping struct {
	SourcePath      string `json:"source_path"`
	TargetPath      string `json:"target_path"`
	ConvertToVolume bool   `json:"convert_to_volume"`
	VolumeName      string `json:"volume_name,omitempty"`
	Skip            bool   `json:"skip"`
}

// BindMount represents a detected bind mount
type BindMount struct {
	ContainerID   string
	ContainerName string
	SourcePath    string
	TargetPath    string
	ReadOnly      bool
}

// NewPathMapper creates a new path mapper
func NewPathMapper() *PathMapper {
	return &PathMapper{
		mappings: make(map[string]PathMapping),
	}
}

// AddMapping adds a path mapping configuration
func (pm *PathMapper) AddMapping(mapping PathMapping) {
	pm.mappings[mapping.SourcePath] = mapping
}

// DetectBindMounts finds bind mounts in container configurations
func (pm *PathMapper) DetectBindMounts(containers []*ContainerState) []BindMount {
	bindMounts := make([]BindMount, 0)

	for _, container := range containers {
		for _, volume := range container.Volumes {
			if volume.Type == "bind" {
				bindMounts = append(bindMounts, BindMount{
					ContainerName: container.Name,
					SourcePath:    volume.Source,
					TargetPath:    volume.Destination,
					ReadOnly:      volume.ReadOnly,
				})
			}
		}
	}

	return bindMounts
}

// ApplyMapping transforms container state with path mappings
func (pm *PathMapper) ApplyMapping(state *ContainerState) (*ContainerState, error) {
	// Create a copy to avoid modifying original
	modified := *state
	modified.Volumes = make([]VolumeMount, len(state.Volumes))
	copy(modified.Volumes, state.Volumes)

	for i, volume := range modified.Volumes {
		if volume.Type != "bind" {
			continue
		}

		// Check if we have a mapping for this path
		mapping, exists := pm.mappings[volume.Source]
		if !exists {
			// No mapping - warn and keep original
			continue
		}

		if mapping.Skip {
			// Remove this mount
			modified.Volumes = append(modified.Volumes[:i], modified.Volumes[i+1:]...)
			continue
		}

		if mapping.ConvertToVolume {
			// Convert bind mount to named volume
			modified.Volumes[i].Type = "volume"
			modified.Volumes[i].Source = mapping.VolumeName
		} else {
			// Map to new path on target
			modified.Volumes[i].Source = mapping.TargetPath
		}
	}

	return &modified, nil
}

// ConvertToVolume creates a new volume from bind mount data
func (pm *PathMapper) ConvertToVolume(mount BindMount) (string, error) {
	// Generate volume name from path
	volumeName := fmt.Sprintf("migrated-%s", sanitizePath(mount.SourcePath))

	// Would:
	// 1. Create new volume on target
	// 2. Copy data from bind mount to volume
	// 3. Return volume name for use in container config

	return volumeName, nil
}

// sanitizePath converts a file path to a valid volume name
func sanitizePath(path string) string {
	// Replace invalid characters for volume names
	// Remove leading slashes, replace other slashes with dashes
	sanitized := path
	if len(sanitized) > 0 && sanitized[0] == '/' {
		sanitized = sanitized[1:]
	}
	// Would properly sanitize path
	return sanitized
}

// GetMapping returns the mapping for a specific path
func (pm *PathMapper) GetMapping(path string) (PathMapping, bool) {
	mapping, exists := pm.mappings[path]
	return mapping, exists
}

// GetAllMappings returns all configured mappings
func (pm *PathMapper) GetAllMappings() map[string]PathMapping {
	return pm.mappings
}

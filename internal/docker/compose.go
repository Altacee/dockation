package docker

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/docker/api/types"
	"go.uber.org/zap"
)

// ComposeProject represents a parsed Docker Compose project
type ComposeProject struct {
	Name     string
	Services composetypes.Services
	Networks composetypes.Networks
	Volumes  composetypes.Volumes
	Secrets  composetypes.Secrets
	Configs  composetypes.Configs
}

// LoadComposeFile loads and parses a Docker Compose file
func (c *Client) LoadComposeFile(ctx context.Context, path string) (*ComposeProject, error) {
	c.logger.Info("loading compose file", zap.String("path", path))

	// Read compose file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read compose file: %w", err)
	}

	// Read .env file if it exists
	envFile := filepath.Join(filepath.Dir(path), ".env")
	envMap := make(map[string]string)
	if envData, err := os.ReadFile(envFile); err == nil {
		envMap = parseEnvFile(envData)
	}

	// Parse compose file
	configDetails := composetypes.ConfigDetails{
		WorkingDir: filepath.Dir(path),
		ConfigFiles: []composetypes.ConfigFile{
			{
				Filename: path,
				Content:  data,
			},
		},
		Environment: envMap,
	}

	project, err := loader.Load(configDetails)
	if err != nil {
		return nil, fmt.Errorf("failed to parse compose file: %w", err)
	}

	composeProject := &ComposeProject{
		Name:     project.Name,
		Services: project.Services,
		Networks: project.Networks,
		Volumes:  project.Volumes,
		Secrets:  project.Secrets,
		Configs:  project.Configs,
	}

	c.logger.Info("compose file loaded",
		zap.String("project", project.Name),
		zap.Int("services", len(project.Services)),
		zap.Int("networks", len(project.Networks)),
		zap.Int("volumes", len(project.Volumes)),
	)

	return composeProject, nil
}

// ValidateComposeProject validates a compose project against current Docker environment
func (c *Client) ValidateComposeProject(ctx context.Context, project *ComposeProject) error {
	c.logger.Info("validating compose project", zap.String("project", project.Name))

	// Validate that required images exist or can be pulled
	for _, service := range project.Services {
		if service.Image == "" {
			c.logger.Warn("service has no image specified",
				zap.String("service", service.Name),
			)
			continue
		}

		// Check if image exists locally
		_, err := c.InspectImage(ctx, service.Image)
		if err != nil {
			c.logger.Warn("service image not found locally",
				zap.String("service", service.Name),
				zap.String("image", service.Image),
			)
		}
	}

	// Validate networks
	for name, netConfig := range project.Networks {
		if netConfig.External {
			// Check if external network exists
			externalName := name
			if netConfig.Name != "" {
				externalName = netConfig.Name
			}
			_, err := c.InspectNetwork(ctx, externalName)
			if err != nil {
				return fmt.Errorf("external network %s not found: %w", name, err)
			}
		}
	}

	// Validate volumes
	for name, volConfig := range project.Volumes {
		if volConfig.External {
			// Check if external volume exists
			externalName := name
			if volConfig.Name != "" {
				externalName = volConfig.Name
			}
			_, err := c.InspectVolume(ctx, externalName)
			if err != nil {
				return fmt.Errorf("external volume %s not found: %w", name, err)
			}
		}
	}

	c.logger.Info("compose project validated successfully",
		zap.String("project", project.Name),
	)

	return nil
}

// ExportComposeResources exports all resources defined in a compose project
func (c *Client) ExportComposeResources(ctx context.Context, project *ComposeProject) (map[string]interface{}, error) {
	c.logger.Info("exporting compose resources", zap.String("project", project.Name))

	resources := make(map[string]interface{})

	// Export images for services
	images := make(map[string]*ImageInfo)
	for _, service := range project.Services {
		if service.Image == "" {
			continue
		}

		info, err := c.GetImageInfo(ctx, service.Image)
		if err != nil {
			c.logger.Warn("failed to get image info for service",
				zap.String("service", service.Name),
				zap.String("image", service.Image),
				zap.Error(err),
			)
			continue
		}

		images[service.Image] = info
	}
	resources["images"] = images

	// Export volumes (non-external)
	volumes := make(map[string]*VolumeInfo)
	for name, volConfig := range project.Volumes {
		if volConfig.External {
			continue
		}

		info, err := c.GetVolumeInfo(ctx, name)
		if err != nil {
			c.logger.Warn("failed to get volume info",
				zap.String("volume", name),
				zap.Error(err),
			)
			continue
		}

		volumes[name] = info
	}
	resources["volumes"] = volumes

	// Export networks (non-external)
	networks := make(map[string]*NetworkInfo)
	for name, netConfig := range project.Networks {
		if netConfig.External {
			continue
		}

		// Find network by name
		netList, err := c.ListNetworks(ctx)
		if err != nil {
			c.logger.Warn("failed to list networks", zap.Error(err))
			continue
		}

		for _, net := range netList {
			if net.Name == name {
				info, err := c.ExportNetwork(ctx, net.ID)
				if err != nil {
					c.logger.Warn("failed to export network",
						zap.String("network", name),
						zap.Error(err),
					)
					continue
				}
				networks[name] = info
				break
			}
		}
	}
	resources["networks"] = networks

	c.logger.Info("compose resources exported",
		zap.String("project", project.Name),
		zap.Int("images", len(images)),
		zap.Int("volumes", len(volumes)),
		zap.Int("networks", len(networks)),
	)

	return resources, nil
}

// DetectComposeStacks finds all running compose projects on the system
func (c *Client) DetectComposeStacks(ctx context.Context) ([]*ComposeStack, error) {
	c.logger.Info("detecting compose stacks")

	// Get all containers and group by compose.project label
	containers, err := c.ListContainers(ctx, true)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	projectContainers := make(map[string][]types.Container)
	for _, container := range containers {
		if project, ok := container.Labels["com.docker.compose.project"]; ok {
			projectContainers[project] = append(projectContainers[project], container)
		}
	}

	stacks := make([]*ComposeStack, 0, len(projectContainers))
	for projectName, containers := range projectContainers {
		stack := &ComposeStack{
			Name: projectName,
		}

		// Try to find compose file from first container
		if len(containers) > 0 {
			if dir, ok := containers[0].Labels["com.docker.compose.project.working_dir"]; ok {
				stack.Directory = dir
				stack.ConfigPath = filepath.Join(dir, "docker-compose.yml")
			}
		}

		// Build service list from containers
		for _, container := range containers {
			service := ComposeService{
				Name:        container.Labels["com.docker.compose.service"],
				Image:       container.Image,
				ContainerID: container.ID,
				Status:      container.State,
			}
			stack.Services = append(stack.Services, service)
		}

		stacks = append(stacks, stack)
	}

	c.logger.Info("compose stacks detected", zap.Int("count", len(stacks)))
	return stacks, nil
}

// ComposeStack represents a detected compose stack
type ComposeStack struct {
	Name       string
	Directory  string
	ConfigPath string
	Services   []ComposeService
	Volumes    []string
	Networks   []string
}

// ComposeService represents a service in a compose stack
type ComposeService struct {
	Name        string
	Image       string
	ContainerID string
	Status      string
	Replicas    int
}

// ExportComposeBundle creates a tarball of compose project with all files
func (c *Client) ExportComposeBundle(stack *ComposeStack) (io.Reader, error) {
	c.logger.Info("exporting compose bundle", zap.String("stack", stack.Name))

	pr, pw := io.Pipe()
	tw := tar.NewWriter(pw)

	go func() {
		defer pw.Close()
		defer tw.Close()

		// Add main compose file
		if err := addFileToTar(tw, stack.ConfigPath, "docker-compose.yml"); err != nil {
			c.logger.Error("failed to add compose file to tar", zap.Error(err))
			return
		}

		// Add override file if exists
		overridePath := filepath.Join(stack.Directory, "docker-compose.override.yml")
		if _, err := os.Stat(overridePath); err == nil {
			addFileToTar(tw, overridePath, "docker-compose.override.yml")
		}

		// Add .env file if exists
		envPath := filepath.Join(stack.Directory, ".env")
		if _, err := os.Stat(envPath); err == nil {
			addFileToTar(tw, envPath, ".env")
		}

		c.logger.Info("compose bundle exported", zap.String("stack", stack.Name))
	}()

	return pr, nil
}

// parseEnvFile parses .env file content into a map
func parseEnvFile(data []byte) map[string]string {
	env := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			env[parts[0]] = parts[1]
		}
	}
	return env
}

// addFileToTar adds a file to a tar archive
func addFileToTar(tw *tar.Writer, filePath, nameInTar string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	header := &tar.Header{
		Name:    nameInTar,
		Size:    stat.Size(),
		Mode:    int64(stat.Mode()),
		ModTime: stat.ModTime(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header: %w", err)
	}

	if _, err := io.Copy(tw, file); err != nil {
		return fmt.Errorf("failed to write file to tar: %w", err)
	}

	return nil
}

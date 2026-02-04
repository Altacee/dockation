package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/artemis/docker-migrate/internal/config"
	"github.com/artemis/docker-migrate/internal/docker"
	"github.com/artemis/docker-migrate/internal/master"
	"github.com/artemis/docker-migrate/internal/migration"
	"github.com/artemis/docker-migrate/internal/observability"
	"github.com/artemis/docker-migrate/internal/peer"
	"github.com/artemis/docker-migrate/internal/server"
	"github.com/artemis/docker-migrate/internal/worker"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	cfgFile string
	logger  *observability.Logger
	cfg     *config.Config
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "docker-migrate",
	Short: "Peer-to-peer Docker resource migration tool",
	Long: `docker-migrate enables secure, peer-to-peer migration of Docker resources
including containers, images, volumes, and networks between hosts.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Initialize logger
		var err error
		logger, err = observability.NewLogger("info")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
			os.Exit(1)
		}

		// Load config
		cfg, err = config.LoadConfig(cfgFile)
		if err != nil {
			logger.Error("failed to load config", zap.Error(err))
			os.Exit(1)
		}

		// Update logger level if specified in config
		if cfg.LogLevel != "" {
			logger, err = observability.NewLogger(cfg.LogLevel)
			if err != nil {
				logger.Warn("failed to set log level, using default", zap.Error(err))
			}
		}
	},
}

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Start web UI server",
	Long:  "Start the web UI server for interactive Docker migration",
	Run: func(cmd *cobra.Command, args []string) {
		if err := runUIServer(cmd, args); err != nil {
			logger.Error("failed to start UI server", zap.Error(err))
			os.Exit(1)
		}
	},
}

func runUIServer(cmd *cobra.Command, args []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize Docker client
	dockerClient, err := docker.NewClient(logger, cfg.DockerHost)
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer dockerClient.Close()

	// Initialize health checker
	healthChecker := observability.NewHealthChecker()
	healthChecker.RegisterCheck("docker", observability.DockerHealthCheck(dockerClient.Ping))
	go healthChecker.StartPeriodicChecks(ctx, 10*time.Second)

	// Initialize metrics
	metrics := observability.NewMetrics()

	// Initialize crypto manager
	cryptoManager, err := peer.NewCryptoManager(logger, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("failed to create crypto manager: %w", err)
	}

	// Initialize pairing manager
	pairingManager := peer.NewPairingManager(cfg, cryptoManager, logger)

	// Initialize transfer manager
	transferManager, err := peer.NewTransferManager(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create transfer manager: %w", err)
	}

	// Initialize peer discovery
	peerDiscovery := peer.NewPeerDiscovery(cfg, pairingManager, cryptoManager, logger)

	// Initialize migration engine (expects *zap.Logger)
	migrationEngine := migration.NewEngine(
		dockerClient,
		peerDiscovery,
		transferManager,
		cfg,
		logger.Logger, // Access embedded *zap.Logger
		metrics,
	)

	// Initialize gRPC server (expects *observability.Logger)
	grpcServer, err := peer.NewGRPCServer(
		dockerClient,
		transferManager,
		pairingManager,
		cryptoManager,
		cfg,
		logger, // Pass Logger directly
	)
	if err != nil {
		return fmt.Errorf("failed to create gRPC server: %w", err)
	}

	// If running in master mode, create master and register its gRPC service
	var masterNode *master.Master
	if cfg.IsMaster() {
		var err error
		masterNode, err = master.New(cfg, dockerClient, cryptoManager, transferManager, logger)
		if err != nil {
			return fmt.Errorf("failed to create master node: %w", err)
		}

		// Register MasterService on the existing gRPC server (before it starts)
		masterNode.RegisterGRPCService(grpcServer.GetServer())

		// Start registry cleanup
		go masterNode.StartBackgroundTasks(ctx)

		logger.Info("master mode enabled",
			zap.String("enrollment_token", cfg.Master.EnrollmentToken),
		)
	}

	// Start background services
	go peerDiscovery.Start(ctx)
	go func() {
		if err := grpcServer.Start(cfg.GRPCAddr); err != nil {
			logger.Error("gRPC server error", zap.Error(err))
		}
	}()

	// Create HTTP server with all dependencies
	httpServer := server.NewServerWithDeps(
		cfg,
		dockerClient,
		migrationEngine,
		pairingManager,
		peerDiscovery,
		healthChecker,
		metrics,
		logger,
	)

	// Register master routes with HTTP server if in master mode
	if masterNode != nil {
		httpServer.SetMaster(masterNode)
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("received shutdown signal")
		cancel()
		grpcServer.Stop()
		httpServer.Stop()
		if masterNode != nil {
			masterNode.Stop()
		}
	}()

	logger.Info("starting docker-migrate UI",
		zap.String("http_addr", cfg.HTTPAddr),
		zap.String("grpc_addr", cfg.GRPCAddr),
	)

	if err := httpServer.Start(); err != nil {
		return fmt.Errorf("HTTP server error: %w", err)
	}

	return nil
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start daemon mode",
	Long:  "Start docker-migrate in daemon mode for automated migrations",
	Run: func(cmd *cobra.Command, args []string) {
		// Same as ui command for now
		uiCmd.Run(cmd, args)
	},
}

var listCmd = &cobra.Command{
	Use:   "list [type]",
	Short: "List Docker resources",
	Long:  "List Docker resources: containers, images, volumes, or networks",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		resourceType := args[0]

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		dockerClient, err := docker.NewClient(logger, cfg.DockerHost)
		if err != nil {
			logger.Error("failed to create docker client", zap.Error(err))
			os.Exit(1)
		}
		defer dockerClient.Close()

		switch resourceType {
		case "containers", "c":
			containers, err := dockerClient.ListContainers(ctx, true)
			if err != nil {
				logger.Error("failed to list containers", zap.Error(err))
				os.Exit(1)
			}
			fmt.Printf("Found %d containers:\n", len(containers))
			for _, c := range containers {
				fmt.Printf("  - %s (%s) [%s]\n", c.Names[0], c.ID[:12], c.State)
			}

		case "images", "i":
			images, err := dockerClient.ListImages(ctx)
			if err != nil {
				logger.Error("failed to list images", zap.Error(err))
				os.Exit(1)
			}
			fmt.Printf("Found %d images:\n", len(images))
			for _, img := range images {
				tags := "<none>"
				if len(img.RepoTags) > 0 {
					tags = img.RepoTags[0]
				}
				fmt.Printf("  - %s (%s) [%.2f MB]\n", tags, img.ID[:12], float64(img.Size)/(1024*1024))
			}

		case "volumes", "v":
			volumes, err := dockerClient.ListVolumes(ctx)
			if err != nil {
				logger.Error("failed to list volumes", zap.Error(err))
				os.Exit(1)
			}
			fmt.Printf("Found %d volumes:\n", len(volumes))
			for _, vol := range volumes {
				fmt.Printf("  - %s [%s]\n", vol.Name, vol.Driver)
			}

		case "networks", "n":
			networks, err := dockerClient.ListNetworks(ctx)
			if err != nil {
				logger.Error("failed to list networks", zap.Error(err))
				os.Exit(1)
			}
			fmt.Printf("Found %d networks:\n", len(networks))
			for _, net := range networks {
				fmt.Printf("  - %s (%s) [%s]\n", net.Name, net.ID[:12], net.Driver)
			}

		default:
			fmt.Fprintf(os.Stderr, "Unknown resource type: %s\n", resourceType)
			fmt.Fprintf(os.Stderr, "Valid types: containers, images, volumes, networks\n")
			os.Exit(1)
		}
	},
}

var pairCmd = &cobra.Command{
	Use:   "pair",
	Short: "Manage peer pairing",
	Long:  "Generate pairing codes or connect to peers",
}

var pairGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate pairing code",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Pairing code generation not yet implemented")
		fmt.Println("This will generate a SPAKE2+ pairing code for peer connection")
	},
}

var pairConnectCmd = &cobra.Command{
	Use:   "connect [code]",
	Short: "Connect using pairing code",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		code := args[0]
		fmt.Printf("Peer connection not yet implemented\n")
		fmt.Printf("Would connect with code: %s\n", code)
	},
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Start migration",
	Long:  "Migrate Docker resources to a peer",
}

var masterCmd = &cobra.Command{
	Use:   "master",
	Short: "Run as master node with web UI",
	Long:  "Start docker-migrate as a master node that manages worker connections and orchestrates migrations",
	Run: func(cmd *cobra.Command, args []string) {
		// Set role to master
		cfg.Role = config.RoleMaster

		// Get or generate enrollment token
		enrollmentToken, _ := cmd.Flags().GetString("enrollment-token")
		if enrollmentToken == "" {
			enrollmentToken = generateEnrollmentToken()
		}

		if cfg.Master == nil {
			cfg.Master = config.DefaultMasterConfig()
		}
		cfg.Master.EnrollmentToken = enrollmentToken

		logger.Info("enrollment token for workers", zap.String("token", enrollmentToken))

		// Run the UI server (which will detect master mode)
		if err := runUIServer(cmd, args); err != nil {
			logger.Error("failed to start master", zap.Error(err))
			os.Exit(1)
		}
	},
}

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Run as worker node",
	Long:  "Start docker-migrate as a worker node that connects to a master for coordinated migrations",
	Run: func(cmd *cobra.Command, args []string) {
		masterURL, _ := cmd.Flags().GetString("master-url")
		token, _ := cmd.Flags().GetString("token")
		workerName, _ := cmd.Flags().GetString("name")

		if masterURL == "" {
			fmt.Fprintln(os.Stderr, "Error: --master-url is required")
			os.Exit(1)
		}
		if token == "" {
			fmt.Fprintln(os.Stderr, "Error: --token is required")
			os.Exit(1)
		}

		cfg.Role = config.RoleWorker
		if cfg.Worker == nil {
			cfg.Worker = config.DefaultWorkerConfig()
		}
		cfg.Worker.MasterURL = masterURL
		cfg.Worker.Name = workerName

		// Worker needs to connect to master and run its own gRPC server
		if err := runWorker(cmd, args, token); err != nil {
			logger.Error("failed to start worker", zap.Error(err))
			os.Exit(1)
		}
	},
}

var (
	migrateTo         string
	migrateContainers []string
	migrateVolumes    []string
	migrateImages     []string
	migrateNetworks   []string
	migrateMode       string
	migrateStrategy   string
	migrateDryRun     bool
)

func generateEnrollmentToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", b)
}

func runWorker(cmd *cobra.Command, args []string, enrollmentToken string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get worker name
	workerName := cfg.Worker.Name
	if workerName == "" {
		hostname, err := os.Hostname()
		if err != nil {
			workerName = "worker-" + generateEnrollmentToken()[:8]
		} else {
			workerName = hostname
		}
		cfg.Worker.Name = workerName
	}

	// Parse labels from flags
	labelStrs, _ := cmd.Flags().GetStringSlice("labels")
	for _, l := range labelStrs {
		parts := strings.SplitN(l, "=", 2)
		if len(parts) == 2 {
			cfg.Worker.Labels[parts[0]] = parts[1]
		}
	}

	// Initialize Docker client
	dockerClient, err := docker.NewClient(logger, cfg.DockerHost)
	if err != nil {
		return fmt.Errorf("failed to create docker client: %w", err)
	}
	defer dockerClient.Close()

	// Initialize crypto manager
	cryptoManager, err := peer.NewCryptoManager(logger, cfg.DataDir)
	if err != nil {
		return fmt.Errorf("failed to create crypto manager: %w", err)
	}

	// Initialize transfer manager
	transferManager, err := peer.NewTransferManager(cfg, logger)
	if err != nil {
		return fmt.Errorf("failed to create transfer manager: %w", err)
	}

	// Create worker instance
	w, err := worker.New(cfg, dockerClient, cryptoManager, transferManager, logger)
	if err != nil {
		return fmt.Errorf("failed to create worker: %w", err)
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		logger.Info("received shutdown signal")
		cancel()
		w.Stop()
	}()

	logger.Info("starting worker",
		zap.String("name", workerName),
		zap.String("master_url", cfg.Worker.MasterURL),
	)

	// Start worker (blocks until context is cancelled)
	return w.Start(ctx, enrollmentToken)
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ~/.docker-migrate/config.json)")

	// Add subcommands
	rootCmd.AddCommand(uiCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(listCmd)
	rootCmd.AddCommand(pairCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(masterCmd)
	rootCmd.AddCommand(workerCmd)

	// Pair subcommands
	pairCmd.AddCommand(pairGenerateCmd)
	pairCmd.AddCommand(pairConnectCmd)

	// Migrate flags
	migrateCmd.Flags().StringVar(&migrateTo, "to", "", "Target peer ID (required)")
	migrateCmd.Flags().StringSliceVar(&migrateContainers, "containers", nil, "Container IDs to migrate")
	migrateCmd.Flags().StringSliceVar(&migrateVolumes, "volumes", nil, "Volume names to migrate")
	migrateCmd.Flags().StringSliceVar(&migrateImages, "images", nil, "Image IDs to migrate")
	migrateCmd.Flags().StringSliceVar(&migrateNetworks, "networks", nil, "Network IDs to migrate")
	migrateCmd.Flags().StringVar(&migrateMode, "mode", "cold", "Migration mode: cold, warm, or live")
	migrateCmd.Flags().StringVar(&migrateStrategy, "strategy", "full", "Migration strategy: full, incremental, or snapshot")
	migrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false, "Perform dry run without actual migration")
	migrateCmd.MarkFlagRequired("to")

	migrateCmd.Run = func(cmd *cobra.Command, args []string) {
		fmt.Println("Migration not yet implemented")
		fmt.Printf("Would migrate to peer: %s\n", migrateTo)
		fmt.Printf("  Containers: %v\n", migrateContainers)
		fmt.Printf("  Volumes: %v\n", migrateVolumes)
		fmt.Printf("  Images: %v\n", migrateImages)
		fmt.Printf("  Networks: %v\n", migrateNetworks)
		fmt.Printf("  Mode: %s\n", migrateMode)
		fmt.Printf("  Strategy: %s\n", migrateStrategy)
		fmt.Printf("  Dry run: %v\n", migrateDryRun)
	}

	// Master flags
	masterCmd.Flags().String("enrollment-token", "", "Token for worker enrollment (auto-generated if empty)")

	// Worker flags
	workerCmd.Flags().String("master-url", "", "Master gRPC URL (required)")
	workerCmd.Flags().String("token", "", "Enrollment token from master (required)")
	workerCmd.Flags().String("name", "", "Worker name (defaults to hostname)")
	workerCmd.Flags().StringSlice("labels", nil, "Worker labels as key=value pairs")
}

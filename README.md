# Dockation

A secure Docker resource migration tool with centralized master-worker architecture.

Dockation enables migration of Docker containers, images, volumes, and networks between hosts through a centralized control plane. A master node orchestrates migrations between connected worker nodes, providing a web UI for management and monitoring.

## Features

- **Master-Worker Architecture**: Central master node manages multiple workers and orchestrates migrations
- **Web UI**: Built-in React-based dashboard for monitoring and control
- **Secure Communication**: TLS-encrypted gRPC communication between all nodes
- **Auto-Reconnect**: Workers automatically reconnect to master with exponential backoff
- **Resource Inventory**: Real-time Docker resource tracking across all workers
- **Cross-Platform**: Supports Linux and macOS on AMD64 and ARM64 architectures

## Architecture

```
                    +------------------+
                    |     Master       |
                    |  - Web UI (:8080)|
                    |  - gRPC  (:9090) |
                    |  - Orchestrator  |
                    +--------+---------+
                             |
            +----------------+----------------+
            |                |                |
    +-------v------+  +------v-------+  +-----v--------+
    |   Worker A   |  |   Worker B   |  |   Worker C   |
    | - Docker API |  | - Docker API |  | - Docker API |
    | - Inventory  |  | - Inventory  |  | - Inventory  |
    +--------------+  +--------------+  +--------------+
```

## Quick Start

### Prerequisites

- Go 1.21 or later
- Docker daemon running
- Node.js 18+ (for building web UI)

### Build

```bash
# Build binary without UI
make build

# Build binary with embedded web UI
make build-full

# Cross-compile for all platforms
make build-all
```

### Running as Master

```bash
# Start master node (enrollment token auto-generated)
./bin/docker-migrate master

# Start with specific enrollment token
./bin/docker-migrate master --enrollment-token YOUR_TOKEN
```

The master will display the enrollment token needed for workers to connect. Access the web UI at `http://localhost:8080`.

### Running as Worker

```bash
# Connect worker to master
./bin/docker-migrate worker \
  --master-url localhost:9090 \
  --token YOUR_ENROLLMENT_TOKEN \
  --name worker-1
```

### One-Line Worker Installation (Linux)

```bash
curl -sSL https://raw.githubusercontent.com/Altacee/dockation/main/scripts/install-worker.sh | bash -s -- \
  --master-url https://master:9090 \
  --token YOUR_TOKEN
```

## Configuration

Configuration file location: `~/.docker-migrate/config.json`

### Master Configuration

```json
{
  "role": "master",
  "http_addr": ":8080",
  "grpc_addr": ":9090",
  "log_level": "info",
  "master": {
    "enrollment_token": "your-secure-token",
    "worker_timeout": "30s",
    "heartbeat_interval": "10s",
    "inventory_interval": "60s"
  }
}
```

### Worker Configuration

```json
{
  "role": "worker",
  "grpc_addr": ":9090",
  "log_level": "info",
  "worker": {
    "master_url": "master-host:9090",
    "name": "worker-1",
    "labels": {
      "region": "us-west",
      "environment": "production"
    },
    "reconnect_interval": "5s",
    "max_reconnect_interval": "5m"
  }
}
```

## API Reference

### Health Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /health` | Health check |
| `GET /ready` | Readiness check |
| `GET /metrics` | Prometheus metrics |

### Resource Management

| Endpoint | Description |
|----------|-------------|
| `GET /api/containers` | List containers |
| `GET /api/containers/:id` | Get container details |
| `GET /api/images` | List images |
| `GET /api/volumes` | List volumes |
| `GET /api/networks` | List networks |

### Worker Management (Master Only)

| Endpoint | Description |
|----------|-------------|
| `GET /api/workers` | List all workers |
| `GET /api/workers/:id` | Get worker details |
| `GET /api/workers/:id/resources` | Get worker's Docker resources |
| `DELETE /api/workers/:id` | Remove worker |
| `GET /api/enrollment-token` | Get enrollment token |
| `POST /api/enrollment-token/regenerate` | Regenerate token |

### Migration Management (Master Only)

| Endpoint | Description |
|----------|-------------|
| `POST /api/migrations` | Start migration |
| `GET /api/migrations` | List migrations |
| `GET /api/migrations/:id` | Get migration status |
| `POST /api/migrations/:id/cancel` | Cancel migration |

### Starting a Migration

```bash
curl -X POST http://localhost:8080/api/migrations \
  -H "Content-Type: application/json" \
  -d '{
    "source_worker_id": "worker-123",
    "target_worker_id": "worker-456",
    "volume_names": ["my-data"],
    "image_ids": ["nginx:latest"],
    "mode": "cold",
    "strategy": "full"
  }'
```

## CLI Commands

```bash
# Start master node
docker-migrate master [--enrollment-token TOKEN]

# Start worker node
docker-migrate worker --master-url URL --token TOKEN [--name NAME] [--labels key=value]

# Start standalone UI (P2P mode)
docker-migrate ui

# List local Docker resources
docker-migrate list containers
docker-migrate list images
docker-migrate list volumes
docker-migrate list networks
```

## Development

### Project Structure

```
dockation/
├── cmd/docker-migrate/     # CLI entry point
├── internal/
│   ├── config/             # Configuration management
│   ├── docker/             # Docker SDK operations
│   ├── master/             # Master node implementation
│   ├── migration/          # Migration engine
│   ├── observability/      # Logging, metrics, health
│   ├── peer/               # P2P communication, crypto
│   ├── server/             # HTTP server and routes
│   └── worker/             # Worker node implementation
├── proto/                  # gRPC protocol definitions
├── scripts/                # Installation scripts
└── web/                    # React frontend
```

### Make Targets

```bash
make build          # Build binary
make build-full     # Build with embedded UI
make build-all      # Cross-compile all platforms
make test           # Run tests
make lint           # Run linters
make proto          # Generate gRPC code
make release        # Create release artifacts
make clean          # Clean build artifacts
```

### Running Tests

```bash
make test

# With coverage
make test-coverage
```

## Security

- All gRPC communication is TLS encrypted
- Workers authenticate using enrollment tokens
- Subsequent requests use per-worker auth tokens
- Secrets are automatically redacted from logs
- Environment variables matching `*PASSWORD*`, `*SECRET*`, `*KEY*`, `*TOKEN*` are redacted

## Metrics

Prometheus metrics are exposed at `/metrics`:

- `docker_migrate_transfer_bytes_total` - Total bytes transferred
- `docker_migrate_transfer_duration_seconds` - Transfer duration histogram
- `docker_migrate_active_migrations` - Currently active migrations
- `docker_migrate_migrations_total` - Total migrations by status
- `docker_migrate_connected_peers` - Connected worker count
- `docker_migrate_docker_operations_total` - Docker API operations

## License

MIT License - see [LICENSE](LICENSE) for details.

## Contributing

Contributions are welcome. Please open an issue to discuss proposed changes before submitting a pull request.

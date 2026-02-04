// Core Docker resource types
export interface PortMapping {
  containerPort: number;
  hostPort: number;
  protocol: 'tcp' | 'udp';
}

export interface Mount {
  type: 'bind' | 'volume' | 'tmpfs';
  source: string;
  destination: string;
  readOnly: boolean;
}

export interface Container {
  id: string;
  name: string;
  image: string;
  status: 'running' | 'stopped' | 'paused' | 'restarting' | 'dead';
  created: string;
  ports: PortMapping[];
  networks: string[];
  mounts: Mount[];
  env: Record<string, string>;
  labels: Record<string, string>;
  command?: string;
  cpuLimit?: number;
  memoryLimit?: number;
}

export interface Image {
  id: string;
  repoTags: string[];
  size: number;
  created: string;
  architecture: string;
  os: string;
  labels: Record<string, string>;
}

export interface Volume {
  name: string;
  driver: string;
  mountpoint: string;
  size?: number;
  created: string;
  labels: Record<string, string>;
  inUse: boolean;
  usedBy: string[];
}

export interface Network {
  id: string;
  name: string;
  driver: string;
  scope: string;
  created: string;
  attachedContainers: string[];
  internal: boolean;
  labels: Record<string, string>;
}

export interface ComposeService {
  Name: string;
  Image: string;
  ContainerID: string;
  Status: string;
  Replicas?: number;
}

export interface ComposeStack {
  Name: string;
  Directory: string;
  ConfigPath: string;
  Services: ComposeService[];
  Volumes: string[];
  Networks: string[];
}

// Peer management types
export interface Peer {
  id: string;
  name: string;
  hostname: string;
  status: 'online' | 'offline' | 'connecting';
  lastSeen: string;
  architecture: string;
  os: string;
  dockerVersion: string;
  availableSpace: number;
}

export interface PairingCode {
  code: string;
  expiresAt: string;
  peerId?: string;
}

// Migration types
export type MigrationPhase =
  | 'idle'
  | 'preflight'
  | 'selecting'
  | 'configuring'
  | 'dryrun'
  | 'running'
  | 'paused'
  | 'complete'
  | 'error';

export type PreflightCheckStatus = 'pending' | 'running' | 'passed' | 'warning' | 'failed';

export interface PreflightCheck {
  id: string;
  name: string;
  status: PreflightCheckStatus;
  message?: string;
  isBlocker: boolean;
  details?: Record<string, any>;
}

export interface MigrationProgress {
  currentStep: number;
  totalSteps: number;
  currentStepName: string;
  currentItem: string;
  currentItemIndex: number;
  totalItems: number;
  bytesTransferred: number;
  totalBytes: number;
  transferSpeed: number; // bytes per second
  startTime: string;
  estimatedCompletion: string | null;
}

export interface MigrationError {
  id: string;
  timestamp: string;
  severity: 'error' | 'warning';
  message: string;
  context: string;
  resourceId?: string;
  resourceName?: string;
  canRetry: boolean;
  canSkip: boolean;
}

export type MigrationMode = 'copy' | 'move';
export type MigrationStrategy = 'cold' | 'warm' | 'snapshot';
export type ConflictResolution = 'error' | 'skip' | 'overwrite' | 'rename';

export interface PathMapping {
  sourcePath: string;
  targetPath: string;
  convertToVolume: boolean;
}

export interface MigrationOptions {
  mode: MigrationMode;
  strategy: MigrationStrategy;
  conflictResolution: ConflictResolution;
  bandwidthLimit?: number; // KB/s
  stopSourceContainers: boolean;
  startTargetContainers: boolean;
  pathMappings: PathMapping[];
  dryRun: boolean;
}

export interface SelectedResource {
  type: 'container' | 'image' | 'volume' | 'network' | 'compose';
  id: string;
  name: string;
  dependencies: string[];
  size?: number;
}

export interface MigrationState {
  id: string;
  phase: MigrationPhase;
  sourcePeerId: string;
  targetPeerId: string;
  selectedResources: SelectedResource[];
  options: MigrationOptions;
  preflightChecks: PreflightCheck[];
  progress: MigrationProgress | null;
  errors: MigrationError[];
  canRetry: boolean;
  canResume: boolean;
  canCancel: boolean;
  completedSteps: string[];
}

export interface DryRunResult {
  operations: DryRunOperation[];
  warnings: string[];
  blockers: string[];
  estimatedDuration: number; // seconds
  estimatedSize: number; // bytes
}

export interface DryRunOperation {
  type: 'transfer_image' | 'transfer_volume' | 'transfer_container' | 'create_network' | 'start_container';
  resourceId: string;
  resourceName: string;
  size?: number;
  dependencies: string[];
}

// WebSocket message types
export type WSMessageType =
  | 'ping'
  | 'pong'
  | 'resource_update'
  | 'peer_status'
  | 'migration_progress'
  | 'migration_error'
  | 'migration_complete'
  | 'preflight_update';

export interface WSMessage {
  type: WSMessageType;
  timestamp: string;
  payload: any;
}

// API response types
export interface APIResponse<T = any> {
  success: boolean;
  data?: T;
  error?: string;
  message?: string;
}

export interface ResourceCounts {
  containers: number;
  images: number;
  volumes: number;
  networks: number;
  composeStacks: number;
  runningContainers: number;
  totalImageSize: number;
  totalVolumeSize: number;
}

// UI state types
export type ConnectionStatus = 'connecting' | 'connected' | 'disconnected' | 'error';

export interface Toast {
  id: string;
  type: 'success' | 'error' | 'warning' | 'info';
  title: string;
  message?: string;
  duration?: number;
}

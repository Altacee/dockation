import { useState, useEffect } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { Server } from 'lucide-react';
import { useWebSocket } from './hooks/useWebSocket';
import { ToastContainer } from './components/common/Toast';
import { ConnectionStatus } from './components/common/ConnectionStatus';
import { ResourceCard } from './components/Dashboard/ResourceCard';
import { PeerList } from './components/Dashboard/PeerList';
import { WorkerList } from './components/Dashboard/WorkerList';
import { QuickActions } from './components/Dashboard/QuickActions';
import { MasterQuickActions } from './components/Dashboard/MasterQuickActions';
import { GenerateCode } from './components/Pairing/GenerateCode';
import { EnterCode } from './components/Pairing/EnterCode';
import { PreFlightChecks } from './components/Migration/PreFlightChecks';
import { MigrationProgress } from './components/Migration/MigrationProgress';
import { MigrationComplete } from './components/Migration/MigrationComplete';
import { MigrationWizard } from './components/Migration/MigrationWizard';
import { WorkerResourceBrowser } from './components/Migration/WorkerResourceBrowser';
import { ResourceListView } from './components/Resources/ResourceListView';
import type {
  Toast as ToastType,
  Peer,
  Worker,
  PairingCode,
  ResourceCounts,
  MigrationState,
  WSMessage,
  ConfigInfo,
} from './types';
import { generateId } from './lib/utils';
import api from './api/client';

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
    },
  },
});

type View = 'dashboard' | 'generate-code' | 'enter-code' | 'migration' | 'migration-wizard' | 'worker-resources' | 'containers' | 'images' | 'volumes' | 'networks' | 'compose';

function App() {
  const [currentView, setCurrentView] = useState<View>('dashboard');
  const [toasts, setToasts] = useState<ToastType[]>([]);
  const [peers, setPeers] = useState<Peer[]>([]);
  const [workers, setWorkers] = useState<Worker[]>([]);
  const [configInfo, setConfigInfo] = useState<ConfigInfo | null>(null);
  const [resourceCounts, setResourceCounts] = useState<ResourceCounts>({
    containers: 0,
    images: 0,
    volumes: 0,
    networks: 0,
    composeStacks: 0,
    runningContainers: 0,
    totalImageSize: 0,
    totalVolumeSize: 0,
  });
  const [pairingCode, setPairingCode] = useState<PairingCode | null>(null);
  const [activeMigration, setActiveMigration] = useState<MigrationState | null>(null);
  const [selectedWorkerForResources, setSelectedWorkerForResources] = useState<Worker | null>(null);
  const [preselectedSourceWorker, setPreselectedSourceWorker] = useState<Worker | null>(null);

  const isMasterMode = configInfo?.role === 'master';

  // WebSocket connection - status shown in header, no toast spam
  const wsUrl = import.meta.env.VITE_WS_URL || 'ws://localhost:8080/ws';
  const { status: wsStatus } = useWebSocket({
    url: wsUrl,
    onMessage: handleWebSocketMessage,
  });

  function handleWebSocketMessage(message: WSMessage) {
    switch (message.type) {
      case 'resource_update':
        loadResourceCounts();
        break;
      case 'peer_status':
        if (isMasterMode) {
          loadWorkers();
        } else {
          loadPeers();
        }
        break;
      case 'worker_update':
        loadWorkers();
        break;
      case 'migration_progress':
        if (activeMigration) {
          setActiveMigration({
            ...activeMigration,
            progress: message.payload,
          });
        }
        break;
      case 'migration_error':
        if (activeMigration) {
          setActiveMigration({
            ...activeMigration,
            errors: [...activeMigration.errors, message.payload],
          });
        }
        break;
      case 'migration_complete':
        if (activeMigration) {
          setActiveMigration({
            ...activeMigration,
            phase: 'complete',
          });
        }
        addToast({
          type: 'success',
          title: 'Migration Complete',
          message: 'All resources have been transferred successfully',
        });
        break;
    }
  }

  // Load initial data
  useEffect(() => {
    loadConfig();
    loadResourceCounts();
  }, []);

  // Load peers or workers based on mode
  useEffect(() => {
    if (configInfo) {
      if (configInfo.role === 'master') {
        loadWorkers();
      } else {
        loadPeers();
      }
    }
  }, [configInfo]);

  // Prevent navigation during active migration
  useEffect(() => {
    if (activeMigration?.phase === 'running') {
      const handleBeforeUnload = (e: BeforeUnloadEvent) => {
        e.preventDefault();
        e.returnValue = '';
      };

      window.addEventListener('beforeunload', handleBeforeUnload);
      document.body.classList.add('migration-active');

      return () => {
        window.removeEventListener('beforeunload', handleBeforeUnload);
        document.body.classList.remove('migration-active');
      };
    }
  }, [activeMigration?.phase]);

  async function loadConfig() {
    const response = await api.config.info();
    if (response.success && response.data) {
      setConfigInfo(response.data);
    }
  }

  async function loadResourceCounts() {
    const response = await api.resources.counts();
    if (response.success && response.data) {
      setResourceCounts(response.data);
    }
  }

  async function loadPeers() {
    const response = await api.peers.list();
    if (response.success && response.data) {
      setPeers(response.data);
    }
  }

  async function loadWorkers() {
    const response = await api.workers.list();
    if (response.success && response.data) {
      setWorkers(response.data);
    }
  }

  function addToast(toast: Omit<ToastType, 'id'>) {
    const newToast: ToastType = { ...toast, id: generateId() };
    setToasts((prev) => [...prev, newToast]);
  }

  function removeToast(id: string) {
    setToasts((prev) => prev.filter((t) => t.id !== id));
  }

  async function handleGeneratePairingCode() {
    const response = await api.pairing.generate();
    if (response.success && response.data) {
      setPairingCode(response.data);
      setCurrentView('generate-code');
    } else {
      addToast({
        type: 'error',
        title: 'Failed to generate pairing code',
        message: response.error,
      });
    }
  }

  async function handleConnectWithCode(code: string) {
    const response = await api.pairing.connect(code);
    if (response.success && response.data) {
      addToast({
        type: 'success',
        title: 'Connected',
        message: `Successfully paired with ${response.data.name}`,
      });
      await loadPeers();
      setCurrentView('dashboard');
    } else {
      addToast({
        type: 'error',
        title: 'Connection failed',
        message: response.error || 'Invalid pairing code',
      });
    }
  }

  function handleStartMigration(_peer: Peer) {
    // In a real app, this would open a resource selection wizard
    addToast({
      type: 'info',
      title: 'Migration wizard',
      message: 'This would open the migration wizard UI',
    });
  }

  return (
    <QueryClientProvider client={queryClient}>
      <div className="min-h-screen bg-gray-50">
        {/* Header */}
        <header className="bg-white border-b border-gray-200 sticky top-0 z-40">
          <div className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-4">
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                <div className="p-2 bg-blue-600 rounded-lg">
                  <Server className="h-6 w-6 text-white" aria-hidden="true" />
                </div>
                <div>
                  <h1 className="text-2xl font-bold text-gray-900">docker-migrate</h1>
                  <p className="text-sm text-gray-600">
                    Safely migrate Docker resources between servers
                  </p>
                </div>
              </div>
              <ConnectionStatus status={wsStatus} />
            </div>
          </div>
        </header>

        {/* Main content */}
        <main className="max-w-7xl mx-auto px-4 sm:px-6 lg:px-8 py-8">
          {currentView === 'dashboard' && (
            <div className="space-y-8">
              {/* Resource overview */}
              <section aria-labelledby="resources-heading">
                <h2 id="resources-heading" className="text-lg font-semibold text-gray-900 mb-4">
                  Your Resources
                </h2>
                <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-5 gap-4">
                  <ResourceCard
                    type="containers"
                    count={resourceCounts.containers}
                    subtext={`${resourceCounts.runningContainers} running`}
                    onClick={() => setCurrentView('containers')}
                  />
                  <ResourceCard
                    type="images"
                    count={resourceCounts.images}
                    size={resourceCounts.totalImageSize}
                    onClick={() => setCurrentView('images')}
                  />
                  <ResourceCard
                    type="volumes"
                    count={resourceCounts.volumes}
                    size={resourceCounts.totalVolumeSize}
                    onClick={() => setCurrentView('volumes')}
                  />
                  <ResourceCard
                    type="networks"
                    count={resourceCounts.networks}
                    onClick={() => setCurrentView('networks')}
                  />
                  <ResourceCard
                    type="compose"
                    count={resourceCounts.composeStacks}
                    onClick={() => setCurrentView('compose')}
                  />
                </div>
              </section>

              {/* Workers/Peers and actions */}
              <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
                <div className="lg:col-span-2">
                  {isMasterMode ? (
                    <WorkerList
                      workers={workers}
                      onRemove={async (worker) => {
                        await api.workers.remove(worker.id);
                        await loadWorkers();
                        addToast({
                          type: 'info',
                          title: 'Worker Removed',
                          message: `Removed worker ${worker.name}`,
                        });
                      }}
                      onViewResources={(worker) => {
                        setSelectedWorkerForResources(worker);
                        setCurrentView('worker-resources');
                      }}
                    />
                  ) : (
                    <PeerList
                      peers={peers}
                      onMigrate={handleStartMigration}
                      onDisconnect={async (peer) => {
                        await api.peers.disconnect(peer.id);
                        await loadPeers();
                        addToast({
                          type: 'info',
                          title: 'Disconnected',
                          message: `Disconnected from ${peer.name}`,
                        });
                      }}
                    />
                  )}
                </div>
                <div>
                  {isMasterMode ? (
                    <MasterQuickActions
                      enrollmentToken={configInfo?.enrollment_token}
                      workerCount={workers.length}
                      onStartMigration={() => {
                        setPreselectedSourceWorker(null);
                        setCurrentView('migration-wizard');
                      }}
                    />
                  ) : (
                    <QuickActions
                      hasPeers={peers.length > 0}
                      onPairDevice={handleGeneratePairingCode}
                      onScanCode={() => setCurrentView('enter-code')}
                      onStartMigration={() =>
                        addToast({ type: 'info', title: 'Select a peer to migrate to' })
                      }
                    />
                  )}
                </div>
              </div>
            </div>
          )}

          {currentView === 'generate-code' && pairingCode && (
            <div className="max-w-2xl mx-auto">
              <GenerateCode
                code={pairingCode}
                onRefresh={handleGeneratePairingCode}
                onCancel={() => {
                  setCurrentView('dashboard');
                  setPairingCode(null);
                }}
              />
            </div>
          )}

          {currentView === 'enter-code' && (
            <div className="max-w-2xl mx-auto">
              <EnterCode
                onConnect={handleConnectWithCode}
                onCancel={() => setCurrentView('dashboard')}
              />
            </div>
          )}

          {currentView === 'migration' && activeMigration && (
            <div className="max-w-4xl mx-auto">
              {activeMigration.phase === 'preflight' && (
                <PreFlightChecks
                  checks={activeMigration.preflightChecks}
                  estimatedDuration={300}
                  onContinue={() => {
                    setActiveMigration({ ...activeMigration, phase: 'running' });
                  }}
                  onCancel={() => {
                    setCurrentView('dashboard');
                    setActiveMigration(null);
                  }}
                />
              )}

              {activeMigration.phase === 'running' && activeMigration.progress && (
                <MigrationProgress
                  progress={activeMigration.progress}
                  errors={activeMigration.errors}
                  connectionStatus={wsStatus}
                  onPause={async () => {
                    await api.migration.pause(activeMigration.id);
                  }}
                  onCancel={async () => {
                    await api.migration.cancel(activeMigration.id);
                    setCurrentView('dashboard');
                    setActiveMigration(null);
                  }}
                />
              )}

              {activeMigration.phase === 'complete' && (
                <MigrationComplete
                  result={{
                    success: true,
                    containersTransferred: 3,
                    imagesTransferred: 5,
                    volumesTransferred: 2,
                    networksCreated: 1,
                    totalSize: 2147483648,
                    duration: 420,
                    sourceStatus: 'Stopped and renamed to *-migrated-backup',
                  }}
                  targetPeerName="Target Server"
                  onDone={() => {
                    setCurrentView('dashboard');
                    setActiveMigration(null);
                  }}
                />
              )}
            </div>
          )}

          {currentView === 'containers' && (
            <ResourceListView type="containers" onBack={() => setCurrentView('dashboard')} />
          )}

          {currentView === 'images' && (
            <ResourceListView type="images" onBack={() => setCurrentView('dashboard')} />
          )}

          {currentView === 'volumes' && (
            <ResourceListView type="volumes" onBack={() => setCurrentView('dashboard')} />
          )}

          {currentView === 'networks' && (
            <ResourceListView type="networks" onBack={() => setCurrentView('dashboard')} />
          )}

          {currentView === 'compose' && (
            <ResourceListView type="compose" onBack={() => setCurrentView('dashboard')} />
          )}

          {currentView === 'migration-wizard' && (
            <MigrationWizard
              workers={workers}
              preselectedSourceWorker={preselectedSourceWorker || undefined}
              onComplete={async (migrationId) => {
                addToast({
                  type: 'success',
                  title: 'Migration Started',
                  message: `Migration ${migrationId} has been started`,
                });
                // Set up migration progress view
                setActiveMigration({
                  id: migrationId,
                  phase: 'running',
                  sourcePeerId: '',
                  targetPeerId: '',
                  selectedResources: [],
                  options: {
                    mode: 'copy',
                    strategy: 'cold',
                    conflictResolution: 'error',
                    stopSourceContainers: true,
                    startTargetContainers: true,
                    pathMappings: [],
                    dryRun: false,
                  },
                  preflightChecks: [],
                  progress: {
                    currentStep: 1,
                    totalSteps: 5,
                    currentStepName: 'Initializing',
                    currentItem: 'Starting migration...',
                    currentItemIndex: 0,
                    totalItems: 1,
                    bytesTransferred: 0,
                    totalBytes: 0,
                    transferSpeed: 0,
                    startTime: new Date().toISOString(),
                    estimatedCompletion: null,
                  },
                  errors: [],
                  canRetry: false,
                  canResume: false,
                  canCancel: true,
                  completedSteps: [],
                });
                setCurrentView('migration');
              }}
              onCancel={() => setCurrentView('dashboard')}
            />
          )}

          {currentView === 'worker-resources' && selectedWorkerForResources && (
            <WorkerResourceBrowser
              worker={selectedWorkerForResources}
              onBack={() => {
                setSelectedWorkerForResources(null);
                setCurrentView('dashboard');
              }}
              onStartMigration={(worker) => {
                setPreselectedSourceWorker(worker);
                setSelectedWorkerForResources(null);
                setCurrentView('migration-wizard');
              }}
            />
          )}
        </main>

        {/* Toast notifications */}
        <ToastContainer toasts={toasts} onClose={removeToast} />
      </div>
    </QueryClientProvider>
  );
}

export default App;

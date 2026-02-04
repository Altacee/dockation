import { useState, useEffect } from 'react';
import {
  ArrowLeft,
  Box,
  Database,
  HardDrive,
  Network,
  Loader2,
  AlertCircle,
  RefreshCw,
  ArrowRightLeft,
} from 'lucide-react';
import type { Worker, WorkerResources } from '../../types';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';
import { Button } from '../ui/Button';
import { Badge } from '../ui/Badge';
import { cn, formatBytes, formatRelativeTime } from '../../lib/utils';
import api from '../../api/client';

type TabType = 'containers' | 'images' | 'volumes' | 'networks';

const TABS: { id: TabType; label: string; icon: React.ElementType }[] = [
  { id: 'containers', label: 'Containers', icon: Box },
  { id: 'images', label: 'Images', icon: Database },
  { id: 'volumes', label: 'Volumes', icon: HardDrive },
  { id: 'networks', label: 'Networks', icon: Network },
];

interface WorkerResourceBrowserProps {
  worker: Worker;
  onBack: () => void;
  onStartMigration?: (worker: Worker) => void;
}

export function WorkerResourceBrowser({
  worker,
  onBack,
  onStartMigration,
}: WorkerResourceBrowserProps) {
  const [activeTab, setActiveTab] = useState<TabType>('containers');
  const [resources, setResources] = useState<WorkerResources | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    loadResources();
  }, [worker.id]);

  const loadResources = async () => {
    setIsLoading(true);
    setError(null);

    const response = await api.workers.resources(worker.id);
    if (response.success && response.data) {
      setResources(response.data);
    } else {
      setError(response.error || 'Failed to load resources');
    }

    setIsLoading(false);
  };

  const getTabCount = (tab: TabType) => {
    if (!resources) return 0;
    switch (tab) {
      case 'containers':
        return resources.containers?.length || 0;
      case 'images':
        return resources.images?.length || 0;
      case 'volumes':
        return resources.volumes?.length || 0;
      case 'networks':
        return resources.networks?.length || 0;
    }
  };

  return (
    <Card className="max-w-4xl mx-auto">
      <CardHeader>
        <div className="flex items-center justify-between">
          <Button variant="ghost" onClick={onBack} className="text-gray-600">
            <ArrowLeft className="h-4 w-4 mr-2" />
            Back to Dashboard
          </Button>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={loadResources} disabled={isLoading}>
              <RefreshCw className={cn('h-4 w-4 mr-2', isLoading && 'animate-spin')} />
              Refresh
            </Button>
            {onStartMigration && (
              <Button
                size="sm"
                onClick={() => onStartMigration(worker)}
                className="bg-blue-600 hover:bg-blue-700"
              >
                <ArrowRightLeft className="h-4 w-4 mr-2" />
                Migrate From This Worker
              </Button>
            )}
          </div>
        </div>
        <div className="mt-4">
          <CardTitle className="text-xl flex items-center gap-3">
            {worker.name}
            <Badge
              className={cn(
                'text-xs',
                worker.online ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-800'
              )}
              variant="outline"
            >
              {worker.online ? 'online' : 'offline'}
            </Badge>
          </CardTitle>
          <p className="text-sm text-gray-600 mt-1">
            {worker.hostname} ({worker.grpc_address})
          </p>
          {resources && (
            <p className="text-xs text-gray-500 mt-2">
              Last updated: {formatRelativeTime(resources.updated_at)}
            </p>
          )}
        </div>
      </CardHeader>

      <CardContent>
        {isLoading ? (
          <div className="flex flex-col items-center justify-center py-12">
            <Loader2 className="h-8 w-8 animate-spin text-blue-600" />
            <p className="mt-3 text-sm text-gray-600">Loading resources...</p>
          </div>
        ) : error ? (
          <div className="flex flex-col items-center justify-center py-12">
            <AlertCircle className="h-8 w-8 text-red-500" />
            <p className="mt-3 text-sm text-red-600">{error}</p>
            <Button onClick={loadResources} className="mt-4">
              Retry
            </Button>
          </div>
        ) : resources ? (
          <div className="space-y-4">
            {/* Tabs */}
            <div className="border-b border-gray-200">
              <nav className="flex -mb-px" aria-label="Resource types">
                {TABS.map((tab) => {
                  const Icon = tab.icon;
                  const count = getTabCount(tab.id);
                  const isActive = activeTab === tab.id;

                  return (
                    <button
                      key={tab.id}
                      onClick={() => setActiveTab(tab.id)}
                      className={cn(
                        'flex items-center gap-2 px-4 py-3 text-sm font-medium border-b-2 transition-colors',
                        isActive
                          ? 'border-blue-500 text-blue-600'
                          : 'border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300'
                      )}
                    >
                      <Icon className="h-4 w-4" />
                      {tab.label}
                      <Badge className="ml-1 bg-gray-100 text-gray-600" variant="outline">
                        {count}
                      </Badge>
                    </button>
                  );
                })}
              </nav>
            </div>

            {/* Content */}
            <div className="border rounded-lg overflow-hidden">
              <div className="max-h-96 overflow-y-auto">
                {activeTab === 'containers' && (
                  <ContainersTable containers={resources.containers || []} />
                )}
                {activeTab === 'images' && <ImagesTable images={resources.images || []} />}
                {activeTab === 'volumes' && <VolumesTable volumes={resources.volumes || []} />}
                {activeTab === 'networks' && <NetworksTable networks={resources.networks || []} />}
              </div>
            </div>
          </div>
        ) : null}
      </CardContent>
    </Card>
  );
}

// Containers table
function ContainersTable({
  containers,
}: {
  containers: NonNullable<WorkerResources['containers']>;
}) {
  if (containers.length === 0) {
    return (
      <div className="p-6 text-center text-sm text-gray-500">No containers on this worker</div>
    );
  }

  return (
    <table className="min-w-full divide-y divide-gray-200">
      <thead className="bg-gray-50">
        <tr>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Name
          </th>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Image
          </th>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Status
          </th>
        </tr>
      </thead>
      <tbody className="bg-white divide-y divide-gray-200">
        {containers.map((container) => {
          const name = container.names?.[0]?.replace(/^\//, '') || container.id.slice(0, 12);
          return (
            <tr key={container.id}>
              <td className="px-4 py-3">
                <span className="text-sm font-medium text-gray-900">{name}</span>
                <p className="text-xs text-gray-500 font-mono">{container.id.slice(0, 12)}</p>
              </td>
              <td className="px-4 py-3">
                <span className="text-sm text-gray-600">{container.image}</span>
              </td>
              <td className="px-4 py-3">
                <Badge
                  className={cn(
                    container.state === 'running'
                      ? 'bg-green-100 text-green-800'
                      : 'bg-gray-100 text-gray-800'
                  )}
                  variant="outline"
                >
                  {container.state}
                </Badge>
                <p className="text-xs text-gray-500 mt-1">{container.status}</p>
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

// Images table
function ImagesTable({ images }: { images: NonNullable<WorkerResources['images']> }) {
  if (images.length === 0) {
    return (
      <div className="p-6 text-center text-sm text-gray-500">No images on this worker</div>
    );
  }

  return (
    <table className="min-w-full divide-y divide-gray-200">
      <thead className="bg-gray-50">
        <tr>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Repository
          </th>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Tag
          </th>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Size
          </th>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            ID
          </th>
        </tr>
      </thead>
      <tbody className="bg-white divide-y divide-gray-200">
        {images.map((image) => {
          const tag = image.repo_tags?.[0] || '<none>:<none>';
          const [repo, tagName] = tag.split(':');
          return (
            <tr key={image.id}>
              <td className="px-4 py-3">
                <span className="text-sm font-medium text-gray-900">{repo}</span>
              </td>
              <td className="px-4 py-3">
                <span className="text-sm text-gray-600">{tagName}</span>
              </td>
              <td className="px-4 py-3">
                <span className="text-sm text-gray-600">{formatBytes(image.size)}</span>
              </td>
              <td className="px-4 py-3">
                <span className="text-xs text-gray-500 font-mono">
                  {image.id.replace('sha256:', '').slice(0, 12)}
                </span>
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

// Volumes table
function VolumesTable({ volumes }: { volumes: NonNullable<WorkerResources['volumes']> }) {
  if (volumes.length === 0) {
    return (
      <div className="p-6 text-center text-sm text-gray-500">No volumes on this worker</div>
    );
  }

  return (
    <table className="min-w-full divide-y divide-gray-200">
      <thead className="bg-gray-50">
        <tr>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Name
          </th>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Driver
          </th>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Mountpoint
          </th>
        </tr>
      </thead>
      <tbody className="bg-white divide-y divide-gray-200">
        {volumes.map((volume) => (
          <tr key={volume.name}>
            <td className="px-4 py-3">
              <span className="text-sm font-medium text-gray-900">{volume.name}</span>
            </td>
            <td className="px-4 py-3">
              <span className="text-sm text-gray-600">{volume.driver}</span>
            </td>
            <td className="px-4 py-3">
              <span className="text-xs text-gray-500 font-mono truncate block max-w-xs">
                {volume.mountpoint}
              </span>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

// Networks table
function NetworksTable({ networks }: { networks: NonNullable<WorkerResources['networks']> }) {
  if (networks.length === 0) {
    return (
      <div className="p-6 text-center text-sm text-gray-500">No networks on this worker</div>
    );
  }

  return (
    <table className="min-w-full divide-y divide-gray-200">
      <thead className="bg-gray-50">
        <tr>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Name
          </th>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Driver
          </th>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            ID
          </th>
        </tr>
      </thead>
      <tbody className="bg-white divide-y divide-gray-200">
        {networks.map((network) => (
          <tr key={network.id}>
            <td className="px-4 py-3">
              <span className="text-sm font-medium text-gray-900">{network.name}</span>
            </td>
            <td className="px-4 py-3">
              <span className="text-sm text-gray-600">{network.driver}</span>
            </td>
            <td className="px-4 py-3">
              <span className="text-xs text-gray-500 font-mono">{network.id.slice(0, 12)}</span>
            </td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

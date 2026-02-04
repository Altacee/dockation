import { useState, useEffect } from 'react';
import {
  Box,
  Database,
  HardDrive,
  Network,
  Loader2,
  AlertCircle,
  CheckSquare,
  Square,
} from 'lucide-react';
import { Button } from '../ui/Button';
import { Badge } from '../ui/Badge';
import { cn, formatBytes } from '../../lib/utils';
import { useMigrationContext } from './MigrationContext';
import api from '../../api/client';

type TabType = 'containers' | 'images' | 'volumes' | 'networks';

const TABS: { id: TabType; label: string; icon: React.ElementType }[] = [
  { id: 'containers', label: 'Containers', icon: Box },
  { id: 'images', label: 'Images', icon: Database },
  { id: 'volumes', label: 'Volumes', icon: HardDrive },
  { id: 'networks', label: 'Networks', icon: Network },
];

export function StepResourceSelect() {
  const {
    state,
    setSourceResources,
    toggleContainer,
    toggleImage,
    toggleVolume,
    toggleNetwork,
    selectAllContainers,
    deselectAllContainers,
    selectAllImages,
    deselectAllImages,
    selectAllVolumes,
    deselectAllVolumes,
    selectAllNetworks,
    deselectAllNetworks,
    nextStep,
    prevStep,
    getTotalSelectedCount,
  } = useMigrationContext();

  const [activeTab, setActiveTab] = useState<TabType>('containers');
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Load resources when component mounts
  useEffect(() => {
    if (state.sourceWorker && !state.sourceResources) {
      loadResources();
    }
  }, [state.sourceWorker]);

  const loadResources = async () => {
    if (!state.sourceWorker) return;

    setIsLoading(true);
    setError(null);

    const response = await api.workers.resources(state.sourceWorker.id);
    if (response.success && response.data) {
      setSourceResources(response.data);
    } else {
      setError(response.error || 'Failed to load resources');
    }

    setIsLoading(false);
  };

  const resources = state.sourceResources;
  const selected = state.selectedResources;

  const getTabCount = (tab: TabType) => {
    switch (tab) {
      case 'containers':
        return selected.containers.length;
      case 'images':
        return selected.images.length;
      case 'volumes':
        return selected.volumes.length;
      case 'networks':
        return selected.networks.length;
    }
  };

  const getTotalCount = (tab: TabType) => {
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

  const handleSelectAll = () => {
    switch (activeTab) {
      case 'containers':
        selectAllContainers();
        break;
      case 'images':
        selectAllImages();
        break;
      case 'volumes':
        selectAllVolumes();
        break;
      case 'networks':
        selectAllNetworks();
        break;
    }
  };

  const handleDeselectAll = () => {
    switch (activeTab) {
      case 'containers':
        deselectAllContainers();
        break;
      case 'images':
        deselectAllImages();
        break;
      case 'volumes':
        deselectAllVolumes();
        break;
      case 'networks':
        deselectAllNetworks();
        break;
    }
  };

  const isAllSelected = () => {
    if (!resources) return false;
    switch (activeTab) {
      case 'containers':
        return (
          (resources.containers?.length || 0) > 0 &&
          selected.containers.length === (resources.containers?.length || 0)
        );
      case 'images':
        return (
          (resources.images?.length || 0) > 0 &&
          selected.images.length === (resources.images?.length || 0)
        );
      case 'volumes':
        return (
          (resources.volumes?.length || 0) > 0 &&
          selected.volumes.length === (resources.volumes?.length || 0)
        );
      case 'networks':
        return (
          (resources.networks?.length || 0) > 0 &&
          selected.networks.length === (resources.networks?.length || 0)
        );
    }
  };

  if (isLoading) {
    return (
      <div className="flex flex-col items-center justify-center py-12">
        <Loader2 className="h-8 w-8 animate-spin text-blue-600" />
        <p className="mt-3 text-sm text-gray-600">
          Loading resources from {state.sourceWorker?.name}...
        </p>
      </div>
    );
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center py-12">
        <AlertCircle className="h-8 w-8 text-red-500" />
        <p className="mt-3 text-sm text-red-600">{error}</p>
        <Button onClick={loadResources} className="mt-4">
          Retry
        </Button>
      </div>
    );
  }

  if (!resources) {
    return null;
  }

  return (
    <div className="space-y-4">
      {/* Selection summary */}
      <div className="flex items-center justify-between p-3 bg-blue-50 border border-blue-200 rounded-lg">
        <span className="text-sm text-blue-900">
          <strong>{getTotalSelectedCount()}</strong> resources selected for migration
        </span>
        <div className="flex gap-2 text-xs">
          <span className="text-blue-700">{selected.containers.length} containers</span>
          <span className="text-blue-300">|</span>
          <span className="text-blue-700">{selected.images.length} images</span>
          <span className="text-blue-300">|</span>
          <span className="text-blue-700">{selected.volumes.length} volumes</span>
          <span className="text-blue-300">|</span>
          <span className="text-blue-700">{selected.networks.length} networks</span>
        </div>
      </div>

      {/* Tabs */}
      <div className="border-b border-gray-200">
        <nav className="flex -mb-px" aria-label="Resource types">
          {TABS.map((tab) => {
            const Icon = tab.icon;
            const count = getTabCount(tab.id);
            const total = getTotalCount(tab.id);
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
                <Badge
                  className={cn(
                    'ml-1',
                    count > 0 ? 'bg-blue-100 text-blue-700' : 'bg-gray-100 text-gray-600'
                  )}
                  variant="outline"
                >
                  {count}/{total}
                </Badge>
              </button>
            );
          })}
        </nav>
      </div>

      {/* Select all/none */}
      <div className="flex items-center justify-between">
        <span className="text-sm text-gray-600">
          {getTabCount(activeTab)} of {getTotalCount(activeTab)} selected
        </span>
        <div className="flex gap-2">
          <Button
            size="sm"
            variant="outline"
            onClick={handleSelectAll}
            disabled={isAllSelected()}
          >
            Select All
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={handleDeselectAll}
            disabled={getTabCount(activeTab) === 0}
          >
            Clear
          </Button>
        </div>
      </div>

      {/* Resource list */}
      <div className="border rounded-lg overflow-hidden">
        <div className="max-h-80 overflow-y-auto">
          {activeTab === 'containers' && (
            <ContainerList
              containers={resources.containers || []}
              selected={selected.containers}
              onToggle={toggleContainer}
            />
          )}
          {activeTab === 'images' && (
            <ImageList
              images={resources.images || []}
              selected={selected.images}
              onToggle={toggleImage}
            />
          )}
          {activeTab === 'volumes' && (
            <VolumeList
              volumes={resources.volumes || []}
              selected={selected.volumes}
              onToggle={toggleVolume}
            />
          )}
          {activeTab === 'networks' && (
            <NetworkList
              networks={resources.networks || []}
              selected={selected.networks}
              onToggle={toggleNetwork}
            />
          )}
        </div>
      </div>

      {/* Actions */}
      <div className="flex items-center justify-between pt-4 border-t">
        <Button variant="outline" onClick={prevStep}>
          Back
        </Button>
        <Button
          onClick={nextStep}
          disabled={getTotalSelectedCount() === 0}
          className="bg-blue-600 hover:bg-blue-700"
        >
          Continue
        </Button>
      </div>
    </div>
  );
}

// Container list component
interface ContainerListProps {
  containers: { id: string; names: string[]; image: string; state: string; status: string }[];
  selected: { id: string }[];
  onToggle: (container: any) => void;
}

function ContainerList({ containers, selected, onToggle }: ContainerListProps) {
  if (containers.length === 0) {
    return (
      <div className="p-6 text-center text-sm text-gray-500">No containers found</div>
    );
  }

  return (
    <table className="min-w-full divide-y divide-gray-200">
      <thead className="bg-gray-50">
        <tr>
          <th className="w-10 px-4 py-3"></th>
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
          const isSelected = selected.some((c) => c.id === container.id);
          const name = container.names?.[0]?.replace(/^\//, '') || container.id.slice(0, 12);

          return (
            <tr
              key={container.id}
              onClick={() => onToggle(container)}
              className={cn(
                'cursor-pointer transition-colors',
                isSelected ? 'bg-blue-50' : 'hover:bg-gray-50'
              )}
            >
              <td className="px-4 py-3">
                {isSelected ? (
                  <CheckSquare className="h-5 w-5 text-blue-600" />
                ) : (
                  <Square className="h-5 w-5 text-gray-300" />
                )}
              </td>
              <td className="px-4 py-3">
                <span className="text-sm font-medium text-gray-900">{name}</span>
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
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

// Image list component
interface ImageListProps {
  images: { id: string; repo_tags: string[] | null; size: number }[];
  selected: { id: string }[];
  onToggle: (image: any) => void;
}

function ImageList({ images, selected, onToggle }: ImageListProps) {
  if (images.length === 0) {
    return <div className="p-6 text-center text-sm text-gray-500">No images found</div>;
  }

  return (
    <table className="min-w-full divide-y divide-gray-200">
      <thead className="bg-gray-50">
        <tr>
          <th className="w-10 px-4 py-3"></th>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Repository
          </th>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Tag
          </th>
          <th className="px-4 py-3 text-left text-xs font-medium text-gray-500 uppercase">
            Size
          </th>
        </tr>
      </thead>
      <tbody className="bg-white divide-y divide-gray-200">
        {images.map((image) => {
          const isSelected = selected.some((i) => i.id === image.id);
          const tag = image.repo_tags?.[0] || '<none>:<none>';
          const [repo, tagName] = tag.split(':');

          return (
            <tr
              key={image.id}
              onClick={() => onToggle(image)}
              className={cn(
                'cursor-pointer transition-colors',
                isSelected ? 'bg-blue-50' : 'hover:bg-gray-50'
              )}
            >
              <td className="px-4 py-3">
                {isSelected ? (
                  <CheckSquare className="h-5 w-5 text-blue-600" />
                ) : (
                  <Square className="h-5 w-5 text-gray-300" />
                )}
              </td>
              <td className="px-4 py-3">
                <span className="text-sm font-medium text-gray-900">{repo}</span>
              </td>
              <td className="px-4 py-3">
                <span className="text-sm text-gray-600">{tagName}</span>
              </td>
              <td className="px-4 py-3">
                <span className="text-sm text-gray-600">{formatBytes(image.size)}</span>
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

// Volume list component
interface VolumeListProps {
  volumes: { name: string; driver: string; mountpoint: string }[];
  selected: { name: string }[];
  onToggle: (volume: any) => void;
}

function VolumeList({ volumes, selected, onToggle }: VolumeListProps) {
  if (volumes.length === 0) {
    return <div className="p-6 text-center text-sm text-gray-500">No volumes found</div>;
  }

  return (
    <table className="min-w-full divide-y divide-gray-200">
      <thead className="bg-gray-50">
        <tr>
          <th className="w-10 px-4 py-3"></th>
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
        {volumes.map((volume) => {
          const isSelected = selected.some((v) => v.name === volume.name);

          return (
            <tr
              key={volume.name}
              onClick={() => onToggle(volume)}
              className={cn(
                'cursor-pointer transition-colors',
                isSelected ? 'bg-blue-50' : 'hover:bg-gray-50'
              )}
            >
              <td className="px-4 py-3">
                {isSelected ? (
                  <CheckSquare className="h-5 w-5 text-blue-600" />
                ) : (
                  <Square className="h-5 w-5 text-gray-300" />
                )}
              </td>
              <td className="px-4 py-3">
                <span className="text-sm font-medium text-gray-900">{volume.name}</span>
              </td>
              <td className="px-4 py-3">
                <span className="text-sm text-gray-600">{volume.driver}</span>
              </td>
              <td className="px-4 py-3">
                <span className="text-sm text-gray-600 truncate max-w-xs block">
                  {volume.mountpoint}
                </span>
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

// Network list component
interface NetworkListProps {
  networks: { id: string; name: string; driver: string }[];
  selected: { id: string }[];
  onToggle: (network: any) => void;
}

function NetworkList({ networks, selected, onToggle }: NetworkListProps) {
  // Filter out default networks that shouldn't be migrated
  const migratable = networks.filter(
    (n) => !['bridge', 'host', 'none'].includes(n.name)
  );

  if (migratable.length === 0) {
    return (
      <div className="p-6 text-center text-sm text-gray-500">
        No custom networks found (default networks cannot be migrated)
      </div>
    );
  }

  return (
    <table className="min-w-full divide-y divide-gray-200">
      <thead className="bg-gray-50">
        <tr>
          <th className="w-10 px-4 py-3"></th>
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
        {migratable.map((network) => {
          const isSelected = selected.some((n) => n.id === network.id);

          return (
            <tr
              key={network.id}
              onClick={() => onToggle(network)}
              className={cn(
                'cursor-pointer transition-colors',
                isSelected ? 'bg-blue-50' : 'hover:bg-gray-50'
              )}
            >
              <td className="px-4 py-3">
                {isSelected ? (
                  <CheckSquare className="h-5 w-5 text-blue-600" />
                ) : (
                  <Square className="h-5 w-5 text-gray-300" />
                )}
              </td>
              <td className="px-4 py-3">
                <span className="text-sm font-medium text-gray-900">{network.name}</span>
              </td>
              <td className="px-4 py-3">
                <span className="text-sm text-gray-600">{network.driver}</span>
              </td>
              <td className="px-4 py-3">
                <span className="text-sm text-gray-600 font-mono">
                  {network.id.slice(0, 12)}
                </span>
              </td>
            </tr>
          );
        })}
      </tbody>
    </table>
  );
}

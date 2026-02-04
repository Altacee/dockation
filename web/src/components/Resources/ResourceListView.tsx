import { useState, useEffect } from 'react';
import {
  ArrowLeft,
  Container,
  Image,
  HardDrive,
  Network,
  Layers,
  Loader2,
  Play,
  Square,
  RotateCcw,
  Trash2,
  Plus,
  Download,
  Eye,
  RefreshCw,
} from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';
import { Button } from '../ui/Button';
import { ContainerDetail } from './ContainerDetail';
import api from '../../api/client';
import { formatBytes, cn } from '../../lib/utils';
import type { Container as ContainerType, Image as ImageType, Volume, Network as NetworkType, ComposeStack } from '../../types';

type ResourceType = 'containers' | 'images' | 'volumes' | 'networks' | 'compose';

interface ResourceListViewProps {
  type: ResourceType;
  onBack: () => void;
}

export function ResourceListView({ type, onBack }: ResourceListViewProps) {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [containers, setContainers] = useState<ContainerType[]>([]);
  const [images, setImages] = useState<ImageType[]>([]);
  const [volumes, setVolumes] = useState<Volume[]>([]);
  const [networks, setNetworks] = useState<NetworkType[]>([]);
  const [composeStacks, setComposeStacks] = useState<ComposeStack[]>([]);
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [selectedContainer, setSelectedContainer] = useState<string | null>(null);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [showPullModal, setShowPullModal] = useState(false);
  const [pullImage, setPullImage] = useState('');
  const [newVolumeName, setNewVolumeName] = useState('');
  const [newNetworkName, setNewNetworkName] = useState('');

  useEffect(() => {
    loadResources();
  }, [type]);

  async function loadResources() {
    setLoading(true);
    setError(null);

    try {
      switch (type) {
        case 'containers': {
          const response = await api.containers.list();
          if (response.success && response.data) {
            setContainers(response.data);
          } else {
            setError(response.error || 'Failed to load containers');
          }
          break;
        }
        case 'images': {
          const response = await api.images.list();
          if (response.success && response.data) {
            setImages(response.data);
          } else {
            setError(response.error || 'Failed to load images');
          }
          break;
        }
        case 'volumes': {
          const response = await api.volumes.list();
          if (response.success && response.data) {
            setVolumes(response.data);
          } else {
            setError(response.error || 'Failed to load volumes');
          }
          break;
        }
        case 'networks': {
          const response = await api.networks.list();
          if (response.success && response.data) {
            setNetworks(response.data);
          } else {
            setError(response.error || 'Failed to load networks');
          }
          break;
        }
        case 'compose': {
          const response = await api.compose.list();
          if (response.success && response.data) {
            setComposeStacks(response.data);
          } else {
            setError(response.error || 'Failed to load compose stacks');
          }
          break;
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An unexpected error occurred');
    } finally {
      setLoading(false);
    }
  }

  async function handleContainerAction(id: string, action: 'start' | 'stop' | 'restart' | 'remove') {
    setActionLoading(`${id}-${action}`);
    try {
      let response;
      switch (action) {
        case 'start':
          response = await api.containers.start(id);
          break;
        case 'stop':
          response = await api.containers.stop(id);
          break;
        case 'restart':
          response = await api.containers.restart(id);
          break;
        case 'remove':
          if (!confirm('Are you sure you want to remove this container?')) {
            setActionLoading(null);
            return;
          }
          response = await api.containers.remove(id, true);
          break;
      }
      if (response && !response.success) {
        setError(response.error || `Failed to ${action} container`);
      } else {
        await loadResources();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${action} container`);
    } finally {
      setActionLoading(null);
    }
  }

  async function handleImageRemove(id: string) {
    if (!confirm('Are you sure you want to remove this image?')) return;
    setActionLoading(`image-${id}`);
    try {
      const response = await api.images.remove(id, true);
      if (!response.success) {
        setError(response.error || 'Failed to remove image');
      } else {
        await loadResources();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove image');
    } finally {
      setActionLoading(null);
    }
  }

  async function handlePullImage() {
    if (!pullImage.trim()) return;
    setActionLoading('pull');
    try {
      const response = await api.images.pull(pullImage.trim());
      if (!response.success) {
        setError(response.error || 'Failed to pull image');
      } else {
        setPullImage('');
        setShowPullModal(false);
        await loadResources();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to pull image');
    } finally {
      setActionLoading(null);
    }
  }

  async function handleVolumeRemove(name: string) {
    if (!confirm('Are you sure you want to remove this volume? All data will be lost.')) return;
    setActionLoading(`volume-${name}`);
    try {
      const response = await api.volumes.remove(name, true);
      if (!response.success) {
        setError(response.error || 'Failed to remove volume');
      } else {
        await loadResources();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove volume');
    } finally {
      setActionLoading(null);
    }
  }

  async function handleCreateVolume() {
    if (!newVolumeName.trim()) return;
    setActionLoading('create-volume');
    try {
      const response = await api.volumes.create(newVolumeName.trim());
      if (!response.success) {
        setError(response.error || 'Failed to create volume');
      } else {
        setNewVolumeName('');
        setShowCreateModal(false);
        await loadResources();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create volume');
    } finally {
      setActionLoading(null);
    }
  }

  async function handleNetworkRemove(id: string) {
    if (!confirm('Are you sure you want to remove this network?')) return;
    setActionLoading(`network-${id}`);
    try {
      const response = await api.networks.remove(id);
      if (!response.success) {
        setError(response.error || 'Failed to remove network');
      } else {
        await loadResources();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to remove network');
    } finally {
      setActionLoading(null);
    }
  }

  async function handleCreateNetwork() {
    if (!newNetworkName.trim()) return;
    setActionLoading('create-network');
    try {
      const response = await api.networks.create(newNetworkName.trim());
      if (!response.success) {
        setError(response.error || 'Failed to create network');
      } else {
        setNewNetworkName('');
        setShowCreateModal(false);
        await loadResources();
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create network');
    } finally {
      setActionLoading(null);
    }
  }

  const config = {
    containers: {
      icon: Container,
      title: 'Containers',
      color: 'text-blue-600',
      bgColor: 'bg-blue-50',
    },
    images: {
      icon: Image,
      title: 'Images',
      color: 'text-purple-600',
      bgColor: 'bg-purple-50',
    },
    volumes: {
      icon: HardDrive,
      title: 'Volumes',
      color: 'text-green-600',
      bgColor: 'bg-green-50',
    },
    networks: {
      icon: Network,
      title: 'Networks',
      color: 'text-orange-600',
      bgColor: 'bg-orange-50',
    },
    compose: {
      icon: Layers,
      title: 'Compose Stacks',
      color: 'text-indigo-600',
      bgColor: 'bg-indigo-50',
    },
  };

  const { icon: Icon, title, color, bgColor } = config[type];

  function getResourceCount() {
    switch (type) {
      case 'containers':
        return containers.length;
      case 'images':
        return images.length;
      case 'volumes':
        return volumes.length;
      case 'networks':
        return networks.length;
      case 'compose':
        return composeStacks.length;
      default:
        return 0;
    }
  }

  function getStatusBadgeClass(status: string) {
    switch (status) {
      case 'running':
        return 'bg-green-100 text-green-800';
      case 'stopped':
      case 'exited':
        return 'bg-gray-100 text-gray-800';
      case 'paused':
        return 'bg-yellow-100 text-yellow-800';
      case 'restarting':
        return 'bg-blue-100 text-blue-800';
      case 'dead':
        return 'bg-red-100 text-red-800';
      default:
        return 'bg-gray-100 text-gray-800';
    }
  }

  // Show container detail view if selected
  if (selectedContainer) {
    return (
      <ContainerDetail
        containerId={selectedContainer}
        onBack={() => setSelectedContainer(null)}
        onRefresh={loadResources}
      />
    );
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button
            variant="outline"
            size="sm"
            onClick={onBack}
            className="gap-2"
            aria-label="Back to dashboard"
          >
            <ArrowLeft className="h-4 w-4" />
            Back
          </Button>
          <div className="flex items-center gap-3">
            <div className={cn('p-2 rounded-lg', bgColor)}>
              <Icon className={cn('h-6 w-6', color)} aria-hidden="true" />
            </div>
            <div>
              <h2 className="text-2xl font-bold text-gray-900">{title}</h2>
              <p className="text-sm text-gray-600">
                {loading ? 'Loading...' : `${getResourceCount()} total`}
              </p>
            </div>
          </div>
        </div>

        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={loadResources} className="gap-2">
            <RefreshCw className="h-4 w-4" />
            Refresh
          </Button>
          {type === 'images' && (
            <Button
              variant="default"
              size="sm"
              onClick={() => setShowPullModal(true)}
              className="gap-2"
            >
              <Download className="h-4 w-4" />
              Pull Image
            </Button>
          )}
          {type === 'volumes' && (
            <Button
              variant="default"
              size="sm"
              onClick={() => setShowCreateModal(true)}
              className="gap-2"
            >
              <Plus className="h-4 w-4" />
              Create Volume
            </Button>
          )}
          {type === 'networks' && (
            <Button
              variant="default"
              size="sm"
              onClick={() => setShowCreateModal(true)}
              className="gap-2"
            >
              <Plus className="h-4 w-4" />
              Create Network
            </Button>
          )}
        </div>
      </div>

      {/* Pull Image Modal */}
      {showPullModal && (
        <Card className="border-purple-200">
          <CardContent className="py-4">
            <div className="flex items-center gap-4">
              <input
                type="text"
                placeholder="Image name (e.g., nginx:latest)"
                value={pullImage}
                onChange={(e) => setPullImage(e.target.value)}
                className="flex-1 px-3 py-2 border rounded-lg text-sm"
              />
              <Button
                variant="default"
                size="sm"
                onClick={handlePullImage}
                disabled={actionLoading === 'pull' || !pullImage.trim()}
              >
                {actionLoading === 'pull' ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  'Pull'
                )}
              </Button>
              <Button variant="outline" size="sm" onClick={() => setShowPullModal(false)}>
                Cancel
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Create Volume/Network Modal */}
      {showCreateModal && (type === 'volumes' || type === 'networks') && (
        <Card className={cn(type === 'volumes' ? 'border-green-200' : 'border-orange-200')}>
          <CardContent className="py-4">
            <div className="flex items-center gap-4">
              <input
                type="text"
                placeholder={type === 'volumes' ? 'Volume name' : 'Network name'}
                value={type === 'volumes' ? newVolumeName : newNetworkName}
                onChange={(e) =>
                  type === 'volumes'
                    ? setNewVolumeName(e.target.value)
                    : setNewNetworkName(e.target.value)
                }
                className="flex-1 px-3 py-2 border rounded-lg text-sm"
              />
              <Button
                variant="default"
                size="sm"
                onClick={type === 'volumes' ? handleCreateVolume : handleCreateNetwork}
                disabled={
                  actionLoading === `create-${type.slice(0, -1)}` ||
                  !(type === 'volumes' ? newVolumeName.trim() : newNetworkName.trim())
                }
              >
                {actionLoading === `create-${type.slice(0, -1)}` ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  'Create'
                )}
              </Button>
              <Button variant="outline" size="sm" onClick={() => setShowCreateModal(false)}>
                Cancel
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Error Message */}
      {error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4">
          <p className="text-sm text-red-800">{error}</p>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setError(null)}
            className="mt-2"
          >
            Dismiss
          </Button>
        </div>
      )}

      {/* Loading State */}
      {loading && (
        <Card>
          <CardContent className="flex items-center justify-center py-12">
            <div className="flex flex-col items-center gap-3">
              <Loader2 className="h-8 w-8 animate-spin text-gray-400" />
              <p className="text-sm text-gray-600">Loading {title.toLowerCase()}...</p>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Empty State */}
      {!loading && !error && getResourceCount() === 0 && (
        <Card>
          <CardContent className="py-12 text-center">
            <Icon className={cn('h-12 w-12 mx-auto mb-3', color)} aria-hidden="true" />
            <p className="text-gray-600">No {title.toLowerCase()} found</p>
          </CardContent>
        </Card>
      )}

      {/* Containers List */}
      {!loading && !error && type === 'containers' && containers.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Container Details</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-gray-200">
                <thead>
                  <tr className="text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    <th className="pb-3 pr-4">Name</th>
                    <th className="pb-3 pr-4">Image</th>
                    <th className="pb-3 pr-4">Status</th>
                    <th className="pb-3 pr-4">Ports</th>
                    <th className="pb-3 pr-4">Created</th>
                    <th className="pb-3">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-200">
                  {containers.map((container: any) => {
                    const isRunning = container.State?.toLowerCase() === 'running';
                    return (
                      <tr key={container.Id} className="text-sm">
                        <td className="py-3 pr-4 font-medium text-gray-900">
                          {container.Names?.[0]?.replace(/^\//, '') || container.Id?.substring(0, 12)}
                        </td>
                        <td className="py-3 pr-4 text-gray-600 font-mono text-xs">
                          {container.Image?.substring(0, 40)}
                        </td>
                        <td className="py-3 pr-4">
                          <span
                            className={cn(
                              'inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium',
                              getStatusBadgeClass(container.State?.toLowerCase() || '')
                            )}
                          >
                            {container.State}
                          </span>
                        </td>
                        <td className="py-3 pr-4 text-gray-600">
                          {container.Ports?.length > 0
                            ? container.Ports.filter((p: any) => p.PublicPort)
                                .map((p: any) => `${p.PublicPort}:${p.PrivatePort}`)
                                .join(', ') || '-'
                            : '-'}
                        </td>
                        <td className="py-3 pr-4 text-gray-600">
                          {new Date(container.Created * 1000).toLocaleDateString()}
                        </td>
                        <td className="py-3">
                          <div className="flex items-center gap-1">
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => setSelectedContainer(container.Id)}
                              title="View details"
                            >
                              <Eye className="h-4 w-4" />
                            </Button>
                            {!isRunning && (
                              <Button
                                variant="ghost"
                                size="sm"
                                onClick={() => handleContainerAction(container.Id, 'start')}
                                disabled={actionLoading === `${container.Id}-start`}
                                title="Start"
                                className="text-green-600 hover:text-green-700"
                              >
                                {actionLoading === `${container.Id}-start` ? (
                                  <Loader2 className="h-4 w-4 animate-spin" />
                                ) : (
                                  <Play className="h-4 w-4" />
                                )}
                              </Button>
                            )}
                            {isRunning && (
                              <>
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  onClick={() => handleContainerAction(container.Id, 'stop')}
                                  disabled={actionLoading === `${container.Id}-stop`}
                                  title="Stop"
                                >
                                  {actionLoading === `${container.Id}-stop` ? (
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                  ) : (
                                    <Square className="h-4 w-4" />
                                  )}
                                </Button>
                                <Button
                                  variant="ghost"
                                  size="sm"
                                  onClick={() => handleContainerAction(container.Id, 'restart')}
                                  disabled={actionLoading === `${container.Id}-restart`}
                                  title="Restart"
                                >
                                  {actionLoading === `${container.Id}-restart` ? (
                                    <Loader2 className="h-4 w-4 animate-spin" />
                                  ) : (
                                    <RotateCcw className="h-4 w-4" />
                                  )}
                                </Button>
                              </>
                            )}
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleContainerAction(container.Id, 'remove')}
                              disabled={actionLoading === `${container.Id}-remove` || isRunning}
                              title={isRunning ? 'Stop container first' : 'Remove'}
                              className="text-red-600 hover:text-red-700"
                            >
                              {actionLoading === `${container.Id}-remove` ? (
                                <Loader2 className="h-4 w-4 animate-spin" />
                              ) : (
                                <Trash2 className="h-4 w-4" />
                              )}
                            </Button>
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Images List */}
      {!loading && !error && type === 'images' && images.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Image Details</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-gray-200">
                <thead>
                  <tr className="text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    <th className="pb-3 pr-4">Repository</th>
                    <th className="pb-3 pr-4">Size</th>
                    <th className="pb-3 pr-4">ID</th>
                    <th className="pb-3 pr-4">Created</th>
                    <th className="pb-3">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-200">
                  {images.map((image: any) => (
                    <tr key={image.Id} className="text-sm">
                      <td className="py-3 pr-4 font-medium text-gray-900 font-mono text-xs">
                        {image.RepoTags?.[0] || '<none>'}
                      </td>
                      <td className="py-3 pr-4 text-gray-600">{formatBytes(image.Size || 0)}</td>
                      <td className="py-3 pr-4 text-gray-600 font-mono text-xs">
                        {image.Id?.replace('sha256:', '').substring(0, 12)}
                      </td>
                      <td className="py-3 pr-4 text-gray-600">
                        {new Date(image.Created * 1000).toLocaleDateString()}
                      </td>
                      <td className="py-3">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleImageRemove(image.Id)}
                          disabled={actionLoading === `image-${image.Id}`}
                          title="Remove image"
                          className="text-red-600 hover:text-red-700"
                        >
                          {actionLoading === `image-${image.Id}` ? (
                            <Loader2 className="h-4 w-4 animate-spin" />
                          ) : (
                            <Trash2 className="h-4 w-4" />
                          )}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Volumes List */}
      {!loading && !error && type === 'volumes' && volumes.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Volume Details</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-gray-200">
                <thead>
                  <tr className="text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    <th className="pb-3 pr-4">Name</th>
                    <th className="pb-3 pr-4">Driver</th>
                    <th className="pb-3 pr-4">Scope</th>
                    <th className="pb-3 pr-4">Created</th>
                    <th className="pb-3">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-200">
                  {volumes.map((volume: any) => (
                    <tr key={volume.Name} className="text-sm">
                      <td className="py-3 pr-4 font-medium text-gray-900 max-w-xs truncate">
                        {volume.Name}
                      </td>
                      <td className="py-3 pr-4 text-gray-600">{volume.Driver}</td>
                      <td className="py-3 pr-4 text-gray-600">{volume.Scope}</td>
                      <td className="py-3 pr-4 text-gray-600">
                        {volume.CreatedAt ? new Date(volume.CreatedAt).toLocaleDateString() : '-'}
                      </td>
                      <td className="py-3">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => handleVolumeRemove(volume.Name)}
                          disabled={actionLoading === `volume-${volume.Name}`}
                          title="Remove volume"
                          className="text-red-600 hover:text-red-700"
                        >
                          {actionLoading === `volume-${volume.Name}` ? (
                            <Loader2 className="h-4 w-4 animate-spin" />
                          ) : (
                            <Trash2 className="h-4 w-4" />
                          )}
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Networks List */}
      {!loading && !error && type === 'networks' && networks.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Network Details</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-gray-200">
                <thead>
                  <tr className="text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    <th className="pb-3 pr-4">Name</th>
                    <th className="pb-3 pr-4">Driver</th>
                    <th className="pb-3 pr-4">Scope</th>
                    <th className="pb-3 pr-4">ID</th>
                    <th className="pb-3 pr-4">Created</th>
                    <th className="pb-3">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-200">
                  {networks.map((network: any) => {
                    const isBuiltIn = ['bridge', 'host', 'none'].includes(network.Name);
                    return (
                      <tr key={network.Id} className="text-sm">
                        <td className="py-3 pr-4 font-medium text-gray-900">{network.Name}</td>
                        <td className="py-3 pr-4 text-gray-600">{network.Driver}</td>
                        <td className="py-3 pr-4 text-gray-600">{network.Scope}</td>
                        <td className="py-3 pr-4 text-gray-600 font-mono text-xs">
                          {network.Id?.substring(0, 12)}
                        </td>
                        <td className="py-3 pr-4 text-gray-600">
                          {network.Created ? new Date(network.Created).toLocaleDateString() : '-'}
                        </td>
                        <td className="py-3">
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleNetworkRemove(network.Id)}
                            disabled={actionLoading === `network-${network.Id}` || isBuiltIn}
                            title={isBuiltIn ? 'Cannot remove built-in network' : 'Remove network'}
                            className={cn(
                              isBuiltIn
                                ? 'text-gray-400 cursor-not-allowed'
                                : 'text-red-600 hover:text-red-700'
                            )}
                          >
                            {actionLoading === `network-${network.Id}` ? (
                              <Loader2 className="h-4 w-4 animate-spin" />
                            ) : (
                              <Trash2 className="h-4 w-4" />
                            )}
                          </Button>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </CardContent>
        </Card>
      )}

      {/* Compose Stacks List */}
      {!loading && !error && type === 'compose' && composeStacks.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Compose Stack Details</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="overflow-x-auto">
              <table className="min-w-full divide-y divide-gray-200">
                <thead>
                  <tr className="text-left text-xs font-medium text-gray-500 uppercase tracking-wider">
                    <th className="pb-3 pr-4">Name</th>
                    <th className="pb-3 pr-4">Services</th>
                    <th className="pb-3 pr-4">Status</th>
                    <th className="pb-3 pr-4">Directory</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-200">
                  {composeStacks.map((stack: ComposeStack) => {
                    const runningCount = stack.Services?.filter(
                      (s) => s.Status === 'running'
                    ).length || 0;
                    const totalCount = stack.Services?.length || 0;
                    return (
                      <tr key={stack.Name} className="text-sm">
                        <td className="py-3 pr-4 font-medium text-gray-900">
                          {stack.Name}
                        </td>
                        <td className="py-3 pr-4 text-gray-600">
                          {totalCount} service{totalCount !== 1 ? 's' : ''}
                        </td>
                        <td className="py-3 pr-4">
                          <span
                            className={cn(
                              'inline-flex items-center px-2.5 py-0.5 rounded-full text-xs font-medium',
                              runningCount === totalCount && totalCount > 0
                                ? 'bg-green-100 text-green-800'
                                : runningCount > 0
                                ? 'bg-yellow-100 text-yellow-800'
                                : 'bg-gray-100 text-gray-800'
                            )}
                          >
                            {runningCount}/{totalCount} running
                          </span>
                        </td>
                        <td className="py-3 pr-4 text-gray-600 font-mono text-xs max-w-xs truncate">
                          {stack.Directory || '-'}
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>

            {/* Services breakdown */}
            {composeStacks.length > 0 && (
              <div className="mt-6 pt-6 border-t border-gray-200">
                <h4 className="text-sm font-medium text-gray-700 mb-4">Services by Stack</h4>
                <div className="space-y-4">
                  {composeStacks.map((stack) => (
                    <div key={stack.Name} className="bg-gray-50 rounded-lg p-4">
                      <h5 className="font-medium text-gray-900 mb-2">{stack.Name}</h5>
                      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-2">
                        {stack.Services?.map((service) => (
                          <div
                            key={service.ContainerID || service.Name}
                            className="flex items-center justify-between bg-white px-3 py-2 rounded border border-gray-200"
                          >
                            <span className="text-sm text-gray-700">{service.Name}</span>
                            <span
                              className={cn(
                                'text-xs px-2 py-0.5 rounded',
                                service.Status === 'running'
                                  ? 'bg-green-100 text-green-700'
                                  : 'bg-gray-100 text-gray-600'
                              )}
                            >
                              {service.Status || 'unknown'}
                            </span>
                          </div>
                        ))}
                      </div>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      )}
    </div>
  );
}

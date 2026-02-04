import { useState } from 'react';
import {
  ArrowRight,
  Box,
  Database,
  HardDrive,
  Network,
  Loader2,
  AlertTriangle,
  Play,
} from 'lucide-react';
import { Button } from '../ui/Button';
import { Badge } from '../ui/Badge';
import { formatBytes } from '../../lib/utils';
import { useMigrationContext } from './MigrationContext';
import api from '../../api/client';

interface StepReviewProps {
  onComplete: (migrationId: string) => void;
  onCancel: () => void;
}

export function StepReview({ onComplete, onCancel }: StepReviewProps) {
  const { state, prevStep, getTotalSelectedCount } = useMigrationContext();
  const [isStarting, setIsStarting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { sourceWorker, targetWorker, selectedResources, options } = state;

  // Calculate total size of selected images
  const totalImageSize = selectedResources.images.reduce((acc, img) => acc + img.size, 0);

  const handleStartMigration = async () => {
    if (!sourceWorker || !targetWorker) return;

    setIsStarting(true);
    setError(null);

    const response = await api.migrations.start({
      source_worker_id: sourceWorker.id,
      target_worker_id: targetWorker.id,
      container_ids: selectedResources.containers.map((c) => c.id),
      image_ids: selectedResources.images.map((i) => i.id),
      volume_names: selectedResources.volumes.map((v) => v.name),
      network_ids: selectedResources.networks.map((n) => n.id),
      mode: options.mode,
      strategy: options.strategy,
      transfer_mode: options.transferMode,
    });

    setIsStarting(false);

    if (response.success && response.data) {
      onComplete(response.data.id);
    } else {
      setError(response.error || 'Failed to start migration');
    }
  };

  const getModeLabel = (mode: string) => {
    switch (mode) {
      case 'cold':
        return 'Cold Migration';
      case 'warm':
        return 'Warm Migration';
      case 'live':
        return 'Live Migration';
      default:
        return mode;
    }
  };

  const getStrategyLabel = (strategy: string) => {
    switch (strategy) {
      case 'full':
        return 'Full Transfer';
      case 'incremental':
        return 'Incremental';
      case 'snapshot':
        return 'Snapshot';
      default:
        return strategy;
    }
  };

  const getTransferModeLabel = (transferMode: string) => {
    switch (transferMode) {
      case 'direct':
        return 'Direct';
      case 'proxy':
        return 'Proxy (via Master)';
      case 'auto':
        return 'Auto-detect';
      default:
        return transferMode;
    }
  };

  return (
    <div className="space-y-6">
      {/* Migration summary */}
      <div className="grid gap-4">
        {/* Source and Target */}
        <div className="flex items-center gap-4 p-4 bg-gray-50 rounded-lg">
          <div className="flex-1">
            <p className="text-xs text-gray-500 mb-1">Source</p>
            <p className="text-sm font-semibold text-gray-900">{sourceWorker?.name}</p>
            <p className="text-xs text-gray-500">{sourceWorker?.hostname}</p>
          </div>
          <ArrowRight className="h-6 w-6 text-gray-400 flex-shrink-0" />
          <div className="flex-1 text-right">
            <p className="text-xs text-gray-500 mb-1">Target</p>
            <p className="text-sm font-semibold text-gray-900">{targetWorker?.name}</p>
            <p className="text-xs text-gray-500">{targetWorker?.hostname}</p>
          </div>
        </div>

        {/* Resources */}
        <div className="p-4 bg-gray-50 rounded-lg">
          <p className="text-xs text-gray-500 mb-3">Resources to Migrate</p>
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
            <div className="flex items-center gap-2">
              <Box className="h-4 w-4 text-gray-500" />
              <span className="text-sm">
                <strong>{selectedResources.containers.length}</strong> containers
              </span>
            </div>
            <div className="flex items-center gap-2">
              <Database className="h-4 w-4 text-gray-500" />
              <span className="text-sm">
                <strong>{selectedResources.images.length}</strong> images
              </span>
            </div>
            <div className="flex items-center gap-2">
              <HardDrive className="h-4 w-4 text-gray-500" />
              <span className="text-sm">
                <strong>{selectedResources.volumes.length}</strong> volumes
              </span>
            </div>
            <div className="flex items-center gap-2">
              <Network className="h-4 w-4 text-gray-500" />
              <span className="text-sm">
                <strong>{selectedResources.networks.length}</strong> networks
              </span>
            </div>
          </div>
          {totalImageSize > 0 && (
            <p className="text-xs text-gray-500 mt-3">
              Total image size: {formatBytes(totalImageSize)}
            </p>
          )}
        </div>

        {/* Configuration */}
        <div className="p-4 bg-gray-50 rounded-lg">
          <p className="text-xs text-gray-500 mb-3">Configuration</p>
          <div className="flex items-center gap-4 flex-wrap">
            <div>
              <p className="text-xs text-gray-500">Mode</p>
              <Badge className="bg-blue-100 text-blue-800" variant="outline">
                {getModeLabel(options.mode)}
              </Badge>
            </div>
            <div>
              <p className="text-xs text-gray-500">Strategy</p>
              <Badge className="bg-blue-100 text-blue-800" variant="outline">
                {getStrategyLabel(options.strategy)}
              </Badge>
            </div>
            <div>
              <p className="text-xs text-gray-500">Transfer</p>
              <Badge className="bg-blue-100 text-blue-800" variant="outline">
                {getTransferModeLabel(options.transferMode)}
              </Badge>
            </div>
          </div>
        </div>
      </div>

      {/* Resource details */}
      <div className="space-y-3">
        <p className="text-sm font-medium text-gray-900">Selected Resources</p>

        {/* Containers */}
        {selectedResources.containers.length > 0 && (
          <div className="border rounded-lg overflow-hidden">
            <div className="px-4 py-2 bg-gray-50 border-b flex items-center gap-2">
              <Box className="h-4 w-4 text-gray-500" />
              <span className="text-sm font-medium text-gray-700">Containers</span>
            </div>
            <div className="divide-y max-h-32 overflow-y-auto">
              {selectedResources.containers.map((container) => (
                <div key={container.id} className="px-4 py-2 text-sm">
                  <span className="font-medium">
                    {container.names?.[0]?.replace(/^\//, '') || container.id.slice(0, 12)}
                  </span>
                  <span className="text-gray-500 ml-2">({container.image})</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Images */}
        {selectedResources.images.length > 0 && (
          <div className="border rounded-lg overflow-hidden">
            <div className="px-4 py-2 bg-gray-50 border-b flex items-center gap-2">
              <Database className="h-4 w-4 text-gray-500" />
              <span className="text-sm font-medium text-gray-700">Images</span>
            </div>
            <div className="divide-y max-h-32 overflow-y-auto">
              {selectedResources.images.map((image) => (
                <div key={image.id} className="px-4 py-2 text-sm flex justify-between">
                  <span className="font-medium">{image.repo_tags?.[0] || '<none>'}</span>
                  <span className="text-gray-500">{formatBytes(image.size)}</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Volumes */}
        {selectedResources.volumes.length > 0 && (
          <div className="border rounded-lg overflow-hidden">
            <div className="px-4 py-2 bg-gray-50 border-b flex items-center gap-2">
              <HardDrive className="h-4 w-4 text-gray-500" />
              <span className="text-sm font-medium text-gray-700">Volumes</span>
            </div>
            <div className="divide-y max-h-32 overflow-y-auto">
              {selectedResources.volumes.map((volume) => (
                <div key={volume.name} className="px-4 py-2 text-sm">
                  <span className="font-medium">{volume.name}</span>
                  <span className="text-gray-500 ml-2">({volume.driver})</span>
                </div>
              ))}
            </div>
          </div>
        )}

        {/* Networks */}
        {selectedResources.networks.length > 0 && (
          <div className="border rounded-lg overflow-hidden">
            <div className="px-4 py-2 bg-gray-50 border-b flex items-center gap-2">
              <Network className="h-4 w-4 text-gray-500" />
              <span className="text-sm font-medium text-gray-700">Networks</span>
            </div>
            <div className="divide-y max-h-32 overflow-y-auto">
              {selectedResources.networks.map((network) => (
                <div key={network.id} className="px-4 py-2 text-sm">
                  <span className="font-medium">{network.name}</span>
                  <span className="text-gray-500 ml-2">({network.driver})</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>

      {/* Warning */}
      {options.mode === 'cold' && selectedResources.containers.some(c => c.state === 'running') && (
        <div className="flex items-start gap-3 p-4 bg-amber-50 border border-amber-200 rounded-lg">
          <AlertTriangle className="h-5 w-5 text-amber-600 flex-shrink-0 mt-0.5" />
          <div className="text-sm text-amber-800">
            <p className="font-medium">Running containers will be stopped</p>
            <p className="mt-1 text-xs">
              Cold migration requires stopping containers on the source worker before transfer.
              They will be started on the target worker after migration completes.
            </p>
          </div>
        </div>
      )}

      {/* Error */}
      {error && (
        <div className="flex items-start gap-3 p-4 bg-red-50 border border-red-200 rounded-lg">
          <AlertTriangle className="h-5 w-5 text-red-600 flex-shrink-0 mt-0.5" />
          <div className="text-sm text-red-800">
            <p className="font-medium">Migration failed to start</p>
            <p className="mt-1 text-xs">{error}</p>
          </div>
        </div>
      )}

      {/* Actions */}
      <div className="flex items-center justify-between pt-4 border-t">
        <Button variant="outline" onClick={prevStep} disabled={isStarting}>
          Back
        </Button>
        <div className="flex gap-3">
          <Button variant="outline" onClick={onCancel} disabled={isStarting}>
            Cancel
          </Button>
          <Button
            onClick={handleStartMigration}
            disabled={isStarting || getTotalSelectedCount() === 0}
            className="bg-blue-600 hover:bg-blue-700"
          >
            {isStarting ? (
              <>
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
                Starting...
              </>
            ) : (
              <>
                <Play className="h-4 w-4 mr-2" />
                Start Migration
              </>
            )}
          </Button>
        </div>
      </div>
    </div>
  );
}

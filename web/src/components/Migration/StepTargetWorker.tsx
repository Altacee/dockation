import { Server, Wifi, WifiOff, Box, Database, HardDrive, Network } from 'lucide-react';
import type { Worker } from '../../types';
import { Button } from '../ui/Button';
import { Badge } from '../ui/Badge';
import { cn, formatRelativeTime } from '../../lib/utils';
import { useMigrationContext } from './MigrationContext';

interface StepTargetWorkerProps {
  workers: Worker[];
}

export function StepTargetWorker({ workers }: StepTargetWorkerProps) {
  const { state, setTargetWorker, nextStep, prevStep } = useMigrationContext();

  // Filter to online workers that are not the source
  const availableWorkers = workers.filter(
    (w) => w.online && w.id !== state.sourceWorker?.id
  );

  const handleSelect = (worker: Worker) => {
    setTargetWorker(worker);
  };

  const handleContinue = () => {
    if (state.targetWorker) {
      nextStep();
    }
  };

  if (availableWorkers.length === 0) {
    return (
      <div className="space-y-6">
        <div className="text-center py-12">
          <Server className="h-12 w-12 mx-auto mb-3 text-gray-400" />
          <p className="text-lg font-medium text-gray-900">No target workers available</p>
          <p className="text-sm text-gray-500 mt-1">
            You need at least one other online worker to migrate resources to.
          </p>
        </div>
        <div className="flex justify-start pt-4 border-t">
          <Button variant="outline" onClick={prevStep}>
            Back
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Source worker reminder */}
      <div className="p-3 bg-gray-50 border border-gray-200 rounded-lg">
        <span className="text-sm text-gray-600">
          Migrating from: <strong className="text-gray-900">{state.sourceWorker?.name}</strong>
        </span>
      </div>

      <div className="grid gap-3">
        {availableWorkers.map((worker) => {
          const isSelected = state.targetWorker?.id === worker.id;
          const StatusIcon = worker.online ? Wifi : WifiOff;

          return (
            <button
              key={worker.id}
              onClick={() => handleSelect(worker)}
              className={cn(
                'flex items-center gap-4 p-4 rounded-lg border text-left transition-all',
                isSelected
                  ? 'border-blue-500 bg-blue-50 ring-2 ring-blue-500/20'
                  : 'border-gray-200 bg-white hover:bg-gray-50 hover:border-gray-300'
              )}
            >
              {/* Status indicator */}
              <div
                className={cn(
                  'flex items-center justify-center w-12 h-12 rounded-full flex-shrink-0',
                  worker.online ? 'bg-green-100' : 'bg-gray-100'
                )}
              >
                <StatusIcon
                  className={cn(
                    'h-6 w-6',
                    worker.online ? 'text-green-600' : 'text-gray-400'
                  )}
                />
              </div>

              {/* Worker info */}
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 mb-1">
                  <h4 className="text-sm font-semibold text-gray-900">{worker.name}</h4>
                  <Badge
                    className={cn(
                      'text-xs',
                      worker.online
                        ? 'bg-green-100 text-green-800'
                        : 'bg-gray-100 text-gray-800'
                    )}
                    variant="outline"
                  >
                    {worker.online ? 'online' : 'offline'}
                  </Badge>
                  {isSelected && (
                    <Badge className="bg-blue-100 text-blue-800" variant="outline">
                      Selected
                    </Badge>
                  )}
                </div>
                <p className="text-xs text-gray-500">
                  {worker.hostname} ({worker.grpc_address})
                </p>
                <div className="flex items-center gap-4 mt-2 text-xs text-gray-500">
                  <span className="flex items-center gap-1">
                    <Box className="h-3.5 w-3.5" /> {worker.container_count} containers
                  </span>
                  <span className="flex items-center gap-1">
                    <Database className="h-3.5 w-3.5" /> {worker.image_count} images
                  </span>
                  <span className="flex items-center gap-1">
                    <HardDrive className="h-3.5 w-3.5" /> {worker.volume_count} volumes
                  </span>
                  <span className="flex items-center gap-1">
                    <Network className="h-3.5 w-3.5" /> {worker.network_count} networks
                  </span>
                </div>
                <p className="text-xs text-gray-400 mt-1">
                  Last seen: {formatRelativeTime(worker.last_heartbeat)}
                </p>
              </div>

              {/* Selection indicator */}
              <div
                className={cn(
                  'w-5 h-5 rounded-full border-2 flex items-center justify-center flex-shrink-0',
                  isSelected ? 'border-blue-500 bg-blue-500' : 'border-gray-300'
                )}
              >
                {isSelected && <div className="w-2 h-2 rounded-full bg-white" />}
              </div>
            </button>
          );
        })}
      </div>

      {/* Actions */}
      <div className="flex items-center justify-between pt-4 border-t">
        <Button variant="outline" onClick={prevStep}>
          Back
        </Button>
        <Button
          onClick={handleContinue}
          disabled={!state.targetWorker}
          className="bg-blue-600 hover:bg-blue-700"
        >
          Continue
        </Button>
      </div>
    </div>
  );
}

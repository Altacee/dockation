import { Server, Wifi, WifiOff, Trash2, Database, HardDrive, Network, Box } from 'lucide-react';
import type { Worker } from '../../types';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';
import { Badge } from '../ui/Badge';
import { Button } from '../ui/Button';
import { formatRelativeTime, cn } from '../../lib/utils';

interface WorkerListProps {
  workers: Worker[];
  onRemove?: (worker: Worker) => void;
  onViewResources?: (worker: Worker) => void;
  className?: string;
}

export function WorkerList({ workers, onRemove, onViewResources, className }: WorkerListProps) {
  if (workers.length === 0) {
    return (
      <Card className={className}>
        <CardHeader>
          <CardTitle className="text-lg">Connected Workers</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-center py-8 text-gray-500">
            <Server className="h-12 w-12 mx-auto mb-3 text-gray-400" aria-hidden="true" />
            <p className="text-sm font-medium">No workers connected</p>
            <p className="text-xs mt-1">Install workers on remote servers to get started</p>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle className="text-lg">
          Connected Workers
          <span className="ml-2 text-sm font-normal text-gray-500">({workers.length})</span>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-3" role="list" aria-label="Connected workers">
          {workers.map((worker) => (
            <WorkerItem
              key={worker.id}
              worker={worker}
              onRemove={onRemove}
              onViewResources={onViewResources}
            />
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

interface WorkerItemProps {
  worker: Worker;
  onRemove?: (worker: Worker) => void;
  onViewResources?: (worker: Worker) => void;
}

function WorkerItem({ worker, onRemove, onViewResources }: WorkerItemProps) {
  const isOnline = worker.online;
  const StatusIcon = isOnline ? Wifi : WifiOff;

  return (
    <div
      className="flex items-center gap-4 p-4 rounded-lg border bg-white hover:bg-gray-50 transition-colors"
      role="listitem"
    >
      {/* Status indicator */}
      <div
        className={cn(
          'flex items-center justify-center w-10 h-10 rounded-full',
          isOnline ? 'bg-green-100' : 'bg-gray-100'
        )}
        aria-hidden="true"
      >
        <StatusIcon
          className={cn('h-5 w-5', isOnline ? 'text-green-600' : 'text-gray-400')}
        />
      </div>

      {/* Worker info */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-1">
          <h4 className="text-sm font-semibold text-gray-900 truncate">{worker.name}</h4>
          <Badge
            className={isOnline ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-800'}
            variant="outline"
          >
            {isOnline ? 'online' : 'offline'}
          </Badge>
        </div>
        <div className="text-xs text-gray-500 space-y-0.5">
          <p>{worker.hostname} ({worker.grpc_address})</p>
          <p>Version: {worker.version}</p>
          <div className="flex items-center gap-3 mt-1">
            <span className="flex items-center gap-1">
              <Box className="h-3 w-3" /> {worker.container_count}
            </span>
            <span className="flex items-center gap-1">
              <Database className="h-3 w-3" /> {worker.image_count}
            </span>
            <span className="flex items-center gap-1">
              <HardDrive className="h-3 w-3" /> {worker.volume_count}
            </span>
            <span className="flex items-center gap-1">
              <Network className="h-3 w-3" /> {worker.network_count}
            </span>
          </div>
          <p className="mt-1">
            Last heartbeat: {formatRelativeTime(worker.last_heartbeat)}
          </p>
        </div>
      </div>

      {/* Actions */}
      <div className="flex items-center gap-2">
        {onViewResources && (
          <Button
            size="sm"
            variant="outline"
            onClick={() => onViewResources(worker)}
            aria-label={`View resources on ${worker.name}`}
          >
            Resources
          </Button>
        )}
        {onRemove && (
          <Button
            size="sm"
            variant="ghost"
            onClick={() => onRemove(worker)}
            aria-label={`Remove ${worker.name}`}
            className="text-red-600 hover:text-red-700 hover:bg-red-50"
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        )}
      </div>
    </div>
  );
}

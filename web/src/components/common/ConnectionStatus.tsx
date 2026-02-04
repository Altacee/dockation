import type { ConnectionStatus as ConnectionStatusType } from '../../types';
import { cn } from '../../lib/utils';
import { Wifi, WifiOff, Loader2 } from 'lucide-react';

interface ConnectionStatusProps {
  status: ConnectionStatusType;
  className?: string;
  showLabel?: boolean;
}

export function ConnectionStatus({ status, className, showLabel = true }: ConnectionStatusProps) {
  const statusConfig = {
    connecting: {
      icon: Loader2,
      label: 'Connecting',
      color: 'text-blue-600',
      bgColor: 'bg-blue-50',
      animate: 'animate-spin',
    },
    connected: {
      icon: Wifi,
      label: 'Connected',
      color: 'text-green-600',
      bgColor: 'bg-green-50',
      animate: '',
    },
    disconnected: {
      icon: WifiOff,
      label: 'Disconnected',
      color: 'text-gray-600',
      bgColor: 'bg-gray-50',
      animate: '',
    },
    error: {
      icon: WifiOff,
      label: 'Connection Error',
      color: 'text-red-600',
      bgColor: 'bg-red-50',
      animate: '',
    },
  };

  const config = statusConfig[status];
  const Icon = config.icon;

  return (
    <div
      className={cn(
        'flex items-center gap-2 px-3 py-1.5 rounded-full border text-sm font-medium',
        config.color,
        config.bgColor,
        className
      )}
      role="status"
      aria-live="polite"
      aria-label={`Connection status: ${config.label}`}
    >
      <Icon className={cn('h-4 w-4', config.animate)} aria-hidden="true" />
      {showLabel && <span>{config.label}</span>}
    </div>
  );
}

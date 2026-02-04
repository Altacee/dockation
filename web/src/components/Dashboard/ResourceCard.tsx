import { Container, Image, HardDrive, Network, Layers, TrendingUp } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';
import { formatBytes } from '../../lib/utils';
import { cn } from '../../lib/utils';

interface ResourceCardProps {
  type: 'containers' | 'images' | 'volumes' | 'networks' | 'compose';
  count: number;
  subtext?: string;
  size?: number;
  trend?: 'up' | 'down' | 'stable';
  onClick?: () => void;
}

export function ResourceCard({ type, count, subtext, size, trend, onClick }: ResourceCardProps) {
  const config = {
    containers: {
      icon: Container,
      label: 'Containers',
      color: 'text-blue-600',
      bgColor: 'bg-blue-50',
      borderColor: 'border-blue-200',
    },
    images: {
      icon: Image,
      label: 'Images',
      color: 'text-purple-600',
      bgColor: 'bg-purple-50',
      borderColor: 'border-purple-200',
    },
    volumes: {
      icon: HardDrive,
      label: 'Volumes',
      color: 'text-green-600',
      bgColor: 'bg-green-50',
      borderColor: 'border-green-200',
    },
    networks: {
      icon: Network,
      label: 'Networks',
      color: 'text-orange-600',
      bgColor: 'bg-orange-50',
      borderColor: 'border-orange-200',
    },
    compose: {
      icon: Layers,
      label: 'Compose Stacks',
      color: 'text-indigo-600',
      bgColor: 'bg-indigo-50',
      borderColor: 'border-indigo-200',
    },
  };

  const { icon: Icon, label, color, bgColor, borderColor } = config[type];

  return (
    <Card
      className={cn(
        'cursor-pointer transition-all hover:shadow-md hover:-translate-y-0.5',
        borderColor
      )}
      onClick={onClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') {
          e.preventDefault();
          onClick?.();
        }
      }}
      aria-label={`View ${label}: ${count} items${size ? `, ${formatBytes(size)}` : ''}`}
    >
      <CardHeader className="flex flex-row items-center justify-between pb-2 space-y-0">
        <CardTitle className="text-sm font-medium text-gray-600">{label}</CardTitle>
        <div className={cn('p-2 rounded-lg', bgColor)}>
          <Icon className={cn('h-4 w-4', color)} aria-hidden="true" />
        </div>
      </CardHeader>
      <CardContent>
        <div className="flex items-baseline gap-2">
          <div className="text-3xl font-bold">{count}</div>
          {trend && (
            <TrendingUp
              className={cn(
                'h-4 w-4',
                trend === 'up' && 'text-green-600',
                trend === 'down' && 'text-red-600 rotate-180',
                trend === 'stable' && 'text-gray-400'
              )}
              aria-hidden="true"
            />
          )}
        </div>
        {(subtext || size) && (
          <p className="text-xs text-gray-500 mt-1">
            {subtext}
            {size && ` • ${formatBytes(size)}`}
          </p>
        )}
      </CardContent>
    </Card>
  );
}

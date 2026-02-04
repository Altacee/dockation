import { Server, Wifi, WifiOff, MoreVertical, ArrowRight } from 'lucide-react';
import type { Peer } from '../../types';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';
import { Badge } from '../ui/Badge';
import { Button } from '../ui/Button';
import { formatRelativeTime, formatBytes, cn, getStatusColor } from '../../lib/utils';

interface PeerListProps {
  peers: Peer[];
  onMigrate?: (peer: Peer) => void;
  onDisconnect?: (peer: Peer) => void;
  className?: string;
}

export function PeerList({ peers, onMigrate, onDisconnect, className }: PeerListProps) {
  if (peers.length === 0) {
    return (
      <Card className={className}>
        <CardHeader>
          <CardTitle className="text-lg">Connected Peers</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-center py-8 text-gray-500">
            <Server className="h-12 w-12 mx-auto mb-3 text-gray-400" aria-hidden="true" />
            <p className="text-sm font-medium">No peers connected</p>
            <p className="text-xs mt-1">Pair with another device to get started</p>
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle className="text-lg">
          Connected Peers
          <span className="ml-2 text-sm font-normal text-gray-500">({peers.length})</span>
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-3" role="list" aria-label="Connected peers">
          {peers.map((peer) => (
            <PeerItem
              key={peer.id}
              peer={peer}
              onMigrate={onMigrate}
              onDisconnect={onDisconnect}
            />
          ))}
        </div>
      </CardContent>
    </Card>
  );
}

interface PeerItemProps {
  peer: Peer;
  onMigrate?: (peer: Peer) => void;
  onDisconnect?: (peer: Peer) => void;
}

function PeerItem({ peer, onMigrate, onDisconnect }: PeerItemProps) {
  const isOnline = peer.status === 'online';
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

      {/* Peer info */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2 mb-1">
          <h4 className="text-sm font-semibold text-gray-900 truncate">{peer.name}</h4>
          <Badge className={getStatusColor(peer.status)} variant="outline">
            {peer.status}
          </Badge>
        </div>
        <div className="text-xs text-gray-500 space-y-0.5">
          <p>{peer.hostname}</p>
          <p>
            {peer.architecture} • {peer.os} • Docker {peer.dockerVersion}
          </p>
          <p className="flex items-center gap-1">
            <span>Last seen: {formatRelativeTime(peer.lastSeen)}</span>
            <span>•</span>
            <span>{formatBytes(peer.availableSpace)} available</span>
          </p>
        </div>
      </div>

      {/* Actions */}
      {isOnline && (
        <div className="flex items-center gap-2">
          {onMigrate && (
            <Button
              size="sm"
              onClick={() => onMigrate(peer)}
              className="bg-blue-600 hover:bg-blue-700"
              aria-label={`Migrate to ${peer.name}`}
            >
              <ArrowRight className="h-4 w-4 mr-1" aria-hidden="true" />
              Migrate
            </Button>
          )}
          {onDisconnect && (
            <Button
              size="sm"
              variant="ghost"
              onClick={() => onDisconnect(peer)}
              aria-label={`Disconnect from ${peer.name}`}
            >
              <MoreVertical className="h-4 w-4" />
            </Button>
          )}
        </div>
      )}
    </div>
  );
}

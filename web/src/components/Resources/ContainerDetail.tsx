import { useState, useEffect, useRef, useCallback } from 'react';
import {
  ArrowLeft,
  Play,
  Square,
  RotateCcw,
  Trash2,
  Terminal,
  Loader2,
  AlertCircle,
  Wifi,
  WifiOff,
} from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';
import { Button } from '../ui/Button';
import api from '../../api/client';
import { cn } from '../../lib/utils';

interface ContainerDetailProps {
  containerId: string;
  onBack: () => void;
  onRefresh?: () => void;
}

export function ContainerDetail({ containerId, onBack, onRefresh }: ContainerDetailProps) {
  const [container, setContainer] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [logs, setLogs] = useState<string>('');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [wsConnected, setWsConnected] = useState(false);
  const [streamLogs, setStreamLogs] = useState(true);
  const logsRef = useRef<HTMLPreElement>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<number | null>(null);

  // Load container info
  useEffect(() => {
    loadContainer();
  }, [containerId]);

  // WebSocket log streaming
  const connectWebSocket = useCallback(() => {
    if (wsRef.current?.readyState === WebSocket.OPEN) {
      return;
    }

    const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsHost = import.meta.env.VITE_WS_HOST || window.location.host;
    const wsUrl = `${wsProtocol}//${wsHost}/ws/containers/${containerId}/logs`;

    const ws = new WebSocket(wsUrl);
    wsRef.current = ws;

    ws.onopen = () => {
      setWsConnected(true);
      setError(null);
    };

    ws.onmessage = (event) => {
      try {
        const message = JSON.parse(event.data);
        if (message.type === 'log' && message.data) {
          setLogs((prev) => {
            const newLogs = prev + message.data;
            // Keep only last 50KB of logs to prevent memory issues
            if (newLogs.length > 50000) {
              return newLogs.slice(-40000);
            }
            return newLogs;
          });
          // Auto-scroll to bottom
          if (logsRef.current) {
            logsRef.current.scrollTop = logsRef.current.scrollHeight;
          }
        } else if (message.type === 'error') {
          setError(message.error);
        }
      } catch {
        // Plain text log
        setLogs((prev) => prev + event.data);
      }
    };

    ws.onclose = () => {
      setWsConnected(false);
      wsRef.current = null;
      // Reconnect after 3 seconds if streaming is enabled
      if (streamLogs) {
        reconnectTimeoutRef.current = window.setTimeout(() => {
          connectWebSocket();
        }, 3000);
      }
    };

    ws.onerror = () => {
      setWsConnected(false);
    };
  }, [containerId, streamLogs]);

  useEffect(() => {
    if (streamLogs) {
      connectWebSocket();
    }

    return () => {
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
    };
  }, [streamLogs, connectWebSocket]);

  // Toggle streaming
  const handleToggleStream = () => {
    if (streamLogs) {
      // Stop streaming
      if (wsRef.current) {
        wsRef.current.close();
        wsRef.current = null;
      }
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
      setStreamLogs(false);
      setWsConnected(false);
    } else {
      // Start streaming
      setStreamLogs(true);
    }
  };

  async function loadContainer() {
    setLoading(true);
    setError(null);
    try {
      const response = await api.containers.get(containerId);
      if (response.success && response.data) {
        setContainer(response.data);
      } else {
        setError(response.error || 'Failed to load container');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'An error occurred');
    } finally {
      setLoading(false);
    }
  }

  async function handleAction(action: 'start' | 'stop' | 'restart' | 'remove') {
    setActionLoading(action);
    try {
      let response;
      switch (action) {
        case 'start':
          response = await api.containers.start(containerId);
          break;
        case 'stop':
          response = await api.containers.stop(containerId);
          break;
        case 'restart':
          response = await api.containers.restart(containerId);
          break;
        case 'remove':
          if (!confirm('Are you sure you want to remove this container?')) {
            setActionLoading(null);
            return;
          }
          response = await api.containers.remove(containerId, true);
          if (response.success) {
            onRefresh?.();
            onBack();
            return;
          }
          break;
      }
      if (response && !response.success) {
        setError(response.error || `Failed to ${action} container`);
      } else {
        // Reload container state after action
        await loadContainer();
        onRefresh?.();
        // Clear logs and reconnect for new container state
        setLogs('');
        if (wsRef.current) {
          wsRef.current.close();
        }
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${action} container`);
    } finally {
      setActionLoading(null);
    }
  }

  function getStatusBadgeClass(status: string) {
    switch (status?.toLowerCase()) {
      case 'running':
        return 'bg-green-100 text-green-800';
      case 'exited':
      case 'stopped':
        return 'bg-gray-100 text-gray-800';
      case 'paused':
        return 'bg-yellow-100 text-yellow-800';
      case 'restarting':
        return 'bg-blue-100 text-blue-800';
      default:
        return 'bg-gray-100 text-gray-800';
    }
  }

  function clearLogs() {
    setLogs('');
  }

  if (loading) {
    return (
      <Card>
        <CardContent className="flex items-center justify-center py-12">
          <Loader2 className="h-8 w-8 animate-spin text-gray-400" />
        </CardContent>
      </Card>
    );
  }

  if (error && !container) {
    return (
      <Card className="border-red-200 bg-red-50">
        <CardContent className="py-6">
          <div className="flex items-center gap-2 text-red-800">
            <AlertCircle className="h-5 w-5" />
            <p className="text-sm">{error}</p>
          </div>
          <Button variant="outline" size="sm" onClick={onBack} className="mt-4">
            Go Back
          </Button>
        </CardContent>
      </Card>
    );
  }

  const isRunning = container?.state?.Status === 'running' || container?.State?.Status === 'running';
  const state = container?.state || container?.State || {};
  const config = container?.config || container?.Config || {};

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-4">
          <Button variant="outline" size="sm" onClick={onBack} className="gap-2">
            <ArrowLeft className="h-4 w-4" />
            Back
          </Button>
          <div>
            <h2 className="text-2xl font-bold text-gray-900">
              {container?.name?.replace(/^\//, '') || container?.Name?.replace(/^\//, '') || containerId.substring(0, 12)}
            </h2>
            <p className="text-sm text-gray-600 font-mono">{containerId.substring(0, 12)}</p>
          </div>
        </div>

        {/* Action buttons */}
        <div className="flex items-center gap-2">
          {!isRunning && (
            <Button
              variant="default"
              size="sm"
              onClick={() => handleAction('start')}
              disabled={actionLoading !== null}
              className="gap-2 bg-green-600 hover:bg-green-700"
            >
              {actionLoading === 'start' ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <Play className="h-4 w-4" />
              )}
              Start
            </Button>
          )}
          {isRunning && (
            <>
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleAction('stop')}
                disabled={actionLoading !== null}
                className="gap-2"
              >
                {actionLoading === 'stop' ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <Square className="h-4 w-4" />
                )}
                Stop
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => handleAction('restart')}
                disabled={actionLoading !== null}
                className="gap-2"
              >
                {actionLoading === 'restart' ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <RotateCcw className="h-4 w-4" />
                )}
                Restart
              </Button>
            </>
          )}
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleAction('remove')}
            disabled={actionLoading !== null || isRunning}
            className="gap-2 text-red-600 hover:text-red-700 hover:bg-red-50"
          >
            {actionLoading === 'remove' ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Trash2 className="h-4 w-4" />
            )}
            Remove
          </Button>
        </div>
      </div>

      {error && (
        <div className="bg-red-50 border border-red-200 rounded-lg p-4">
          <p className="text-sm text-red-800">{error}</p>
        </div>
      )}

      {/* Container Info */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Container Info</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex justify-between">
              <span className="text-sm text-gray-600">Status</span>
              <span
                className={cn(
                  'px-2.5 py-0.5 rounded-full text-xs font-medium',
                  getStatusBadgeClass(state.Status || '')
                )}
              >
                {state.Status || 'Unknown'}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-sm text-gray-600">Image</span>
              <span className="text-sm font-mono text-gray-900 truncate max-w-[200px]">
                {container?.image || config.Image || '-'}
              </span>
            </div>
            <div className="flex justify-between">
              <span className="text-sm text-gray-600">Created</span>
              <span className="text-sm text-gray-900">
                {container?.created
                  ? new Date(container.created).toLocaleString()
                  : container?.Created
                  ? new Date(container.Created).toLocaleString()
                  : '-'}
              </span>
            </div>
            {state.StartedAt && (
              <div className="flex justify-between">
                <span className="text-sm text-gray-600">Started</span>
                <span className="text-sm text-gray-900">
                  {new Date(state.StartedAt).toLocaleString()}
                </span>
              </div>
            )}
            {state.FinishedAt && state.FinishedAt !== '0001-01-01T00:00:00Z' && (
              <div className="flex justify-between">
                <span className="text-sm text-gray-600">Finished</span>
                <span className="text-sm text-gray-900">
                  {new Date(state.FinishedAt).toLocaleString()}
                </span>
              </div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Configuration</CardTitle>
          </CardHeader>
          <CardContent className="space-y-4">
            {config.Cmd && (
              <div>
                <span className="text-sm text-gray-600 block mb-1">Command</span>
                <code className="text-xs bg-gray-100 px-2 py-1 rounded block overflow-x-auto">
                  {Array.isArray(config.Cmd) ? config.Cmd.join(' ') : config.Cmd}
                </code>
              </div>
            )}
            {config.WorkingDir && (
              <div className="flex justify-between">
                <span className="text-sm text-gray-600">Working Dir</span>
                <span className="text-sm font-mono text-gray-900">{config.WorkingDir}</span>
              </div>
            )}
            {config.Entrypoint && (
              <div>
                <span className="text-sm text-gray-600 block mb-1">Entrypoint</span>
                <code className="text-xs bg-gray-100 px-2 py-1 rounded block overflow-x-auto">
                  {Array.isArray(config.Entrypoint)
                    ? config.Entrypoint.join(' ')
                    : config.Entrypoint}
                </code>
              </div>
            )}
            {container?.mounts && container.mounts.length > 0 && (
              <div>
                <span className="text-sm text-gray-600 block mb-1">Mounts</span>
                <div className="space-y-1">
                  {container.mounts.slice(0, 3).map((mount: any, i: number) => (
                    <code
                      key={i}
                      className="text-xs bg-gray-100 px-2 py-1 rounded block overflow-x-auto"
                    >
                      {mount.Source || mount.source} : {mount.Target || mount.target}
                    </code>
                  ))}
                  {container.mounts.length > 3 && (
                    <span className="text-xs text-gray-500">
                      +{container.mounts.length - 3} more
                    </span>
                  )}
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      </div>

      {/* Real-time Logs */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-lg flex items-center gap-2">
            <Terminal className="h-5 w-5" />
            Container Logs
            {wsConnected ? (
              <span className="flex items-center gap-1 text-xs font-normal text-green-600">
                <Wifi className="h-3 w-3" />
                Live
              </span>
            ) : streamLogs ? (
              <span className="flex items-center gap-1 text-xs font-normal text-yellow-600">
                <Loader2 className="h-3 w-3 animate-spin" />
                Connecting...
              </span>
            ) : (
              <span className="flex items-center gap-1 text-xs font-normal text-gray-500">
                <WifiOff className="h-3 w-3" />
                Paused
              </span>
            )}
          </CardTitle>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={clearLogs}
              className="text-xs"
            >
              Clear
            </Button>
            <Button
              variant={streamLogs ? 'default' : 'outline'}
              size="sm"
              onClick={handleToggleStream}
              className="gap-2"
            >
              {streamLogs ? (
                <>
                  <Square className="h-3 w-3" />
                  Stop
                </>
              ) : (
                <>
                  <Play className="h-3 w-3" />
                  Stream
                </>
              )}
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <pre
            ref={logsRef}
            className="bg-gray-900 text-gray-100 p-4 rounded-lg text-xs font-mono overflow-auto max-h-96 whitespace-pre-wrap"
          >
            {logs || (wsConnected ? 'Waiting for logs...' : 'No logs available. Click "Stream" to start.')}
          </pre>
        </CardContent>
      </Card>
    </div>
  );
}

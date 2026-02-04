import { CheckCircle2, AlertTriangle, XCircle, Loader2, Clock } from 'lucide-react';
import type { PreflightCheck, PreflightCheckStatus } from '../../types';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';
import { Button } from '../ui/Button';
import { cn, formatBytes, formatDuration } from '../../lib/utils';

interface PreFlightChecksProps {
  checks: PreflightCheck[];
  estimatedDuration?: number;
  onContinue?: () => void;
  onCancel?: () => void;
  isRunning?: boolean;
}

export function PreFlightChecks({
  checks,
  estimatedDuration,
  onContinue,
  onCancel,
  isRunning = false,
}: PreFlightChecksProps) {
  const hasBlockers = checks.some((c) => c.status === 'failed' && c.isBlocker);
  const hasWarnings = checks.some((c) => c.status === 'warning');
  const allPassed = checks.every((c) => c.status === 'passed' || c.status === 'warning');
  const canContinue = allPassed && !isRunning;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-xl">Pre-Flight Checks</CardTitle>
        <p className="text-sm text-gray-600 mt-1">
          Validating system readiness before migration
        </p>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Checks list */}
        <div
          className="space-y-2"
          role="list"
          aria-label="Pre-flight checks"
          aria-live="polite"
          aria-atomic="false"
        >
          {checks.map((check) => (
            <CheckItem key={check.id} check={check} />
          ))}
        </div>

        {/* Summary */}
        {allPassed && estimatedDuration && (
          <div className="flex items-center gap-2 p-4 bg-blue-50 border border-blue-200 rounded-lg">
            <Clock className="h-5 w-5 text-blue-600 flex-shrink-0" aria-hidden="true" />
            <div className="text-sm">
              <span className="font-medium text-blue-900">Estimated duration: </span>
              <span className="text-blue-700">{formatDuration(estimatedDuration)}</span>
            </div>
          </div>
        )}

        {hasBlockers && (
          <div className="p-4 bg-red-50 border border-red-200 rounded-lg">
            <p className="text-sm font-medium text-red-900">
              Cannot proceed: Critical issues must be resolved first
            </p>
          </div>
        )}

        {hasWarnings && !hasBlockers && (
          <div className="p-4 bg-yellow-50 border border-yellow-200 rounded-lg">
            <p className="text-sm font-medium text-yellow-900">
              Warning: Some issues detected but migration can proceed
            </p>
          </div>
        )}

        {/* Actions */}
        <div className="flex items-center justify-end gap-3 pt-4 border-t">
          {onCancel && (
            <Button variant="outline" onClick={onCancel} disabled={isRunning}>
              Cancel
            </Button>
          )}
          {onContinue && (
            <Button
              onClick={onContinue}
              disabled={!canContinue || hasBlockers}
              className="bg-blue-600 hover:bg-blue-700"
            >
              {hasWarnings ? 'Continue Anyway' : 'Continue'}
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

interface CheckItemProps {
  check: PreflightCheck;
}

function CheckItem({ check }: CheckItemProps) {
  const statusConfig: Record<
    PreflightCheckStatus,
    { icon: any; color: string; bgColor: string; borderColor: string }
  > = {
    pending: {
      icon: Clock,
      color: 'text-gray-600',
      bgColor: 'bg-gray-50',
      borderColor: 'border-gray-200',
    },
    running: {
      icon: Loader2,
      color: 'text-blue-600',
      bgColor: 'bg-blue-50',
      borderColor: 'border-blue-200',
    },
    passed: {
      icon: CheckCircle2,
      color: 'text-green-600',
      bgColor: 'bg-green-50',
      borderColor: 'border-green-200',
    },
    warning: {
      icon: AlertTriangle,
      color: 'text-yellow-600',
      bgColor: 'bg-yellow-50',
      borderColor: 'border-yellow-200',
    },
    failed: {
      icon: XCircle,
      color: 'text-red-600',
      bgColor: 'bg-red-50',
      borderColor: 'border-red-200',
    },
  };

  const config = statusConfig[check.status];
  const Icon = config.icon;

  return (
    <div
      className={cn(
        'flex items-start gap-3 p-4 rounded-lg border transition-colors',
        config.bgColor,
        config.borderColor
      )}
      role="listitem"
      aria-label={`${check.name}: ${check.status}`}
    >
      <Icon
        className={cn(
          'h-5 w-5 flex-shrink-0 mt-0.5',
          config.color,
          check.status === 'running' && 'animate-spin'
        )}
        aria-hidden="true"
      />
      <div className="flex-1 min-w-0">
        <div className="flex items-start justify-between gap-2">
          <p className="text-sm font-medium text-gray-900">{check.name}</p>
          {check.isBlocker && check.status === 'failed' && (
            <span className="text-xs font-semibold text-red-600 bg-red-100 px-2 py-0.5 rounded">
              BLOCKER
            </span>
          )}
        </div>
        {check.message && (
          <p className="text-xs text-gray-600 mt-1">{check.message}</p>
        )}
        {check.details && (
          <div className="text-xs text-gray-500 mt-2 space-y-1">
            {Object.entries(check.details).map(([key, value]) => (
              <div key={key}>
                <span className="font-medium">{key}: </span>
                <span>
                  {typeof value === 'number' && key.includes('size')
                    ? formatBytes(value)
                    : String(value)}
                </span>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

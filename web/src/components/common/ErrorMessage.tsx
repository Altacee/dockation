import { AlertCircle, AlertTriangle, XCircle } from 'lucide-react';
import type { MigrationError } from '../../types';
import { cn } from '../../lib/utils';
import { Button } from '../ui/Button';

interface ErrorMessageProps {
  error: MigrationError;
  onRetry?: () => void;
  onSkip?: () => void;
  onCancel?: () => void;
  className?: string;
}

export function ErrorMessage({ error, onRetry, onSkip, onCancel, className }: ErrorMessageProps) {
  const Icon = error.severity === 'error' ? XCircle : AlertTriangle;
  const isError = error.severity === 'error';

  return (
    <div
      className={cn(
        'rounded-lg border p-4',
        isError ? 'bg-red-50 border-red-200' : 'bg-yellow-50 border-yellow-200',
        className
      )}
      role="alert"
      aria-live="assertive"
    >
      <div className="flex items-start gap-3">
        <Icon
          className={cn(
            'h-5 w-5 flex-shrink-0 mt-0.5',
            isError ? 'text-red-600' : 'text-yellow-600'
          )}
          aria-hidden="true"
        />
        <div className="flex-1 min-w-0">
          <h3
            className={cn(
              'text-sm font-semibold mb-1',
              isError ? 'text-red-900' : 'text-yellow-900'
            )}
          >
            {isError ? 'Migration Error' : 'Warning'}
          </h3>
          <p className={cn('text-sm mb-2', isError ? 'text-red-800' : 'text-yellow-800')}>
            {error.message}
          </p>
          {error.context && (
            <p className={cn('text-xs mb-3', isError ? 'text-red-700' : 'text-yellow-700')}>
              Context: {error.context}
              {error.resourceName && ` (${error.resourceName})`}
            </p>
          )}

          {/* Action buttons */}
          {(error.canRetry || error.canSkip || onCancel) && (
            <div className="flex items-center gap-2 mt-3">
              {error.canRetry && onRetry && (
                <Button
                  size="sm"
                  onClick={onRetry}
                  className="bg-blue-600 hover:bg-blue-700 text-white"
                >
                  Retry
                </Button>
              )}
              {error.canSkip && onSkip && (
                <Button size="sm" variant="outline" onClick={onSkip}>
                  Skip This Item
                </Button>
              )}
              {onCancel && (
                <Button size="sm" variant="destructive" onClick={onCancel}>
                  Cancel Migration
                </Button>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

interface ErrorListProps {
  errors: MigrationError[];
  onRetry?: (errorId: string) => void;
  onSkip?: (errorId: string) => void;
  onCancel?: () => void;
  className?: string;
}

export function ErrorList({ errors, onRetry, onSkip, onCancel, className }: ErrorListProps) {
  if (errors.length === 0) return null;

  return (
    <div className={cn('space-y-3', className)} role="group" aria-label="Migration errors">
      <div className="flex items-center gap-2 text-sm font-semibold text-red-900">
        <AlertCircle className="h-4 w-4" aria-hidden="true" />
        <span>
          {errors.length} {errors.length === 1 ? 'Issue' : 'Issues'} Detected
        </span>
      </div>
      {errors.map((error) => (
        <ErrorMessage
          key={error.id}
          error={error}
          onRetry={onRetry ? () => onRetry(error.id) : undefined}
          onSkip={onSkip ? () => onSkip(error.id) : undefined}
          onCancel={onCancel}
        />
      ))}
    </div>
  );
}

import { useEffect, useState } from 'react';
import { Pause, X, Loader2 } from 'lucide-react';
import type { MigrationProgress as MigrationProgressType, MigrationError } from '../../types';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';
import { Button } from '../ui/Button';
import { ProgressBar, StepProgress } from '../common/ProgressBar';
import { ErrorList } from '../common/ErrorMessage';
import { ConnectionStatus } from '../common/ConnectionStatus';
import {
  formatBytes,
  formatSpeed,
  formatDuration,
  calculateETA,
} from '../../lib/utils';

interface MigrationProgressProps {
  progress: MigrationProgressType;
  errors: MigrationError[];
  canPause?: boolean;
  canCancel?: boolean;
  connectionStatus: 'connected' | 'connecting' | 'disconnected' | 'error';
  onPause?: () => void;
  onCancel?: () => void;
  onRetryError?: (errorId: string) => void;
  onSkipError?: (errorId: string) => void;
  className?: string;
}

export function MigrationProgress({
  progress,
  errors,
  canPause = true,
  canCancel = true,
  connectionStatus,
  onPause,
  onCancel,
  onRetryError,
  onSkipError,
  className,
}: MigrationProgressProps) {
  const [elapsedTime, setElapsedTime] = useState(0);

  useEffect(() => {
    const startTime = new Date(progress.startTime).getTime();
    const interval = setInterval(() => {
      setElapsedTime(Math.floor((Date.now() - startTime) / 1000));
    }, 1000);

    return () => clearInterval(interval);
  }, [progress.startTime]);

  const eta = calculateETA(
    progress.bytesTransferred,
    progress.totalBytes,
    progress.transferSpeed
  );

  // Build steps array
  const steps = [
    { name: 'Preparing migration', status: 'completed' as const },
    { name: 'Transferring images', status: 'completed' as const },
    { name: progress.currentStepName, status: 'in_progress' as const },
    { name: 'Verifying data', status: 'pending' as const },
    { name: 'Starting containers', status: 'pending' as const },
  ].slice(0, progress.totalSteps);

  // Update step statuses based on current step
  steps.forEach((step, index) => {
    if (index < progress.currentStep - 1) {
      step.status = 'completed';
    } else if (index === progress.currentStep - 1) {
      step.status = 'in_progress';
    } else {
      step.status = 'pending';
    }
  });

  return (
    <Card className={className}>
      <CardHeader>
        <div className="flex items-start justify-between">
          <div className="flex-1">
            <CardTitle className="text-xl flex items-center gap-2">
              <Loader2 className="h-5 w-5 animate-spin text-blue-600" aria-hidden="true" />
              Migrating your data...
            </CardTitle>
            <p className="text-sm text-gray-600 mt-1">
              Step {progress.currentStep} of {progress.totalSteps}: {progress.currentStepName}
            </p>
          </div>
          <ConnectionStatus status={connectionStatus} />
        </div>
      </CardHeader>

      <CardContent className="space-y-6">
        {/* Overall progress */}
        <div className="space-y-2">
          <ProgressBar
            value={progress.bytesTransferred}
            max={progress.totalBytes}
            label="Overall Progress"
            showPercentage={true}
            animate={true}
            size="lg"
          />
          <div className="flex items-center justify-between text-xs text-gray-600">
            <span>
              {formatBytes(progress.bytesTransferred)} of {formatBytes(progress.totalBytes)}
            </span>
            <span>{formatSpeed(progress.transferSpeed)}</span>
          </div>
        </div>

        {/* Current item progress */}
        <div
          className="p-4 bg-blue-50 border border-blue-200 rounded-lg"
          role="status"
          aria-live="polite"
        >
          <div className="space-y-2">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium text-blue-900">Currently processing:</span>
              <span className="text-sm text-blue-700">
                Item {progress.currentItemIndex} of {progress.totalItems}
              </span>
            </div>
            <p className="text-sm text-blue-800 font-medium truncate">
              {progress.currentItem}
            </p>
          </div>
        </div>

        {/* Time estimates */}
        <div className="grid grid-cols-2 gap-4">
          <div className="p-3 bg-gray-50 rounded-lg">
            <p className="text-xs text-gray-600 mb-1">Elapsed Time</p>
            <p className="text-lg font-semibold text-gray-900">
              {formatDuration(elapsedTime)}
            </p>
          </div>
          <div className="p-3 bg-gray-50 rounded-lg">
            <p className="text-xs text-gray-600 mb-1">Remaining</p>
            <p className="text-lg font-semibold text-gray-900">
              {eta ? formatDuration((eta.getTime() - Date.now()) / 1000) : 'Calculating...'}
            </p>
          </div>
        </div>

        {/* Step progress */}
        <StepProgress
          currentStep={progress.currentStep}
          totalSteps={progress.totalSteps}
          steps={steps}
        />

        {/* Heartbeat indicator */}
        <div className="flex items-center justify-center">
          <div className="flex items-center gap-2 text-sm text-gray-500">
            <div
              className="w-2 h-2 rounded-full bg-green-500 animate-pulse-subtle"
              aria-hidden="true"
            />
            <span>System is working...</span>
          </div>
        </div>

        {/* Errors */}
        {errors.length > 0 && (
          <ErrorList
            errors={errors}
            onRetry={onRetryError}
            onSkip={onSkipError}
            onCancel={onCancel}
          />
        )}

        {/* Actions */}
        <div className="flex items-center justify-end gap-3 pt-4 border-t">
          {canPause && onPause && (
            <Button variant="outline" onClick={onPause}>
              <Pause className="h-4 w-4 mr-2" aria-hidden="true" />
              Pause Migration
            </Button>
          )}
          {canCancel && onCancel && (
            <Button variant="destructive" onClick={onCancel}>
              <X className="h-4 w-4 mr-2" aria-hidden="true" />
              Cancel Migration
            </Button>
          )}
        </div>

        {/* Warning about closing */}
        <div className="text-xs text-gray-500 text-center">
          Please keep this window open. Closing it may interrupt the migration.
        </div>
      </CardContent>
    </Card>
  );
}

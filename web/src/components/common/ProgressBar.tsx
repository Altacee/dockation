import { cn } from '../../lib/utils';

interface ProgressBarProps {
  value: number;
  max?: number;
  label?: string;
  showPercentage?: boolean;
  size?: 'sm' | 'md' | 'lg';
  className?: string;
  animate?: boolean;
}

export function ProgressBar({
  value,
  max = 100,
  label,
  showPercentage = true,
  size = 'md',
  className,
  animate = false,
}: ProgressBarProps) {
  const percentage = Math.min(Math.max((value / max) * 100, 0), 100);

  const sizeClasses = {
    sm: 'h-2',
    md: 'h-3',
    lg: 'h-4',
  };

  return (
    <div className={cn('w-full', className)}>
      {(label || showPercentage) && (
        <div className="flex items-center justify-between mb-2">
          {label && <span className="text-sm font-medium text-gray-700">{label}</span>}
          {showPercentage && (
            <span className="text-sm font-medium text-gray-600">{Math.round(percentage)}%</span>
          )}
        </div>
      )}
      <div
        className={cn(
          'relative w-full overflow-hidden rounded-full bg-gray-200',
          sizeClasses[size]
        )}
        role="progressbar"
        aria-valuenow={value}
        aria-valuemin={0}
        aria-valuemax={max}
        aria-label={label}
      >
        <div
          className={cn(
            'h-full bg-gradient-to-r from-blue-500 to-blue-600 transition-all duration-300 ease-out',
            animate && 'animate-pulse-subtle'
          )}
          style={{ width: `${percentage}%` }}
        />
      </div>
    </div>
  );
}

interface StepProgressProps {
  currentStep: number;
  totalSteps: number;
  steps: Array<{ name: string; status: 'completed' | 'in_progress' | 'pending' }>;
  className?: string;
}

export function StepProgress({ currentStep, totalSteps, steps, className }: StepProgressProps) {
  return (
    <div className={cn('space-y-4', className)}>
      {/* Overall progress */}
      <ProgressBar value={currentStep} max={totalSteps} label="Overall Progress" />

      {/* Step list */}
      <div className="space-y-2" role="list" aria-label="Migration steps">
        {steps.map((step, index) => {
          const stepNumber = index + 1;
          const isCompleted = step.status === 'completed';
          const isInProgress = step.status === 'in_progress';
          const isPending = step.status === 'pending';

          return (
            <div
              key={index}
              className={cn(
                'flex items-center gap-3 p-3 rounded-lg border',
                isCompleted && 'bg-green-50 border-green-200',
                isInProgress && 'bg-blue-50 border-blue-200',
                isPending && 'bg-gray-50 border-gray-200'
              )}
              role="listitem"
              aria-current={isInProgress ? 'step' : undefined}
            >
              <div
                className={cn(
                  'flex items-center justify-center w-6 h-6 rounded-full text-xs font-semibold',
                  isCompleted && 'bg-green-600 text-white',
                  isInProgress && 'bg-blue-600 text-white animate-pulse-subtle',
                  isPending && 'bg-gray-300 text-gray-600'
                )}
                aria-label={`Step ${stepNumber}`}
              >
                {isCompleted ? (
                  <span aria-hidden="true">✓</span>
                ) : isInProgress ? (
                  <span className="animate-pulse-subtle" aria-hidden="true">→</span>
                ) : (
                  stepNumber
                )}
              </div>
              <span
                className={cn(
                  'text-sm font-medium',
                  isCompleted && 'text-green-900',
                  isInProgress && 'text-blue-900',
                  isPending && 'text-gray-600'
                )}
              >
                {step.name}
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

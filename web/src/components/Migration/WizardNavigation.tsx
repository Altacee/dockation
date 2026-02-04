import { Check } from 'lucide-react';
import { cn } from '../../lib/utils';

interface WizardStep {
  number: number;
  label: string;
}

const STEPS: WizardStep[] = [
  { number: 1, label: 'Source' },
  { number: 2, label: 'Resources' },
  { number: 3, label: 'Target' },
  { number: 4, label: 'Options' },
  { number: 5, label: 'Review' },
];

interface WizardNavigationProps {
  currentStep: number;
  className?: string;
}

export function WizardNavigation({ currentStep, className }: WizardNavigationProps) {
  return (
    <nav aria-label="Migration wizard progress" className={className}>
      <ol className="flex items-center justify-between">
        {STEPS.map((step, index) => {
          const isCompleted = currentStep > step.number;
          const isCurrent = currentStep === step.number;
          const isUpcoming = currentStep < step.number;

          return (
            <li key={step.number} className="flex items-center flex-1 last:flex-none">
              {/* Step indicator */}
              <div className="flex flex-col items-center">
                <div
                  className={cn(
                    'flex items-center justify-center w-10 h-10 rounded-full border-2 transition-colors',
                    isCompleted && 'bg-blue-600 border-blue-600',
                    isCurrent && 'border-blue-600 bg-white',
                    isUpcoming && 'border-gray-300 bg-white'
                  )}
                  aria-current={isCurrent ? 'step' : undefined}
                >
                  {isCompleted ? (
                    <Check className="w-5 h-5 text-white" aria-hidden="true" />
                  ) : (
                    <span
                      className={cn(
                        'text-sm font-semibold',
                        isCurrent && 'text-blue-600',
                        isUpcoming && 'text-gray-400'
                      )}
                    >
                      {step.number}
                    </span>
                  )}
                </div>
                <span
                  className={cn(
                    'mt-2 text-xs font-medium',
                    isCompleted && 'text-blue-600',
                    isCurrent && 'text-blue-600',
                    isUpcoming && 'text-gray-400'
                  )}
                >
                  {step.label}
                </span>
              </div>

              {/* Connector line */}
              {index < STEPS.length - 1 && (
                <div
                  className={cn(
                    'flex-1 h-0.5 mx-4 mt-[-1.5rem]',
                    currentStep > step.number ? 'bg-blue-600' : 'bg-gray-200'
                  )}
                  aria-hidden="true"
                />
              )}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}

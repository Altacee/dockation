import { ArrowLeft } from 'lucide-react';
import type { Worker } from '../../types';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';
import { Button } from '../ui/Button';
import { MigrationProvider, useMigrationContext } from './MigrationContext';
import { WizardNavigation } from './WizardNavigation';
import { StepSourceWorker } from './StepSourceWorker';
import { StepResourceSelect } from './StepResourceSelect';
import { StepTargetWorker } from './StepTargetWorker';
import { StepConfiguration } from './StepConfiguration';
import { StepReview } from './StepReview';

interface MigrationWizardProps {
  workers: Worker[];
  onComplete: (migrationId: string) => void;
  onCancel: () => void;
  preselectedSourceWorker?: Worker;
}

function WizardContent({
  workers,
  onComplete,
  onCancel,
  preselectedSourceWorker,
}: MigrationWizardProps) {
  const { state } = useMigrationContext();

  const stepTitles: Record<number, string> = {
    1: 'Select Source Worker',
    2: 'Select Resources to Migrate',
    3: 'Select Target Worker',
    4: 'Configure Migration Options',
    5: 'Review and Start Migration',
  };

  return (
    <Card className="max-w-4xl mx-auto">
      <CardHeader>
        <div className="flex items-center justify-between mb-4">
          <Button variant="ghost" onClick={onCancel} className="text-gray-600">
            <ArrowLeft className="h-4 w-4 mr-2" />
            Back to Dashboard
          </Button>
        </div>
        <CardTitle className="text-xl">{stepTitles[state.currentStep]}</CardTitle>
        <p className="text-sm text-gray-600 mt-1">
          {state.currentStep === 1 && 'Choose which worker to migrate resources from'}
          {state.currentStep === 2 && 'Select the containers, images, volumes, and networks to migrate'}
          {state.currentStep === 3 && 'Choose where to migrate the selected resources'}
          {state.currentStep === 4 && 'Configure how the migration should be performed'}
          {state.currentStep === 5 && 'Review your selections and start the migration'}
        </p>
      </CardHeader>

      <CardContent className="space-y-6">
        {/* Step navigation */}
        <WizardNavigation currentStep={state.currentStep} />

        {/* Step content */}
        <div className="pt-6 border-t">
          {state.currentStep === 1 && (
            <StepSourceWorker
              workers={workers}
              preselectedWorker={preselectedSourceWorker}
            />
          )}
          {state.currentStep === 2 && <StepResourceSelect />}
          {state.currentStep === 3 && (
            <StepTargetWorker workers={workers} />
          )}
          {state.currentStep === 4 && <StepConfiguration />}
          {state.currentStep === 5 && (
            <StepReview onComplete={onComplete} onCancel={onCancel} />
          )}
        </div>
      </CardContent>
    </Card>
  );
}

export function MigrationWizard(props: MigrationWizardProps) {
  return (
    <MigrationProvider>
      <WizardContent {...props} />
    </MigrationProvider>
  );
}

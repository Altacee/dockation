import { Snowflake, Thermometer, Zap, Package, GitBranch, Camera, Info, ArrowRight, Server, Shuffle } from 'lucide-react';
import { Button } from '../ui/Button';
import { cn } from '../../lib/utils';
import { useMigrationContext, type MigrationMode, type MigrationStrategy, type TransferMode } from './MigrationContext';

interface ModeOption {
  value: MigrationMode;
  label: string;
  description: string;
  icon: React.ElementType;
  recommended?: boolean;
}

interface StrategyOption {
  value: MigrationStrategy;
  label: string;
  description: string;
  icon: React.ElementType;
}

interface TransferModeOption {
  value: TransferMode;
  label: string;
  description: string;
  icon: React.ElementType;
  recommended?: boolean;
}

const MODES: ModeOption[] = [
  {
    value: 'cold',
    label: 'Cold Migration',
    description: 'Stop containers, transfer all data, then start on target. Most reliable but causes downtime.',
    icon: Snowflake,
    recommended: true,
  },
  {
    value: 'warm',
    label: 'Warm Migration',
    description: 'Pre-copy data while running, then brief pause for final sync. Minimal downtime.',
    icon: Thermometer,
  },
  {
    value: 'live',
    label: 'Live Migration',
    description: 'Transfer while running with no pause. Requires compatible storage. Experimental.',
    icon: Zap,
  },
];

const STRATEGIES: StrategyOption[] = [
  {
    value: 'full',
    label: 'Full Transfer',
    description: 'Transfer all data from scratch. Most thorough but slowest.',
    icon: Package,
  },
  {
    value: 'incremental',
    label: 'Incremental',
    description: 'Only transfer changed data since last migration. Faster for repeated migrations.',
    icon: GitBranch,
  },
  {
    value: 'snapshot',
    label: 'Snapshot',
    description: 'Create point-in-time snapshot and transfer. Good for consistent state.',
    icon: Camera,
  },
];

const TRANSFER_MODES: TransferModeOption[] = [
  {
    value: 'direct',
    label: 'Direct',
    description: 'Workers connect directly to each other. Fastest when workers can reach each other.',
    icon: ArrowRight,
    recommended: true,
  },
  {
    value: 'proxy',
    label: 'Proxy (via Master)',
    description: 'Data flows through the master server. Use when workers cannot connect directly (NAT/firewall).',
    icon: Server,
  },
  {
    value: 'auto',
    label: 'Auto-detect',
    description: 'Automatically detect connectivity and choose the best mode.',
    icon: Shuffle,
  },
];

export function StepConfiguration() {
  const { state, setOptions, nextStep, prevStep } = useMigrationContext();

  const handleModeChange = (mode: MigrationMode) => {
    setOptions({ mode });
  };

  const handleStrategyChange = (strategy: MigrationStrategy) => {
    setOptions({ strategy });
  };

  const handleTransferModeChange = (transferMode: TransferMode) => {
    setOptions({ transferMode });
  };

  return (
    <div className="space-y-8">
      {/* Migration Mode */}
      <div className="space-y-4">
        <div>
          <h3 className="text-sm font-semibold text-gray-900">Migration Mode</h3>
          <p className="text-xs text-gray-500 mt-1">
            Choose how containers should be handled during migration
          </p>
        </div>

        <div className="grid gap-3">
          {MODES.map((mode) => {
            const Icon = mode.icon;
            const isSelected = state.options.mode === mode.value;

            return (
              <button
                key={mode.value}
                onClick={() => handleModeChange(mode.value)}
                className={cn(
                  'flex items-start gap-4 p-4 rounded-lg border text-left transition-all',
                  isSelected
                    ? 'border-blue-500 bg-blue-50 ring-2 ring-blue-500/20'
                    : 'border-gray-200 bg-white hover:bg-gray-50 hover:border-gray-300'
                )}
              >
                <div
                  className={cn(
                    'flex items-center justify-center w-10 h-10 rounded-lg flex-shrink-0',
                    isSelected ? 'bg-blue-100' : 'bg-gray-100'
                  )}
                >
                  <Icon
                    className={cn('h-5 w-5', isSelected ? 'text-blue-600' : 'text-gray-500')}
                  />
                </div>
                <div className="flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-gray-900">{mode.label}</span>
                    {mode.recommended && (
                      <span className="text-xs bg-green-100 text-green-700 px-2 py-0.5 rounded">
                        Recommended
                      </span>
                    )}
                  </div>
                  <p className="text-xs text-gray-500 mt-1">{mode.description}</p>
                </div>
                <div
                  className={cn(
                    'w-5 h-5 rounded-full border-2 flex items-center justify-center flex-shrink-0 mt-1',
                    isSelected ? 'border-blue-500 bg-blue-500' : 'border-gray-300'
                  )}
                >
                  {isSelected && <div className="w-2 h-2 rounded-full bg-white" />}
                </div>
              </button>
            );
          })}
        </div>
      </div>

      {/* Transfer Strategy */}
      <div className="space-y-4">
        <div>
          <h3 className="text-sm font-semibold text-gray-900">Transfer Strategy</h3>
          <p className="text-xs text-gray-500 mt-1">
            Choose how data should be transferred
          </p>
        </div>

        <div className="grid gap-3">
          {STRATEGIES.map((strategy) => {
            const Icon = strategy.icon;
            const isSelected = state.options.strategy === strategy.value;

            return (
              <button
                key={strategy.value}
                onClick={() => handleStrategyChange(strategy.value)}
                className={cn(
                  'flex items-start gap-4 p-4 rounded-lg border text-left transition-all',
                  isSelected
                    ? 'border-blue-500 bg-blue-50 ring-2 ring-blue-500/20'
                    : 'border-gray-200 bg-white hover:bg-gray-50 hover:border-gray-300'
                )}
              >
                <div
                  className={cn(
                    'flex items-center justify-center w-10 h-10 rounded-lg flex-shrink-0',
                    isSelected ? 'bg-blue-100' : 'bg-gray-100'
                  )}
                >
                  <Icon
                    className={cn('h-5 w-5', isSelected ? 'text-blue-600' : 'text-gray-500')}
                  />
                </div>
                <div className="flex-1">
                  <span className="text-sm font-medium text-gray-900">{strategy.label}</span>
                  <p className="text-xs text-gray-500 mt-1">{strategy.description}</p>
                </div>
                <div
                  className={cn(
                    'w-5 h-5 rounded-full border-2 flex items-center justify-center flex-shrink-0 mt-1',
                    isSelected ? 'border-blue-500 bg-blue-500' : 'border-gray-300'
                  )}
                >
                  {isSelected && <div className="w-2 h-2 rounded-full bg-white" />}
                </div>
              </button>
            );
          })}
        </div>
      </div>

      {/* Transfer Mode */}
      <div className="space-y-4">
        <div>
          <h3 className="text-sm font-semibold text-gray-900">Transfer Mode</h3>
          <p className="text-xs text-gray-500 mt-1">
            Choose how data is transferred between workers
          </p>
        </div>

        <div className="grid gap-3">
          {TRANSFER_MODES.map((transferMode) => {
            const Icon = transferMode.icon;
            const isSelected = state.options.transferMode === transferMode.value;

            return (
              <button
                key={transferMode.value}
                onClick={() => handleTransferModeChange(transferMode.value)}
                className={cn(
                  'flex items-start gap-4 p-4 rounded-lg border text-left transition-all',
                  isSelected
                    ? 'border-blue-500 bg-blue-50 ring-2 ring-blue-500/20'
                    : 'border-gray-200 bg-white hover:bg-gray-50 hover:border-gray-300'
                )}
              >
                <div
                  className={cn(
                    'flex items-center justify-center w-10 h-10 rounded-lg flex-shrink-0',
                    isSelected ? 'bg-blue-100' : 'bg-gray-100'
                  )}
                >
                  <Icon
                    className={cn('h-5 w-5', isSelected ? 'text-blue-600' : 'text-gray-500')}
                  />
                </div>
                <div className="flex-1">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-medium text-gray-900">{transferMode.label}</span>
                    {transferMode.recommended && (
                      <span className="text-xs bg-green-100 text-green-700 px-2 py-0.5 rounded">
                        Recommended
                      </span>
                    )}
                  </div>
                  <p className="text-xs text-gray-500 mt-1">{transferMode.description}</p>
                </div>
                <div
                  className={cn(
                    'w-5 h-5 rounded-full border-2 flex items-center justify-center flex-shrink-0 mt-1',
                    isSelected ? 'border-blue-500 bg-blue-500' : 'border-gray-300'
                  )}
                >
                  {isSelected && <div className="w-2 h-2 rounded-full bg-white" />}
                </div>
              </button>
            );
          })}
        </div>
      </div>

      {/* Info box */}
      <div className="flex items-start gap-3 p-4 bg-amber-50 border border-amber-200 rounded-lg">
        <Info className="h-5 w-5 text-amber-600 flex-shrink-0 mt-0.5" />
        <div className="text-sm text-amber-800">
          <p className="font-medium">Important</p>
          <p className="mt-1 text-xs">
            Cold migration with full transfer is the most reliable option for first-time migrations.
            Live migration is experimental and may not work with all container configurations.
            Use Proxy mode if workers are behind NAT or firewalls and cannot connect directly.
          </p>
        </div>
      </div>

      {/* Actions */}
      <div className="flex items-center justify-between pt-4 border-t">
        <Button variant="outline" onClick={prevStep}>
          Back
        </Button>
        <Button onClick={nextStep} className="bg-blue-600 hover:bg-blue-700">
          Continue
        </Button>
      </div>
    </div>
  );
}

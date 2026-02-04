import { useState } from 'react';
import { Input } from '../ui/Input';
import { Button } from '../ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';
import { Loader2 } from 'lucide-react';
import { validatePairingCode } from '../../lib/utils';

interface EnterCodeProps {
  onConnect?: (code: string) => void;
  onCancel?: () => void;
  isConnecting?: boolean;
  error?: string;
  className?: string;
}

export function EnterCode({
  onConnect,
  onCancel,
  isConnecting = false,
  error,
  className,
}: EnterCodeProps) {
  const [code, setCode] = useState('');
  const [validationError, setValidationError] = useState('');

  const handleCodeChange = (value: string) => {
    // Only allow digits
    const digitsOnly = value.replace(/\D/g, '').slice(0, 6);
    setCode(digitsOnly);
    setValidationError('');
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();

    if (!validatePairingCode(code)) {
      setValidationError('Please enter a valid 6-digit code');
      return;
    }

    onConnect?.(code);
  };

  const isValid = validatePairingCode(code);

  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle className="text-xl">Connect to Peer</CardTitle>
        <p className="text-sm text-gray-600 mt-1">
          Enter the 6-digit pairing code from the other device
        </p>
      </CardHeader>

      <CardContent>
        <form onSubmit={handleSubmit} className="space-y-6">
          {/* Code input */}
          <div className="space-y-2">
            <label htmlFor="pairing-code" className="text-sm font-medium text-gray-700">
              Pairing Code
            </label>
            <Input
              id="pairing-code"
              type="text"
              inputMode="numeric"
              pattern="[0-9]*"
              placeholder="000000"
              value={code}
              onChange={(e) => handleCodeChange(e.target.value)}
              className="text-center text-3xl font-mono tracking-wider h-16"
              disabled={isConnecting}
              autoFocus
              aria-invalid={!!validationError || !!error}
              aria-describedby={
                validationError || error ? 'code-error' : undefined
              }
            />
            {(validationError || error) && (
              <p id="code-error" className="text-sm text-red-600" role="alert">
                {validationError || error}
              </p>
            )}
          </div>

          {/* Visual progress indicator */}
          <div className="flex items-center justify-center gap-2">
            {Array.from({ length: 6 }).map((_, i) => (
              <div
                key={i}
                className={`w-3 h-3 rounded-full transition-colors ${
                  i < code.length
                    ? 'bg-blue-600'
                    : 'bg-gray-200'
                }`}
                aria-hidden="true"
              />
            ))}
          </div>

          {/* Instructions */}
          <div className="p-4 bg-gray-50 border border-gray-200 rounded-lg">
            <p className="text-sm text-gray-700">
              The pairing code is displayed on the device you want to connect to. It's valid for
              5 minutes.
            </p>
          </div>

          {/* Actions */}
          <div className="flex items-center gap-3 pt-4 border-t">
            {onCancel && (
              <Button
                type="button"
                variant="outline"
                onClick={onCancel}
                className="flex-1"
                disabled={isConnecting}
              >
                Cancel
              </Button>
            )}
            <Button
              type="submit"
              disabled={!isValid || isConnecting}
              className="flex-1 bg-blue-600 hover:bg-blue-700"
            >
              {isConnecting ? (
                <>
                  <Loader2 className="h-4 w-4 mr-2 animate-spin" aria-hidden="true" />
                  Connecting...
                </>
              ) : (
                'Connect'
              )}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

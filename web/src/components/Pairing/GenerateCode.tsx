import { useEffect, useState } from 'react';
import { Copy, Check, Clock, RefreshCw } from 'lucide-react';
import type { PairingCode } from '../../types';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';
import { Button } from '../ui/Button';
import { copyToClipboard } from '../../lib/utils';

interface GenerateCodeProps {
  code: PairingCode;
  onRefresh?: () => void;
  onCancel?: () => void;
  className?: string;
}

export function GenerateCode({ code, onRefresh, onCancel, className }: GenerateCodeProps) {
  const [copied, setCopied] = useState(false);
  const [timeRemaining, setTimeRemaining] = useState(0);

  useEffect(() => {
    const expiresAt = new Date(code.expiresAt).getTime();

    const interval = setInterval(() => {
      const remaining = Math.max(0, Math.floor((expiresAt - Date.now()) / 1000));
      setTimeRemaining(remaining);

      if (remaining === 0) {
        clearInterval(interval);
      }
    }, 1000);

    return () => clearInterval(interval);
  }, [code.expiresAt]);

  const handleCopy = async () => {
    try {
      await copyToClipboard(code.code);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch (error) {
      console.error('Failed to copy code:', error);
    }
  };

  const minutes = Math.floor(timeRemaining / 60);
  const seconds = timeRemaining % 60;
  const isExpiringSoon = timeRemaining < 60;

  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle className="text-xl">Pairing Code Generated</CardTitle>
        <p className="text-sm text-gray-600 mt-1">
          Share this code with the device you want to connect to
        </p>
      </CardHeader>

      <CardContent className="space-y-6">
        {/* Large code display */}
        <div className="flex flex-col items-center">
          <div
            className="relative p-8 bg-gradient-to-br from-blue-50 to-indigo-50 border-2 border-blue-200 rounded-xl"
            role="group"
            aria-label="Pairing code"
          >
            <div className="text-6xl font-bold text-blue-900 tracking-wider font-mono text-center">
              {code.code}
            </div>
          </div>

          {/* Timer */}
          <div
            className={`mt-4 flex items-center gap-2 px-4 py-2 rounded-full ${
              isExpiringSoon ? 'bg-red-50 text-red-700' : 'bg-gray-100 text-gray-700'
            }`}
            role="timer"
            aria-live="polite"
            aria-label={`Code expires in ${minutes} minutes and ${seconds} seconds`}
          >
            <Clock className="h-4 w-4" aria-hidden="true" />
            <span className="text-sm font-medium">
              Expires in {minutes}:{seconds.toString().padStart(2, '0')}
            </span>
          </div>
        </div>

        {/* Copy button */}
        <Button
          onClick={handleCopy}
          variant="outline"
          size="lg"
          className="w-full"
          aria-label={copied ? 'Code copied' : 'Copy code to clipboard'}
        >
          {copied ? (
            <>
              <Check className="h-5 w-5 mr-2 text-green-600" aria-hidden="true" />
              Copied!
            </>
          ) : (
            <>
              <Copy className="h-5 w-5 mr-2" aria-hidden="true" />
              Copy Code
            </>
          )}
        </Button>

        {/* Instructions */}
        <div className="p-4 bg-blue-50 border border-blue-200 rounded-lg">
          <p className="text-sm text-blue-900 font-medium mb-2">To pair:</p>
          <ol className="text-sm text-blue-800 space-y-1 list-decimal list-inside">
            <li>Open docker-migrate on the other device</li>
            <li>Click "Connect to Peer" or "Scan Pairing Code"</li>
            <li>Enter this 6-digit code</li>
          </ol>
        </div>

        {/* Actions */}
        <div className="flex items-center gap-3 pt-4 border-t">
          {onCancel && (
            <Button variant="outline" onClick={onCancel} className="flex-1">
              Cancel
            </Button>
          )}
          {onRefresh && timeRemaining === 0 && (
            <Button onClick={onRefresh} className="flex-1 bg-blue-600 hover:bg-blue-700">
              <RefreshCw className="h-4 w-4 mr-2" aria-hidden="true" />
              Generate New Code
            </Button>
          )}
        </div>

        {timeRemaining === 0 && (
          <div className="p-3 bg-red-50 border border-red-200 rounded-lg text-center">
            <p className="text-sm text-red-800 font-medium">Code has expired</p>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

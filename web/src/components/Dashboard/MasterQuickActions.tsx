import { useState } from 'react';
import { Copy, Check, Terminal, ArrowRightLeft } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';
import { Button } from '../ui/Button';

interface MasterQuickActionsProps {
  enrollmentToken?: string;
  workerCount: number;
  onStartMigration?: () => void;
}

export function MasterQuickActions({ enrollmentToken, workerCount, onStartMigration }: MasterQuickActionsProps) {
  const [copied, setCopied] = useState(false);

  const installCommand = enrollmentToken
    ? `curl -sSL https://raw.githubusercontent.com/Altacee/dockation/main/scripts/install-worker.sh | sudo bash -s -- --master-url <MASTER_IP>:9090 --token ${enrollmentToken}`
    : '';

  const copyToClipboard = async () => {
    if (!installCommand) return;
    await navigator.clipboard.writeText(installCommand);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-lg">Quick Actions</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {/* Add Worker Section */}
        <div className="space-y-2">
          <h4 className="text-sm font-medium text-gray-700 flex items-center gap-2">
            <Terminal className="h-4 w-4" />
            Add New Worker
          </h4>
          <p className="text-xs text-gray-500">
            Run this command on a remote server to install and connect a worker:
          </p>
          <div className="relative">
            <pre className="text-xs bg-gray-900 text-gray-100 p-3 rounded-lg overflow-x-auto whitespace-pre-wrap break-all">
              {installCommand || 'Loading...'}
            </pre>
            <Button
              size="sm"
              variant="ghost"
              className="absolute top-2 right-2 text-gray-400 hover:text-white"
              onClick={copyToClipboard}
              disabled={!enrollmentToken}
            >
              {copied ? <Check className="h-4 w-4" /> : <Copy className="h-4 w-4" />}
            </Button>
          </div>
          <p className="text-xs text-gray-500">
            Replace <code className="bg-gray-100 px-1 rounded">&lt;MASTER_IP&gt;</code> with this machine's IP address.
          </p>
        </div>

        {/* Start Migration */}
        <Button
          className="w-full justify-start gap-2"
          variant="outline"
          disabled={workerCount < 2}
          onClick={onStartMigration}
        >
          <ArrowRightLeft className="h-4 w-4" />
          Start Migration
        </Button>
        {workerCount < 2 && (
          <p className="text-xs text-gray-500 text-center">
            Connect at least 2 workers to start migrating
          </p>
        )}
      </CardContent>
    </Card>
  );
}

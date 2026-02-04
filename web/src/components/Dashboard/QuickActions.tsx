import { Plus, QrCode, ArrowRightLeft } from 'lucide-react';
import { Button } from '../ui/Button';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';

interface QuickActionsProps {
  onPairDevice?: () => void;
  onStartMigration?: () => void;
  onScanCode?: () => void;
  hasPeers?: boolean;
  className?: string;
}

export function QuickActions({
  onPairDevice,
  onStartMigration,
  onScanCode,
  hasPeers = false,
  className,
}: QuickActionsProps) {
  return (
    <Card className={className}>
      <CardHeader>
        <CardTitle className="text-lg">Quick Actions</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-3">
          <Button
            onClick={onPairDevice}
            className="w-full justify-start bg-blue-600 hover:bg-blue-700"
            size="lg"
          >
            <Plus className="h-5 w-5 mr-2" aria-hidden="true" />
            Pair New Device
          </Button>

          <Button
            onClick={onScanCode}
            variant="outline"
            className="w-full justify-start"
            size="lg"
          >
            <QrCode className="h-5 w-5 mr-2" aria-hidden="true" />
            Scan Pairing Code
          </Button>

          <Button
            onClick={onStartMigration}
            variant="outline"
            className="w-full justify-start"
            size="lg"
            disabled={!hasPeers}
          >
            <ArrowRightLeft className="h-5 w-5 mr-2" aria-hidden="true" />
            Start Migration
          </Button>

          {!hasPeers && (
            <p className="text-xs text-gray-500 text-center mt-2">
              Pair with a device to start migrating
            </p>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

import { CheckCircle2, ExternalLink, XCircle } from 'lucide-react';
import { Card, CardContent, CardHeader, CardTitle } from '../ui/Card';
import { Button } from '../ui/Button';
import { formatBytes, formatDuration } from '../../lib/utils';

interface MigrationResult {
  success: boolean;
  containersTransferred: number;
  imagesTransferred: number;
  volumesTransferred: number;
  networksCreated: number;
  totalSize: number;
  duration: number;
  sourceStatus?: string;
  errors?: string[];
}

interface MigrationCompleteProps {
  result: MigrationResult;
  targetPeerName: string;
  onViewTarget?: () => void;
  onDone?: () => void;
  className?: string;
}

export function MigrationComplete({
  result,
  targetPeerName,
  onViewTarget,
  onDone,
  className,
}: MigrationCompleteProps) {
  const isSuccess = result.success;

  return (
    <Card className={className}>
      <CardHeader>
        <div className="flex flex-col items-center text-center space-y-3">
          {isSuccess ? (
            <>
              <div className="p-4 bg-green-100 rounded-full">
                <CheckCircle2 className="h-12 w-12 text-green-600" aria-hidden="true" />
              </div>
              <CardTitle className="text-2xl text-green-900">Migration Complete!</CardTitle>
              <p className="text-sm text-gray-600">
                Your resources have been successfully transferred to {targetPeerName}
              </p>
            </>
          ) : (
            <>
              <div className="p-4 bg-red-100 rounded-full">
                <XCircle className="h-12 w-12 text-red-600" aria-hidden="true" />
              </div>
              <CardTitle className="text-2xl text-red-900">Migration Incomplete</CardTitle>
              <p className="text-sm text-gray-600">
                The migration completed with some errors
              </p>
            </>
          )}
        </div>
      </CardHeader>

      <CardContent className="space-y-6">
        {/* Summary stats */}
        <div className="grid grid-cols-2 gap-4">
          <div className="p-4 bg-blue-50 border border-blue-200 rounded-lg text-center">
            <p className="text-3xl font-bold text-blue-900">{result.containersTransferred}</p>
            <p className="text-sm text-blue-700 mt-1">Containers</p>
          </div>
          <div className="p-4 bg-purple-50 border border-purple-200 rounded-lg text-center">
            <p className="text-3xl font-bold text-purple-900">{result.imagesTransferred}</p>
            <p className="text-sm text-purple-700 mt-1">Images</p>
          </div>
          <div className="p-4 bg-green-50 border border-green-200 rounded-lg text-center">
            <p className="text-3xl font-bold text-green-900">{result.volumesTransferred}</p>
            <p className="text-sm text-green-700 mt-1">Volumes</p>
          </div>
          <div className="p-4 bg-orange-50 border border-orange-200 rounded-lg text-center">
            <p className="text-3xl font-bold text-orange-900">{result.networksCreated}</p>
            <p className="text-sm text-orange-700 mt-1">Networks</p>
          </div>
        </div>

        {/* Additional details */}
        <div className="space-y-3">
          <div className="flex items-center justify-between p-3 bg-gray-50 rounded-lg">
            <span className="text-sm text-gray-600">Total data transferred</span>
            <span className="text-sm font-semibold text-gray-900">
              {formatBytes(result.totalSize)}
            </span>
          </div>
          <div className="flex items-center justify-between p-3 bg-gray-50 rounded-lg">
            <span className="text-sm text-gray-600">Migration duration</span>
            <span className="text-sm font-semibold text-gray-900">
              {formatDuration(result.duration)}
            </span>
          </div>
          {result.sourceStatus && (
            <div className="flex items-center justify-between p-3 bg-gray-50 rounded-lg">
              <span className="text-sm text-gray-600">Source status</span>
              <span className="text-sm font-semibold text-gray-900">{result.sourceStatus}</span>
            </div>
          )}
        </div>

        {/* Errors if any */}
        {result.errors && result.errors.length > 0 && (
          <div className="p-4 bg-yellow-50 border border-yellow-200 rounded-lg">
            <p className="text-sm font-semibold text-yellow-900 mb-2">
              Issues encountered ({result.errors.length})
            </p>
            <ul className="text-sm text-yellow-800 space-y-1 list-disc list-inside">
              {result.errors.slice(0, 5).map((error, index) => (
                <li key={index}>{error}</li>
              ))}
              {result.errors.length > 5 && (
                <li className="text-yellow-600">
                  ...and {result.errors.length - 5} more
                </li>
              )}
            </ul>
          </div>
        )}

        {/* Success message */}
        {isSuccess && (
          <div className="p-4 bg-green-50 border border-green-200 rounded-lg">
            <p className="text-sm text-green-800">
              <span className="font-semibold">Your data is safe.</span> All resources have been
              transferred successfully. Original resources remain on the source system for your
              records.
            </p>
          </div>
        )}

        {/* Actions */}
        <div className="flex items-center gap-3 pt-4 border-t">
          {onDone && (
            <Button variant="outline" onClick={onDone} className="flex-1">
              Done
            </Button>
          )}
          {onViewTarget && (
            <Button onClick={onViewTarget} className="flex-1 bg-blue-600 hover:bg-blue-700">
              <ExternalLink className="h-4 w-4 mr-2" aria-hidden="true" />
              View on {targetPeerName}
            </Button>
          )}
        </div>
      </CardContent>
    </Card>
  );
}

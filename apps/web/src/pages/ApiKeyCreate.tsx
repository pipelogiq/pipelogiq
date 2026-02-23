import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { AppHeader } from '@/components/layout/AppHeader';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { apiKeysApi } from '@/api/client';
import { useApplications } from '@/hooks/use-applications';
import { ChevronLeft, Loader2, Key, Copy, Check } from 'lucide-react';

export default function ApiKeyCreate() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: applications } = useApplications();

  const hasApplications = (applications?.length ?? 0) > 0;

  const [applicationMode, setApplicationMode] = useState<'existing' | 'new'>('new');
  const [selectedApplicationId, setSelectedApplicationId] = useState<string>('');

  // New application fields
  const [appName, setAppName] = useState('');
  const [appDescription, setAppDescription] = useState('');

  // API Key fields
  const [keyName, setKeyName] = useState('');
  const [expiresIn, setExpiresIn] = useState('never');

  // Generated key state
  const [generatedKey, setGeneratedKey] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  // Generate API key mutation
  const generateKeyMutation = useMutation({
    mutationFn: apiKeysApi.generate,
  });

  useEffect(() => {
    if (!hasApplications) {
      setApplicationMode('new');
      setSelectedApplicationId('');
      return;
    }
    if (!selectedApplicationId && applications && applications.length > 0) {
      setSelectedApplicationId(String(applications[0].id));
    }
  }, [applications, hasApplications, selectedApplicationId]);

  const isLoading = generateKeyMutation.isPending;

  const canGenerate = useMemo(() => {
    if (applicationMode === 'existing') {
      return selectedApplicationId !== '';
    }
    return appName.trim() !== '';
  }, [applicationMode, appName, selectedApplicationId]);

  const handleGenerate = async () => {
    if (!canGenerate) return;

    try {
      // Calculate expiration date
      let expiresAt: string | undefined;
      if (expiresIn !== 'never') {
        const now = new Date();
        switch (expiresIn) {
          case '30days':
            now.setDate(now.getDate() + 30);
            break;
          case '90days':
            now.setDate(now.getDate() + 90);
            break;
          case '1year':
            now.setFullYear(now.getFullYear() + 1);
            break;
        }
        expiresAt = now.toISOString();
      }

      const payload = applicationMode === 'existing'
        ? {
            applicationId: Number(selectedApplicationId),
            name: keyName.trim() || undefined,
            expiresAt,
          }
        : {
            newApplication: {
              name: appName.trim(),
              description: appDescription.trim() || undefined,
            },
            name: keyName.trim() || undefined,
            expiresAt,
          };

      const apiKey = await generateKeyMutation.mutateAsync(payload);

      // Store the generated key to display
      if (apiKey.key) {
        setGeneratedKey(apiKey.key);
      }

      // Invalidate queries
      queryClient.invalidateQueries({ queryKey: ['applications'] });
      queryClient.invalidateQueries({ queryKey: ['apiKeys'] });
    } catch (error) {
      console.error('Failed to generate API key:', error);
    }
  };

  const handleCopy = async () => {
    if (generatedKey) {
      await navigator.clipboard.writeText(generatedKey);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  const handleDone = () => {
    navigate('/settings');
  };

  return (
    <div className="flex flex-col min-h-screen">
      <AppHeader
        title="Create API Key"
        subtitle="Set up a new application and generate an API key"
      />

      <div className="flex-1 p-6 max-w-2xl">
        <button
          onClick={() => navigate('/settings')}
          className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground transition-colors mb-6"
        >
          <ChevronLeft className="h-4 w-4" />
          Back to Settings
        </button>

        {generatedKey ? (
          // Show generated key
          <div className="rounded-xl border border-border bg-card p-6 space-y-6">
            <div className="flex items-center gap-3">
              <div className="h-12 w-12 rounded-xl bg-status-success-bg flex items-center justify-center">
                <Key className="h-6 w-6 text-status-success" />
              </div>
              <div>
                <h2 className="text-lg font-semibold text-foreground">API Key Generated</h2>
                <p className="text-sm text-muted-foreground">
                  Make sure to copy your API key now. You won't be able to see it again.
                </p>
              </div>
            </div>

            <div className="space-y-2">
              <Label>Your API Key</Label>
              <div className="flex gap-2">
                <code className="flex-1 rounded-lg bg-muted p-3 font-mono text-sm break-all">
                  {generatedKey}
                </code>
                <Button variant="outline" size="icon" onClick={handleCopy}>
                  {copied ? (
                    <Check className="h-4 w-4 text-status-success" />
                  ) : (
                    <Copy className="h-4 w-4" />
                  )}
                </Button>
              </div>
            </div>

            <div className="pt-4 border-t">
              <Button onClick={handleDone} className="w-full">
                Done
              </Button>
            </div>
          </div>
        ) : (
          // Show form
          <div className="space-y-6">
            {/* Application Section */}
            <div className="rounded-xl border border-border bg-card p-6 space-y-4">
              <h3 className="font-semibold text-foreground">Application</h3>

              {hasApplications && (
                <div className="flex items-center gap-2">
                  <Button
                    type="button"
                    variant={applicationMode === 'existing' ? 'default' : 'outline'}
                    size="sm"
                    disabled={isLoading}
                    onClick={() => setApplicationMode('existing')}
                  >
                    Use Existing
                  </Button>
                  <Button
                    type="button"
                    variant={applicationMode === 'new' ? 'default' : 'outline'}
                    size="sm"
                    disabled={isLoading}
                    onClick={() => setApplicationMode('new')}
                  >
                    Create New
                  </Button>
                </div>
              )}

              {applicationMode === 'existing' ? (
                <div className="space-y-2">
                  <Label htmlFor="existingApplication">Choose Existing Application *</Label>
                  <Select
                    value={selectedApplicationId}
                    onValueChange={setSelectedApplicationId}
                    disabled={isLoading || !hasApplications}
                  >
                    <SelectTrigger id="existingApplication">
                      <SelectValue placeholder="Select application" />
                    </SelectTrigger>
                    <SelectContent>
                      {(applications ?? []).map((app) => (
                        <SelectItem key={app.id} value={String(app.id)}>
                          {app.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              ) : (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="appName">Application Name *</Label>
                    <Input
                      id="appName"
                      placeholder="My Application"
                      value={appName}
                      onChange={(e) => setAppName(e.target.value)}
                      disabled={isLoading}
                    />
                  </div>

                  <div className="space-y-2">
                    <Label htmlFor="appDescription">Description</Label>
                    <Textarea
                      id="appDescription"
                      placeholder="Optional description for this application"
                      value={appDescription}
                      onChange={(e) => setAppDescription(e.target.value)}
                      disabled={isLoading}
                      rows={3}
                    />
                  </div>
                </>
              )}
            </div>

            {/* API Key Section */}
            <div className="rounded-xl border border-border bg-card p-6 space-y-4">
              <h3 className="font-semibold text-foreground">API Key</h3>

              <div className="space-y-2">
                <Label htmlFor="keyName">Key Name</Label>
                <Input
                  id="keyName"
                  placeholder="e.g., Production Key"
                  value={keyName}
                  onChange={(e) => setKeyName(e.target.value)}
                  disabled={isLoading}
                />
                <p className="text-xs text-muted-foreground">
                  A friendly name to identify this key
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="expiresIn">Expiration</Label>
                <Select value={expiresIn} onValueChange={setExpiresIn} disabled={isLoading}>
                  <SelectTrigger id="expiresIn">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="never">Never expires</SelectItem>
                    <SelectItem value="30days">30 days</SelectItem>
                    <SelectItem value="90days">90 days</SelectItem>
                    <SelectItem value="1year">1 year</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            {/* Actions */}
            <div className="flex gap-3">
              <Button
                variant="outline"
                onClick={() => navigate('/settings')}
                disabled={isLoading}
                className="flex-1"
              >
                Cancel
              </Button>
              <Button
                onClick={handleGenerate}
                disabled={isLoading || !canGenerate}
                className="flex-1"
              >
                {isLoading ? (
                  <>
                    <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                    Generating...
                  </>
                ) : (
                  <>
                    <Key className="mr-2 h-4 w-4" />
                    Generate API Key
                  </>
                )}
              </Button>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}

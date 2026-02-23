import { useNavigate } from 'react-router-dom';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { AppHeader } from '@/components/layout/AppHeader';
import { Button } from '@/components/ui/button';
import { useApplications } from '@/hooks/use-applications';
import { apiKeysApi } from '@/api/client';
import {
  Key,
  Plus,
  Trash2,
  Loader2,
  Building2,
} from 'lucide-react';
import { format } from 'date-fns';

export default function Settings() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { data: applications, isLoading } = useApplications();

  const disableKeyMutation = useMutation({
    mutationFn: (apiKeyId: number) => apiKeysApi.disable({ apiKeyId }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['applications'] });
    },
  });

  const handleAddApiKey = () => {
    navigate('/settings/api-key/new');
  };

  const handleDisableKey = (apiKeyId: number) => {
    if (confirm('Are you sure you want to disable this API key?')) {
      disableKeyMutation.mutate(apiKeyId);
    }
  };

  return (
    <div className="flex flex-col">
      <AppHeader
        title="Settings"
        subtitle="Manage applications and API keys"
      />

      <div className="flex-1 p-6 space-y-6">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-lg font-semibold text-foreground">API Keys</h2>
            <p className="text-sm text-muted-foreground">
              Manage API keys for programmatic access to your pipelines
            </p>
          </div>
          <Button onClick={handleAddApiKey} className="gap-2">
            <Plus className="h-4 w-4" />
            Add API Key
          </Button>
        </div>

        {/* Content */}
        {isLoading ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="h-8 w-8 animate-spin text-primary" />
          </div>
        ) : !applications?.length ? (
          <div className="rounded-xl border border-dashed border-border bg-card p-12 text-center">
            <Key className="h-12 w-12 mx-auto text-muted-foreground/50" />
            <h3 className="mt-4 text-lg font-medium text-foreground">No API Keys</h3>
            <p className="mt-2 text-sm text-muted-foreground">
              Create your first API key to start using the Pipelogiq API
            </p>
            <Button onClick={handleAddApiKey} className="mt-6 gap-2">
              <Plus className="h-4 w-4" />
              Create API Key
            </Button>
          </div>
        ) : (
          <div className="space-y-4">
            {applications.map((app) => (
              <div
                key={app.id}
                className="rounded-xl border border-border bg-card overflow-hidden"
              >
                {/* Application Header */}
                <div className="px-5 py-4 border-b border-border bg-muted/30">
                  <div className="flex items-center gap-3">
                    <div className="h-10 w-10 rounded-lg bg-primary/10 flex items-center justify-center">
                      <Building2 className="h-5 w-5 text-primary" />
                    </div>
                    <div>
                      <h3 className="font-semibold text-foreground">{app.name}</h3>
                      {app.description && (
                        <p className="text-sm text-muted-foreground">{app.description}</p>
                      )}
                    </div>
                  </div>
                </div>

                {/* API Keys List */}
                {app.apiKeys?.length ? (
                  <div className="divide-y divide-border">
                    {app.apiKeys.map((key) => (
                      <div
                        key={key.id}
                        className="flex items-center justify-between px-5 py-4"
                      >
                        <div className="flex items-center gap-3">
                          <Key className="h-4 w-4 text-muted-foreground" />
                          <div>
                            <div className="flex items-center gap-2">
                              <span className="font-medium text-foreground">
                                {key.name || 'API Key'}
                              </span>
                              {key.disabledAt && (
                                <span className="text-xs px-2 py-0.5 rounded-full bg-status-error-bg text-status-error">
                                  Disabled
                                </span>
                              )}
                            </div>
                            <div className="flex items-center gap-3 mt-1 text-xs text-muted-foreground">
                              {key.createdAt && (
                                <span>Created {format(new Date(key.createdAt), 'MMM d, yyyy')}</span>
                              )}
                              {key.expiresAt && (
                                <span>Expires {format(new Date(key.expiresAt), 'MMM d, yyyy')}</span>
                              )}
                              {key.lastUsed && (
                                <span>Last used {format(new Date(key.lastUsed), 'MMM d, yyyy')}</span>
                              )}
                            </div>
                          </div>
                        </div>
                        {!key.disabledAt && (
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => handleDisableKey(key.id)}
                            disabled={disableKeyMutation.isPending}
                            className="text-status-error hover:bg-status-error-bg"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        )}
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="px-5 py-8 text-center text-sm text-muted-foreground">
                    No API keys for this application
                  </div>
                )}
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

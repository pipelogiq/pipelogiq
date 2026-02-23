import { useEffect, useRef } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import type { PipelineResponse } from '@/types/api';
import type { ObservabilityConfig } from '@/types/observability';
import type { PipelineExecution } from '@/components/pipelines/PipelineDetailPanel';
import {
  mapPipelineToExecution,
  getTraceLinkTemplateFromConfig,
  getLogsLinkTemplateFromConfig,
  resolveObservabilityConfig,
} from './use-pipelines';

function getWsUrl(): string {
  const base = import.meta.env.VITE_API_BASE_URL;
  if (base && /^https?:\/\//.test(base)) {
    // Convert absolute http(s)://host(:port)/... to ws(s)://host(:port)/ws
    const parsed = new URL(base);
    parsed.protocol = parsed.protocol === 'https:' ? 'wss:' : 'ws:';
    parsed.pathname = '/ws';
    parsed.search = '';
    parsed.hash = '';
    return parsed.toString();
  }

  // Relative API base (e.g. /api) or unset -> use same host, Vite/nginx should proxy /ws.
  const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
  return `${proto}//${window.location.host}/ws`;
}

const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 30000;
const DETAIL_REFETCH_MIN_MS = 1500;
const LIST_REFETCH_MIN_MS = 1000;

export function usePipelineWebSocket() {
  const queryClient = useQueryClient();
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeout = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reconnectDelay = useRef(RECONNECT_BASE_MS);
  const lastDetailRefreshRef = useRef<Map<number, number>>(new Map());
  const lastListRefreshRef = useRef(0);
  const linkTemplatesRef = useRef<{ traceLinkTemplate?: string; logsLinkTemplate?: string }>({});

  useEffect(() => {
    let unmounted = false;
    const cachedConfig = queryClient.getQueryData<ObservabilityConfig>(['observability', 'config']);
    linkTemplatesRef.current = {
      traceLinkTemplate: getTraceLinkTemplateFromConfig(cachedConfig),
      logsLinkTemplate: getLogsLinkTemplateFromConfig(cachedConfig),
    };
    void resolveObservabilityConfig(queryClient).then((config) => {
      if (!unmounted) {
        linkTemplatesRef.current = {
          traceLinkTemplate: getTraceLinkTemplateFromConfig(config),
          logsLinkTemplate: getLogsLinkTemplateFromConfig(config),
        };
      }
    });

    function connect() {
      if (unmounted) return;

      const url = getWsUrl();
      const ws = new WebSocket(url);
      wsRef.current = ws;

      ws.onopen = () => {
        console.log('[WS] connected to', url);
        reconnectDelay.current = RECONNECT_BASE_MS;
      };

      ws.onmessage = (event) => {
        try {
          const raw = JSON.parse(event.data) as PipelineResponse;
          const pipelineId = Number(raw.id);
          if (!Number.isFinite(pipelineId) || pipelineId <= 0) return;

          const execution = mapPipelineToExecution(raw, linkTemplatesRef.current);
          const previousDetail = queryClient.getQueryData<PipelineExecution>(['pipeline', pipelineId]);
          if (!execution.traceUrl && previousDetail?.traceUrl) {
            execution.traceUrl = previousDetail.traceUrl;
          }
          if (!execution.logTraceUrl && previousDetail?.logTraceUrl) {
            execution.logTraceUrl = previousDetail.logTraceUrl;
          }

          // Update single pipeline query
          queryClient.setQueryData<PipelineExecution>(
            ['pipeline', pipelineId],
            execution,
          );

          // Update pipelines list queries (all filter variants)
          queryClient.setQueriesData<{ items: PipelineExecution[]; totalCount: number; pageNumber: number; pageSize: number }>(
            { queryKey: ['pipelines'] },
            (old) => {
              if (!old) return old;
              const idx = old.items.findIndex((p) => p.id === String(pipelineId));
              if (idx === -1) return old;
              const items = [...old.items];
              const previousListItem = old.items[idx];
              items[idx] = {
                ...execution,
                traceUrl: execution.traceUrl || previousListItem?.traceUrl || '',
                logTraceUrl: execution.logTraceUrl || previousListItem?.logTraceUrl || '',
              };
              return { ...old, items };
            },
          );

          // Keep active detail query in sync with server source of truth (logs/context can change).
          const now = Date.now();
          const lastDetailRefresh = lastDetailRefreshRef.current.get(pipelineId) ?? 0;
          if (now - lastDetailRefresh >= DETAIL_REFETCH_MIN_MS) {
            lastDetailRefreshRef.current.set(pipelineId, now);
            void queryClient.invalidateQueries({
              queryKey: ['pipeline', pipelineId],
              refetchType: 'active',
            });
          }

          // Refresh active list queries to keep filters/pagination/totals accurate.
          if (now - lastListRefreshRef.current >= LIST_REFETCH_MIN_MS) {
            lastListRefreshRef.current = now;
            void queryClient.invalidateQueries({
              queryKey: ['pipelines'],
              refetchType: 'active',
            });
          }
        } catch {
          // Ignore malformed messages
        }
      };

      ws.onclose = () => {
        if (unmounted) return;
        console.log('[WS] disconnected, reconnecting in', reconnectDelay.current, 'ms');
        reconnectTimeout.current = setTimeout(() => {
          reconnectDelay.current = Math.min(reconnectDelay.current * 2, RECONNECT_MAX_MS);
          connect();
        }, reconnectDelay.current);
      };

      ws.onerror = () => {
        // onclose will fire after this
        ws.close();
      };
    }

    connect();

    return () => {
      unmounted = true;
      if (reconnectTimeout.current) {
        clearTimeout(reconnectTimeout.current);
      }
      if (wsRef.current) {
        wsRef.current.onclose = null;
        wsRef.current.close();
      }
    };
  }, [queryClient]);
}

import { useId, useMemo, useState } from 'react';
import { ChevronDown, ChevronRight, Copy, LockKeyhole, Search } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { PipelinePayload } from '@/components/pipelines/PipelinePayload';
import { toast } from '@/hooks/use-toast';
import type { ContextItem, PipelineResponse } from '@/types/api';

interface PipelineContextTableProps {
  items: NonNullable<PipelineResponse['pipelineContextItems']>;
}

function describeValue(value: string): { type: string; preview: string; empty?: boolean } {
  if (value === '') return { type: 'Text', preview: 'Empty text', empty: true };
  try {
    const parsed: unknown = JSON.parse(value);
    if (parsed === null) return { type: 'Null', preview: 'null' };
    if (typeof parsed === 'string') {
      return { type: 'String', preview: parsed || 'Empty string', empty: parsed === '' };
    }
    // Use the original text for numeric and structured values. Reserializing
    // parsed JSON would change large numbers and discard duplicate object keys.
    if (typeof parsed === 'number') return { type: 'Number', preview: value.trim() };
    if (typeof parsed === 'boolean') return { type: 'Boolean', preview: value.trim() };
    return { type: Array.isArray(parsed) ? 'Array' : 'Object', preview: value };
  } catch {
    return { type: 'Text', preview: value };
  }
}

function ContextRow({ item }: { item: ContextItem }) {
  const [expanded, setExpanded] = useState(false);
  const detailsId = useId();
  const display = useMemo(() => describeValue(item.value), [item.value]);

  async function copyValue() {
    try {
      await navigator.clipboard.writeText(item.value);
      toast({ title: 'Context value copied', description: `The original value of ${item.key} was copied.` });
    } catch {
      toast({ title: 'Could not copy context value', description: 'Expand the row to select and copy its value manually.', variant: 'destructive' });
    }
  }

  return (
    <>
      <tr className="group border-b border-border/60 last:border-b-0 hover:bg-muted/25">
        <th scope="row" className="px-4 py-3 text-left align-top font-normal">
          <button
            type="button"
            aria-label={`${expanded ? 'Collapse' : 'Expand'} ${item.key}`}
            aria-expanded={expanded}
            aria-controls={expanded ? detailsId : undefined}
            onClick={() => setExpanded((value) => !value)}
            className="-ml-1 flex w-full items-start gap-2 rounded p-1 text-left hover:text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
          >
            {expanded ? <ChevronDown className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" /> : <ChevronRight className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />}
            <span className="min-w-0 break-words font-mono text-xs font-medium leading-5 [overflow-wrap:anywhere]">{item.key}</span>
          </button>
          {(item.isSensitive || item.isDeleted) && (
            <div className="ml-6 mt-1 flex flex-wrap gap-x-3 gap-y-1 text-[11px] text-muted-foreground">
              {item.isSensitive && <span className="inline-flex items-center gap-1"><LockKeyhole className="h-3 w-3" />Sensitive</span>}
              {item.isDeleted && <span>Deleted</span>}
            </div>
          )}
        </th>
        <td className="px-4 py-4 align-top text-xs text-muted-foreground" title={item.valueType}>
          {display.type}
        </td>
        <td className="min-w-0 px-4 py-4 align-top">
          <span className={`line-clamp-2 whitespace-pre-wrap break-words font-mono text-xs leading-5 [overflow-wrap:anywhere] ${display.empty ? 'font-sans italic text-muted-foreground' : 'text-foreground'}`}>
            {display.preview}
          </span>
        </td>
        <td className="px-4 py-2.5 text-right align-top">
          <Button type="button" variant="ghost" size="icon" className="h-8 w-8 text-muted-foreground hover:text-foreground" aria-label={`Copy ${item.key} value`} title="Copy original value" onClick={() => void copyValue()}>
            <Copy className="h-3.5 w-3.5" />
          </Button>
        </td>
      </tr>
      {expanded && (
        <tr className="border-b border-border/60 bg-muted/20">
          <td colSpan={4} className="px-4 py-4 sm:px-10">
            <div id={detailsId} className="min-w-0 space-y-3">
              <PipelinePayload title={item.key} value={item.value} compact />
              {item.valueType && (
                <p className="break-words text-xs leading-5 text-muted-foreground [overflow-wrap:anywhere]">
                  <span className="mr-2 font-medium">Declared type</span>{item.valueType}
                </p>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  );
}

export function PipelineContextTable({ items }: PipelineContextTableProps) {
  const [search, setSearch] = useState('');
  const filtered = useMemo(() => {
    const query = search.trim().toLowerCase();
    return query ? items.filter((item) => item.key.toLowerCase().includes(query) || item.value.toLowerCase().includes(query)) : items;
  }, [items, search]);

  return (
    <section aria-label="Pipeline context" className="min-w-0 overflow-hidden">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3">
        <div className="relative w-full sm:max-w-sm">
          <Search className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
          <Input aria-label="Search context" placeholder="Search keys and values…" value={search} onChange={(event) => setSearch(event.target.value)} className="h-9 pl-9" />
        </div>
        <span role="status" className="text-xs text-muted-foreground">Showing {filtered.length} of {items.length} entries</span>
      </div>
      <div className="overflow-x-auto">
        <table aria-label="Pipeline context items" className="w-full min-w-[560px] table-fixed border-collapse">
          <colgroup>
            <col className="w-[28%]" />
            <col className="w-[88px]" />
            <col />
            <col className="w-[72px]" />
          </colgroup>
          <thead className="border-b border-border bg-muted/25 text-left text-xs text-muted-foreground">
            <tr>
              <th scope="col" className="px-4 py-2.5 font-medium">Key</th>
              <th scope="col" className="px-4 py-2.5 font-medium">Type</th>
              <th scope="col" className="px-4 py-2.5 font-medium">Value</th>
              <th scope="col" className="px-4 py-2.5"><span className="sr-only">Actions</span></th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((item) => <ContextRow key={item.key} item={item} />)}
            {filtered.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-12 text-center text-sm text-muted-foreground">
                  <p>{items.length === 0 ? 'No context recorded for this pipeline.' : 'No context values match your search.'}</p>
                  {items.length > 0 && <Button type="button" variant="ghost" size="sm" onClick={() => setSearch('')} className="mt-2">Clear search</Button>}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </section>
  );
}

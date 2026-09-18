import { useId, useMemo, useState } from 'react';
import { Copy } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { toast } from '@/hooks/use-toast';
import { cn } from '@/lib/utils';

interface PipelinePayloadProps {
  title: string;
  value?: string;
  compact?: boolean;
}

// Validate JSON, then change whitespace only. Parsing and reserializing could
// round large numbers or discard duplicate keys in the recorded payload.
function formatPayload(value: string): { isJson: boolean; formatted: string } {
  try {
    JSON.parse(value);
  } catch {
    return { isJson: false, formatted: value };
  }
  const tokens = value.match(/"(?:\\.|[^"\\])*"|[{}[\],:]|[^\s{}[\],:]+/g) ?? [];
  let depth = 0;
  let formatted = '';
  const newline = () => '\n' + '  '.repeat(depth);
  tokens.forEach((token, index) => {
    if (token === '{' || token === '[') {
      formatted += token;
      depth += 1;
      if (tokens[index + 1] !== '}' && tokens[index + 1] !== ']') formatted += newline();
    } else if (token === '}' || token === ']') {
      depth -= 1;
      if (tokens[index - 1] !== '{' && tokens[index - 1] !== '[') formatted += newline();
      formatted += token;
    } else if (token === ',') {
      formatted += ',' + newline();
    } else if (token === ':') {
      formatted += ': ';
    } else {
      formatted += token;
    }
  });
  return { isJson: true, formatted };
}

export function PipelinePayload({ title, value, compact = false }: PipelinePayloadProps) {
  const headingId = useId();
  const [showRaw, setShowRaw] = useState(false);
  const display = useMemo(() => formatPayload(value ?? ''), [value]);

  async function copyPayload() {
    if (value === undefined) return;
    try {
      await navigator.clipboard.writeText(value);
      toast({ title: `${title} copied`, description: 'The original payload was copied to the clipboard.' });
    } catch {
      toast({
        title: `Could not copy ${title.toLowerCase()}`,
        description: 'Select the payload text and copy it manually.',
        variant: 'destructive',
      });
    }
  }

  return (
    <section aria-labelledby={headingId} className="min-w-0 overflow-hidden rounded-lg border border-border bg-card">
      <div className={cn("flex flex-wrap items-center justify-between gap-3 border-b border-border px-4 py-3", compact && "gap-2 px-3 py-1.5")}>
        <div className="flex min-w-0 items-center gap-2">
          <h3 id={headingId} className="min-w-0 break-words text-sm font-semibold [overflow-wrap:anywhere]">{title}</h3>
          {value !== undefined && value !== '' && (
            <span className="rounded border border-border px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
              {display.isJson ? 'JSON' : 'Text'}
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          {display.isJson && (
            <div role="group" aria-label={`${title} display format`} className="flex rounded-md border border-border p-0.5">
              <Button type="button" variant={showRaw ? 'ghost' : 'secondary'} size="sm" className="h-7 px-2 text-xs" aria-pressed={!showRaw} onClick={() => setShowRaw(false)}>
                Formatted
              </Button>
              <Button type="button" variant={showRaw ? 'secondary' : 'ghost'} size="sm" className="h-7 px-2 text-xs" aria-pressed={showRaw} onClick={() => setShowRaw(true)}>
                Raw
              </Button>
            </div>
          )}
          <Button type="button" variant="ghost" size="sm" className="h-8 gap-1.5 px-2" aria-label={`Copy ${title}`} disabled={value === undefined} onClick={() => void copyPayload()}>
            <Copy className="h-3.5 w-3.5" />
            Copy
          </Button>
        </div>
      </div>
      {value === undefined ? (
        <p className="px-4 py-5 text-sm text-muted-foreground">No payload recorded.</p>
      ) : value === '' ? (
        <p className="px-4 py-5 text-sm text-muted-foreground">Empty text payload.</p>
      ) : (
        <pre aria-label={`${title} payload`} tabIndex={0} className={cn("max-h-[36rem] min-w-0 overflow-auto whitespace-pre-wrap break-words p-4 font-mono text-xs leading-6 text-foreground [overflow-wrap:anywhere] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring", compact && "max-h-80 p-3 leading-5")}>
          {showRaw ? value : display.formatted}
        </pre>
      )}
    </section>
  );
}

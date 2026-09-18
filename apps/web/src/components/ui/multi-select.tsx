import { useMemo, useState } from 'react';
import { ChevronDown, Search } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';

interface MultiSelectProps {
  id?: string;
  label: string;
  values: string[];
  options: Array<{ value: string; label: string }>;
  onChange: (values: string[]) => void;
}

export function MultiSelect({ id, label, values, options, onChange }: MultiSelectProps) {
  const [search, setSearch] = useState('');
  const choices = useMemo(() => {
    const known = new Map(options.map(option => [option.value, option]));
    // Keep saved targets visible even when they are absent from the latest options.
    for (const value of values) if (!known.has(value)) known.set(value, { value, label: value });
    return [...known.values()];
  }, [options, values]);
  const selected = new Set(values);
  const query = search.trim().toLowerCase();
  const filtered = choices.filter(option => `${option.label} ${option.value}`.toLowerCase().includes(query));
  const selectionLabel = values.length === 0 ? 'All' : values.length === 1
    ? choices.find(option => option.value === values[0])?.label ?? values[0]
    : `${values.length} selected`;

  return (
    <Popover onOpenChange={() => setSearch('')}>
      <PopoverTrigger asChild>
        <Button id={id} type="button" variant="outline" aria-label={label} className="w-full justify-between gap-2 font-normal">
          <span className="min-w-0 truncate">{selectionLabel}</span>
          <ChevronDown className="h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-[var(--radix-popover-trigger-width)] min-w-56 max-w-[calc(100vw-2rem)] p-2">
        <div className="relative mb-2">
          <Search className="pointer-events-none absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input aria-label={`Search ${label.toLowerCase()}`} placeholder="Search…" value={search} onChange={event => setSearch(event.target.value)} className="h-9 pl-8" />
        </div>
        <div className="max-h-60 space-y-0.5 overflow-y-auto" role="group" aria-label={`${label} options`}>
          {filtered.map(option => (
            <label key={option.value} className="flex cursor-pointer items-start gap-2.5 rounded-md px-2 py-1.5 text-sm hover:bg-muted">
              <Checkbox checked={selected.has(option.value)} onCheckedChange={checked => onChange(checked === true ? [...values, option.value] : values.filter(value => value !== option.value))} className="mt-0.5 shrink-0" />
              <span className="min-w-0 break-words [overflow-wrap:anywhere]">{option.label}</span>
            </label>
          ))}
          {!filtered.length && <p className="px-2 py-4 text-sm text-muted-foreground">{choices.length ? 'No matches.' : 'No options available.'}</p>}
        </div>
        <div className="mt-2 flex items-center justify-between border-t border-border px-2 pt-2 text-xs text-muted-foreground">
          <span>{values.length ? `${values.length} selected` : 'All included'}</span>
          <Button type="button" variant="ghost" size="sm" className="h-7 px-2 text-xs" disabled={!values.length} onClick={() => onChange([])}>Clear</Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}

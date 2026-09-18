import { afterEach, expect, it, vi } from 'vitest';
import { pipelinesApi } from '@/api/client';
import { contextDisplayValue } from '@/hooks/use-pipelines';

afterEach(() => vi.unstubAllGlobals());

it('sends structured filters to the server alongside pagination without mixing label keys and tag values', async () => {
  const fetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ items: [], totalCount: 0 }), { headers: { 'content-type': 'application/json' } }));
  vi.stubGlobal('fetch', fetch);
  await pipelinesApi.getAll({ pageNumber: 3, pageSize: 10, tags: ['critical'], labels: [{ key: 'priority', value: 'critical' }], contextFilters: [{ key: 'customer', value: '50%_off & special' }], environment: 'prod', owner: 'alice' });
  const url = new URL(fetch.mock.calls[0][0], 'https://local.test');
  expect(url.searchParams.get('pageNumber')).toBe('3');
  expect(url.searchParams.getAll('keywords')).toEqual([]);
  expect(url.searchParams.getAll('tags')).toEqual(['critical']);
  expect(JSON.parse(url.searchParams.get('labels')!)).toEqual({ key: 'priority', value: 'critical' });
  expect(JSON.parse(url.searchParams.get('contextFilters')!)).toEqual({ key: 'customer', value: '50%_off & special' });
  expect(url.searchParams.get('environment')).toBe('prod');
  expect(url.searchParams.get('owner')).toBe('alice');
});

it.each([['"prod"', 'prod'], ['prod', 'prod'], ['"a\\"b"', 'a"b'], ['42', '42'], ['{"a":1}', '{"a":1}'], ['[REDACTED]', '[REDACTED]']])('decodes display context %s', (input, expected) => {
  expect(contextDisplayValue(input)).toBe(expected);
});

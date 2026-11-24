import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, render, screen } from '@testing-library/react';
import { BrowserRouter, Link, Route, Routes } from 'react-router-dom';
import { appUrl, getBasePath, getWebSocketUrl } from '../app-url';
import { pipelinesApi } from '@/api/client';

afterEach(() => {
  cleanup();
  document.head.querySelector('meta[name="pipelogiq-base-path"]')?.remove();
  window.history.replaceState({}, '', '/');
  vi.unstubAllGlobals();
});

describe.each(['', '/pipelogiq', '/tools/pipelogiq-test'])('installation at %s', (base) => {
  it('keeps deep routes, navigation, API requests and WebSockets inside the installation', async () => {
    const meta = document.createElement('meta');
    meta.name = 'pipelogiq-base-path';
    meta.content = base;
    document.head.append(meta);
    window.history.replaceState({}, '', `${base}/pipelines/42`);
    render(<BrowserRouter basename={getBasePath() || '/'}><Routes>
      <Route path="/pipelines/:id" element={<Link to="/settings">Settings</Link>} />
    </Routes></BrowserRouter>);
    expect(screen.getByRole('link').getAttribute('href')).toBe(`${base}/settings`);
    expect(appUrl('/pipelines?id=42')).toBe(`${base}/pipelines?id=42`);
    const fetchMock = vi.fn().mockResolvedValue(new Response('{}', {
      headers: { 'Content-Type': 'application/json' },
    }));
    vi.stubGlobal('fetch', fetchMock);
    await pipelinesApi.getById(42);
    expect(fetchMock.mock.calls[0][0]).toBe(`${base}/api/pipelines/42`);
    expect(fetchMock.mock.calls[0][1].credentials).toBe('include');
    expect(new URL(getWebSocketUrl()).pathname).toBe(`${base}/ws`);
  });
});

it('defaults to root in development without runtime configuration', () => {
  expect(getBasePath()).toBe('');
  expect(appUrl('/api/me')).toBe('/api/me');
});

it('preserves the path of an explicit WebSocket API override', () => {
  expect(getWebSocketUrl('https://example.com/pipelogiq/api?unused=1#fragment'))
    .toBe('wss://example.com/pipelogiq/ws');
  expect(getWebSocketUrl('http://localhost:8080')).toBe('ws://localhost:8080/ws');
});

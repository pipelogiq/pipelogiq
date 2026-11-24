// Supplied by the container at startup, before the bundled application loads.
// Empty in Vite development and for installations at the domain root.
export function getBasePath(): string {
  return document.querySelector<HTMLMetaElement>('meta[name="pipelogiq-base-path"]')?.content ?? '';
}

export function appUrl(path: string): string {
  return `${getBasePath()}${path}`;
}

export function getWebSocketUrl(apiOverride?: string): string {
  const url = new URL(apiOverride || appUrl('/api'), window.location.origin);
  url.protocol = url.protocol === 'https:' ? 'wss:' : 'ws:';
  url.pathname = url.pathname.replace(/\/api\/?$/, '').replace(/\/$/, '') + '/ws';
  url.search = '';
  url.hash = '';
  return url.toString();
}

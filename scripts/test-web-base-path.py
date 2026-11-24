#!/usr/bin/env python3
"""Exercise the built app image's nginx, assets, proxy and runtime prefix.

Usage: python3 scripts/test-web-base-path.py <locally-built-app-image>
No database or external services are contacted; the API is a local HTTP fixture.
"""
import http.client
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
from urllib.parse import urljoin, urlsplit


def docker(*args, check=True):
    return subprocess.run(['docker', *args], text=True, capture_output=True, check=check)


image = sys.argv[1]
fixture = '''from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
class API(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.headers.get('Upgrade', '').lower() == 'websocket':
            self.send_response(101)
            self.send_header('Upgrade', 'websocket')
            self.send_header('Connection', 'Upgrade')
            self.send_header('X-Backend-Path', self.path)
            self.end_headers()
            return
        self.send_response(200)
        self.send_header('Set-Cookie', 'authKey=fixture; Path=/; HttpOnly; SameSite=Lax')
        self.end_headers()
        self.wfile.write(self.path.encode())
    def log_message(self, *args): pass
ThreadingHTTPServer(('127.0.0.1', 8088), API).serve_forever()
'''

with tempfile.TemporaryDirectory() as directory:
    mock = Path(directory) / 'mock.py'
    mock.write_text(fixture)
    mock.chmod(0o644)
    for base in ('', '/pipelogiq', '/tools/pipelogiq-test'):
        container = docker('run', '-d', '--rm', '-p', '127.0.0.1::8080',
                           '-e', 'PIPELOGIQ_BASE_PATH=' + base,
                           '-v', f'{mock}:/tmp/mock.py:ro', '--entrypoint', 'sh', image,
                           '-ec', 'python3 /tmp/mock.py & /usr/local/bin/configure-web.sh; nginx -t; exec nginx -g "daemon off;"').stdout.strip()
        try:
            info = json.loads(docker('inspect', container).stdout)[0]
            port = int(info['NetworkSettings']['Ports']['8080/tcp'][0]['HostPort'])

            def request(path, headers=None):
                conn = http.client.HTTPConnection('127.0.0.1', port, timeout=3)
                try:
                    conn.request('GET', path, headers=headers or {})
                    response = conn.getresponse()
                    return response.status, dict(response.getheaders()), response.read()
                finally:
                    conn.close()

            for attempt in range(40):
                try:
                    status, headers, html = request(base + '/')
                    if status == 200:
                        break
                except (OSError, http.client.HTTPException):
                    pass
                time.sleep(0.1)
            else:
                raise AssertionError(docker('logs', container, check=False).stdout)
            text = html.decode()
            assert f'<base href="{base}/"' in text, text
            assert f'name="pipelogiq-base-path" content="{base}"' in text
            assert request(base + '/pipelines/42?from=link')[2] == html
            assert request(base + '/assets/missing.js')[0] == 404
            # Resolve exactly as a browser does, including nested routes and favicon.
            for asset in re.findall(r'(?:src|href)="([^\"]+)"', text):
                if asset == base + '/':
                    continue
                path = urlsplit(urljoin('http://example.test' + base + '/', asset)).path
                asset_status, asset_headers, body = request(path)
                assert asset_status == 200 and body != html, path
                if path.endswith('.css'):
                    for font in re.findall(r'url\(([^)]+)\)', body.decode()):
                        if not font.startswith('data:'):
                            font_path = urlsplit(urljoin('http://example.test' + path, font.strip('"\''))).path
                            assert request(font_path)[0] == 200, font_path
            status, headers, body = request(base + '/api/me?test=1')
            assert status == 200 and body == b'/me?test=1'
            assert f'Path={base}/;' in headers['Set-Cookie'], headers
            status, headers, _ = request(base + '/ws?test=1', {'Upgrade': 'websocket', 'Connection': 'Upgrade'})
            assert status == 101 and headers['X-Backend-Path'] == '/ws?test=1', (status, headers)
            if base:
                status, headers, _ = request(base + '?next=42')
                assert status == 308 and headers['Location'] == base + '/?next=42', headers
                for outside in ('/', '/api/me', '/ws', '/wolfgroup', base + '-other/'):
                    assert request(outside)[0] == 404, outside
            # Templates must survive reuse; invalid input must not overwrite configuration.
            for bad in ('/', '/bad/', '/bad;path', '/bad\npath', '/a/../b', '//host'):
                assert docker('exec', '-e', 'PIPELOGIQ_BASE_PATH=' + bad, container,
                              '/usr/local/bin/configure-web.sh', check=False).returncode != 0
            assert request(base + '/')[2] == html
            for new_base in ('/changed', ''):
                docker('exec', '-e', 'PIPELOGIQ_BASE_PATH=' + new_base, container,
                       '/usr/local/bin/configure-web.sh')
                rendered = docker('exec', container, 'cat', '/usr/share/nginx/html/index.html').stdout
                assert f'<base href="{new_base}/"' in rendered
                assert f'name="pipelogiq-base-path" content="{new_base}"' in rendered
                docker('exec', container, 'nginx', '-t')
            print(f'PASS: {base or "/"} assets, deep links, API, cookie path, WebSocket, prefix validation and reconfiguration')
        finally:
            docker('rm', '-f', container, check=False)

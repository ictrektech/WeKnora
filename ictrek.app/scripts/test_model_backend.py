import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import yaml


ROOT = Path(__file__).resolve().parents[2]
ENTRYPOINT = (ROOT / 'scripts/docker-entrypoint.sh').read_text()
TEMPLATE = 'builtin_models:\n' + ENTRYPOINT.split('\nbuiltin_models:\n', 1)[1].split('\nEOF\n', 1)[0]
CONVERTER = ENTRYPOINT.split("<<'PYBACKEND'\n", 1)[1].split('\nPYBACKEND\n', 1)[0]


class ModelBackendTests(unittest.TestCase):
    def render(self, backend, scheduler_available=None, **values):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'models.yaml'
            path.write_text(TEMPLATE)
            env = dict(os.environ, HYBRAG_MODEL_BACKEND=backend, **values)
            prefix = ''
            if scheduler_available is not None:
                prefix = 'import socket\noriginal_getaddrinfo = socket.getaddrinfo\n'
                prefix += 'def resolve(host, *args, **kwargs):\n    if host == "v-scheduler-gateway":\n'
                prefix += '        return []\n' if scheduler_available else '        raise socket.gaierror()\n'
                prefix += '    return original_getaddrinfo(host, *args, **kwargs)\nsocket.getaddrinfo = resolve\n'
            result = subprocess.run([sys.executable, '-', str(path)], input=prefix + CONVERTER, text=True, capture_output=True, env=env)
            return result, yaml.safe_load(path.read_text()), path.stat().st_mode & 0o777

    def legacy_result(self, published):
        observed = []
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                observed.append((self.path, self.headers.get('Authorization')))
                self.send_response(200)
                self.end_headers()
                self.wfile.write(json.dumps({'data': [{'id': model} for model in published]}).encode())
            def log_message(self, *args):
                pass
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            result, config, permissions = self.render('model-hub', HYBRAG_V_ROUTER_URL=f'http://127.0.0.1:{server.server_port}', HYBRAG_V_ROUTER_API_KEY='legacy-key')
            return result, config, permissions, observed
        finally:
            server.shutdown()
            server.server_close()

    def test_legacy_models_use_verified_legacy_router_ids(self):
        result, config, permissions, observed = self.legacy_result(['model-hub/qwen3.5:2b', 'model-hub/bge-m3:latest', 'model-hub/qllama/bge-reranker-v2-m3:q8_0'])
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(permissions, 0o600)
        self.assertEqual(observed, [('/v1/models', 'Bearer legacy-key')])
        models = config['builtin_models']
        self.assertEqual(models[0]['name'], 'model-hub/qwen3.5:2b')
        self.assertEqual(models[2]['name'], 'model-hub/bge-m3:latest')
        self.assertEqual(models[3]['name'], 'model-hub/qllama/bge-reranker-v2-m3:q8_0')
        self.assertTrue(all('model-hub-ollama-' not in model['parameters']['base_url'] for model in models))

    def test_missing_legacy_router_models_do_not_fall_back_to_direct_ollama(self):
        result, _, _, _ = self.legacy_result(['model-hub/qwen3.5:2b'])
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('direct Ollama fallback is disabled', result.stderr)

    def test_scheduler_uses_qualified_ids_unified_gateway_and_explicit_key(self):
        result, config, permissions = self.render('scheduler', HYBRAG_QA_MODEL='qwen3:4b', HYBRAG_V_ROUTER_API_KEY='test-key')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(permissions, 0o600)
        models = config['builtin_models']
        self.assertEqual(models[0]['name'], 'v-scheduler/ollama-qa/qwen3:4b')
        self.assertEqual(models[1]['name'], models[0]['name'])
        self.assertEqual(models[2]['name'], 'v-scheduler/ollama-embedding/bge-m3:latest')
        self.assertEqual(models[3]['name'], 'v-scheduler/ollama-rerank/qllama/bge-reranker-v2-m3:q8_0')
        self.assertTrue(all(model['parameters']['base_url'] == 'http://v-scheduler-gateway:8080/v1' for model in models))
        self.assertTrue(all(model['parameters']['api_key'] == 'test-key' for model in models))

    def test_empty_key_is_not_invalid_placeholder_bearer(self):
        result, config, _ = self.render('scheduler', HYBRAG_V_ROUTER_API_KEY='')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(all(model['parameters']['api_key'] == '' for model in config['builtin_models']))

    def test_unknown_backend_is_rejected(self):
        result, _, _ = self.render('invalid')
        self.assertNotEqual(result.returncode, 0)

    def test_auto_without_local_services_keeps_cloud_only_startup_available(self):
        result, config, _ = self.render('auto', scheduler_available=False, HYBRAG_V_ROUTER_URL='http://127.0.0.1:1')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(config['builtin_models'], [])

    def test_auto_scheduler_does_not_replace_cloud_defaults(self):
        result, config, _ = self.render('auto', scheduler_available=True, HYBRAG_V_ROUTER_URL='http://127.0.0.1:1')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(len(config['builtin_models']), 4)
        self.assertTrue(all(not model['is_default'] for model in config['builtin_models']))

    def test_auto_adds_both_services_with_distinct_model_ids(self):
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200)
                self.end_headers()
                self.wfile.write(json.dumps({'data': [{'id': 'model-hub/qwen3.5:2b'}]}).encode())
            def log_message(self, *args):
                pass
        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            result, config, _ = self.render('auto', scheduler_available=True, HYBRAG_LEGACY_V_ROUTER_URL=f'http://127.0.0.1:{server.server_port}', HYBRAG_LEGACY_V_ROUTER_API_KEY='legacy-only', HYBRAG_V_ROUTER_API_KEY='scheduler-only')
            self.assertEqual(result.returncode, 0, result.stderr)
            models = config['builtin_models']
            self.assertEqual(len(models), 6)
            self.assertEqual(len({model['id'] for model in models}), 6)
            self.assertEqual(sum(model['name'].startswith('model-hub/') for model in models), 2)
            self.assertTrue(all(not model['is_default'] for model in models))
            for model in models:
                legacy = model['name'].startswith('model-hub/')
                self.assertEqual(model['parameters']['api_key'], 'legacy-only' if legacy else 'scheduler-only')
                self.assertEqual(model['parameters']['base_url'], f'http://127.0.0.1:{server.server_port}/v1' if legacy else 'http://v-scheduler-gateway:8080/v1')
        finally:
            server.shutdown()
            server.server_close()


if __name__ == '__main__':
    unittest.main()

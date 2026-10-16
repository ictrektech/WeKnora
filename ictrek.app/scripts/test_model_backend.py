import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import yaml


ROOT = Path(__file__).resolve().parents[2]
ENTRYPOINT = (ROOT / 'scripts/docker-entrypoint.sh').read_text()
TEMPLATE = 'builtin_models:\n' + ENTRYPOINT.split('\nbuiltin_models:\n', 1)[1].split('\nEOF\n', 1)[0]
CONVERTER = ENTRYPOINT.split("<<'PYBACKEND'\n", 1)[1].split('\nPYBACKEND\n', 1)[0]


class ModelBackendTests(unittest.TestCase):
    def render(self, backend, **values):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'models.yaml'
            path.write_text(TEMPLATE)
            env = dict(os.environ, HYBRAG_MODEL_BACKEND=backend, **values)
            result = subprocess.run([sys.executable, '-', str(path)], input=CONVERTER, text=True, capture_output=True, env=env)
            return result, yaml.safe_load(path.read_text()), path.stat().st_mode & 0o777

    def test_legacy_models_remain_unchanged(self):
        result, config, _ = self.render('model-hub')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(config, yaml.safe_load(TEMPLATE))

    def test_scheduler_uses_qualified_ids_unified_gateway_and_explicit_key(self):
        result, config, permissions = self.render('scheduler', HYBRAG_QA_MODEL='qwen3:4b', HYBRAG_V_ROUTER_API_KEY='test-key')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(permissions, 0o600)
        models = config['builtin_models']
        self.assertEqual(models[0]['name'], 'v-scheduler/ollama-qa/qwen3:4b')
        self.assertEqual(models[1]['name'], models[0]['name'])
        self.assertEqual(models[2]['name'], 'v-scheduler/ollama-embedding/bge-m3')
        self.assertEqual(models[3]['name'], 'v-scheduler/ollama-rerank/qllama/bge-reranker-v2-m3:q8_0')
        self.assertTrue(all(model['parameters']['base_url'] == 'http://v-router:8080/v1' for model in models))
        self.assertTrue(all(model['parameters']['api_key'] == 'test-key' for model in models))

    def test_empty_key_is_not_invalid_placeholder_bearer(self):
        result, config, _ = self.render('scheduler', HYBRAG_V_ROUTER_API_KEY='')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(all(model['parameters']['api_key'] == '' for model in config['builtin_models']))

    def test_unknown_backend_is_rejected(self):
        result, _, _ = self.render('invalid')
        self.assertNotEqual(result.returncode, 0)


if __name__ == '__main__':
    unittest.main()

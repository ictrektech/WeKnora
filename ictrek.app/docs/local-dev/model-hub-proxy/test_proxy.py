import http.client
import http.server
import json
import threading
import unittest
from unittest.mock import MagicMock, patch

import proxy


class ProxyTest(unittest.TestCase):
    def test_main_registers_rerank_listener(self):
        with patch.dict(proxy.os.environ, {}, clear=True), patch.object(
            proxy.sys, "argv", ["proxy.py"]
        ), patch.object(proxy, "start_listener") as listen, patch.object(
            proxy.time, "sleep", side_effect=KeyboardInterrupt
        ):
            proxy.main()
        self.assertEqual(listen.call_args_list[-1].args,
                         ("rerank", 11537, ("model-hub-ollama-rerank", 11434)))

    def test_rerank_request_and_response_are_preserved(self):
        received = []
        result = {"results": [{"index": 0, "relevance_score": 0.9}]}

        class Handler(http.server.BaseHTTPRequestHandler):
            def do_POST(self):
                received.append((self.path, json.loads(
                    self.rfile.read(int(self.headers["Content-Length"])))))
                body = json.dumps(result).encode()
                self.send_response(200)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

            def log_message(self, *args):
                pass

        upstream = http.server.HTTPServer(("127.0.0.1", 0), Handler)
        threading.Thread(target=upstream.serve_forever, daemon=True).start()
        forwarder = proxy.start_listener("rerank", 0, upstream.server_address)
        connection = http.client.HTTPConnection("127.0.0.1", forwarder.server_address[1], timeout=3)
        body = {"model": "qllama/bge-reranker-v2-m3:q8_0",
                "query": "知识库检索", "documents": ["知识库检索流程", "天气"]}
        try:
            connection.request("POST", "/v1/rerank", json.dumps(body),
                               {"Content-Type": "application/json"})
            response = connection.getresponse()
            self.assertEqual(response.status, 200)
            self.assertEqual(json.loads(response.read()), result)
            self.assertEqual(received, [("/v1/rerank", body)])
        finally:
            connection.close()
            forwarder.shutdown()
            forwarder.server_close()
            upstream.shutdown()
            upstream.server_close()

    def test_health_fails_when_rerank_is_unavailable(self):
        with patch.dict(proxy.os.environ, {}, clear=True), patch.object(
            proxy.socket, "create_connection",
            side_effect=[MagicMock(), MagicMock(), OSError("rerank offline")]
        ) as connect:
            self.assertEqual(proxy.check_upstreams(), 1)
            self.assertEqual(connect.call_args.args[0], ("model-hub-ollama-rerank", 11434))


if __name__ == "__main__":
    unittest.main()

#!/usr/bin/env python3
"""Forward local-dev ports to the VOS Model Hub Gateway services.

The local-dev Go process runs on the host, while Model Hub exposes its Ollama
Gateway on the shared Docker network. This small byte-for-byte TCP forwarder
keeps streaming responses intact and exposes the Gateway only through the
host's loopback-published ports.
"""

from __future__ import annotations

import os
import select
import socket
import socketserver
import sys
import threading
import time
from typing import Final


BUFFER_SIZE: Final = 64 * 1024


def parse_endpoint(raw: str) -> tuple[str, int]:
    host, separator, port = raw.rpartition(":")
    if not separator or not host or not port:
        raise ValueError(f"invalid upstream endpoint: {raw!r}")
    return host, int(port)


class ProxyServer(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True

    def __init__(self, listen_port: int, upstream: tuple[str, int]) -> None:
        self.upstream = upstream
        super().__init__(("0.0.0.0", listen_port), ProxyHandler)


class ProxyHandler(socketserver.BaseRequestHandler):
    def handle(self) -> None:
        try:
            upstream = socket.create_connection(
                self.server.upstream, timeout=10
            )  # type: ignore[attr-defined]
        except OSError:
            return

        with upstream:
            self.request.settimeout(None)
            upstream.settimeout(None)
            sockets = [self.request, upstream]
            while True:
                try:
                    readable, _, exceptional = select.select(
                        sockets, [], sockets, 60
                    )
                except (OSError, ValueError):
                    return
                if exceptional:
                    return
                if not readable:
                    continue
                for source in readable:
                    try:
                        data = source.recv(BUFFER_SIZE)
                    except OSError:
                        return
                    if not data:
                        return
                    target = upstream if source is self.request else self.request
                    try:
                        target.sendall(data)
                    except OSError:
                        return


def start_listener(
    name: str, listen_port: int, upstream: tuple[str, int]
) -> ProxyServer:
    server = ProxyServer(listen_port, upstream)
    thread = threading.Thread(
        target=server.serve_forever,
        name=name,
        daemon=True,
    )
    thread.start()
    print(
        f"{name}: 0.0.0.0:{listen_port} -> "
        f"{upstream[0]}:{upstream[1]}",
        flush=True,
    )
    return server


def check_upstreams() -> int:
    endpoints = (
        os.environ.get(
            "MODEL_HUB_QA_UPSTREAM",
            "model-hub-ollama-qa:11535",
        ),
        os.environ.get(
            "MODEL_HUB_EMBEDDING_UPSTREAM",
            "model-hub-ollama-embedding:11535",
        ),
    )
    try:
        for raw in endpoints:
            with socket.create_connection(parse_endpoint(raw), timeout=3):
                pass
    except (OSError, ValueError):
        return 1
    return 0


def main() -> None:
    if len(sys.argv) == 2 and sys.argv[1] == "--health":
        raise SystemExit(check_upstreams())

    qa_upstream = parse_endpoint(
        os.environ.get(
            "MODEL_HUB_QA_UPSTREAM",
            "model-hub-ollama-qa:11535",
        )
    )
    embedding_upstream = parse_endpoint(
        os.environ.get(
            "MODEL_HUB_EMBEDDING_UPSTREAM",
            "model-hub-ollama-embedding:11535",
        )
    )
    qa_port = int(
        os.environ.get("MODEL_HUB_QA_LISTEN_PORT", "11535")
    )
    embedding_port = int(
        os.environ.get(
            "MODEL_HUB_EMBEDDING_LISTEN_PORT",
            "11536",
        )
    )

    servers = [
        start_listener("qa", qa_port, qa_upstream),
        start_listener("embedding", embedding_port, embedding_upstream),
    ]
    try:
        while True:
            time.sleep(3600)
    except KeyboardInterrupt:
        pass
    finally:
        for server in servers:
            server.shutdown()
            server.server_close()


if __name__ == "__main__":
    main()

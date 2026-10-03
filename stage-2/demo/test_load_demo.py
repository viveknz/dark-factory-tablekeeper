#!/usr/bin/env python3
"""Automated test for load_demo.py: standard library only, no outbound network.

Starts a disposable in-process HTTP server that stands in for the real /_test/reset endpoint,
points load_demo.py at it, and checks the posted payload. Also checks that load_demo.py fails
loudly (non-zero exit, message on stderr) against an unreachable server.

Run with: python demo/test_load_demo.py
"""
import json
import subprocess
import sys
import threading
import os
from http.server import BaseHTTPRequestHandler, HTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
LOAD_DEMO = os.path.join(HERE, "load_demo.py")

received = {}


class RecordingHandler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        pass

    def do_POST(self):
        if self.path != "/_test/reset":
            self.send_response(404)
            self.end_headers()
            return
        length = int(self.headers.get("Content-Length", 0))
        body = self.rfile.read(length)
        received["payload"] = json.loads(body)
        self.send_response(204)
        self.end_headers()


def run_server():
    server = HTTPServer(("127.0.0.1", 0), RecordingHandler)
    port = server.server_address[1]
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, port


def test_happy_path():
    server, port = run_server()
    try:
        base_url = f"http://127.0.0.1:{port}"
        result = subprocess.run([sys.executable, LOAD_DEMO, base_url], capture_output=True, text=True, timeout=30)
        assert result.returncode == 0, f"expected exit 0, got {result.returncode}\nstdout={result.stdout}\nstderr={result.stderr}"
        assert "payload" in received, "server never received a POST /_test/reset"
        payload = received["payload"]
        assert len(payload.get("users", [])) == 2, f"expected 2 demo users, got {payload.get('users')}"
        assert len(payload.get("restaurants", [])) == 2, f"expected 2 demo restaurants, got {payload.get('restaurants')}"
        reservations = payload.get("reservations", [])
        assert len(reservations) == 5, f"expected 5 demo reservations, got {len(reservations)}"
        combo = [r for r in reservations if r.get("table_ids")]
        assert len(combo) == 1, f"expected exactly one combined-table demo reservation, got {combo}"
        assert combo[0]["table_ids"] == ["t_window_1", "t_window_2"]
        emails = {u["email"] for u in payload["users"]}
        assert emails == {"diner@df-demo.example", "manager@df-demo.example"}, emails
        print("test_happy_path: OK")
    finally:
        server.shutdown()


def test_unreachable_server_fails_loudly():
    # Port 1 is reserved/unlikely to be listening; connection should be refused immediately.
    result = subprocess.run([sys.executable, LOAD_DEMO, "http://127.0.0.1:1"], capture_output=True, text=True, timeout=30)
    assert result.returncode != 0, f"expected non-zero exit against an unreachable server, got 0\nstdout={result.stdout}"
    assert "FAILED" in result.stderr, f"expected a clear failure message on stderr, got: {result.stderr!r}"
    print("test_unreachable_server_fails_loudly: OK")


if __name__ == "__main__":
    test_happy_path()
    test_unreachable_server_fails_loudly()
    print("All load_demo.py tests passed.")

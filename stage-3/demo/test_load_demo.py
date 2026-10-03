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
    """Implements only /_test/reset -- everything else 404s, so load_demo.py's series-adoption
    step (login, list, adopt) must fail gracefully without aborting the rest of the load."""

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

    def do_GET(self):
        self.send_response(404)
        self.end_headers()


class FullDemoHandler(BaseHTTPRequestHandler):
    """Implements the full sequence load_demo.py drives: reset, login, list, adopt a series."""

    def log_message(self, fmt, *args):
        pass

    def _read_json(self):
        length = int(self.headers.get("Content-Length", 0))
        return json.loads(self.rfile.read(length)) if length else {}

    def _reply(self, status, body=None):
        self.send_response(status)
        if body is not None:
            self.send_header("Content-Type", "application/json")
        self.end_headers()
        if body is not None:
            self.wfile.write(json.dumps(body).encode("utf-8"))

    def do_POST(self):
        if self.path == "/_test/reset":
            received["payload"] = self._read_json()
            self._reply(204)
        elif self.path == "/auth/login":
            self._reply(200, {"token": "faketoken", "display_name": "Demo Diner"})
        elif self.path == "/series":
            self._reply(201, {"series_id": "ser_fake", "revision": 1, "interval_weeks": 1, "occurrences": []})
        else:
            self._reply(404)

    def do_GET(self):
        if self.path == "/reservations":
            self._reply(200, {"reservations": [
                {"reference": "FAKEWIN01", "restaurant_id": "r_harbour_table", "table_id": "t_window_1"},
            ]})
        else:
            self._reply(404)


def run_server(handler_cls):
    server = HTTPServer(("127.0.0.1", 0), handler_cls)
    port = server.server_address[1]
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    return server, port


def test_happy_path():
    server, port = run_server(RecordingHandler)
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
        # The series-adoption step has nothing to adopt against (login/list/series all 404
        # here), so it must log a warning and still exit 0 -- the rest of the demo load is
        # unaffected.
        assert "adopt-series" in result.stdout and "WARNING" in result.stdout, \
            f"expected a graceful adopt-series warning, got stdout={result.stdout!r}"
        print("test_happy_path: OK")
    finally:
        server.shutdown()


def test_series_adoption_succeeds_against_a_full_server():
    server, port = run_server(FullDemoHandler)
    try:
        base_url = f"http://127.0.0.1:{port}"
        result = subprocess.run([sys.executable, LOAD_DEMO, base_url], capture_output=True, text=True, timeout=30)
        assert result.returncode == 0, f"expected exit 0, got {result.returncode}\nstdout={result.stdout}\nstderr={result.stderr}"
        assert "adopted FAKEWIN01" in result.stdout, f"expected a successful adoption log line, got: {result.stdout!r}"
        assert "WARNING" not in result.stdout, f"did not expect a warning on the success path: {result.stdout!r}"
        print("test_series_adoption_succeeds_against_a_full_server: OK")
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
    test_series_adoption_succeeds_against_a_full_server()
    test_unreachable_server_fails_loudly()
    print("All load_demo.py tests passed.")

#!/usr/bin/env python3
"""Verify the capacity reporter against a disposable 10-host HTTP fixture."""
import argparse
import importlib.util
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
import sys
from threading import Thread


def main():
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location(
        "capacity", Path(__file__).with_name("measure-capacity.py")
    )
    capacity = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(capacity)
    capacity.getpass = lambda _: "fixture-only-password"
    hosts = [
        {"id": f"h_{index:032x}", "transport": "ssh" if index < 5 else "agent"}
        for index in range(10)
    ]
    host_ids = {host["id"] for host in hosts}
    stale_host = hosts[-1]["id"]
    state = {"stale": False, "logouts": 0}

    class Fixture(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def respond(self, code, payload=None, cookie=False):
            self.send_response(code)
            if cookie:
                self.send_header("Set-Cookie", "fixture_session=present; Path=/; HttpOnly")
            data = json.dumps(payload).encode() if payload is not None else b""
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            if data:
                self.wfile.write(data)

        def do_POST(self):
            origin = f"http://127.0.0.1:{self.server.server_port}"
            if self.headers.get("Origin") != origin:
                return self.respond(403)
            if self.path == "/api/v1/auth/login":
                data = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
                if data["password"] == "fixture-only-password":
                    return self.respond(200, {"csrfToken": "fixture"}, cookie=True)
                return self.respond(401)
            if (
                self.path == "/api/v1/auth/logout"
                and self.headers.get("X-CSRF-Token") == "fixture"
                and self.headers.get("Cookie") == "fixture_session=present"
            ):
                state["logouts"] += 1
                return self.respond(204)
            self.respond(404)

        def do_GET(self):
            if self.headers.get("Cookie") != "fixture_session=present":
                return self.respond(401)
            if self.path == "/api/v1/hosts":
                return self.respond(200, {"hosts": hosts})
            if self.path.startswith("/api/v1/hosts/") and self.path.endswith("/inventory"):
                host = self.path.split("/")[-2]
                if host in host_ids:
                    return self.respond(200, {
                        "hostId": host,
                        "sessions": [{}] * 5,
                        "stale": state["stale"] and host == stale_host,
                    })
            self.respond(404)

    with ThreadingHTTPServer(("127.0.0.1", 0), Fixture) as server:
        worker = Thread(target=server.serve_forever)
        worker.start()
        try:
            args = argparse.Namespace(
                origin=f"http://127.0.0.1:{server.server_port}",
                username="fixture",
                rounds=20,
                workers=2,
            )
            idle = capacity.run(args)
            assert idle["complete_rounds"] == 20
            assert idle["sessions_per_complete_round"] == [50]
            assert idle["inventory_by_transport"]["ssh"]["outcomes"] == {"200": 100}
            assert idle["inventory_by_transport"]["agent"]["outcomes"] == {"200": 100}
            state["stale"], args.rounds = True, 2
            stale = capacity.run(args)
            assert stale["complete_rounds"] == 0
            assert stale["inventory"]["outcomes"] == {"200": 18, "stale": 2}
            assert state["logouts"] == 2
            print(json.dumps({"fixture": "synthetic HTTP", "idle": idle, "stale": stale}))
        finally:
            server.shutdown()
            worker.join()


if __name__ == "__main__":
    main()

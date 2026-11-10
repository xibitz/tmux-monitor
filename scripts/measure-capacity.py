#!/usr/bin/env python3
"""Measure the read-only 10-host capacity fixture and print aggregate JSON."""
import argparse
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
from getpass import getpass
from http.cookiejar import CookieJar
import json
import math
import re
import sys
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import (
    HTTPRedirectHandler,
    HTTPCookieProcessor,
    ProxyHandler,
    Request,
    build_opener,
)

MAX_RESPONSE = 8 * 1024 * 1024
HOST_ID = re.compile(r"h_[a-f0-9]{32}")


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def exact_origin(value):
    url = urlsplit(value)
    if (
        url.username is not None
        or url.password is not None
        or not url.hostname
        or url.path not in ("", "/")
        or "?" in value
        or "#" in value
        or not (
            (url.scheme == "https" and url.hostname.endswith(".ts.net"))
            or (url.scheme == "http" and url.hostname == "127.0.0.1")
        )
    ):
        raise argparse.ArgumentTypeError("use an HTTPS Tailnet origin or the loopback development origin")
    return f"{url.scheme}://{url.netloc}"


def fetch(opener, origin, path, method="GET", payload=None, csrf=None):
    headers = {}
    if method != "GET":
        headers["Origin"] = origin
    if csrf is not None:
        headers["X-CSRF-Token"] = csrf
    body = None
    if payload is not None:
        headers["Content-Type"] = "application/json"
        body = json.dumps(payload).encode()

    started = time.perf_counter()
    try:
        request = Request(origin + "/api/v1" + path, data=body, method=method, headers=headers)
        with opener.open(request, timeout=15) as response:
            data = response.read(MAX_RESPONSE + 1)
            if len(data) > MAX_RESPONSE:
                return "response_limit", time.perf_counter() - started, None
            result = json.loads(data) if data else None
            return str(response.status), time.perf_counter() - started, result
    except HTTPError as err:
        err.close()
        return str(err.code), time.perf_counter() - started, None
    except (URLError, TimeoutError, OSError):
        return "transport_error", time.perf_counter() - started, None
    except (ValueError, UnicodeError):
        return "invalid_response", time.perf_counter() - started, None


def measure_hosts(opener, origin):
    code, elapsed, payload = fetch(opener, origin, "/hosts")
    if code == "200" and not (isinstance(payload, dict) and isinstance(payload.get("hosts"), list)):
        return "invalid_response", elapsed, None
    return code, elapsed, payload


def measure_inventory(opener, origin, host_id):
    code, elapsed, payload = fetch(opener, origin, f"/hosts/{host_id}/inventory")
    if code == "200":
        if not (
            isinstance(payload, dict)
            and payload.get("hostId") == host_id
            and isinstance(payload.get("sessions"), list)
            and isinstance(payload.get("stale"), bool)
        ):
            code = "invalid_response"
        elif payload["stale"]:
            code = "stale"
    return code, elapsed, len(payload["sessions"]) if code == "200" else 0


def summary(samples):
    counts = Counter(code for code, _, _ in samples)
    elapsed = sorted(duration for code, duration, _ in samples if code == "200")
    index = math.ceil(len(elapsed) * 0.95) - 1
    return {
        "requests": len(samples),
        "outcomes": dict(sorted(counts.items())),
        "p95_success_ms": round(elapsed[index] * 1000, 2) if elapsed else None,
    }


def run(args):
    opener = build_opener(ProxyHandler({}), HTTPCookieProcessor(CookieJar()), NoRedirect())
    code, _, auth = fetch(opener, args.origin, "/auth/login", "POST", {
        "username": args.username,
        "password": getpass("Lab password: "),
    })
    if code != "200" or not isinstance(auth, dict) or not isinstance(auth.get("csrfToken"), str):
        raise ValueError(f"lab login failed ({code})")

    try:
        code, _, payload = measure_hosts(opener, args.origin)
        if code != "200":
            raise ValueError(f"cannot read lab host list ({code})")
        hosts = payload["hosts"]
        if len(hosts) != 10 or any(
            not isinstance(host, dict) or not isinstance(host.get("id"), str)
            or not HOST_ID.fullmatch(host["id"]) or host.get("transport") not in ("ssh", "agent")
            for host in hosts
        ) or len({host["id"] for host in hosts}) != 10:
            raise ValueError("the lab must have exactly 10 distinct SSH or Agent profiles")

        host_samples, session_counts = [], []
        inventory_samples = {"ssh": [], "agent": []}
        with ThreadPoolExecutor(max_workers=args.workers) as pool:
            for _ in range(args.rounds):
                code, elapsed, _ = measure_hosts(opener, args.origin)
                host_samples.append((code, elapsed, None))
                pending = [
                    pool.submit(measure_inventory, opener, args.origin, host["id"])
                    for host in hosts
                ]
                count = 0
                complete = True
                for host, result in zip(hosts, pending):
                    code, elapsed, sessions = result.result()
                    inventory_samples[host["transport"]].append((code, elapsed, None))
                    if code == "200":
                        count += sessions
                    else:
                        complete = False
                if complete:
                    session_counts.append(count)

        return {
            "rounds": args.rounds,
            "workers": args.workers,
            "hosts_by_transport": dict(sorted(Counter(host["transport"] for host in hosts).items())),
            "complete_rounds": len(session_counts),
            "sessions_per_complete_round": sorted(set(session_counts)),
            "host_list": summary(host_samples),
            "inventory": summary(inventory_samples["ssh"] + inventory_samples["agent"]),
            "inventory_by_transport": {
                transport: summary(samples) for transport, samples in inventory_samples.items() if samples
            },
        }
    finally:
        fetch(opener, args.origin, "/auth/logout", "POST", csrf=auth["csrfToken"])


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--origin", required=True, type=exact_origin)
    parser.add_argument("--username", required=True)
    parser.add_argument("--rounds", default=20, type=int)
    parser.add_argument("--workers", default=2, type=int, choices=range(1, 5))
    args = parser.parse_args()
    if not 1 <= args.rounds <= 100:
        parser.error("--rounds must be between 1 and 100")
    if not sys.stdin.isatty():
        parser.error("run from an interactive terminal so the password prompt stays private")
    try:
        print(json.dumps(run(args), indent=2))
    except ValueError as err:
        parser.exit(1, str(err) + "\n")


if __name__ == "__main__":
    main()

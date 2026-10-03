#!/usr/bin/env python3
"""Load Tablekeeper's demo data into a running stage-2 service.

Standard library only. Posts demo/fixture.json plus a handful of confirmed reservations on
dates computed from the day this script runs (so the demo never goes stale) to the service's
own POST /_test/reset. See demo/DEMO.md for the exact commands and both demo logins.

Usage:
    python demo/load_demo.py [base_url]

base_url defaults to http://localhost:8080, or the TABLEKEEPER_BASE_URL environment variable.
"""
import json
import os
import sys
import urllib.error
import urllib.request
from datetime import datetime, timedelta, timezone


def log(step, msg):
    print(f"[load_demo] {step}: {msg}", flush=True)


def fail(step, msg):
    print(f"[load_demo] FAILED at {step}: {msg}", file=sys.stderr, flush=True)
    sys.exit(1)


def future_day(offset_days):
    today = datetime.now(timezone.utc)
    return (today + timedelta(days=offset_days)).strftime("%Y-%m-%d")


def build_demo_reservations():
    # Both demo restaurants are open every day of the week, so any future date works; only the
    # table/time/party-size combinations need to respect each restaurant's capacity and hours.
    return [
        {
            "id": "res_demo_1", "user_id": "u_demo_diner", "restaurant_id": "r_harbour_table",
            "table_id": "t_window_1", "party_size": 2, "status": "confirmed",
            "starts_at_local": f"{future_day(1)}T18:00",
        },
        {
            "id": "res_demo_2", "user_id": "u_demo_manager", "restaurant_id": "r_harbour_table",
            "table_ids": ["t_window_1", "t_window_2"], "party_size": 4, "status": "confirmed",
            "starts_at_local": f"{future_day(3)}T19:00",
        },
        {
            "id": "res_demo_3", "user_id": "u_demo_diner", "restaurant_id": "r_harbour_table",
            "table_id": "t_long_table", "party_size": 5, "status": "confirmed",
            "starts_at_local": f"{future_day(7)}T18:30",
        },
        {
            "id": "res_demo_4", "user_id": "u_demo_manager", "restaurant_id": "r_lantern_noodle_bar",
            "table_id": "t_counter_1", "party_size": 2, "status": "confirmed",
            "starts_at_local": f"{future_day(1)}T18:00",
        },
        {
            "id": "res_demo_5", "user_id": "u_demo_diner", "restaurant_id": "r_lantern_noodle_bar",
            "table_id": "t_family", "party_size": 6, "status": "confirmed",
            "starts_at_local": f"{future_day(5)}T18:30",
        },
    ]


def load_fixture(fixture_path):
    log("read-fixture", fixture_path)
    try:
        with open(fixture_path, "r", encoding="utf-8") as f:
            return json.load(f)
    except OSError as e:
        fail("read-fixture", f"could not read {fixture_path}: {e}")
    except json.JSONDecodeError as e:
        fail("read-fixture", f"{fixture_path} is not valid JSON: {e}")


def post_reset(base_url, payload):
    url = base_url.rstrip("/") + "/_test/reset"
    body = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(url, data=body, method="POST",
                                  headers={"Content-Type": "application/json"})
    log("post-reset", url)
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            status = resp.status
    except urllib.error.HTTPError as e:
        fail("post-reset", f"{url} returned {e.code}: {e.read().decode('utf-8', 'replace')}")
    except urllib.error.URLError as e:
        fail("post-reset", f"could not reach {url}: {e.reason}")
    if status != 204:
        fail("post-reset", f"expected 204 from {url}, got {status}")
    log("post-reset", f"{status} No Content")


def main():
    base_url = sys.argv[1] if len(sys.argv) > 1 else os.environ.get("TABLEKEEPER_BASE_URL", "http://localhost:8080")
    fixture_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "fixture.json")

    fixture = load_fixture(fixture_path)
    reservations = build_demo_reservations()
    payload = dict(fixture)
    payload["reservations"] = reservations

    post_reset(base_url, payload)

    log("summary", f"{len(fixture.get('users', []))} users, {len(fixture.get('restaurants', []))} restaurants, {len(reservations)} reservations loaded into {base_url}")
    print()
    print("Demo logins:")
    for user in fixture.get("users", []):
        print(f"  {user['email']} / {user['password']}  ({user['display_name']})")
    print()
    print(f"Try it: curl {base_url.rstrip('/')}/restaurants")


if __name__ == "__main__":
    main()

"""Exercise a built example image using isolated containers and a temporary volume.

Usage: python3 examples/python-container/smoke_test.py [image]
Requires Docker, Python 3, and amd64 support (native or emulated).
"""
import json
import subprocess
import sys
import time
import urllib.error
import urllib.request
import uuid

IMAGE = sys.argv[1] if len(sys.argv) > 1 else "drillip-python-example"
NAME = "drillip-example-test-" + uuid.uuid4().hex[:12]
VOLUME = NAME + "-data"


def docker(*args, check=True):
    return subprocess.run(["docker", *args], check=check, capture_output=True, text=True)


def wait_for(description, predicate, timeout=40):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            print(description + ": passed", flush=True)
            return
        time.sleep(0.25)
    raise AssertionError("Timed out: " + description)


def health():
    return docker("inspect", "--format", "{{.State.Health.Status}}", NAME).stdout.strip()


def start(*extra):
    docker("run", "--detach", "--platform", "linux/amd64", "--name", NAME,
           "--publish", "127.0.0.1::8000", "--mount", f"type=volume,source={VOLUME},target=/var/lib/drillip",
           *extra, IMAGE)
    address = docker("port", NAME, "8000/tcp").stdout.strip()
    return "http://" + address


def request(base, path):
    try:
        response = urllib.request.urlopen(base + path, timeout=3)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        return response.status, response.read()


def summaries():
    result = docker("exec", NAME, "python", "-c",
                    "import urllib.request; print(urllib.request.urlopen('http://127.0.0.1:8300/api/0/top/', timeout=2).read().decode())")
    return json.loads(result.stdout)


def event_count():
    return next((row["count"] for row in summaries() if row["value"] == "Example checkout failed"), 0)


def control(*args):
    return docker("exec", NAME, "supervisorctl", "-c", "/app/supervisord.conf", *args).stdout.strip()


def pid(program):
    return int(control("pid", program))


def crash_and_recover(program):
    previous = pid(program)
    assert previous > 0
    docker("exec", NAME, "python", "-c", "import os,signal,sys; os.kill(int(sys.argv[1]),signal.SIGKILL)", str(previous))
    wait_for(program + " restarts after a crash", lambda: pid(program) not in (0, previous))
    wait_for("healthy after " + program + " restart", lambda: health() == "healthy")


docker("volume", "create", VOLUME)
try:
    base = start()
    wait_for("initial health", lambda: health() == "healthy")
    assert request(base, "/health") == (200, b"ok")
    assert docker("exec", NAME, "python", "-c", "import os; print(os.getuid())").stdout.strip() == "10001"
    assert not docker("port", NAME, "8300/tcp", check=False).stdout.strip()
    for count in (1, 2):
        status, body = request(base, "/fail")
        assert status == 500 and json.loads(body)["event_id"]
        wait_for("SDK event " + str(count), lambda: event_count() == count)
    assert "Example checkout failed" in docker("exec", NAME, "drillip", "top").stdout
    crash_and_recover("drillip")
    crash_and_recover("app")

    application_pid = pid("app")
    control("stop", "drillip")
    wait_for("tracker outage marks container unhealthy", lambda: health() == "unhealthy")
    assert request(base, "/health") == (200, b"ok")
    assert pid("app") == application_pid
    control("start", "drillip")
    wait_for("tracker recovery", lambda: health() == "healthy")
    assert pid("app") == application_pid

    # Stop immediately after capture; the application must drain before Drillip exits.
    assert request(base, "/fail")[0] == 500
    docker("stop", "--time", "40", NAME)
    assert docker("inspect", "--format", "{{.State.ExitCode}}", NAME).stdout.strip() == "0"
    logs = docker("logs", NAME).stdout
    assert "Application stopped after draining Sentry events" in logs
    assert 0 <= logs.rfind("stopped: app") < logs.rfind("stopped: drillip")
    docker("rm", NAME)
    base = start()
    wait_for("recreated container health", lambda: health() == "healthy")
    assert event_count() == 3
    print("SDK drain, ordered shutdown, and volume persistence: passed", flush=True)
    docker("stop", "--time", "40", NAME)
    docker("rm", NAME)

    # A persistently broken tracker must not prevent the application from starting.
    base = start("--env", "DRILLIP_DB=/unwritable/errors.db")
    def app_available():
        try:
            return request(base, "/health") == (200, b"ok")
        except (OSError, urllib.error.URLError):
            return False
    wait_for("application starts despite tracker startup failure", app_available)
    wait_for("failed tracker is visible in health", lambda: health() == "unhealthy")
    wait_for("failed tracker reaches FATAL", lambda: "FATAL" in docker(
        "exec", NAME, "supervisorctl", "-c", "/app/supervisord.conf", "status", "drillip", check=False).stdout)
    logs = docker("logs", NAME)
    assert "degraded error reporting" in logs.stdout + logs.stderr
except Exception:
    logs = docker("logs", NAME, check=False)
    print(logs.stdout + logs.stderr, file=sys.stderr)
    raise
finally:
    docker("stop", "--time", "40", NAME, check=False)
    docker("rm", "--force", NAME, check=False)
    docker("volume", "rm", VOLUME, check=False)

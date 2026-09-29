"""Check the packaged Compose lifecycle with an isolated project and volume."""
import json
import os
from pathlib import Path
import subprocess
import time
import urllib.error
import urllib.request
import uuid

PROJECT = "drillip-compose-test-" + uuid.uuid4().hex[:12]
FILE = Path(__file__).with_name("compose.yaml")
ENV = {**os.environ, "APP_PORT": "0"}


def run(*args, check=True):
    return subprocess.run(args, env=ENV, check=check, capture_output=True, text=True)


def compose(*args, check=True):
    return run("docker", "compose", "--file", str(FILE), "--project-name", PROJECT,
               *args, check=check)


def start():
    compose("up", "--build", "--wait", "--wait-timeout", "60")
    containers = compose("ps", "--quiet").stdout.split()
    assert len(containers) == 1
    container = containers[0]
    assert run("docker", "inspect", "--format", "{{.Config.StopTimeout}}", container).stdout.strip() == "40"
    compose("exec", "--no-TTY", "app", "drillip", "health")
    return container


def count():
    result = compose("exec", "--no-TTY", "app", "python", "-c",
                     "import urllib.request; print(urllib.request.urlopen('http://127.0.0.1:8300/api/0/top/', timeout=2).read().decode())")
    rows = json.loads(result.stdout)
    return next((row["count"] for row in rows if row["value"] == "Example checkout failed"), 0)


try:
    config = json.loads(compose("config", "--format", "json").stdout)
    assert list(config["services"]) == ["app"]
    container = start()
    address = compose("port", "app", "8000").stdout.strip()
    try:
        urllib.request.urlopen("http://" + address + "/fail", timeout=3)
        raise AssertionError("Expected the tutorial's deliberate HTTP 500")
    except urllib.error.HTTPError as response:
        with response:
            assert response.code == 500 and json.loads(response.read())["event_id"]
    # The plain Compose stop command must allow the SDK to drain before Drillip exits.
    compose("stop")
    logs = compose("logs", "--no-color").stdout
    assert "Application stopped after draining Sentry events" in logs
    assert 0 <= logs.rfind("stopped: app") < logs.rfind("stopped: drillip")
    assert run("docker", "inspect", "--format", "{{.State.ExitCode}}", container).stdout.strip() == "0"
    compose("down")
    start()
    deadline = time.monotonic() + 10
    while count() != 1:
        assert time.monotonic() < deadline, "Recorded error did not survive Compose down/up"
        time.sleep(0.25)
    print("Compose: one container, stop grace period, SDK drain, and volume persistence passed")
except Exception:
    logs = compose("logs", "--no-color", check=False)
    print(logs.stdout + logs.stderr)
    raise
finally:
    compose("down", "--volumes", "--rmi", "local", check=False)

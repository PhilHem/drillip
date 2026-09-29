"""Check application availability by default, or diagnose Drillip separately."""
import argparse
import os
import urllib.error
import urllib.request

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("target", nargs="?", choices=("app", "drillip"), default="app")
target = parser.parse_args().target
url = ("http://127.0.0.1:8000/health" if target == "app" else
       "http://" + os.environ["DRILLIP_ADDR"] + "/-/healthy")
try:
    with urllib.request.urlopen(url, timeout=1) as response:
        if response.status != 200 or response.read() != b"ok":
            raise SystemExit(target + ": unavailable")
except (OSError, urllib.error.URLError):
    raise SystemExit(target + ": unavailable")
print(target + ": ok")

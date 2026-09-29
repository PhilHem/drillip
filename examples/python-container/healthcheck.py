"""Report application or error-tracker failure without terminating either process."""
import os
import urllib.request

for url in ("http://127.0.0.1:8000/health",
            "http://" + os.environ["DRILLIP_ADDR"] + "/-/healthy"):
    with urllib.request.urlopen(url, timeout=1) as response:
        if response.status != 200 or response.read() != b"ok":
            raise SystemExit("Unhealthy: " + url)

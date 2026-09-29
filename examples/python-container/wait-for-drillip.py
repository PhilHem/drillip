"""Allow a short startup window, then run the application even without Drillip."""
import os
import sys
import time
import urllib.error
import urllib.request

deadline = time.monotonic() + 10
url = "http://" + os.environ["DRILLIP_ADDR"] + "/-/healthy"
while time.monotonic() < deadline:
    try:
        with urllib.request.urlopen(url, timeout=1) as response:
            if response.read() == b"ok":
                break
    except (OSError, urllib.error.URLError):
        pass
    time.sleep(0.1)
else:
    print("Drillip is unavailable; starting the application with degraded error reporting.",
          file=sys.stderr, flush=True)

os.execvp(sys.argv[1], sys.argv[1:])

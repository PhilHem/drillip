"""Allow a short startup window, then run the application even without Drillip."""
import os
import sys
import time
import subprocess

deadline = time.monotonic() + 10
while time.monotonic() < deadline:
    try:
        result = subprocess.run(["drillip", "health"], timeout=1,
                                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if result.returncode == 0:
            break
    except subprocess.TimeoutExpired:
        pass
    time.sleep(0.1)
else:
    print("Drillip is unavailable; starting the application with degraded error reporting.",
          file=sys.stderr, flush=True)

os.execvp(sys.argv[1], sys.argv[1:])

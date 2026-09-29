"""Report whether the application can serve requests."""
import urllib.error
import urllib.request

try:
    with urllib.request.urlopen("http://127.0.0.1:8000/health", timeout=1) as response:
        if response.status != 200 or response.read() != b"ok":
            raise SystemExit("app: unavailable")
except (OSError, urllib.error.URLError):
    raise SystemExit("app: unavailable")
print("app: ok")

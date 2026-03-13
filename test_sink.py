"""Send realistic errors to error-sink via the Sentry Python SDK."""
import sentry_sdk
import time

sentry_sdk.init(
    dsn="http://testkey@127.0.0.1:8301/1",
    release="v1.2.0",
    environment="staging",
    traces_sample_rate=0,  # no performance tracing, just errors
)
sentry_sdk.set_user({"id": "42", "email": "alice@example.com", "username": "alice"})


def parse_config(path):
    """Simulate a config parsing error."""
    raise ValueError(f"invalid YAML at line 12 in {path}")


def fetch_data(url):
    """Simulate a network timeout."""
    raise ConnectionError(f"timeout after 30s connecting to {url}")


def process_order(order_id):
    """Simulate a business logic error."""
    items = None
    return items[0]  # TypeError


# Error 1: config parsing
try:
    parse_config("/etc/myapp/config.yml")
except ValueError:
    sentry_sdk.capture_exception()

# Error 2: same config error again (should increment count)
try:
    parse_config("/etc/myapp/config.yml")
except ValueError:
    sentry_sdk.capture_exception()

# Error 3: network timeout
try:
    fetch_data("https://api.external.com/data")
except ConnectionError:
    sentry_sdk.capture_exception()

# Error 4: null pointer / type error
try:
    process_order("ORD-001")
except TypeError:
    sentry_sdk.capture_exception()

# Error 5: add breadcrumbs before an error
sentry_sdk.add_breadcrumb(category="ui.click", message="button#submit")
sentry_sdk.add_breadcrumb(category="http", message="POST /api/orders", data={"status_code": 500})
try:
    raise RuntimeError("order submission failed after retry")
except RuntimeError:
    sentry_sdk.capture_exception()

# Flush to ensure all events are sent
sentry_sdk.flush(timeout=5)
print("Done — sent 5 errors (4 unique, 1 duplicate)")

"""A small HTTP service for the embedded-Drillip tutorial."""
import json
import os
import signal
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

import sentry_sdk


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/health":
            self.reply(200, b"ok", "text/plain")
        elif self.path == "/fail":
            try:
                raise RuntimeError("Example checkout failed")
            except RuntimeError as error:
                event_id = sentry_sdk.capture_exception(error)
                self.reply(500, json.dumps({"error": str(error), "event_id": event_id}).encode())
        else:
            self.reply(404, b'{"error":"not found"}')

    def reply(self, status, body, content_type="application/json"):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main():
    sentry_sdk.init(
        dsn=os.environ["SENTRY_DSN"],
        release="embedded-python-example@1",
        environment="tutorial",
        send_default_pii=False,
        shutdown_timeout=5,
    )
    stopped = threading.Event()
    for sig in (signal.SIGTERM, signal.SIGINT):
        signal.signal(sig, lambda *_: stopped.set())
    with HTTPServer(("0.0.0.0", 8000), Handler) as server:
        worker = threading.Thread(target=server.serve_forever)
        worker.start()
        print("Application listening on :8000", flush=True)
        stopped.wait()
        server.shutdown()
        worker.join()
    sentry_sdk.get_client().close(timeout=5)
    print("Application stopped after draining Sentry events", flush=True)


if __name__ == "__main__":
    main()

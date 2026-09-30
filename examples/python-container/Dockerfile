ARG DRILLIP_IMAGE=ghcr.io/philhem/drillip:v0.3.17
FROM ${DRILLIP_IMAGE} AS drillip
FROM python:3.13-slim-bookworm

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt
COPY --from=drillip /drillip /usr/local/bin/drillip
COPY app.py healthcheck.py wait-for-drillip.py supervisord.conf ./
RUN mkdir -p /var/lib/drillip && chown 10001:10001 /var/lib/drillip

ENV PYTHONUNBUFFERED=1 \
    DRILLIP_ADDR=127.0.0.1:8300 \
    DRILLIP_DB=/var/lib/drillip/errors.db \
    SENTRY_DSN=http://anykey@127.0.0.1:8300/1
USER 10001:10001
EXPOSE 8000
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=5s --timeout=3s --start-period=15s --retries=3 \
    CMD ["python", "/app/healthcheck.py"]
CMD ["supervisord", "-c", "/app/supervisord.conf"]

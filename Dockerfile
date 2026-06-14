FROM python:3.14-alpine AS builder

COPY --from=ghcr.io/astral-sh/uv:latest /uv /uvx /bin/

ENV UV_PYTHON_DOWNLOADS=never \
    UV_COMPILE_BYTECODE=1

WORKDIR /app
COPY pyproject.toml uv.lock ./
RUN uv sync --frozen --no-dev --no-editable

COPY . .
RUN uv sync --frozen --no-dev --no-editable

FROM python:3.14-alpine

ARG VERSION=0.0.0
ARG GIT_COMMIT=unknown
ARG BUILD_DATE=unknown

ENV VERSION=${VERSION} \
    GIT_COMMIT=${GIT_COMMIT} \
    BUILD_DATE=${BUILD_DATE} \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    PATH="/app/.venv/bin:$PATH"

COPY --from=builder /app/.venv /app/.venv
COPY main.py config.py cloudflare_client.py updater.py /app/

WORKDIR /app

LABEL org.opencontainers.image.source="https://github.com/eznix86/cloudflare-dns-updater"
LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.revision="${GIT_COMMIT}"
LABEL org.opencontainers.image.created="${BUILD_DATE}"

RUN find /app/.venv -type d -name '__pycache__' -exec rm -rf {} + 2>/dev/null; \
    find /app/.venv -type f -name '*.pyc' -delete; \
    adduser -D -H app && \
    chown -R app:app /app

USER app

ENTRYPOINT ["python", "main.py"]

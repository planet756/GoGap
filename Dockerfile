FROM node:22-trixie AS web-build

WORKDIR /src

COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./web/

RUN set -eux; \
    corepack enable; \
    pnpm --dir web install --frozen-lockfile

COPY web/index.html web/tsconfig.json ./web/
COPY web/src ./web/src

RUN set -eux; \
    pnpm --dir web build


FROM golang:1.26.4-trixie AS go-build

WORKDIR /src

COPY go.mod go.sum ./

RUN set -eux; \
    go mod download

COPY . .
COPY --from=web-build /src/web/dist ./web/dist

RUN set -eux; \
    CGO_ENABLED=0 go build -o /out/gogap ./cmd/gogap


FROM debian:trixie-slim

LABEL org.opencontainers.image.authors="planet756" \
      org.opencontainers.image.source="https://github.com/planet756/GoGap"

WORKDIR /app

COPY --from=go-build /out/gogap /usr/local/bin/gogap

ENV GOGAP_ADDR=0.0.0.0:8080 \
    GOGAP_DB=/app/data/gogap.db

EXPOSE 8080

VOLUME ["/app/data"]

ENTRYPOINT ["gogap"]

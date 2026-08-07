# syntax=docker/dockerfile:1

# 1) Build the web bundle.
FROM node:18-bookworm-slim AS web
WORKDIR /web
COPY web/package.json web/package-lock.json* ./
RUN npm install
COPY web/ ./
RUN npm run build

# 2) Build the Go binaries with the web bundle embedded.
FROM golang:1.25-bookworm AS gobuild
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
# Place the freshly built web assets where the embed directive expects them.
RUN rm -rf internal/webassets/dist
COPY --from=web /web/dist internal/webassets/dist
ARG VERSION=0.0.0-docker
RUN CGO_ENABLED=0 go build -ldflags "-s -w -X github.com/TheOriUHD/aperture/internal/version.Version=${VERSION}" -o /out/nvrd ./cmd/nvrd \
 && CGO_ENABLED=0 go build -ldflags "-s -w -X github.com/TheOriUHD/aperture/internal/version.Version=${VERSION}" -o /out/nvrctl ./cmd/nvrctl

# 2b) Deno runtime for backend extensions. Its permission flags ARE the sandbox
# (see internal/ext); the binary is a single static-ish executable we copy in.
FROM denoland/deno:bin AS deno

# 3) Runtime image with ffmpeg for recording + thumbnails + Deno for extensions.
FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg ca-certificates tini \
 && rm -rf /var/lib/apt/lists/*
COPY --from=gobuild /out/nvrd /usr/local/bin/nvrd
COPY --from=gobuild /out/nvrctl /usr/local/bin/nvrctl
COPY --from=deno /deno /usr/local/bin/deno
# The backend SDK (@aperture/sdk) as a read-only local module. The supervisor's
# generated import map resolves the bare specifier to this path and adds it to the
# child's --allow-read, so extensions import it under --deny-import (no remote code).
# APERTURE_EXT_SDK in cmd/nvrd/main.go defaults to exactly this path.
COPY deploy/aperture-sdk/mod.ts /usr/local/share/aperture-sdk/mod.ts
RUN chmod 0444 /usr/local/share/aperture-sdk/mod.ts
# A non-root identity for backend-extension processes (nvrd stays root so it can
# setuid-drop each child). Added via /etc/passwd directly to avoid depending on
# useradd in the slim image. Per-extension uids (T1.5) build on this base.
RUN echo 'aperture-ext:x:65533:65533::/data/ext:/usr/sbin/nologin' >> /etc/passwd \
 && echo 'aperture-ext:x:65533:' >> /etc/group
COPY deploy/entrypoint.sh /usr/local/bin/aperture-entrypoint
RUN chmod +x /usr/local/bin/aperture-entrypoint
VOLUME ["/data", "/recordings"]
EXPOSE 8080
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/aperture-entrypoint"]
CMD ["/usr/local/bin/nvrd"]

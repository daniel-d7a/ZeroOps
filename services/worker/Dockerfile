FROM golang:1.24-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker

FROM debian:bookworm-slim
# docker CLI for debugging; the worker itself uses the Go SDK
# railpack: builds student apps through the buildkit service (plan §2)
ARG RAILPACK_VERSION=v0.40.1
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl docker.io \
 && curl -fsSL -o /tmp/railpack.tgz \
      "https://github.com/railwayapp/railpack/releases/download/${RAILPACK_VERSION}/railpack-${RAILPACK_VERSION}-x86_64-unknown-linux-musl.tar.gz" \
 && tar -xzf /tmp/railpack.tgz -C /usr/local/bin railpack \
 && rm /tmp/railpack.tgz \
 && apt-get purge -y curl && apt-get autoremove -y \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/worker /usr/local/bin/worker
CMD ["worker"]

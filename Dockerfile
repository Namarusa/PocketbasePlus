# PocketBasePlus: the single static binary on a minimal image (CA certificates
# and time zones included, no shell), for linux/amd64,
# linux/arm64 and linux/arm/v7.
#
#   docker build -t pocketbaseplus .
#   docker run -p 8090:8090 -v pocketbaseplus_data:/pb_data pocketbaseplus
#
# Settings are environment variables (see the README, "Docker").

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS TARGETARCH TARGETVARIANT
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN if [ "$TARGETARCH" = arm ]; then export GOARM=${TARGETVARIANT#v}; fi; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /out/pocketbaseplus . \
    && mkdir -p /out/pb_data

FROM gcr.io/distroless/static-debian12
ARG VERSION=dev
LABEL org.opencontainers.image.title="PocketBasePlus" \
      org.opencontainers.image.description="Extends PocketBase (https://github.com/pocketbase/pocketbase) with dashboards, permissions, emails, flows, AI and a page builder, in one binary" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.base.name="gcr.io/distroless/static-debian12"

COPY --from=build /out/pocketbaseplus /usr/local/bin/pocketbaseplus
# the data volume, owned by the app's user (uid 65532)
COPY --from=build --chown=65532:65532 /out/pb_data /pb_data

# The container starts as root only to give /pb_data to PUID:PGID (folders a
# NAS creates as root for the volume), then the app drops to that user before
# opening the database. With "--user", it runs as that user from the start.
ENV PUID=65532 PGID=65532 POCKETBASEPLUS_HTTP=0.0.0.0:8090
VOLUME /pb_data
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD ["/usr/local/bin/pocketbaseplus", "health"]
ENTRYPOINT ["/usr/local/bin/pocketbaseplus", "--dir", "/pb_data"]
CMD ["serve"]

# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build

WORKDIR /src

# cache module downloads
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# version stamped into the binary via ldflags
ARG VERSION=docker
ARG BRANCH=unknown
ARG REVISION=unknown
ARG BUILD_USER=docker
ARG BUILD_DATE=unknown

# only ambot is shipped in the image; the scanner is a local-only tool
RUN CGO_ENABLED=0 go build \
        -ldflags " \
          -X github.com/prometheus/common/version.Version=${VERSION} \
          -X github.com/prometheus/common/version.Branch=${BRANCH} \
          -X github.com/prometheus/common/version.Revision=${REVISION} \
          -X github.com/prometheus/common/version.BuildUser=${BUILD_USER} \
          -X github.com/prometheus/common/version.BuildDate=${BUILD_DATE}" \
        -o /out/ambot ./cmd/ambot

FROM alpine:3.21

RUN apk add --no-cache tini chromium ca-certificates \
    && addgroup -S ambot \
    && adduser -S -G ambot -h /home/ambot ambot \
    && mkdir -p /home/ambot/.cache \
    && chown -R ambot:ambot /home/ambot

COPY --from=build /out/ambot /usr/local/bin/ambot

# Set HOME explicitly so os.UserCacheDir() resolves to /home/ambot/.cache
# (Docker does not always set HOME for system users created with adduser -S)
ENV HOME=/home/ambot

USER ambot

# Mount a volume here to persist the Chrome profile (cookies/session) across
# container restarts. Without it, authentication is lost on every restart.
VOLUME ["/home/ambot/.cache"]

EXPOSE 9150

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:9150/metrics >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["ambot"]

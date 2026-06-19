# syntax=docker/dockerfile:1

# --- build stage ---
FROM golang:1.26-alpine AS build

WORKDIR /src

# cache module downloads
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# version stamped into the binary via ldflags
ARG VERSION=docker

# only ambot is shipped in the image; the scanner is a local-only tool
RUN CGO_ENABLED=0 go build \
        -ldflags "-X github.com/prometheus/common/version.Version=${VERSION}" \
        -o /out/ambot ./cmd/ambot

# --- runtime stage ---
FROM alpine:3.21

RUN apk add --no-cache tini chromium ca-certificates \
    && addgroup -S ambot \
    && adduser -S -G ambot ambot

COPY --from=build /out/ambot /usr/local/bin/ambot

USER ambot

EXPOSE 9150

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:9150/metrics >/dev/null 2>&1 || exit 1

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["ambot"]

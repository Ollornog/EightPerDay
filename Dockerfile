# One static binary in an image without a shell. The base images are pinned by digest, and Dependabot moves them;
# scripts/check-image.sh in the CI accepts each update. The build stage cross-compiles for the target (GOARCH).
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine3.24@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY assets ./assets
COPY cmd ./cmd
COPY core ./core
COPY docs ./docs
COPY internal ./internal
COPY web ./web
# Reproducible: no cgo, no paths in the binary, no toolchain download.
ENV CGO_ENABLED=0 GOFLAGS=-trimpath GOTOOLCHAIN=local
ARG TARGETOS=linux
ARG TARGETARCH
RUN GOOS="$TARGETOS" GOARCH="$TARGETARCH" go build -ldflags "-s -w" -o /out/eightperday ./cmd/eightperday

FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
LABEL org.opencontainers.image.title="EightPerDay" \
      org.opencontainers.image.source="https://github.com/Ollornog/EightPerDay" \
      org.opencontainers.image.licenses="MIT"
COPY --from=build /out/eightperday /eightperday
USER 65532:65532
# The program binds to loopback by default. In a container, the published port decides who can reach it; the health
# endpoint stays inside.
ENV EIGHTPERDAY_ADDR=:8080 EIGHTPERDAY_HEALTH_ADDR=127.0.0.1:8081 TZ=Europe/Berlin
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --start-interval=1s --retries=3 CMD ["/eightperday", "probe"]
ENTRYPOINT ["/eightperday", "serve"]

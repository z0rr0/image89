ARG GOLANG_VERSION=1.27.2
ARG ALPINE_VERSION=3.24

# The builder runs natively on the host arch ($BUILDPLATFORM) and cross-compiles:
# with CGO_ENABLED=0 that is free, while an emulated amd64 builder would not be.
FROM --platform=$BUILDPLATFORM golang:${GOLANG_VERSION}-alpine${ALPINE_VERSION} AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG GO_LDFLAGS=""
# TARGETOS/TARGETARCH come from BuildKit's --platform: `make docker` builds for
# the host arch, `make docker-push` for linux/amd64 and linux/arm64. Not named
# LDFLAGS: that one conventionally holds C linker flags and is often already
# exported by the host shell (Homebrew sets it).
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags "$GO_LDFLAGS" -o /app/image89 .

# No --platform here: BuildKit resolves this to the target platform, so the
# binary above and this base image always match.
FROM alpine:${ALPINE_VERSION}
# No ca-certificates: the service makes no outgoing TLS calls.
RUN adduser -D appuser
LABEL org.opencontainers.image.authors="me@axv.email" \
    org.opencontainers.image.url="https://hub.docker.com/r/z0rr0/image89" \
    org.opencontainers.image.documentation="https://github.com/z0rr0/image89" \
    org.opencontainers.image.source="https://github.com/z0rr0/image89" \
    org.opencontainers.image.licenses="MIT" \
    org.opencontainers.image.title="Image89" \
    org.opencontainers.image.description="Generator of math figure images: CLI tool and web application"

COPY --from=builder /app/image89 /app/image89
USER appuser
EXPOSE 8080
ENTRYPOINT ["/app/image89"]
CMD ["-s"]

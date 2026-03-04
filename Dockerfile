# syntax=docker/dockerfile:1.4


# Build the manager binary
ARG builder_image

# Build architecture
ARG ARCH

# Ignore Hadolint rule "Always tag the version of an image explicitly."
# It's an invalid finding since the image is explicitly set in the Makefile.
# https://github.com/hadolint/hadolint/wiki/DL3006
# hadolint ignore=DL3006
FROM ${builder_image} as builder
WORKDIR /workspace

# Run this with docker build --build-arg goproxy=$(go env GOPROXY) to override the goproxy
ARG goproxy=https://proxy.golang.org
# Run this with docker build --build-arg package=./controlplane or --build-arg package=./bootstrap
ENV GOPROXY=$goproxy

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

# Cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

# Copy the sources
COPY ./ ./

# Build
ARG package=.
ARG ARCH
ARG ldflags

# Do not force rebuild of up-to-date packages (do not use -a) and use the compiler cache folder
RUN --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} \
    go build -trimpath -ldflags "${ldflags} -extldflags '-static'" \
    -o manager ${package}

COPY scripts/get-docker-cli.sh get-docker-cli.sh
RUN mkdir -p out && ./get-docker-cli.sh ${ARCH} out

# Download kind binary
ARG KIND_VERSION=v0.27.0
RUN case "${ARCH}" in \
      amd64) KIND_ARCH="amd64" ;; \
      arm64) KIND_ARCH="arm64" ;; \
      *) echo "unsupported arch ${ARCH}"; exit 1 ;; \
    esac && \
    wget -O out/kind "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-${KIND_ARCH}" && \
    chmod +x out/kind


# Production image
#FROM gcr.io/distroless/static:nonroot-${ARCH}
FROM alpine:3.21

RUN apk add --no-cache \
		ca-certificates \
# DOCKER_HOST=ssh://... -- https://github.com/docker/cli/pull/1014
		openssh-client

LABEL org.opencontainers.image.source=https://github.com/capi-samples/cluster-api-provider-kwok
WORKDIR /
COPY --from=builder /workspace/manager .
COPY --from=builder /workspace/out/docker /usr/local/bin/docker
COPY --from=builder /workspace/out/kind /usr/local/bin/kind

RUN mkdir -p /usr/local/libexec/docker/cli-plugins
COPY --from=builder /workspace/out/docker-compose /usr/local/libexec/docker/cli-plugins/docker-compose
RUN docker --version && /usr/local/libexec/docker/cli-plugins/docker-compose --version && kind --version

# Run as root — Docker socket requires root access
USER 0
ENTRYPOINT ["/manager"]

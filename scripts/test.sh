#!/bin/sh
# Runs vet + tests in the same pinned Go image the Dockerfile uses; no local Go needed.
set -eu
IMAGE="golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb"
[ $# -eq 0 ] && set -- ./...
exec docker run --rm -v "$(pwd)":/src:ro -w /src \
  -e CGO_ENABLED=0 -e GOTOOLCHAIN=local -e GOPROXY=off -e GOFLAGS=-mod=readonly \
  "$IMAGE" sh -c 'go vet ./... && go test "$@"' sh "$@"

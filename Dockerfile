# Build stage. Pinned by digest so the toolchain can't change underneath us.
# golang:1.27-alpine
FROM golang:1.27-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build
# No toolchain or module downloads are possible during the build.
ENV CGO_ENABLED=0 GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=readonly
WORKDIR /src
COPY . .
# Enforce the zero-dependency rule: no go.sum and no require lines.
RUN test ! -e go.sum && ! grep -q '^require' go.mod
RUN go build -trimpath -ldflags="-s -w" -o /out/personal-time-tracker . && mkdir -p /out/data

# Runtime stage: distroless static, no shell or package manager.
# gcr.io/distroless/static-debian13:nonroot
FROM gcr.io/distroless/static-debian13:nonroot@sha256:18f4cc26c9ffa7f20a359298be7deadac5c607c8e03a02b8119f1a5fc2f82c99
COPY --from=build /out/personal-time-tracker /personal-time-tracker
# Owned by the nonroot user so a fresh named volume inherits writable ownership.
COPY --from=build --chown=65532:65532 /out/data /data
ENV PORT=24871 DATA_DIR=/data
USER 65532:65532
EXPOSE 24871
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s CMD ["/personal-time-tracker", "-healthcheck"]
ENTRYPOINT ["/personal-time-tracker"]

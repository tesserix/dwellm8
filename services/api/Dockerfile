# The Dwellm8 API.
#
# The org's own bases, not upstream ones: base-go-builder carries git, CA
# certificates and tzdata with CGO_ENABLED=0 baked in, and base-distroless-static
# is gcr.io/distroless/static:nonroot with the org's provenance on it. Both are
# rebuilt centrally when a CVE lands, which is the whole reason for having them —
# a service pinning upstream images is a service nobody can patch in one place.
# See tesserix-k8s/docs/distroless-migration.md.

# Pinned by digest, not by :latest. A base image that moves under a service is a
# service whose build is not reproducible and whose rollback lands somewhere new;
# Dependabot raises the bump, and the bump is a reviewable line.
FROM ghcr.io/tesserix/base-go-builder:latest@sha256:26832d7a02b1cc85fd5f5848849cd51a32c7bf788e570622ef7cd48176cb5c95 AS build
# The builder runs as 10001 so a consumer can keep that uid; this stage writes
# to /src and /out, neither of which that user owns. Root here is a build-time
# concern only — nothing from this stage ships, only the binary it produces.
USER 0:0
WORKDIR /src

# Dependencies first, so a code change does not re-download the module cache.
COPY go.mod go.sum* ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags "-s -w -X main.version=${VERSION}" \
      -o /out/api ./cmd/api

# The tests run in CI, but running them here means a broken build cannot produce
# an image at all. The database-backed tests skip without their DSNs, so what
# this catches is the compile-and-unit half — which is exactly the half that
# should never reach a registry broken.
RUN go vet ./... && go test ./...

# No shell, no package manager, nothing to exec into. uid 65532.
FROM ghcr.io/tesserix/base-distroless-static:latest@sha256:83a2fa25dfd831cb01ca3d45150df6ebf920ac48682face647a8397ce724e8b8
COPY --from=build --chown=65532:65532 /out/api /api
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/api"]

# The Dwellm8 API.
#
# The org's own bases, not upstream ones: base-go-builder carries git, CA
# certificates and tzdata with CGO_ENABLED=0 baked in, and base-distroless-static
# is gcr.io/distroless/static:nonroot with the org's provenance on it. Both are
# rebuilt centrally when a CVE lands, which is the whole reason for having them —
# a service pinning upstream images is a service nobody can patch in one place.
# See tesserix-k8s/docs/distroless-migration.md.

FROM ghcr.io/tesserix/base-go-builder:latest AS build
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
FROM ghcr.io/tesserix/base-distroless-static:latest
COPY --from=build --chown=65532:65532 /out/api /api
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/api"]

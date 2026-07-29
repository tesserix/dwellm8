# The Dwellm8 API.
#
# Distroless static: no shell, no package manager, nothing to exec into. The
# binary is static because the base image has no libc to link against.

FROM golang:1.26-alpine AS build
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

# The tests run in CI, but running them here means a broken build cannot
# produce an image at all.
RUN go vet ./... && go test ./...

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api"]

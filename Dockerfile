# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

# Dependencies are cached independently of the source tree.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=none
ARG BUILD_TIME=unknown

# -X flags bake build metadata into internal/version.
RUN set -eux; \
    LDFLAGS="-s -w \
      -X github.com/dmitriy-zverev/kazna/internal/version.Version=${VERSION} \
      -X github.com/dmitriy-zverev/kazna/internal/version.Commit=${COMMIT} \
      -X github.com/dmitriy-zverev/kazna/internal/version.BuildTime=${BUILD_TIME}"; \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "${LDFLAGS}" -o /out/api ./cmd/api; \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "${LDFLAGS}" -o /out/worker ./cmd/worker; \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.22 AS runtime

RUN apk add --no-cache ca-certificates wget tzdata \
    && adduser -D -u 10001 -g app app

COPY --from=build /out/api /out/worker /out/migrate /usr/local/bin/

USER app
EXPOSE 8080

# Overridden by compose: /usr/local/bin/worker for the worker service.
# CMD (not ENTRYPOINT) so compose `command:` replaces it cleanly.
CMD ["/usr/local/bin/api"]

# syntax=docker/dockerfile:1.7
# CLI is shipped as a binary, not a long-running service. This Dockerfile produces
# an image suitable for `docker run hopnet-cli ...` invocations and CI use.

FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG HOPNET_VERSION=0.0.1
ARG HOPNET_COMMIT=dev
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
      -ldflags="-s -w -X main.version=${HOPNET_VERSION} -X main.commit=${HOPNET_COMMIT}" \
      -o /out/hopnet ./cmd/hopnet

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build --chown=nonroot:nonroot /out/hopnet /hopnet
USER nonroot
ENTRYPOINT ["/hopnet"]

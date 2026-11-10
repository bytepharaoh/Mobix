FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG SERVICE
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mobix ./cmd/${SERVICE}

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -S -g 10001 mobix && adduser -S -D -H -u 10001 -G mobix mobix
COPY --from=build /out/mobix /usr/local/bin/mobix
USER 10001:10001
ENTRYPOINT ["/usr/local/bin/mobix"]

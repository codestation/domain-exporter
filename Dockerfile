FROM golang:1.26-alpine AS builder

ARG CI_COMMIT_TAG
ARG GOPROXY

RUN apk add --no-cache git

COPY go.mod go.sum /src/
RUN set -ex; \
    cd /src; \
    go mod download
COPY . /src/

RUN set -ex; \
    cd /src; \
    CGO_ENABLED=0 go build -o release/domain-exporter \
    -trimpath \
    -ldflags "-w -s \
    -X main.Tag=${CI_COMMIT_TAG}"

FROM alpine:3.23
LABEL maintainer="codestation <codestation@megpoid.dev>"

RUN apk add --no-cache ca-certificates tzdata

RUN set -eux; \
    addgroup -S runner -g 1000; \
    adduser -S runner -G runner -u 1000

COPY --from=builder /src/release/domain-exporter /usr/bin/domain-exporter

USER runner

CMD ["/usr/bin/domain-exporter"]

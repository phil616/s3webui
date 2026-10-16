FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Include preferred source, build scripts, notices and Go dependencies.
RUN go mod vendor && tar -czf /source.tar.gz . && CGO_ENABLED=0 go build -mod=vendor -trimpath -o /s3webui ./cmd/s3webui

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -g 65532 app && adduser -D -u 65532 -G app app
LABEL org.opencontainers.image.title="S3 Web UI (Go downstream)" \
      org.opencontainers.image.licenses="AGPL-3.0-only"
COPY --from=build /s3webui /usr/local/bin/s3webui
COPY --from=build /source.tar.gz /usr/share/s3webui/source.tar.gz
COPY LICENSE THIRD_PARTY_LICENSES.md NOTICE.md /usr/share/s3webui/
ENV HTTP_ADDR=:8080 SOURCE_ARCHIVE=/usr/share/s3webui/source.tar.gz
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/s3webui"]

FROM golang:1.26-alpine AS build

ARG GOOSE_VERSION=v3.27.3

RUN CGO_ENABLED=0 go install \
    -tags='no_clickhouse no_libsql no_mssql no_mysql no_sqlite3 no_vertica no_ydb' \
    github.com/pressly/goose/v3/cmd/goose@${GOOSE_VERSION}

FROM alpine:3.23

RUN apk add --no-cache ca-certificates

COPY --from=build /go/bin/goose /usr/local/bin/goose
COPY migrations /migrations

USER nobody

ENTRYPOINT ["goose"]

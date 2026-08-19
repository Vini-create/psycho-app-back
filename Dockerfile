FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM alpine:3.23 AS runtime

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S anamnesys \
    && adduser -S -G anamnesys anamnesys

COPY --from=build /out/api /usr/local/bin/anamnesys-api

USER anamnesys

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/anamnesys-api"]

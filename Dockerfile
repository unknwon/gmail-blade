FROM golang:1.24-alpine AS build

ARG BUILD_VERSION

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build \
    -ldflags "-X main.version=${BUILD_VERSION}" \
    -trimpath \
    -o gmail-blade \
    ./cmd/gmail-blade

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata

COPY --from=build /app/gmail-blade /usr/local/bin/gmail-blade

ENTRYPOINT ["gmail-blade"]

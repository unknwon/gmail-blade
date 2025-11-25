FROM golang:1.25-alpine AS build

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -o gmail-blade \
    ./cmd/gmail-blade

FROM alpine:3.22

COPY --from=build /app/gmail-blade /usr/local/bin/gmail-blade

ENTRYPOINT ["gmail-blade"]

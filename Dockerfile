FROM golang:1.27-alpine3.24@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18 AS build

RUN apk add --no-cache ca-certificates

WORKDIR /app
COPY . .
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -o gmail-blade \
    ./cmd/gmail-blade

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf

COPY --from=build /app/gmail-blade /usr/local/bin/gmail-blade

ENTRYPOINT ["gmail-blade"]

# Multi-stage build for the mobile-gateway (HTTP -> NATS bridge).
FROM golang:1.26-alpine AS build
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/mobile-gateway .

FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/mobile-gateway /usr/local/bin/mobile-gateway
ENTRYPOINT ["/usr/local/bin/mobile-gateway"]

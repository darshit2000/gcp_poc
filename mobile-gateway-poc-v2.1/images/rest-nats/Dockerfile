# Multi-stage build for rest-nats (ex-REST service as a NATS responder).
FROM golang:1.26-alpine AS build
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/rest-nats .

FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/rest-nats /usr/local/bin/rest-nats
ENTRYPOINT ["/usr/local/bin/rest-nats"]

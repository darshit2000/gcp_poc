# Multi-stage build for the non-home client.
FROM golang:1.26-alpine AS build
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/client .

FROM alpine:3.20
RUN adduser -D -u 10001 app
USER app
COPY --from=build /out/client /usr/local/bin/client
ENTRYPOINT ["/usr/local/bin/client"]

FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /stonebridge ./cmd/stonebridge

FROM alpine:3.20
RUN addgroup -S app && adduser -S -G app app
WORKDIR /data
COPY --from=build /stonebridge /usr/local/bin/stonebridge
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/stonebridge", "-dir", "/data", "-http", ":8080"]

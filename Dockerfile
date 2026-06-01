FROM golang:1.26 AS build

WORKDIR /src
COPY go.mod ./
COPY go.sum ./
COPY cmd ./cmd
COPY internal ./internal
RUN go build -o /out/vclaw ./cmd/vclaw

FROM debian:bookworm-slim
WORKDIR /app
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/vclaw /usr/local/bin/vclaw
EXPOSE 8080
ENTRYPOINT ["vclaw"]

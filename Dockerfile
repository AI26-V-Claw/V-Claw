FROM golang:1.23 AS build

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal
RUN go build -o /out/vclaw ./cmd/vclaw

FROM debian:bookworm-slim
WORKDIR /app
COPY --from=build /out/vclaw /usr/local/bin/vclaw
EXPOSE 8080
ENTRYPOINT ["vclaw"]

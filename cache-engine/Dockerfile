FROM golang:1.26.3-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

FROM gcr.io/distroless/base-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/server /app/server

ENV CACHE_ENGINE_ADDR=:8080

EXPOSE 8080
VOLUME ["/app/data"]

ENTRYPOINT ["/app/server"]

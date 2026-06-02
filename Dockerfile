# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api     ./cmd/api
RUN CGO_ENABLED=0 go build -o /out/worker  ./cmd/worker
RUN CGO_ENABLED=0 go build -o /out/migrate ./cmd/migrate

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/ /app/
# Default command is overridden per service in docker-compose.
CMD ["/app/api"]

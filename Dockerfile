# -- build stage --
FROM golang:latest AS build

RUN apt-get update && apt-get install -y --no-install-recommends git && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN GOTOOLCHAIN=auto go mod download

# Copy source and build
COPY . .
RUN GOTOOLCHAIN=auto CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/api ./cmd/api

# -- runtime stage --
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

COPY --from=build /bin/api /usr/local/bin/api

EXPOSE 8080

ENTRYPOINT ["api"]

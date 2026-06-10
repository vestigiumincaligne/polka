# Polka — home library server.
# Build: docker build -t polka .
# Run:   docker run -p 12791:12791 -v polka-data:/data -v /path/to/books:/books polka

# --- Frontend ---
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# --- Server (pure Go, no CGO) ---
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /polka ./cmd/polka

# --- Runtime ---
FROM alpine:3.21
RUN adduser -D -H polka && mkdir -p /data /books && chown polka /data /books
COPY --from=build /polka /usr/local/bin/polka
USER polka
VOLUME ["/data", "/books"]
EXPOSE 12791
ENTRYPOINT ["polka", "serve", "--addr", ":12791", "--data-dir", "/data", "--library-dir", "/books"]

# Multi-stage build: UI (Node) + server (Go) → one small image.
# Not used by `make demo` (which runs natively); provided for convenience. Untested in this repo's CI.
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS server
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 go build -o /out/loupe ./cmd/loupe

FROM alpine:3.20
RUN adduser -D loupe
USER loupe
WORKDIR /app
COPY --from=server /out/loupe /app/loupe
COPY --from=web /src/web/dist /app/web
COPY config/prices.json /app/prices.json
EXPOSE 7070
VOLUME ["/data"]
ENTRYPOINT ["/app/loupe", "serve", "-addr", "0.0.0.0:7070", "-db", "/data/loupe.db", "-web", "/app/web", "-prices", "/app/prices.json"]

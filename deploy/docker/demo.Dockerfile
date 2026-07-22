# Portable Goodman product demo: collector + synthetic end-to-end demo runner.
FROM node:22-bookworm AS ui
WORKDIR /ui
COPY dashboard/package.json dashboard/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY dashboard/ ./
RUN npm run build

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /ui/dist ./internal/api/ui/dist
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/collector ./cmd/collector
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/goodman-demo ./cmd/goodman-demo

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/collector /usr/local/bin/collector
COPY --from=build /out/goodman-demo /usr/local/bin/goodman-demo
ENV HOME=/tmp
EXPOSE 8844
USER nonroot
ENTRYPOINT ["/usr/local/bin/goodman-demo", "-host", "0.0.0.0", "-collector", "/usr/local/bin/collector", "-db", "/tmp/goodman-demo.db"]

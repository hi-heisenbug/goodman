# Collector image: builds the dashboard, embeds it, ships the Go binary.
FROM node:22-bookworm AS ui
WORKDIR /ui
COPY dashboard/package.json dashboard/package-lock.json* ./
RUN npm ci --no-audit --no-fund
COPY dashboard/ ./
RUN npm run build

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /ui/dist ./internal/api/ui/dist
# Loader package needs a BPF object present to satisfy //go:embed even though
# the collector never loads it; provide an empty placeholder.
RUN touch internal/loader/goodman.bpf.o
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/collector ./cmd/collector

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/collector /usr/local/bin/collector
ENV GOODMAN_DSN=/tmp/goodman.db
EXPOSE 8844
USER nonroot
ENTRYPOINT ["/usr/local/bin/collector"]

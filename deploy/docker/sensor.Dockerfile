# Sensor image: builds the eBPF object + the Go loader, ships a minimal image.
# Runs privileged as a DaemonSet.
FROM golang:1.23-bookworm AS build
RUN apt-get update && apt-get install -y --no-install-recommends \
      clang llvm libbpf-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The eBPF object is prebuilt and committed (bpf/goodman.bpf.o, copied to the
# loader package). Rebuild it here so the image is reproducible from source.
RUN clang -O2 -g -target bpf -D__TARGET_ARCH_x86 -I bpf -I bpf/include \
      -c bpf/goodman.bpf.c -o internal/loader/goodman.bpf.o
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /out/sensor ./cmd/sensor

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
# The sensor must run as root/privileged to load BPF; the DaemonSet overrides
# runAsNonRoot. Ship CA certs for the collector HTTPS case.
COPY --from=build /out/sensor /usr/local/bin/sensor
USER 0:0
ENTRYPOINT ["/usr/local/bin/sensor"]

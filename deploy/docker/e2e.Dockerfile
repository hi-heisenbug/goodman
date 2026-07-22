# Root-capable live verification image. The container uses the host kernel,
# PID namespace, and cgroup hierarchy at runtime; it does not emulate eBPF.
FROM golang:1.25-bookworm AS go

FROM node:24-bookworm
COPY --from=go /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH

RUN apt-get update && apt-get install -y --no-install-recommends \
    bpftool \
    ca-certificates \
    clang \
    curl \
    jq \
    libbpf-dev \
    libelf-dev \
    llvm \
    make \
    python3 \
  && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN make build workload

ENTRYPOINT ["bash", "scripts/live-e2e.sh"]

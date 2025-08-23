FROM golang:1.23.2 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -buildvcs=false -o proxmox-talos-vm-deployer .

FROM debian:12-slim AS runtime
RUN apt-get update && apt-get install -y curl && \
    curl -sL https://talos.dev/install | sh && \
    apt-get clean && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/proxmox-talos-vm-deployer .
CMD ["/app/proxmox-talos-vm-deployer"]

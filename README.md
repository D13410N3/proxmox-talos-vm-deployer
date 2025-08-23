# Talos VM Deployer

This is an HTTP service that deploys Talos Linux VMs on Proxmox VE and automatically registers them with a Talos Kubernetes cluster.

> **⚠️ Mikrotik Dependency Notice**
> This deployer requires a Mikrotik router acting as DHCP server to discover VM IP addresses. I initially tried using qemu-guest-agent but encountered a deadlock: the agent only starts after VM provisioning, but I need the IP address before provisioning completes. The Mikrotik approach queries DHCP leases by MAC address, eliminating this chicken-and-egg problem. If you know alternative solutions for reliable IP discovery during VM bootstrap, I'd love to hear from you!

## What do you need to use it:

- **Proxmox VE**: A Proxmox VE cluster with at least one node running Talos Linux.
- **Mikrotik router**: this deployer relies on the Mikrotik router to obtain DHCP leases for the VMs.

## Features

- **Talos Linux Deployment**: Automatically deploys and configures Talos Linux VMs
- **Kubernetes Cluster Integration**: Registers new nodes with existing Talos clusters
- **Weighted Node Selection**: Automatically selects the best Proxmox node based on configurable weights
- **Template-based VM Creation**: Uses predefined VM templates with specific CPU, memory, and disk configurations
- **NUMA Topology Support**: Configures VMs with proper NUMA topology for optimal performance
- **CPU Affinity**: Pins VMs to specific CPU cores (physical and/or hyperthreaded)
- **Hugepages Support**: Enables hugepages for VMs when configured
- **Bulk VM Creation**: Create multiple VMs in a single API call
- **VM Management**: Start, stop, reset, and delete VMs
- **Monitoring**: Prometheus metrics and health checks
- **Error Tracking**: Sentry integration for error reporting
- **Mikrotik Integration**: Integrates with Mikrotik router for DHCP lease queries

## Configuration

### Environment Variables

- `PROXMOX_BASE_ADDR`: Proxmox VE API base URL (e.g., `https://proxmox.example.com:8006/api2/json`)
- `PROXMOX_TOKEN`: Proxmox VE API token (format: `user@realm!tokenname=token-value`)
- `SENTRY_DSN`: Sentry DSN for error tracking
- `LISTEN_ADDR`: HTTP server listen address (default: `0.0.0.0`)
- `LISTEN_PORT`: HTTP server listen port (default: `8080`)
- `CONFIG_PATH`: Path to YAML configuration file (default: `config.yaml`)
- `AUTH_TOKEN`: Authentication token for API access
- `TALOS_MACHINE_TEMPLATE`: Talos machine configuration template (required)
- `TALOS_CONTROLPLANE_ENDPOINT`: Talos control plane endpoint (required)
- `DEBUG`: Enable debug mode (default: `false`)
- `LOG_LEVEL`: Log level (0: Debug, 1: Info, 2: Error) (default: `1`)
- `VERIFY_SSL`: Verify SSL certificates (default: `true`)
- `MIKROTIK_IP`: IP address of Mikrotik router for DHCP lease queries
- `MIKROTIK_PORT`: Port for Mikrotik REST API (default: `8080`)
- `MIKROTIK_USERNAME`: Username for Mikrotik API authentication
- `MIKROTIK_PASSWORD`: Password for Mikrotik API authentication

### Configuration File (config.yaml)

The service uses a YAML configuration file to define nodes and VM templates:

```yaml
nodes:
  - name: proxmox-node1       # Name of the node
    weight: 10                # Weight of the node (higher weight means more VMs)
    suffix: "1"               # Suffix for VM names, i.e. talos-worker-small-<suffix>-<....>
    ht: true                  # Is Hyperthreading enabled. Used for CPU allocation
    hugepages: false          # Is hugepages enabled. Used for memory allocation
    numa:                     # Host node NUMA topology. Check yours with numactl --hardware
      - id: 0                 # NUMA node id
        cores:
          phy: 0-15           # Physical cores of NUMA node 0
          ht: 32-47           # Hyperthreaded cores of NUMA node 0
      - id: 1
        cores:
          phy: 16-31
          ht: 48-63
    base_templates:           # Proxmox templates to use for VM creation
      - name: talos-template  # Name (used in "base_template" parameter in deploy)
        id: 1901              # Actual template VM id

vm_templates:
  - name: talos-worker-small  # Template name (used in "vm_template" parameter in deploy)
    cpu: 4                    # Number of cores
    memory: 8192              # Memory
    disk: 20                  # Boot disk size
    cpu_model: kvm64          # CPU model to set
    role: worker              # Role, will be used in machine configuration template
  - name: talos-worker-medium
    cpu: 8
    memory: 16384
    disk: 50
    cpu_model: kvm64
    role: worker
  - name: talos-controlplane
    cpu: 4
    memory: 8192
    disk: 30
    cpu_model: kvm64
    role: controlplane
```

### Talos Machine Configuration Template

The `TALOS_MACHINE_TEMPLATE` environment variable should contain a Talos machine configuration template with placeholders:

```yaml
version: v1alpha1
debug: false
persist: true
machine:
    type: {role}  # Will be replaced with actual role (worker/controlplane)
    token: your-machine-token-here
    network:
        hostname: {vm_name}  # Will be replaced with actual VM name
cluster:
    id: your-cluster-id-here
    secret: your-cluster-secret-here
    controlPlane:
        endpoint: https://your-controlplane:6443
    clusterName: your-cluster-name
    network:
        dnsDomain: cluster.local
        podSubnets:
            - 10.244.0.0/16
        serviceSubnets:
            - 10.96.0.0/12
    token: your-cluster-token-here
```

**Placeholder Syntax:**
- `{role}` - Replaced with the VM template's role (worker/controlplane)
- `{vm_name}` - Replaced with the generated or specified VM name

## API Endpoints

### Create VM

**POST** `/api/v1/create`

Creates a new Talos VM and registers it with the cluster.

**Headers:**
- `X-Auth-Token`: Authentication token

**Parameters:**
- `base_template` (required): Name of the base template to clone from
- `vm_template` (required): Name of the VM template configuration to use
- `name` (optional): Custom VM name. If not provided, auto-generated
- `node` (optional): Specific node to deploy on. If not provided, auto-selected based on weights
- `numa` (optional): Specific NUMA node ID to use
- `phy` (optional): Specific physical cores to pin to (e.g., "0-3,8-11")
- `ht` (optional): Specific hyperthreaded cores to pin to (e.g., "32-35,40-43")
- `phy_only` (optional): Use only physical cores (set to "1")
- `ht_only` (optional): Use only hyperthreaded cores (set to "1")
- `reset` (optional): Reset VM after creation (set to "1")
- `count` (optional): Number of VMs to create (for bulk creation)

**Response:**
```json
{
  "vm_id": 12345,
  "node": "proxmox-node1",
  "name": "talos-worker-small-1-12345-abc123",
  "ip": "192.168.88.175",
  "role": "worker",
  "reset": false
}
```

### Delete VM

**POST** `/api/v1/delete`

Deletes an existing VM.

**Headers:**
- `X-Auth-Token`: Authentication token

**Parameters:**
- `vm_name` (optional): Name of the VM to delete
- `node` (optional): Node where the VM is located (required if using vm_id)
- `vm_id` (optional): ID of the VM to delete (required if not using vm_name)
- `stop_method` (optional): Method to stop VM before deletion ("shutdown" or "stop", default: "shutdown")

**Response:**
```json
{
  "node": "proxmox-node1",
  "vm_id": 12345
}
```

### Health Check

**GET** `/health-check`

Returns service health status.

**Response:**
```
200 OK
```

### Metrics

**GET** `/metrics`

Returns Prometheus metrics.

## Usage Examples

### Create a Talos worker node

```bash
curl -X POST http://localhost:8080/api/v1/create \
  -H "X-Auth-Token: your-auth-token" \
  -d "base_template=talos-template" \
  -d "vm_template=talos-worker-small"
```

### Create a Talos control plane node

```bash
curl -X POST http://localhost:8080/api/v1/create \
  -H "X-Auth-Token: your-auth-token" \
  -d "base_template=talos-template" \
  -d "vm_template=talos-controlplane"
```

### Create multiple worker nodes

```bash
curl -X POST http://localhost:8080/api/v1/create \
  -H "X-Auth-Token: your-auth-token" \
  -d "base_template=talos-template" \
  -d "vm_template=talos-worker-medium" \
  -d "count=3"
```

## Building and Running

### Prerequisites

- Go 1.21 or later
- Access to Proxmox VE cluster with Talos Linux template
- Proxmox API token with appropriate permissions
- Existing Talos Kubernetes cluster or control plane
- Mikrotik router that acts as a DHCP server for the VMs
- `talosctl` installed (or use Docker image)

### Build

```bash
go mod tidy
go build -o proxmox-talos-vm-deployer
```

### Run

```bash
export PROXMOX_BASE_ADDR="https://your-proxmox.example.com:8006/api2/json"
export PROXMOX_TOKEN="user@pve!token=your-token-here"
export AUTH_TOKEN="your-api-auth-token"
export SENTRY_DSN="your-sentry-dsn"
export LISTEN_ADDR="0.0.0.0"
export LISTEN_PORT="8080"
export CONFIG_PATH="config.yaml"
export TALOS_CONTROLPLANE_ENDPOINT="https://your-controlplane:6443"
export TALOS_MACHINE_TEMPLATE="./talos-machine-config.yaml"
export MIKROTIK_IP="10.100.0.1"
export MIKROTIK_PORT="8080"
export MIKROTIK_USERNAME="admin"
export MIKROTIK_PASSWORD="your-password"
export DEBUG="true"
export LOG_LEVEL="0"
export VERIFY_SSL="false"

./proxmox-talos-vm-deployer
```

### Docker

```bash
docker build -t proxmox-talos-vm-deployer .
docker run -p 8080:8080 \
  -e LISTEN_ADDR="0.0.0.0" \
  -e LISTEN_PORT="8080" \
  -e CONFIG_PATH="/app/config.yaml" \
  -e PROXMOX_BASE_ADDR="https://your-proxmox.example.com:8006/api2/json" \
  -e PROXMOX_TOKEN="user@pve!token=your-token-here" \
  -e AUTH_TOKEN="your-api-auth-token" \
  -e SENTRY_DSN="your-sentry-dsn" \
  -e TALOS_CONTROLPLANE_ENDPOINT="https://your-controlplane:6443" \
  -e TALOS_MACHINE_TEMPLATE="/app/talos-machine-config.yaml" \
  -e MIKROTIK_IP="10.100.0.1" \
  -e MIKROTIK_PORT="8080" \
  -e MIKROTIK_USERNAME="admin" \
  -e MIKROTIK_PASSWORD="your-password" \
  -e DEBUG="true" \
  -e LOG_LEVEL="0" \
  -e VERIFY_SSL="false" \
  -v $(pwd)/config.yaml:/app/config.yaml \
  -v $(pwd)/talos-machine-config.yaml:/app/talos-machine-config.yaml \
  proxmox-talos-vm-deployer
```

## Talos Setup

### Prerequisites

1. **Talos Template**: Create a Talos Linux template in Proxmox with:
   - Talos Linux ISO installed
   - QEMU Guest Agent enabled
   - Network interface configured for DHCP

2. **Talos Cluster**: Have an existing Talos cluster or control plane running

3. **Mikrotik Router**: Configure Mikrotik router with:
   - REST API enabled
   - DHCP server running
   - API user with appropriate permissions

## How It Works

1. **VM Creation**: Creates a new VM by cloning from a base template
2. **Configuration**: Configures CPU, memory, disk, and NUMA topology
3. **Network Setup**: Configures network interfaces and starts the VM
4. **IP Discovery**: Gets VM's MAC address and queries Mikrotik DHCP leases to find IP
5. **Talos Integration**: Generates and applies Talos machine configuration
6. **Cluster Registration**: Registers the new node with the Talos cluster

## Monitoring

The service exposes Prometheus metrics at `/metrics` endpoint:

- `vm_deployer_vms_created_total`: Total number of VMs created
- `vm_deployer_vms_deleted_total`: Total number of VMs deleted
- `vm_deployer_handler_errors_total`: Total number of handler errors

## Troubleshooting

### Common Issues

1. **SSL Certificate Verification**: If using self-signed certificates, set `VERIFY_SSL=false`
2. **Proxmox Token Permissions**: Ensure API token has sufficient permissions for VM operations
3. **QEMU Guest Agent**: Verify guest agent is installed and running in Talos template
4. **Network Configuration**: Ensure VMs can reach the Talos control plane endpoint
5. **Talos Configuration**: Verify machine config template has correct cluster credentials

### Logs

The service provides detailed logging. Set `LOG_LEVEL=0` for debug output.

### Debugging Talos Registration

- Check VM IP discovery: Ensure QEMU guest agent is working
- Verify Talos API connectivity: Test connection to VM's Talos API
- Validate machine config: Ensure template placeholders are correctly replaced
- Check cluster credentials: Verify tokens and certificates in machine config

## License

This project is licensed under the MIT License.

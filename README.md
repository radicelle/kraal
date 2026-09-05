# Kraal: Modular Connector Architecture (Go)

A high-performance, modular connector platform based on the **Inverted Out-of-Process (gRPC / Sidecar)** architecture. Kraal enables dozens of heterogeneous connectors to extract **lineage data and metadata** (database tables, views, foreign keys, CRM objects, custom schemas, and association graphs) while running identically in both **Desktop Mode** (local subprocess with zero container overhead) and **Cloud Mode** (independent micro-containers scaling freely), completely solving the shared library dependency hell.

---

## 🏛️ Architecture Highlights

### The "Smart Host, Thin Connector" Principle
* **Lineage & Metadata First:** Connectors are scoped to extracting structured metadata, schema attributes, and cross-object lineage graphs rather than heavy bulk row replication.
* **No Database/ORM Coupling:** Connectors do **not** import the Host's internal database drivers or internal data models.
* **Invariant SDK Contract:** The shared `pkg/sdk` and `pkg/protocol` packages define only the standard protocol (`Spec`, `Check`, `Discover`, `Read`, `Write`) and record envelopes. Updating host storage engines or adding new connector features never triggers breaking version bumps across 50+ connectors.
* **Dual Execution Mode:**
  * **Desktop Mode:** The Host spawns the connector as a local child process on an ephemeral loopback port (`127.0.0.1:0`), discovering the assigned port via a readiness handshake (`[KRAAL_READY] address=...`). Instant startup (< 10ms), tiny RAM (~15MB), zero Docker required.
  * **Cloud Mode:** The connector runs inside its own lightweight Docker container (`alpine` or `scratch`, < 15MB) exposing gRPC on port `50051`. Each connector can scale, sleep, or autoscale independently.

```
+-----------------------------------------------------------------------------------+
|                                  DEPLOYMENT MODES                                 |
+----------------------------------------+------------------------------------------+
|              DESKTOP MODE              |                CLOUD MODE                |
|  Single Desktop App / Host Process     |  Kubernetes / Container Orchestrator     |
|                                        |                                          |
|  +----------------------------------+  |  +----------------+  +----------------+  |
|  | Host Runtime Engine (Ingestion)  |  |  | Postgres       |  | HubSpot CRM    |  |
|  +-----------------+----------------+  |  | Connector (Go) |  | Connector (Go) |  |
|                    | Local Loopback    |  +-------+--------+  +-------+--------+  |
|  +-----------------+----------------+  |          \                  /            |
|  | Connector Local Processes        |  |           \   gRPC / TCP   /             |
|  | (Postgres, HubSpot, etc.)        |  |            v              v              |
|  +----------------------------------+  |  +------------------------------------+  |
|                                        |  | Host Lineage & Catalog Service     |  |
|                                        |  +-----------------+------------------+  |
|                                        |                    | Lineage Graph Sink  |
|                                        |                    v                     |
|                                        |            [( Graph / Store )]           |
+----------------------------------------+------------------------------------------+
```

---

## 📂 Repository Structure

```text
├── proto/
│   └── kraal/v1/
│       └── connector.proto      # Invariant gRPC protocol definition (Lineage, Relations, Schema)
├── pkg/
│   ├── protocol/v1/             # Generated Protobuf & gRPC Go stubs
│   └── sdk/                     # Shared connector toolkit: Server, Client, Subprocess Launcher
├── connectors/
│   ├── postgres/                # PostgreSQL Lineage Connector (tables, views, foreign key graph)
│   │   ├── config.go
│   │   ├── connector.go
│   │   ├── connector_test.go
│   │   └── Dockerfile
│   └── hubspot/                 # HubSpot CRM Lineage Connector (objects, properties, associations)
│       ├── config.go
│       ├── client.go
│       ├── connector.go
│       ├── connector_test.go
│       └── Dockerfile
├── cmd/
│   ├── connector-postgres/      # Main entrypoint for standalone Postgres connector binary
│   │   └── main.go
│   ├── connector-hubspot/       # Main entrypoint for standalone HubSpot connector binary
│   │   └── main.go
│   └── kraal-host/              # Host runtime engine (Desktop & Cloud runner)
│       └── main.go
├── architecture-research.md     # Architectural options research document
├── Dockerfile                   # Default container build
└── README.md
```

---

## 🚀 Quickstart & Usage

### 1. Build Binaries Locally

```powershell
go build -o bin/connector-postgres.exe ./cmd/connector-postgres
go build -o bin/connector-hubspot.exe ./cmd/connector-hubspot
go build -o bin/kraal-host.exe ./cmd/kraal-host
```

### 2. Desktop Mode (Subprocess Execution)

In Desktop mode, `kraal-host` spawns any connector executable locally on an ephemeral port without any manual network configuration:

#### PostgreSQL Connector
```powershell
# Query connector specification
.\bin\kraal-host.exe -mode=desktop -binary=.\bin\connector-postgres.exe -action=spec

# Run schema & foreign key lineage discovery
.\bin\kraal-host.exe -mode=desktop -binary=.\bin\connector-postgres.exe -action=discover -config='{\"host\":\"localhost\",\"port\":5432,\"database\":\"mydb\",\"user\":\"postgres\",\"password\":\"secret\"}'

# Sync lineage records into host catalog
.\bin\kraal-host.exe -mode=desktop -binary=.\bin\connector-postgres.exe -action=sync -stream="lineage" -config='{\"host\":\"localhost\",\"port\":5432,\"database\":\"mydb\",\"user\":\"postgres\",\"password\":\"secret\"}'
```

#### HubSpot CRM Connector
```powershell
# Query connector specification
.\bin\kraal-host.exe -mode=desktop -binary=.\bin\connector-hubspot.exe -action=spec

# Check connectivity with HubSpot Private App Token
.\bin\kraal-host.exe -mode=desktop -binary=.\bin\connector-hubspot.exe -action=check -config='{\"access_token\":\"pat-na1-xxxx\"}'

# Discover CRM objects, custom schemas, and association graph
.\bin\kraal-host.exe -mode=desktop -binary=.\bin\connector-hubspot.exe -action=discover -config='{\"access_token\":\"pat-na1-xxxx\"}'

# Stream CRM lineage records
.\bin\kraal-host.exe -mode=desktop -binary=.\bin\connector-hubspot.exe -action=sync -stream="lineage" -config='{\"access_token\":\"pat-na1-xxxx\"}'
```

---

### 3. Cloud Mode (Independent Docker Container)

#### Build the Connector Container

```bash
docker build -t kraal-connector-hubspot:latest -f connectors/hubspot/Dockerfile .
```

#### Run the Container

```bash
docker run -d --name hubspot-connector -p 50051:50051 kraal-connector-hubspot:latest
```

#### Connect via Host in Cloud Mode

```powershell
.\bin\kraal-host.exe -mode=cloud -remote=127.0.0.1:50051 -action=spec
```

---

## 🧪 Running Tests

```powershell
go test ./...
```

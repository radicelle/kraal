# Kraal: Modular Connector Architecture (Go)

A high-performance, modular connector platform based on the **Inverted Out-of-Process (gRPC / Sidecar)** architecture. Kraal enables dozens of heterogeneous data connectors (Postgres, APIs, queues) to run identically in both **Desktop Mode** (local subprocess with zero container overhead) and **Cloud Mode** (independent micro-containers scaling freely), while solving the shared library dependency hell.

---

## 🏛️ Architecture Highlights

### The "Smart Host, Thin Connector" Principle
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
|  | Host Runtime Engine (Ingestion)  |  |  | Postgres       |  | API            |  |
|  +-----------------+----------------+  |  | Connector (Go) |  | Connector (Go) |  |
|                    | Local Loopback    |  +-------+--------+  +-------+--------+  |
|  +-----------------+----------------+  |          \                  /            |
|  | Connector Local Processes        |  |           \   gRPC / TCP   /             |
|  | (Postgres, API, etc.)            |  |            v              v              |
|  +----------------------------------+  |  +------------------------------------+  |
|                                        |  | Host Ingestion / Storage Service   |  |
|                                        |  +-----------------+------------------+  |
|                                        |                    | Storage Sink        |
|                                        |                    v                     |
|                                        |            [( Target Store )]            |
+----------------------------------------+------------------------------------------+
```

---

## 📂 Repository Structure

```text
├── proto/
│   └── kraal/v1/
│       └── connector.proto      # Invariant gRPC protocol definition
├── pkg/
│   ├── protocol/v1/             # Generated Protobuf & gRPC Go stubs
│   └── sdk/                     # Shared connector toolkit: Server, Client, Subprocess Launcher
├── connectors/
│   └── postgres/                # PostgreSQL Source & Sink connector (pure Go pgx)
│       ├── config.go            # Connection config & validation
│       ├── connector.go         # Implementation of sdk.Connector
│       ├── connector_test.go    # Unit tests
│       └── Dockerfile           # Minimal container build (< 15MB)
├── cmd/
│   ├── connector-postgres/      # Main entrypoint for standalone Postgres connector binary
│   │   └── main.go
│   └── kraal-host/              # Host runtime engine (Desktop & Cloud runner)
│       └── main.go
├── architecture-research.md     # Architectural options research document
├── Dockerfile                   # Standalone connector container image
└── README.md
```

---

## 🚀 Quickstart & Usage

### 1. Build Binaries Locally

```powershell
go build -o bin/connector-postgres.exe ./cmd/connector-postgres
go build -o bin/kraal-host.exe ./cmd/kraal-host
```

### 2. Desktop Mode (Subprocess Execution)

In Desktop mode, `kraal-host` spawns the connector executable locally on an ephemeral port without any manual network configuration:

```powershell
# Query connector specification
.\bin\kraal-host.exe -mode=desktop -binary=.\bin\connector-postgres.exe -action=spec

# Run connection check against a database
.\bin\kraal-host.exe -mode=desktop -binary=.\bin\connector-postgres.exe -action=check -config='{\"host\":\"localhost\",\"port\":5432,\"database\":\"mydb\",\"user\":\"postgres\",\"password\":\"secret\"}'

# Run schema discovery
.\bin\kraal-host.exe -mode=desktop -binary=.\bin\connector-postgres.exe -action=discover -config='{\"host\":\"localhost\",\"port\":5432,\"database\":\"mydb\",\"user\":\"postgres\",\"password\":\"secret\"}'

# Stream records into host data layer
.\bin\kraal-host.exe -mode=desktop -binary=.\bin\connector-postgres.exe -action=sync -stream="public.users" -config='{\"host\":\"localhost\",\"port\":5432,\"database\":\"mydb\",\"user\":\"postgres\",\"password\":\"secret\"}'
```

---

### 3. Cloud Mode (Independent Docker Container)

#### Build the Connector Container

```bash
docker build -t kraal-connector-postgres:latest .
```

#### Run the Container

```bash
docker run -d --name pg-connector -p 50051:50051 kraal-connector-postgres:latest
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

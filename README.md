# Ktor Native Server

A minimalist, high-performance [Ktor](https://ktor.io/) HTTP server compiled directly to **native machine code via Kotlin/Native (LLVM)** without GraalVM or JVM overhead, packaged into a minimal **Docker** container.

---

## 🚀 Features

- **Kotlin/Native (LLVM Backend)**: Compiles directly to native binaries (`linuxX64` / `mingwX64`).
- **No GraalVM / Reflection Configs**: Pure Kotlin Multiplatform AOT compilation without needing `reflect-config.json` or reachability metadata.
- **Ktor 3.x with CIO**: High-throughput asynchronous Coroutine I/O.
- **Ultra-fast Startup**: Starts in **~3 ms**.
- **Multi-stage Dockerfile**: Builds the Linux binary with Kotlin/Native and packages it into `debian:bookworm-slim` (~35 MB compressed).

---

## 📂 Project Structure

```text
├── Dockerfile                  # Multi-stage Docker build for Kotlin/Native
├── build.gradle.kts            # Kotlin Multiplatform build configuration
├── settings.gradle.kts         # Gradle settings
├── gradle.properties           # Gradle JVM settings
├── gradlew / gradlew.bat       # Gradle wrapper scripts
├── src
│   ├── commonMain
│   │   └── kotlin
│   │       └── com/example/
│   │           └── Application.kt   # Ktor server entrypoint & routes
│   └── commonTest
│       └── kotlin
│           └── com/example/
│               └── ApplicationTest.kt # Integration tests
└── README.md
```

---

## 🛠️ Endpoints

| Method | Path      | Description          | Response                              |
| ------ | --------- | -------------------- | ------------------------------------- |
| `GET`  | `/`       | Main greeting route  | `Hello from Ktor Native!`             |
| `GET`  | `/health` | Healthcheck endpoint | `OK`                                  |

---

## 🐳 Building and Running with Docker (Recommended)

### 1. Build the Docker Image

```bash
docker build -t kraal-server:latest .
```

### 2. Run the Container

```bash
docker run --rm -p 8080:8080 kraal-server:latest
```

### 3. Test the Endpoint

```bash
curl http://localhost:8080/
# Output: Hello from Ktor Native!
```

---

## 💻 Local Development

### Run Tests

```bash
# Windows (runs mingwX64 test)
.\gradlew.bat mingwX64Test

# Linux (runs linuxX64 test)
./gradlew linuxX64Test
```

### Compile Native Executable Locally

```bash
# Windows: outputs build/bin/mingwX64/releaseExecutable/kraal-server.exe
.\gradlew.bat linkReleaseExecutableMingwX64

# Linux: outputs build/bin/linuxX64/releaseExecutable/kraal-server.kexe
./gradlew linkReleaseExecutableLinuxX64
```

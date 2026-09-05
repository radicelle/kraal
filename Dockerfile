# Stage 1: Build the Kotlin/Native binary using standard JDK
FROM eclipse-temurin:21-jdk AS builder

WORKDIR /workspace

# Copy Gradle wrapper & build files first for layer caching
COPY gradle gradle
COPY gradlew gradlew.bat build.gradle.kts settings.gradle.kts gradle.properties ./

# Grant execution permission for Gradle wrapper
RUN chmod +x gradlew

# Pre-fetch Gradle dependencies
RUN ./gradlew dependencies --no-daemon || true

# Copy source files
COPY src src

# Build the Linux x64 release executable with Kotlin/Native LLVM compiler
RUN ./gradlew linkReleaseExecutableLinuxX64 --no-daemon

# Stage 2: Minimal runtime image
FROM debian:bookworm-slim

WORKDIR /app

# Install runtime dependencies and CA certificates
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    curl \
    && rm -rf /var/lib/apt/lists/*

# Run as a dedicated non-root user
RUN useradd -u 10001 -m ktor && chown -R ktor:ktor /app
USER ktor

# Copy the native executable from the build stage
COPY --from=builder --chown=ktor:ktor /workspace/build/bin/linuxX64/releaseExecutable/kraal-server.kexe /app/kraal-server

EXPOSE 8080

ENTRYPOINT ["/app/kraal-server"]

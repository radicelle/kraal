# Stage 1: Build the statically-linked Go binary
FROM golang:1.24-alpine AS builder

WORKDIR /workspace

# Layer cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source tree
COPY . .

# Compile static binary for Linux
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bin/connector-hubspot ./cmd/connector-hubspot

# Stage 2: Minimal runtime image (< 15MB)
FROM alpine:3.20

RUN apk --no-cache add ca-certificates

WORKDIR /app
COPY --from=builder /bin/connector-hubspot /app/connector-hubspot

# Run as non-root user
USER 10001:10001

EXPOSE 50051

ENTRYPOINT ["/app/connector-hubspot"]
CMD ["--listen=0.0.0.0:50051"]

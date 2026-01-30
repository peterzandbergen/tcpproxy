# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Copy go mod files first for better layer caching
COPY go.mod go.sum* ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o tcpproxy ./cmd/tcpproxy

# Runtime stage
FROM scratch

# Copy CA certificates for TLS connections
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary
COPY --from=builder /build/tcpproxy /tcpproxy

# Run as non-root user
USER 65534:65534

ENTRYPOINT ["/tcpproxy"]

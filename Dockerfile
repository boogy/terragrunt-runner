# Multi-stage build for optimal size and speed
FROM golang:latest-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git make gcc musl-dev

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -extldflags '-static'" \
    -o terragrunt-runner \
    main.go

# Final stage - minimal runtime image
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    git \
    bash \
    curl \
    openssh-client \
    jq \
    unzip

# Copy binary from builder
COPY --from=builder /app/terragrunt-runner /usr/local/bin/

# Copy entrypoint script
COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod +x /usr/local/bin/docker-entrypoint.sh /usr/local/bin/terragrunt-runner

# Create non-root user
RUN addgroup -g 1000 -S runner && \
    adduser -u 1000 -S runner -G runner

# Create directory for tool installations
RUN mkdir -p /opt/tools && \
    chown -R runner:runner /opt/tools

# Switch to non-root user
USER runner

# Set working directory
WORKDIR /workspace

# Set entrypoint
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

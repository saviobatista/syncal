# Stage 1: Build the Go binary
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum files
COPY go.mod go.sum ./
# Download dependencies
RUN go mod download

# Copy the source code
COPY . .

# Build the application as a static binary
# CGO_ENABLED=0 is important for creating a static binary that works in a minimal image
# -ldflags="-w -s" strips debugging information, reducing binary size
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags="-w -s" -o syncal cmd/main.go

# Stage 2: Create the final, minimal image
FROM alpine:latest

WORKDIR /app

# The ca-certificates package is needed to access HTTPS endpoints.
RUN apk --no-cache add ca-certificates

# Copy the static binary from the builder stage
COPY --from=builder /app/syncal .

# Copy config files that might exist in the build context
# These can be overridden by mounting volumes.
COPY .env.example .
COPY sync-state.json* . 2>/dev/null || true
COPY token*.json . 2>/dev/null || true


# Set the binary as the entrypoint
ENTRYPOINT ["./syncal"]

# Default command can be overridden
CMD ["sync", "--watch"] 
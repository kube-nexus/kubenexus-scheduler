# Multi-stage build for smaller final image
FROM golang:1.25 AS builder

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY pkg/ pkg/

# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o kubenexus-scheduler cmd/scheduler/main.go

# Final stage
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/kubenexus-scheduler /opt/kubenexus-scheduler
COPY config/config.yaml /etc/config/config.yaml

USER 65532:65532

ENTRYPOINT ["/opt/kubenexus-scheduler"]

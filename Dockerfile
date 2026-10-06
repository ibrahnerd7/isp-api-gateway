# Stage 1: Building the Go binary
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o isp-api main.go

# Stage 2: Create the minimal production image
FROM alpine:latest
WORKDIR /root/

# Install radclient utility
RUN apk add --no-cache freeradius-utils

# Copy compiled binary from builder
COPY --from=builder /app/isp-api .

EXPOSE 8080
CMD ["./isp-api"]
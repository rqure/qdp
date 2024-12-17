# Build the application from source
FROM golang:1.22-alpine AS build-stage

WORKDIR /app

RUN apk add --no-cache libusb-dev build-base

COPY go.mod go.sum ./

COPY lib/go ./lib/go
COPY *.go ./

RUN go mod tidy

RUN CGO_ENABLED=1 GOOS=linux go build -o /qapp

# Deploy the application binary into a lean image
FROM alpine:latest AS build-release-stage

RUN apk add --no-cache libusb

WORKDIR /

COPY --from=build-stage /qapp /qapp

ENTRYPOINT ["/qapp"]

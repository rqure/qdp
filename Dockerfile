# Build the application from source
FROM golang:1.22 AS build-stage

WORKDIR /app

RUN apt-get update && apt-get install -y libusb-1.0-0-dev

COPY go.mod go.sum ./

COPY lib/go ./lib/go
COPY *.go ./

RUN go mod tidy

RUN CGO_ENABLED=1 GOOS=linux go build -o /qapp

# Deploy the application binary into a lean image
FROM gcr.io/distroless/base-debian11 AS build-release-stage

WORKDIR /

COPY --from=build-stage /qapp /qapp

RUN apt-get update && apt-get install -y libusb-1.0-0 libusb-1.0-0-dev

USER nonroot:nonroot

ENTRYPOINT ["/qapp"]

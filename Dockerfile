# Use the official Go image as the base image
FROM golang:1.22.1 AS builder

# Set the working directory inside the container
WORKDIR /src

ENV GOPRIVATE=github.com/n0needt0/goodies

ARG GH_PAT

RUN echo "machine github.com login ci password ${GH_PAT}" > ~/.netrc

# Copy the Go module files and download dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire project into the container
COPY . .

# Build the Go server
RUN CGO_ENABLED=0 go build -o /app main.go

# Use the ls command to list the contents of the /app directory
RUN ls -l /app

# Use a minimal image as the final image
FROM alpine:latest


# Copy the binary built in the previous stage
COPY --from=builder /app /app

# Create the directory in case it doesn't already exist
RUN mkdir -p /etc/app

# Create the directory for cache
RUN mkdir -p /var/kflow-cache


# Use the ls command to list the contents of the /app directory
RUN ls /app

# Expose the port your Go service listens on
EXPOSE 8080

# Copy config file
COPY ./config.yaml /etc/app/config.yaml

# Expose the config volume
VOLUME ["/etc/app", "/var/kflow-cache"]


# Command to run your Go server
CMD ["/app", "--config", "/etc/app/config.yaml"]

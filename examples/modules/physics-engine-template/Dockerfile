# Physics engine module template
#
# Builds a standalone binary that serves the EngineModule gRPC service.

FROM golang:1.22 AS build

WORKDIR /src

# Copy only the template module first to maximize layer caching.
COPY examples/modules/physics-engine-template/go.mod ./examples/modules/physics-engine-template/go.mod

# The template uses a local replace to import the shared engine proto stubs.
# Copy the root go.mod/go.sum so that the replace target can be resolved.
COPY go.mod go.sum ./

WORKDIR /src/examples/modules/physics-engine-template
RUN go mod download

# Copy sources.
WORKDIR /src
COPY . .

WORKDIR /src/examples/modules/physics-engine-template
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/physics-engine-template ./

FROM gcr.io/distroless/static:nonroot

WORKDIR /
COPY --from=build /out/physics-engine-template /physics-engine-template

USER nonroot:nonroot
EXPOSE 50051

ENV NATS_URL=""

ENTRYPOINT ["/physics-engine-template"]

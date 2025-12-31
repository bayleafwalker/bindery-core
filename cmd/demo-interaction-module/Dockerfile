# Demo image for an "interaction" module that can call into the physics module if injected.

FROM golang:1.22 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/demo-interaction-module ./cmd/demo-interaction-module

FROM gcr.io/distroless/static:nonroot

WORKDIR /
COPY --from=build /out/demo-interaction-module /demo-interaction-module

USER nonroot:nonroot
EXPOSE 50051

ENTRYPOINT ["/demo-interaction-module"]


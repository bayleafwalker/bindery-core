# Demo web client module image (HTTP UI for the sample world).

FROM golang:1.22 AS build

WORKDIR /src

# Cache deps for both the platform module and the sample-game module.
COPY go.mod go.sum ./
COPY examples/booklet-bindery-sample/go.mod examples/booklet-bindery-sample/go.sum ./examples/booklet-bindery-sample/
RUN cd examples/booklet-bindery-sample && go mod download

COPY . .

RUN cd examples/booklet-bindery-sample && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/demo-web ./cmd/demo-web-module

FROM gcr.io/distroless/static:nonroot

WORKDIR /
COPY --from=build /out/demo-web /demo-web

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/demo-web"]


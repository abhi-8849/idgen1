# Build from the repo root: docker build -f deploy/Dockerfile .
FROM golang:1.26 AS build
WORKDIR /src

# Copy module files first so the dependency layer is cached until they change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO off gives a static binary that runs on the minimal image below.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /idserver ./cmd/idserver

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /idserver /idserver
EXPOSE 8080
ENTRYPOINT ["/idserver"]

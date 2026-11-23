# Dockerfile = a recipe that builds a container image. Build it from the repo root:
#   docker build -f deploy/Dockerfile .
# It has two stages: stage 1 compiles the program, stage 2 is the small image we actually ship.

# ---- Stage 1: build ----
# Start from an image that has the Go toolchain. "AS build" names this stage so stage 2 can copy from it.
FROM golang:1.26 AS build

# Set the working folder inside the image; later commands run here.
WORKDIR /src

# Copy only the dependency files first. Docker caches each step, so the (slow) download
# below is reused on later builds unless go.mod or go.sum change.
COPY go.mod go.sum ./

# Download the Go dependencies (for example the etcd client).
RUN go mod download

# Now copy the rest of the source code. "." on the left is the build context (repo root), "." on the right is /src.
COPY . .

# Compile the server. CGO_ENABLED=0 produces a fully static binary with no C libraries, so it runs on the tiny image below.
# -trimpath removes local file paths from the binary; -ldflags="-s -w" strips debug info to make it smaller.
# -o /idserver is the output file; ./cmd/idserver is the package to build.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /idserver ./cmd/idserver

# ---- Stage 2: run ----
# Start again from a nearly empty "distroless" image: no shell, no package manager, so it is small and has little to attack.
# ":nonroot" makes the program run as a normal user instead of root.
FROM gcr.io/distroless/static-debian12:nonroot

# Copy just the compiled binary from stage 1. The Go toolchain and source code are left behind.
COPY --from=build /idserver /idserver

# Documentation only: the server listens on port 8080 inside the container. It does not publish the port by itself.
EXPOSE 8080

# The command that runs when the container starts. Extra arguments (flags) are appended after it.
ENTRYPOINT ["/idserver"]

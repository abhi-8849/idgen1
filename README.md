# idgen
Go proof of concept of a Unique ID Generator: 64-bit, unique, roughly time-sortable (Snowflake-style) IDs.
Layout: `snowflake/` library, `workerid/` lease allocators, `server/` HTTP, `client/` SDK, `cmd/idserver/` binary.
Build: `go build ./... && go vet ./...`
Status: scaffold only.
Docker and Kubernetes files will live in `deploy/`.

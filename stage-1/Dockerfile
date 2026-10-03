# Build stage: compiles a static binary. Network access here is fine (module download);
# the final image below needs none at runtime.
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

# Final stage: a minimal, static image with no shell, no package manager, no outbound
# network dependency at runtime. The IANA timezone database is embedded in the binary via
# the internal/timeutil package's "time/tzdata" import, so no system tzdata file is needed.
FROM scratch
COPY --from=build /out/server /server
COPY static /static
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/server"]

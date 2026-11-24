FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/noisefloor ./cmd/noisefloor

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/noisefloor /usr/local/bin/noisefloor
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/noisefloor"]

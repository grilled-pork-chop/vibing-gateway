# Multi-stage build → static, non-root, distroless image.
# Only the final ref (ghcr.io/<org>/llm-models-aggregator:<tag>) is pinned in images.txt;
# the build-stage bases are build-time only.
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/models-aggregator ./cmd/app

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/models-aggregator /models-aggregator
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/models-aggregator"]

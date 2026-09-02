FROM golang:1.24 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /gort ./cmd/gort

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /gort /gort

ENV GORT_PORT=8080 \
    GORT_DATA_DIR=/data
VOLUME /data
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s \
  CMD ["/gort", "-healthcheck"]

ENTRYPOINT ["/gort"]

FROM golang:1.27.1 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/veto ./cmd/veto

FROM gcr.io/distroless/static-debian13:nonroot AS runtime
LABEL io.modelcontextprotocol.server.name="io.github.aiveto/veto"
ENTRYPOINT ["/veto"]
CMD ["--help"]

FROM runtime AS goreleaser
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/veto /veto

FROM runtime
COPY --from=build /out/veto /veto

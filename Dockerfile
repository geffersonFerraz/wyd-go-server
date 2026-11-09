# Build de um dos programas de cmd/ (dbsrv, tmsrv, admin...).
#   docker build --build-arg CMD=tmsrv -t wyd-tmsrv .
FROM golang:1.26-alpine AS build
ARG CMD
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/app ./cmd/${CMD}

FROM alpine:3.22
RUN apk add --no-cache tzdata ca-certificates libcap
ENV TZ=America/Sao_Paulo
# Os programas usam caminhos relativos (config/..., tmsrv.yaml): a raiz do
# repositório é montada em /app pelo docker-compose.
WORKDIR /app
COPY --from=build /out/app /usr/local/bin/app
# Permite abrir portas < 1024 (admin -public-listen :80) sem rodar como root.
RUN setcap cap_net_bind_service=+ep /usr/local/bin/app
ENTRYPOINT ["/usr/local/bin/app"]

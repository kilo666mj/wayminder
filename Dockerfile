FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build

RUN apk add --no-cache --upgrade ca-certificates
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/wayminder ./cmd/wayminder
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/wayminder-healthcheck ./cmd/wayminder-healthcheck

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /out/wayminder /wayminder
COPY --from=build /out/wayminder-healthcheck /wayminder-healthcheck
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/wayminder"]

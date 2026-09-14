FROM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build

RUN apk add --no-cache ca-certificates
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

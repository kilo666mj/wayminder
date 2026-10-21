FROM golang:1.27.3-alpine@sha256:f2c872bcee949b7be9c490efd02b48a8a2fbf947cdf533f1c58525ec9d8170fd AS build

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

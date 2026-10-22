FROM golang:1.27.3-alpine@sha256:d530cf60723abf1ffae0cd16bb546b379ec0550d2f3815e5b16ed69dbc00fba4 AS build

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

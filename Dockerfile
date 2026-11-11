FROM golang:1.27.4-alpine@sha256:0a5f7fe796599ff487605987432e7619ae45beeb2fba4c2ce0b907879932aebf AS build

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

FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/wayminder ./cmd/wayminder

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/wayminder /usr/local/bin/wayminder
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/wayminder"]

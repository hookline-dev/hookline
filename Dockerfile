FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/hookline ./cmd/hookline && CGO_ENABLED=0 go build -trimpath -o /out/sink ./cmd/sink && CGO_ENABLED=0 go build -trimpath -o /out/telegram-sink ./cmd/telegram-sink
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D hookline
COPY --from=build /out/* /usr/local/bin/
USER hookline
ENTRYPOINT ["/usr/local/bin/hookline"]

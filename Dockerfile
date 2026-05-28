FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/4dl ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
	&& addgroup -S fourdl \
	&& adduser -S -G fourdl fourdl

USER fourdl
WORKDIR /app

COPY --from=build /out/4dl /usr/local/bin/4dl

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/4dl"]
CMD ["server", "--listen", ":8080"]

FROM golang:1.26-alpine AS build

WORKDIR /src

RUN apk add --no-cache build-base make npm

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN make build BUILD_DIR=/out BINARY=4dl CGO_ENABLED=1 GOOS=linux GOARCH=amd64

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
	&& addgroup -S fourdl \
	&& adduser -S -G fourdl fourdl \
	&& mkdir -p /app/downloads \
	&& chown -R fourdl:fourdl /app

USER fourdl
WORKDIR /app

COPY --from=build /out/4dl /usr/local/bin/4dl

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/4dl"]
CMD ["--listen", ":8080", "--save-to", "/app/downloads"]

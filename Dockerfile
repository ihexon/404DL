FROM golang:1.26-alpine AS build

WORKDIR /src

COPY go.mod ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mvdl ./cmd/server

FROM alpine:3.22

RUN apk add --no-cache ca-certificates \
	&& addgroup -S mvdl \
	&& adduser -S -G mvdl mvdl

USER mvdl
WORKDIR /app

COPY --from=build /out/mvdl /usr/local/bin/mvdl

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/mvdl"]
CMD ["server", "--listen", ":8080"]

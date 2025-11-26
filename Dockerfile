FROM golang:tip-alpine AS builder
WORKDIR /builder
COPY . .
RUN mkdir bin
RUN go mod download
RUN go build -o bin/deathstar

FROM golang:tip-alpine AS app
WORKDIR /app
COPY --from=builder /builder/bin/deathstar .
CMD ["./deathstar"]
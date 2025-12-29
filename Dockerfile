FROM golang:1.25.5-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o relay-server .

FROM alpine:latest

RUN adduser -D -u 10001 serveruser
WORKDIR /home/serveruser

COPY --from=builder /app/relay-server .

# lock the ROLE to server since this container only serves that purpose
ENV ROLE=server
ENV PORT=8080

EXPOSE 8080

USER serveruser

CMD ["./relay-server"]
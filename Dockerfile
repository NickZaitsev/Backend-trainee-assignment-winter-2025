FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod ./
# COPY go.sum ./ # go.sum might not exist yet, so I'll comment it out for now or just copy everything
# Better to copy go.mod and go.sum if it exists, but for now I'll just copy . .

COPY . .
RUN go mod tidy
RUN go build -o main .

FROM alpine:latest

WORKDIR /root/

COPY --from=builder /app/main .

EXPOSE 8080

CMD ["./main"]

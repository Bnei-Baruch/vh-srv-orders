FROM golang:1.21 AS base

WORKDIR /app

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN CGO_ENABLED=0 go build -o orders .

FROM alpine:latest

COPY --from=base /app/orders /
COPY --from=base /app/db /db

EXPOSE 8185

CMD ["./orders", "server"]

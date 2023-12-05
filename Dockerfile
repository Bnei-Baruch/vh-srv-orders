FROM golang:1.21.4-bookworm AS base

RUN apt-get update && apt-get upgrade -y

RUN mkdir /app

ADD . /app

WORKDIR /app

RUN CGO_ENABLED=0 go build -o orders .

FROM alpine:latest

COPY --from=base /app/orders /
COPY --from=base /app/db /db

EXPOSE 8185

CMD ["./orders", "server"]
